package shell

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"mvdan.cc/sh/v3/interp"
)

// Long option names that appear in more than one command, hoisted to constants
// so the linter does not see repeated string literals.
const (
	flagBytes      = "--bytes"
	flagLines      = "--lines"
	flagIgnoreCase = "--ignore-case"
	flagQuiet      = "--quiet"
)

// This file implements the "Layer 3" cross-platform parity utilities: plain
// stream filters plus `fd`/`rg`, which the model frequently uses but which the
// upstream moreinterp/coreutils middleware does not provide (notably on
// Windows, where no system `head`/`tail`/`wc`/`tee`/`sort`/`uniq`/`cut`/`tr`/`fd`/`rg`
// exist). `fd` and `rg` prefer a real binary when installed (see fd.go, rg.go).
//
// Deliberately NOT implemented here (routed to dedicated tools instead):
// grep/find/cat/ls (structured tools), sed/awk (edit / ts_run / py_run).

// pipeEnv carries the interpreter I/O for a stream-utility invocation. Using
// a plain struct (rather than interp.HandlerContext, whose fields are
// unexported) keeps every command directly unit-testable.
type pipeEnv struct {
	dir    string
	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer
}

// pipeCommands maps a command name to its implementation. Only names listed
// here are shadowed; everything else falls through to the next handler.
var pipeCommands = map[string]func(pipeEnv, []string) error{
	"head": runHead,
	"tail": runTail,
	"wc":   runWC,
	"tee":  runTee,
	"sort": runSort,
	"uniq": runUniq,
	"cut":  runCut,
	"tr":   runTr,
	"fd":   runFd,
	"rg":   runRg,
}

// pipeUtilsHandler returns an interpreter middleware that resolves the commands
// in pipeCommands. It is registered only when pipeUtilsEnabled() is true, and
// always after the security block handler so blocks still win.
func pipeUtilsHandler(next interp.ExecHandlerFunc) interp.ExecHandlerFunc {
	return func(ctx context.Context, args []string) error {
		if len(args) == 0 {
			return next(ctx, args)
		}
		run, ok := pipeCommands[args[0]]
		if !ok {
			return next(ctx, args)
		}
		// Prefer a real `fd`/`rg` binary when installed so behavior matches the host.
		if (args[0] == "fd" && hostFdPath() != "") || (args[0] == "rg" && hostRgPath() != "") {
			return next(ctx, args)
		}
		hc := interp.HandlerCtx(ctx)
		return run(pipeEnv{dir: hc.Dir, stdin: hc.Stdin, stdout: hc.Stdout, stderr: hc.Stderr}, args[1:])
	}
}

// pipeErr prints a GNU-style diagnostic and returns a non-fatal exit status so
// the surrounding script keeps running with $? set to 1.
func pipeErr(name, msg string, stderr io.Writer) error {
	fmt.Fprintf(stderr, "%s: %s\n", name, msg)
	return interp.ExitStatus(1)
}

// openInput resolves a file argument against the interpreter's working
// directory. An empty name or "-" means standard input.
func openInput(dir, name string, stdin io.Reader) (io.Reader, func(), error) {
	if name == "" || name == "-" {
		if stdin == nil {
			stdin = strings.NewReader("")
		}
		return stdin, func() {}, nil
	}
	path := name
	if !filepath.IsAbs(path) {
		path = filepath.Join(dir, name)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, fmt.Errorf("open %s: %w", path, err)
	}
	return f, func() { _ = f.Close() }, nil
}

// readAll reads every byte from r.
func readAll(r io.Reader) ([]byte, error) {
	if r == nil {
		return nil, nil
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("read input: %w", err)
	}
	return data, nil
}

// splitLinesKeep splits data on '\n' and keeps the terminator on each line. A
// trailing empty segment (input ended with '\n') is dropped.
func splitLinesKeep(data []byte) [][]byte {
	var out [][]byte
	start := 0
	for i := 0; i < len(data); i++ {
		if data[i] == '\n' {
			out = append(out, data[start:i+1])
			start = i + 1
		}
	}
	if start < len(data) {
		out = append(out, data[start:])
	}
	return out
}

// readLines reads r and returns lines without their trailing '\n'.
func readLines(r io.Reader) ([]string, error) {
	data, err := readAll(r)
	if err != nil {
		return nil, err
	}
	lines := splitLinesKeep(data)
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		out = append(out, strings.TrimSuffix(string(l), "\n"))
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// head
// ---------------------------------------------------------------------------

func runHead(env pipeEnv, args []string) error {
	const name = "head"
	lines := 10
	allButLast := false
	byteCount := -1
	quiet, verbose := false, false
	var files []string

	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			files = append(files, args[i+1:]...)
			i = len(args)
		case a == "-q" || a == flagQuiet || a == "--silent":
			quiet = true
		case a == "-v" || a == "--verbose":
			verbose = true
		case a == "-n" || a == flagLines:
			if i+1 >= len(args) {
				return pipeErr(name, "option requires an argument -- 'n'", env.stderr)
			}
			i++
			n, ab, err := parseHeadCount(args[i])
			if err != nil {
				return pipeErr(name, err.Error(), env.stderr)
			}
			lines, allButLast = n, ab
		case strings.HasPrefix(a, flagLines+"="):
			n, ab, err := parseHeadCount(strings.TrimPrefix(a, flagLines+"="))
			if err != nil {
				return pipeErr(name, err.Error(), env.stderr)
			}
			lines, allButLast = n, ab
		case len(a) > 2 && a[:2] == "-n":
			n, ab, err := parseHeadCount(a[2:])
			if err != nil {
				return pipeErr(name, err.Error(), env.stderr)
			}
			lines, allButLast = n, ab
		case a == "-c" || a == flagBytes:
			if i+1 >= len(args) {
				return pipeErr(name, "option requires an argument -- 'c'", env.stderr)
			}
			i++
			n, err := strconv.Atoi(args[i])
			if err != nil || n < 0 {
				return pipeErr(name, fmt.Sprintf("invalid number of bytes: %q", args[i]), env.stderr)
			}
			byteCount = n
		case strings.HasPrefix(a, flagBytes+"="):
			n, err := strconv.Atoi(strings.TrimPrefix(a, flagBytes+"="))
			if err != nil || n < 0 {
				return pipeErr(name, "invalid number of bytes", env.stderr)
			}
			byteCount = n
		case len(a) > 2 && a[:2] == "-c":
			n, err := strconv.Atoi(a[2:])
			if err != nil || n < 0 {
				return pipeErr(name, fmt.Sprintf("invalid number of bytes: %q", a[2:]), env.stderr)
			}
			byteCount = n
		case a == "-" || !strings.HasPrefix(a, "-"):
			files = append(files, a)
		default:
			if n, err := strconv.Atoi(a[1:]); err == nil {
				lines, allButLast = n, false
				continue
			}
			return pipeErr(name, fmt.Sprintf("invalid option -- %q", a), env.stderr)
		}
	}

	if len(files) == 0 {
		files = []string{""}
	}
	for idx, f := range files {
		if showFileHeader(len(files) > 1, quiet, verbose) {
			if idx > 0 {
				fmt.Fprintln(env.stdout)
			}
			fmt.Fprintln(env.stdout, fileHeader(f))
		}
		r, done, err := openInput(env.dir, f, env.stdin)
		if err != nil {
			return pipeErr(name, err.Error(), env.stderr)
		}
		err = headStream(r, env.stdout, lines, allButLast, byteCount)
		done()
		if err != nil {
			return pipeErr(name, err.Error(), env.stderr)
		}
	}
	return nil
}

func parseHeadCount(s string) (int, bool, error) {
	if s == "" {
		return 0, false, errors.New("invalid number of lines")
	}
	if s[0] == '-' {
		n, err := strconv.Atoi(s[1:])
		if err != nil || n < 0 {
			return 0, false, fmt.Errorf("invalid number of lines: %q", s)
		}
		return n, true, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return 0, false, fmt.Errorf("invalid number of lines: %q", s)
	}
	return n, false, nil
}

func headStream(r io.Reader, w io.Writer, lineCount int, allButLast bool, byteCount int) error {
	if r == nil {
		return nil
	}
	if byteCount >= 0 {
		if _, err := io.CopyN(w, r, int64(byteCount)); err != nil && !errors.Is(err, io.EOF) {
			return fmt.Errorf("read input: %w", err)
		}
		return nil
	}
	br := bufio.NewReaderSize(r, 64*1024)
	if allButLast {
		return headAllButLast(br, w, lineCount)
	}
	return headFirstN(br, w, lineCount)
}

// headAllButLast streams every line except the final n.
func headAllButLast(br *bufio.Reader, w io.Writer, n int) error {
	if n == 0 {
		return nil
	}
	ring := make([][]byte, 0, n)
	for {
		line, err := br.ReadBytes('\n')
		if len(line) > 0 {
			if len(ring) >= n {
				if _, werr := w.Write(ring[0]); werr != nil {
					return fmt.Errorf("write output: %w", werr)
				}
				copy(ring, ring[1:])
				ring = ring[:len(ring)-1]
			}
			ring = append(ring, line)
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return fmt.Errorf("read input: %w", err)
		}
	}
}

// headFirstN streams at most the first n lines and then stops reading.
func headFirstN(br *bufio.Reader, w io.Writer, n int) error {
	for i := 0; i < n; i++ {
		line, err := br.ReadBytes('\n')
		if len(line) > 0 {
			if _, werr := w.Write(line); werr != nil {
				return fmt.Errorf("write output: %w", werr)
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return fmt.Errorf("read input: %w", err)
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// tail
// ---------------------------------------------------------------------------

func runTail(env pipeEnv, args []string) error {
	const name = "tail"
	count := 10
	fromLine := false
	byteCount := -1
	byteFromStart := false
	quiet, verbose := false, false
	var files []string

	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			files = append(files, args[i+1:]...)
			i = len(args)
		case a == "-q" || a == flagQuiet || a == "--silent":
			quiet = true
		case a == "-v" || a == "--verbose":
			verbose = true
		case a == "-n" || a == flagLines:
			if i+1 >= len(args) {
				return pipeErr(name, "option requires an argument -- 'n'", env.stderr)
			}
			i++
			n, fs, err := parseTailCount(args[i])
			if err != nil {
				return pipeErr(name, err.Error(), env.stderr)
			}
			count, fromLine = n, fs
		case strings.HasPrefix(a, flagLines+"="):
			n, fs, err := parseTailCount(strings.TrimPrefix(a, flagLines+"="))
			if err != nil {
				return pipeErr(name, err.Error(), env.stderr)
			}
			count, fromLine = n, fs
		case len(a) > 2 && a[:2] == "-n":
			n, fs, err := parseTailCount(a[2:])
			if err != nil {
				return pipeErr(name, err.Error(), env.stderr)
			}
			count, fromLine = n, fs
		case a == "-c" || a == flagBytes:
			if i+1 >= len(args) {
				return pipeErr(name, "option requires an argument -- 'c'", env.stderr)
			}
			i++
			n, fs, err := parseTailCount(args[i])
			if err != nil {
				return pipeErr(name, err.Error(), env.stderr)
			}
			byteCount, byteFromStart = n, fs
		case strings.HasPrefix(a, flagBytes+"="):
			n, fs, err := parseTailCount(strings.TrimPrefix(a, flagBytes+"="))
			if err != nil {
				return pipeErr(name, err.Error(), env.stderr)
			}
			byteCount, byteFromStart = n, fs
		case len(a) > 2 && a[:2] == "-c":
			n, fs, err := parseTailCount(a[2:])
			if err != nil {
				return pipeErr(name, err.Error(), env.stderr)
			}
			byteCount, byteFromStart = n, fs
		case a == "-" || !strings.HasPrefix(a, "-"):
			files = append(files, a)
		default:
			if n, err := strconv.Atoi(a[1:]); err == nil {
				count, fromLine = n, false
				continue
			}
			return pipeErr(name, fmt.Sprintf("invalid option -- %q", a), env.stderr)
		}
	}

	if len(files) == 0 {
		files = []string{""}
	}
	for idx, f := range files {
		if showFileHeader(len(files) > 1, quiet, verbose) {
			if idx > 0 {
				fmt.Fprintln(env.stdout)
			}
			fmt.Fprintln(env.stdout, fileHeader(f))
		}
		r, done, err := openInput(env.dir, f, env.stdin)
		if err != nil {
			return pipeErr(name, err.Error(), env.stderr)
		}
		data, err := readAll(r)
		done()
		if err != nil {
			return pipeErr(name, err.Error(), env.stderr)
		}
		if err := tailWrite(data, env.stdout, count, fromLine, byteCount, byteFromStart); err != nil {
			return pipeErr(name, err.Error(), env.stderr)
		}
	}
	return nil
}

func parseTailCount(s string) (int, bool, error) {
	if s == "" {
		return 0, false, errors.New("invalid number of lines")
	}
	if s[0] == '+' {
		n, err := strconv.Atoi(s[1:])
		if err != nil || n < 1 {
			return 0, false, fmt.Errorf("invalid number of lines: %q", s)
		}
		return n, true, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return 0, false, fmt.Errorf("invalid number of lines: %q", s)
	}
	return n, false, nil
}

func tailWrite(data []byte, w io.Writer, count int, fromLine bool, byteCount int, byteFromStart bool) error {
	if byteCount >= 0 {
		start := len(data) - byteCount
		if byteFromStart {
			start = byteCount - 1
		}
		if start < 0 {
			start = 0
		}
		if start >= len(data) {
			return nil
		}
		if _, err := w.Write(data[start:]); err != nil {
			return fmt.Errorf("write output: %w", err)
		}
		return nil
	}
	lines := splitLinesKeep(data)
	start := 0
	if fromLine {
		start = count - 1
	} else {
		start = len(lines) - count
	}
	if start < 0 {
		start = 0
	}
	for _, l := range lines[start:] {
		if _, err := w.Write(l); err != nil {
			return fmt.Errorf("write output: %w", err)
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// wc
// ---------------------------------------------------------------------------

func runWC(env pipeEnv, args []string) error {
	const name = "wc"
	showLines, showWords, showBytes, showChars, showMax := false, false, false, false, false
	var files []string

	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			files = append(files, args[i+1:]...)
			i = len(args)
		case a == "-" || !strings.HasPrefix(a, "-"):
			files = append(files, a)
		default:
			for _, c := range a[1:] {
				switch c {
				case 'l':
					showLines = true
				case 'w':
					showWords = true
				case 'c':
					showBytes = true
				case 'm':
					showChars = true
				case 'L':
					showMax = true
				default:
					return pipeErr(name, fmt.Sprintf("invalid option -- %q", string(c)), env.stderr)
				}
			}
		}
	}
	if !showLines && !showWords && !showBytes && !showChars && !showMax {
		showLines, showWords, showBytes = true, true, true
	}

	if len(files) == 0 {
		files = []string{""}
	}
	var total wcCounts
	multi := len(files) > 1
	for _, f := range files {
		r, done, err := openInput(env.dir, f, env.stdin)
		if err != nil {
			return pipeErr(name, err.Error(), env.stderr)
		}
		data, err := readAll(r)
		done()
		if err != nil {
			return pipeErr(name, err.Error(), env.stderr)
		}
		c := countBytes(data)
		total.add(c)
		label := f
		if !multi {
			label = ""
		}
		wcPrint(env.stdout, c, label, showLines, showWords, showChars, showBytes, showMax)
	}
	if multi {
		wcPrint(env.stdout, total, "total", showLines, showWords, showChars, showBytes, showMax)
	}
	return nil
}

type wcCounts struct {
	lines, words, chars, bytes, max int
}

func (c *wcCounts) add(o wcCounts) {
	c.lines += o.lines
	c.words += o.words
	c.chars += o.chars
	c.bytes += o.bytes
	if o.max > c.max {
		c.max = o.max
	}
}

func countBytes(data []byte) wcCounts {
	c := wcCounts{bytes: len(data), chars: utf8.RuneCount(data)}
	inWord := false
	for _, r := range string(data) {
		if r == '\n' {
			c.lines++
		}
		if unicode.IsSpace(r) {
			inWord = false
		} else if !inWord {
			inWord = true
			c.words++
		}
	}
	for _, line := range strings.Split(string(data), "\n") {
		if w := utf8.RuneCountInString(line); w > c.max {
			c.max = w
		}
	}
	return c
}

func wcPrint(w io.Writer, c wcCounts, label string, lines, words, chars, bytes, showMax bool) {
	var parts []string
	if lines {
		parts = append(parts, strconv.Itoa(c.lines))
	}
	if words {
		parts = append(parts, strconv.Itoa(c.words))
	}
	if chars {
		parts = append(parts, strconv.Itoa(c.chars))
	}
	if bytes {
		parts = append(parts, strconv.Itoa(c.bytes))
	}
	if showMax {
		parts = append(parts, strconv.Itoa(c.max))
	}
	line := strings.Join(parts, " ")
	if label != "" {
		line += " " + label
	}
	fmt.Fprintln(w, line)
}

// ---------------------------------------------------------------------------
// tee
// ---------------------------------------------------------------------------

func runTee(env pipeEnv, args []string) error {
	const name = "tee"
	appendMode := false
	var files []string

	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			files = append(files, args[i+1:]...)
			i = len(args)
		case a == "-a" || a == "--append":
			appendMode = true
		case a == "-i" || a == "--ignore-interrupts":
			// accepted for compatibility; no signal handling needed here
		case a == "-" || !strings.HasPrefix(a, "-"):
			files = append(files, a)
		default:
			return pipeErr(name, fmt.Sprintf("invalid option -- %q", a), env.stderr)
		}
	}

	writers := []io.Writer{env.stdout}
	var closers []io.Closer
	closeAll := func() {
		for _, c := range closers {
			_ = c.Close()
		}
	}
	for _, f := range files {
		path := f
		if !filepath.IsAbs(path) {
			path = filepath.Join(env.dir, f)
		}
		flags := os.O_CREATE | os.O_WRONLY
		if appendMode {
			flags |= os.O_APPEND
		} else {
			flags |= os.O_TRUNC
		}
		file, err := os.OpenFile(path, flags, 0o600)
		if err != nil {
			closeAll()
			return pipeErr(name, err.Error(), env.stderr)
		}
		writers = append(writers, file)
		closers = append(closers, file)
	}

	stdin := env.stdin
	if stdin == nil {
		stdin = strings.NewReader("")
	}
	_, err := io.Copy(io.MultiWriter(writers...), stdin)
	closeAll()
	if err != nil {
		return pipeErr(name, err.Error(), env.stderr)
	}
	return nil
}

// ---------------------------------------------------------------------------
// sort
// ---------------------------------------------------------------------------

func runSort(env pipeEnv, args []string) error {
	const name = "sort"
	numeric, reverse, unique, fold := false, false, false, false
	keyField := 0
	delim := ""
	var files []string

	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			files = append(files, args[i+1:]...)
			i = len(args)
		case a == "-" || !strings.HasPrefix(a, "-"):
			files = append(files, a)
		case strings.HasPrefix(a, "--"):
			switch a {
			case "--numeric-sort":
				numeric = true
			case "--reverse":
				reverse = true
			case "--unique":
				unique = true
			case flagIgnoreCase:
				fold = true
			default:
				return pipeErr(name, fmt.Sprintf("unrecognized option %q", a), env.stderr)
			}
		default:
			for j := 1; j < len(a); j++ {
				switch a[j] {
				case 'n':
					numeric = true
				case 'r':
					reverse = true
				case 'u':
					unique = true
				case 'f':
					fold = true
				case 'k':
					rest := a[j+1:]
					if rest == "" {
						if i+1 >= len(args) {
							return pipeErr(name, "option requires an argument -- 'k'", env.stderr)
						}
						i++
						rest = args[i]
					}
					n, err := parseKeyField(rest)
					if err != nil {
						return pipeErr(name, err.Error(), env.stderr)
					}
					keyField = n
					j = len(a)
				case 't':
					rest := a[j+1:]
					if rest == "" {
						if i+1 >= len(args) {
							return pipeErr(name, "option requires an argument -- 't'", env.stderr)
						}
						i++
						rest = args[i]
					}
					delim = rest
					j = len(a)
				default:
					return pipeErr(name, fmt.Sprintf("invalid option -- %q", string(a[j])), env.stderr)
				}
			}
		}
	}

	var lines []string
	for _, f := range files {
		r, done, err := openInput(env.dir, f, env.stdin)
		if err != nil {
			return pipeErr(name, err.Error(), env.stderr)
		}
		ls, err := readLines(r)
		done()
		if err != nil {
			return pipeErr(name, err.Error(), env.stderr)
		}
		lines = append(lines, ls...)
	}
	if len(files) == 0 {
		ls, err := readLines(env.stdin)
		if err != nil {
			return pipeErr(name, err.Error(), env.stderr)
		}
		lines = ls
	}

	less := func(i, j int) bool {
		a := sortKey(lines[i], keyField, delim, fold)
		b := sortKey(lines[j], keyField, delim, fold)
		var lt bool
		if numeric {
			lt = numPrefix(a) < numPrefix(b)
		} else {
			lt = a < b
		}
		if reverse {
			return !lt && a != b
		}
		return lt
	}
	sort.SliceStable(lines, less)

	prev := ""
	for i, l := range lines {
		if unique && i > 0 && l == prev {
			continue
		}
		prev = l
		fmt.Fprintln(env.stdout, l)
	}
	return nil
}

func parseKeyField(s string) (int, error) {
	// Accept "N" or "N,M"; only the starting field is used.
	if idx := strings.IndexAny(s, ".,"); idx >= 0 {
		s = s[:idx]
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 {
		return 0, fmt.Errorf("invalid field specification: %q", s)
	}
	return n, nil
}

func sortKey(line string, field int, delim string, fold bool) string {
	key := line
	if field > 1 {
		key = extractField(line, field, delim)
	}
	if fold {
		key = strings.ToLower(key)
	}
	return key
}

func extractField(line string, field int, delim string) string {
	var fields []string
	if delim != "" {
		fields = strings.Split(line, delim)
	} else {
		fields = strings.Fields(line)
	}
	if field-1 < len(fields) {
		return fields[field-1]
	}
	return ""
}

func numPrefix(s string) float64 {
	s = strings.TrimSpace(s)
	end := 0
	for end < len(s) && (s[end] == '-' || s[end] == '+' || s[end] == '.' || (s[end] >= '0' && s[end] <= '9')) {
		end++
	}
	if end == 0 {
		return 0
	}
	v, err := strconv.ParseFloat(s[:end], 64)
	if err != nil {
		return 0
	}
	return v
}

// ---------------------------------------------------------------------------
// uniq
// ---------------------------------------------------------------------------

func runUniq(env pipeEnv, args []string) error {
	const name = "uniq"
	count, onlyDup, onlyUniq := false, false, false
	var files []string

	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			files = append(files, args[i+1:]...)
			i = len(args)
		case a == "-" || !strings.HasPrefix(a, "-"):
			files = append(files, a)
		default:
			for _, c := range a[1:] {
				switch c {
				case 'c':
					count = true
				case 'd':
					onlyDup = true
				case 'u':
					onlyUniq = true
				default:
					return pipeErr(name, fmt.Sprintf("invalid option -- %q", string(c)), env.stderr)
				}
			}
		}
	}

	if len(files) > 1 {
		return pipeErr(name, "extra operand", env.stderr)
	}
	input := ""
	if len(files) == 1 {
		input = files[0]
	}
	r, done, err := openInput(env.dir, input, env.stdin)
	if err != nil {
		return pipeErr(name, err.Error(), env.stderr)
	}
	lines, err := readLines(r)
	done()
	if err != nil {
		return pipeErr(name, err.Error(), env.stderr)
	}

	i := 0
	for i < len(lines) {
		j := i + 1
		for j < len(lines) && lines[j] == lines[i] {
			j++
		}
		n := j - i
		emit := true
		if onlyDup && n < 2 {
			emit = false
		}
		if onlyUniq && n != 1 {
			emit = false
		}
		if onlyDup && onlyUniq {
			emit = false
		}
		if emit {
			if count {
				fmt.Fprintf(env.stdout, "%7d %s\n", n, lines[i])
			} else {
				fmt.Fprintln(env.stdout, lines[i])
			}
		}
		i = j
	}
	return nil
}

// ---------------------------------------------------------------------------
// cut
// ---------------------------------------------------------------------------

func runCut(env pipeEnv, args []string) error {
	const name = "cut"
	delim := "\t"
	suppress := false
	mode := ""
	selector := ""
	var files []string

	setMode := func(m string) error {
		if mode != "" && mode != m {
			return pipeErr(name, "only one type of list may be specified", env.stderr)
		}
		mode = m
		return nil
	}

	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			files = append(files, args[i+1:]...)
			i = len(args)
		case a == "-s" || a == "--only-delimited":
			suppress = true
		case a == "-d" || a == "--delimiter":
			if i+1 >= len(args) {
				return pipeErr(name, "option requires an argument -- 'd'", env.stderr)
			}
			i++
			delim = args[i]
		case strings.HasPrefix(a, "--delimiter="):
			delim = strings.TrimPrefix(a, "--delimiter=")
		case len(a) > 2 && a[:2] == "-d":
			delim = a[2:]
		case a == "-f" || a == "--fields":
			if i+1 >= len(args) {
				return pipeErr(name, "option requires an argument -- 'f'", env.stderr)
			}
			i++
			if err := setMode("f"); err != nil {
				return err
			}
			selector = args[i]
		case strings.HasPrefix(a, "--fields="):
			if err := setMode("f"); err != nil {
				return err
			}
			selector = strings.TrimPrefix(a, "--fields=")
		case len(a) > 2 && a[:2] == "-f":
			if err := setMode("f"); err != nil {
				return err
			}
			selector = a[2:]
		case a == "-c" || a == "--characters":
			if i+1 >= len(args) {
				return pipeErr(name, "option requires an argument -- 'c'", env.stderr)
			}
			i++
			if err := setMode("c"); err != nil {
				return err
			}
			selector = args[i]
		case strings.HasPrefix(a, "--characters="):
			if err := setMode("c"); err != nil {
				return err
			}
			selector = strings.TrimPrefix(a, "--characters=")
		case len(a) > 2 && a[:2] == "-c":
			if err := setMode("c"); err != nil {
				return err
			}
			selector = a[2:]
		case a == "-b" || a == flagBytes:
			if i+1 >= len(args) {
				return pipeErr(name, "option requires an argument -- 'b'", env.stderr)
			}
			i++
			if err := setMode("b"); err != nil {
				return err
			}
			selector = args[i]
		case strings.HasPrefix(a, flagBytes+"="):
			if err := setMode("b"); err != nil {
				return err
			}
			selector = strings.TrimPrefix(a, flagBytes+"=")
		case len(a) > 2 && a[:2] == "-b":
			if err := setMode("b"); err != nil {
				return err
			}
			selector = a[2:]
		case a == "-" || !strings.HasPrefix(a, "-"):
			files = append(files, a)
		default:
			return pipeErr(name, fmt.Sprintf("invalid option -- %q", a), env.stderr)
		}
	}

	if mode == "" {
		return pipeErr(name, "you must specify a list of bytes, characters, or fields", env.stderr)
	}
	if delim == "" {
		return pipeErr(name, "delimiter must be a single character", env.stderr)
	}
	ranges, err := parseSelectors(selector)
	if err != nil {
		return pipeErr(name, err.Error(), env.stderr)
	}

	if len(files) == 0 {
		files = []string{""}
	}
	for _, f := range files {
		r, done, err := openInput(env.dir, f, env.stdin)
		if err != nil {
			return pipeErr(name, err.Error(), env.stderr)
		}
		lines, err := readLines(r)
		done()
		if err != nil {
			return pipeErr(name, err.Error(), env.stderr)
		}
		for _, line := range lines {
			out, ok := cutLine(line, mode, delim, ranges, suppress)
			if ok {
				fmt.Fprintln(env.stdout, out)
			}
		}
	}
	return nil
}

type selRange struct{ start, end int } // end<=0 means open-ended

func parseSelectors(s string) ([]selRange, error) {
	var out []selRange
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		r, err := parseSelector(part)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	if len(out) == 0 {
		return nil, errors.New("invalid empty list")
	}
	return out, nil
}

// parseSelector parses a single "N", "N-M", "N-" or "-M" selector.
func parseSelector(part string) (selRange, error) {
	i := strings.IndexByte(part, '-')
	if i < 0 {
		v, err := strconv.Atoi(part)
		if err != nil || v < 1 {
			return selRange{}, fmt.Errorf("invalid list value: %q", part)
		}
		return selRange{v, v}, nil
	}

	lo, hi := part[:i], part[i+1:]
	start := 1
	if lo != "" {
		v, err := strconv.Atoi(lo)
		if err != nil || v < 1 {
			return selRange{}, fmt.Errorf("invalid list value: %q", part)
		}
		start = v
	}
	end := 0
	if hi != "" {
		v, err := strconv.Atoi(hi)
		if err != nil || v < 1 {
			return selRange{}, fmt.Errorf("invalid list value: %q", part)
		}
		end = v
	}
	if end != 0 && end < start {
		return selRange{}, fmt.Errorf("invalid decreasing range: %q", part)
	}
	return selRange{start, end}, nil
}

func inSelectors(ranges []selRange, i int) bool {
	for _, r := range ranges {
		if i >= r.start && (r.end == 0 || i <= r.end) {
			return true
		}
	}
	return false
}

func cutLine(line, mode, delim string, ranges []selRange, suppress bool) (string, bool) {
	switch mode {
	case "f":
		if !strings.Contains(line, delim) {
			if suppress {
				return "", false
			}
			// GNU prints the whole line unchanged when it has no delimiter.
			return line, true
		}
		fields := strings.Split(line, delim)
		var picked []string
		for i, f := range fields {
			if inSelectors(ranges, i+1) {
				picked = append(picked, f)
			}
		}
		return strings.Join(picked, delim), true
	case "c":
		var b strings.Builder
		i := 1
		for _, r := range line {
			if inSelectors(ranges, i) {
				b.WriteRune(r)
			}
			i++
		}
		return b.String(), true
	default: // bytes
		var b strings.Builder
		for i := 0; i < len(line); i++ {
			if inSelectors(ranges, i+1) {
				b.WriteByte(line[i])
			}
		}
		return b.String(), true
	}
}

// ---------------------------------------------------------------------------
// tr
// ---------------------------------------------------------------------------

func runTr(env pipeEnv, args []string) error {
	const name = "tr"
	del, squeeze, complement := false, false, false
	var operands []string

	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			operands = append(operands, args[i+1:]...)
			i = len(args)
		case a == "-" || !strings.HasPrefix(a, "-"):
			operands = append(operands, a)
		default:
			for _, c := range a[1:] {
				switch c {
				case 'd':
					del = true
				case 's':
					squeeze = true
				case 'c', 'C':
					complement = true
				default:
					return pipeErr(name, fmt.Sprintf("invalid option -- %q", string(c)), env.stderr)
				}
			}
		}
	}

	if len(operands) == 0 {
		return pipeErr(name, "missing operand", env.stderr)
	}
	if len(operands) < 2 && !del && !squeeze {
		return pipeErr(name, "missing operand after "+strconv.Quote(operands[0]), env.stderr)
	}
	if len(operands) > 2 {
		return pipeErr(name, "extra operand "+strconv.Quote(operands[2]), env.stderr)
	}

	set1 := expandSet(operands[0])
	var set2 []rune
	if len(operands) >= 2 {
		set2 = expandSet(operands[1])
	}

	member := func(set []rune, r rune) bool {
		for _, c := range set {
			if c == r {
				return true
			}
		}
		return false
	}
	inSet1 := func(r rune) bool { return member(set1, r) }
	if complement {
		inSet1 = func(r rune) bool { return !member(set1, r) }
	}

	translate := func(r rune) (rune, bool) {
		if len(set2) == 0 {
			return r, false
		}
		for i, c := range set1 {
			if c == r {
				if i < len(set2) {
					return set2[i], true
				}
				return set2[len(set2)-1], true
			}
		}
		return r, false
	}

	data, err := readAll(env.stdin)
	if err != nil {
		return pipeErr(name, err.Error(), env.stderr)
	}

	var out []rune
	var lastSqueeze rune
	haveLast := false
	for _, r := range string(data) {
		if del {
			if inSet1(r) {
				continue
			}
			if squeeze && r == lastSqueeze && haveLast {
				continue
			}
			lastSqueeze, haveLast = r, true
			out = append(out, r)
			continue
		}
		res, _ := translate(r)
		squeezeSet := set2
		if len(squeezeSet) == 0 {
			squeezeSet = set1
		}
		if squeeze && haveLast && res == lastSqueeze && member(squeezeSet, res) {
			continue
		}
		lastSqueeze, haveLast = res, true
		out = append(out, res)
	}
	_, err = io.WriteString(env.stdout, string(out))
	if err != nil {
		return pipeErr(name, err.Error(), env.stderr)
	}
	return nil
}

// expandSet interprets a tr set string, expanding "a-z" ranges. Escapes such
// as \n, \t and \\ are honored.
func expandSet(s string) []rune {
	runes := unescape(s)
	var out []rune
	for i := 0; i < len(runes); i++ {
		if i+2 < len(runes) && runes[i+1] == '-' {
			lo, hi := runes[i], runes[i+2]
			if lo <= hi {
				for r := lo; r <= hi; r++ {
					out = append(out, r)
				}
				i += 2
				continue
			}
		}
		out = append(out, runes[i])
	}
	return out
}

func unescape(s string) []rune {
	var out []rune
	rs := []rune(s)
	for i := 0; i < len(rs); i++ {
		if rs[i] == '\\' && i+1 < len(rs) {
			i++
			switch rs[i] {
			case 'n':
				out = append(out, '\n')
			case 't':
				out = append(out, '\t')
			case 'r':
				out = append(out, '\r')
			case '\\':
				out = append(out, '\\')
			default:
				out = append(out, rs[i])
			}
			continue
		}
		out = append(out, rs[i])
	}
	return out
}

// ---------------------------------------------------------------------------
// shared helpers
// ---------------------------------------------------------------------------

func showFileHeader(multiple, quiet, verbose bool) bool {
	if quiet {
		return false
	}
	return multiple || verbose
}

func fileHeader(name string) string {
	if name == "" || name == "-" {
		return "==> standard input <=="
	}
	return fmt.Sprintf("==> %s <==", name)
}
