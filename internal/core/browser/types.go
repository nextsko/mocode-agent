package browser

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

// ConsoleEntry is one console message or uncaught page error.
type ConsoleEntry struct {
	// Level is CDP's console type (log/info/warning/error/debug) or "exception"
	// for an uncaught error, which has no console.* counterpart.
	Level string
	Text  string
	URL   string
	Line  int
	Time  time.Time
}

// String renders an entry the way the console action prints it.
func (e ConsoleEntry) String() string {
	loc := ""
	if e.URL != "" {
		loc = fmt.Sprintf(" (%s:%d)", e.URL, e.Line)
	}
	return fmt.Sprintf("[%s] %s%s", e.Level, e.Text, loc)
}

// defaultConsoleBuffer bounds memory for pages that log in a loop.
const defaultConsoleBuffer = 500

// consoleBuffer keeps the most recent console messages.
//
// A bounded buffer is the point: a page with a runaway logging loop would
// otherwise grow without limit and take the agent's context window with it.
type consoleBuffer struct {
	mu      sync.Mutex
	entries []ConsoleEntry
	limit   int
	dropped int
}

func newConsoleBuffer(limit int) *consoleBuffer {
	if limit <= 0 {
		limit = defaultConsoleBuffer
	}
	return &consoleBuffer{limit: limit}
}

func (b *consoleBuffer) add(e ConsoleEntry) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.entries) >= b.limit {
		// Drop the oldest, and say so, so a gap is never silent.
		b.entries = b.entries[1:]
		b.dropped++
	}
	b.entries = append(b.entries, e)
}

func (b *consoleBuffer) snapshot() ([]ConsoleEntry, int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]ConsoleEntry{}, b.entries...), b.dropped
}

func (b *consoleBuffer) drain() []ConsoleEntry {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := b.entries
	b.entries = nil
	b.dropped = 0
	return out
}

// Console returns buffered console messages without clearing them.
func (s *Session) Console() ([]ConsoleEntry, int) { return s.console.snapshot() }

// DrainConsole returns buffered console messages and clears the buffer, so
// repeated reads only show what is new.
func (s *Session) DrainConsole() []ConsoleEntry { return s.console.drain() }

// onConsole handles Runtime.consoleAPICalled.
func (s *Session) onConsole(_ string, params json.RawMessage) {
	var p struct {
		Type string `json:"type"`
		Args []struct {
			Type        string          `json:"type"`
			Subtype     string          `json:"subtype"`
			Value       json.RawMessage `json:"value"`
			Description string          `json:"description"`
		} `json:"args"`
		StackTrace *struct {
			CallFrames []struct {
				URL        string `json:"url"`
				LineNumber int    `json:"lineNumber"`
			} `json:"callFrames"`
		} `json:"stackTrace"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return
	}

	parts := make([]string, 0, len(p.Args))
	for _, a := range p.Args {
		parts = append(parts, renderRemoteObject(a.Type, a.Subtype, a.Value, a.Description))
	}

	e := ConsoleEntry{Level: p.Type, Text: strings.Join(parts, " "), Time: time.Now()}
	if p.StackTrace != nil && len(p.StackTrace.CallFrames) > 0 {
		e.URL = p.StackTrace.CallFrames[0].URL
		e.Line = p.StackTrace.CallFrames[0].LineNumber + 1
	}
	s.console.add(e)
}

// onException handles Runtime.exceptionThrown: an uncaught error is exactly what
// the user is usually debugging, so it must not be missed just because it never
// reached console.error.
func (s *Session) onException(_ string, params json.RawMessage) {
	var p struct {
		ExceptionDetails *struct {
			Text      string `json:"text"`
			URL       string `json:"url"`
			Line      int    `json:"lineNumber"`
			Exception *struct {
				Description string `json:"description"`
			} `json:"exception"`
		} `json:"exceptionDetails"`
	}
	if err := json.Unmarshal(params, &p); err != nil || p.ExceptionDetails == nil {
		return
	}

	text := p.ExceptionDetails.Text
	if p.ExceptionDetails.Exception != nil && p.ExceptionDetails.Exception.Description != "" {
		text = p.ExceptionDetails.Exception.Description
	}
	s.console.add(ConsoleEntry{
		Level: "exception",
		Text:  firstLine(text),
		URL:   p.ExceptionDetails.URL,
		Line:  p.ExceptionDetails.Line + 1,
		Time:  time.Now(),
	})
}

// renderRemoteObject turns a CDP RemoteObject into display text, preferring the
// description because a plain Value loses the difference between 1 (number) and
// "1" (string) in some cases and is empty for objects entirely.
func renderRemoteObject(typ, subtype string, value json.RawMessage, description string) string {
	if description != "" {
		return description
	}
	if len(value) == 0 {
		if subtype != "" {
			return "<" + subtype + ">"
		}
		return "<" + typ + ">"
	}
	var s string
	if err := json.Unmarshal(value, &s); err == nil {
		return s
	}
	return string(value)
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// decodeInto unmarshals a CDP JSON value into out.
func decodeInto(raw json.RawMessage, out any) error {
	return json.Unmarshal(raw, out)
}
