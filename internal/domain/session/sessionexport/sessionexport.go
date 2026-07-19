package sessionexport

import (
	"fmt"
	"html"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/package-register/mocode/internal/domain/session/message"
	"github.com/package-register/mocode/internal/util/infra"
)

var (
	DefaultDir = filepath.Join(infra.DataDir(), "export")
	SummaryDir = filepath.Join(infra.DataDir(), "export", "summary")
)

type Options struct {
	SessionID  string
	Format     string
	Scope      string
	WorkingDir string
	Now        time.Time
}

type Result struct {
	Path         string `json:"path"`
	Format       string `json:"format"`
	MessageCount int    `json:"message_count"`
}

type SummaryOptions struct {
	SessionID  string
	Title      string
	Content    string
	WorkingDir string
	Now        time.Time
}

func Export(messages []message.Message, options Options) (Result, error) {
	if options.SessionID == "" {
		return Result{}, fmt.Errorf("session id is required")
	}
	if options.WorkingDir == "" {
		return Result{}, fmt.Errorf("working dir is required")
	}
	if options.Now.IsZero() {
		options.Now = time.Now()
	}
	if options.Scope == "recent10" && len(messages) > 10 {
		messages = messages[len(messages)-10:]
	}

	ext := Extension(options.Format)
	if ext == "" {
		return Result{}, fmt.Errorf("unsupported export format: %s", options.Format)
	}

	dir := DefaultDir
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Result{}, err
	}

	path := filepath.Join(dir, fmt.Sprintf("session-%s-%s.%s", SanitizeName(options.SessionID), options.Now.Format("20060102-150405"), ext))
	content := Render(options.SessionID, options.Format, messages, options.Now)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return Result{}, err
	}

	return Result{Path: path, Format: ext, MessageCount: len(messages)}, nil
}

func ExportSummary(options SummaryOptions) (Result, error) {
	if options.SessionID == "" {
		return Result{}, fmt.Errorf("session id is required")
	}
	if options.WorkingDir == "" {
		return Result{}, fmt.Errorf("working dir is required")
	}
	if options.Now.IsZero() {
		options.Now = time.Now()
	}
	dir := SummaryDir
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Result{}, err
	}
	path := filepath.Join(dir, fmt.Sprintf("summary-%s-%s.md", SanitizeName(options.SessionID), options.Now.Format("20060102-150405")))
	content := RenderSummaryMarkdown(options)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return Result{}, err
	}
	return Result{Path: path, Format: "md", MessageCount: 1}, nil
}

func RenderSummaryMarkdown(options SummaryOptions) string {
	if options.Now.IsZero() {
		options.Now = time.Now()
	}
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("type: skcode-session-summary\n")
	b.WriteString("session_id: \"")
	b.WriteString(strings.ReplaceAll(options.SessionID, "\"", "\\\""))
	b.WriteString("\"\n")
	b.WriteString("title: \"")
	b.WriteString(strings.ReplaceAll(options.Title, "\"", "\\\""))
	b.WriteString("\"\n")
	b.WriteString("exported_at: \"" + options.Now.Format(time.RFC3339) + "\"\n")
	b.WriteString("---\n\n")
	b.WriteString("# Session Summary\n\n")
	b.WriteString(strings.TrimSpace(options.Content))
	b.WriteString("\n")
	return b.String()
}

func Extension(format string) string {
	switch format {
	case "markdown", "obsidian-md", "md":
		return "md"
	case "html":
		return "html"
	default:
		return ""
	}
}

func SanitizeName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "unknown"
	}
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "unknown"
	}
	return out
}

func Render(sessionID, format string, messages []message.Message, now time.Time) string {
	if now.IsZero() {
		now = time.Now()
	}
	if format == "html" {
		return RenderHTML(sessionID, messages, now)
	}
	return RenderMarkdown(sessionID, messages, now)
}

func RenderMarkdown(sessionID string, messages []message.Message, now time.Time) string {
	if now.IsZero() {
		now = time.Now()
	}
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("type: skcode-session-export\n")
	b.WriteString("session_id: \"")
	b.WriteString(strings.ReplaceAll(sessionID, "\"", "\\\""))
	b.WriteString("\"\n")
	b.WriteString("exported_at: \"" + now.Format(time.RFC3339) + "\"\n")
	b.WriteString("message_count: ")
	b.WriteString(fmt.Sprintf("%d\n", len(messages)))
	b.WriteString("---\n\n")
	b.WriteString("# Session Export\n\n")
	for _, msg := range messages {
		WriteMarkdownMessage(&b, msg)
	}
	return b.String()
}

func WriteMarkdownMessage(b *strings.Builder, msg message.Message) {
	b.WriteString("## ")
	b.WriteString(titleRole(string(msg.Role)))
	if msg.IsSummaryMessage {
		b.WriteString(" Summary")
	}
	b.WriteString("\n\n")
	if text := strings.TrimSpace(msg.Content().Text); text != "" {
		b.WriteString(text)
		b.WriteString("\n\n")
	}
	if reasoning := strings.TrimSpace(msg.ReasoningContent().Thinking); reasoning != "" {
		b.WriteString("<details><summary>Reasoning</summary>\n\n")
		b.WriteString(reasoning)
		b.WriteString("\n\n</details>\n\n")
	}
	for _, call := range msg.ToolCalls() {
		b.WriteString("### Tool Call: ")
		b.WriteString(call.Name)
		b.WriteString("\n\n```json\n")
		b.WriteString(call.Input)
		b.WriteString("\n```\n\n")
	}
	for _, result := range msg.ToolResults() {
		b.WriteString("### Tool Result: ")
		b.WriteString(result.Name)
		b.WriteString("\n\n```\n")
		b.WriteString(result.Content)
		b.WriteString("\n```\n\n")
	}
}

func RenderHTML(sessionID string, messages []message.Message, now time.Time) string {
	markdown := RenderMarkdown(sessionID, messages, now)
	return "<!doctype html>\n<html><head><meta charset=\"utf-8\"><title>Session Export</title>" +
		"<style>body{font-family:ui-sans-serif,system-ui;margin:2rem;line-height:1.6;max-width:960px}pre{background:#111;color:#eee;padding:1rem;overflow:auto;border-radius:8px}code{font-family:ui-monospace,monospace}</style>" +
		"</head><body><pre>" + html.EscapeString(markdown) + "</pre></body></html>\n"
}

func titleRole(role string) string {
	if role == "" {
		return "Unknown"
	}
	return strings.ToUpper(role[:1]) + strings.ToLower(role[1:])
}

var RecentsDir = filepath.Join(infra.DataDir(), "export", "recents")

type RecentExportOptions struct {
	Messages   []message.Message
	WorkingDir string
	DirName    string // override directory name, defaults to working dir basename
	Format     string
	Now        time.Time
}

func ExportRecents(opts RecentExportOptions) (Result, error) {
	if opts.WorkingDir == "" {
		return Result{}, fmt.Errorf("working dir is required")
	}
	if len(opts.Messages) == 0 {
		return Result{}, fmt.Errorf("no messages to export")
	}
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}
	if opts.Format == "" {
		opts.Format = "md"
	}

	ext := Extension(opts.Format)
	if ext == "" {
		return Result{}, fmt.Errorf("unsupported export format: %s", opts.Format)
	}

	dir := RecentsDir
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Result{}, err
	}

	dirName := opts.DirName
	if dirName == "" {
		dirName = filepath.Base(opts.WorkingDir)
	}
	dirName = SanitizeName(dirName)

	path := filepath.Join(dir, fmt.Sprintf("%s_%s.%s", dirName, opts.Now.Format("20060102-150405"), ext))

	var b strings.Builder
	for i, msg := range opts.Messages {
		if i > 0 {
			b.WriteString("\n---\n\n")
		}
		WriteMarkdownMessage(&b, msg)
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		return Result{}, err
	}

	return Result{Path: path, Format: ext, MessageCount: len(opts.Messages)}, nil
}
