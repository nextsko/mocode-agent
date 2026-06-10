package evolution

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// ErrorPattern represents a recurring tool execution error that the system
// has learned from. Patterns are stored as Obsidian-flavored Markdown with
// YAML frontmatter for both human readability and machine parsing.
type ErrorPattern struct {
	// ID is a deterministic hash of Provider+Tool+Pattern.
	ID string `json:"id" yaml:"id"`
	// Provider is the LLM provider (e.g., "minimax", "anthropic").
	Provider string `json:"provider" yaml:"provider"`
	// Model is the specific model ID (optional, for model-specific quirks).
	Model string `json:"model,omitempty" yaml:"model,omitempty"`
	// Tool is the tool name (e.g., "bash").
	Tool string `json:"tool" yaml:"tool"`
	// Pattern is the error pattern to match (substring or regex).
	Pattern string `json:"pattern" yaml:"pattern"`
	// Correction is the instruction to inject when this pattern is detected.
	Correction string `json:"correction" yaml:"correction"`
	// Occurrences is how many times this error has been seen.
	Occurrences int `json:"occurrences" yaml:"occurrences"`
	// LastSeen is when this pattern was last encountered.
	LastSeen time.Time `json:"last_seen" yaml:"last_seen"`
	// PromotedAt is when the pattern exceeded the threshold and became a rule.
	PromotedAt time.Time `json:"promoted_at,omitempty" yaml:"promoted_at,omitempty"`
	// IsPromoted means the correction is actively injected into the system prompt.
	IsPromoted bool `json:"is_promoted" yaml:"is_promoted"`
}

// ErrorLearner tracks tool execution errors and auto-generates corrections
// when the same error pattern repeats across sessions. It persists patterns
// as YAML-frontmatter Markdown files in .mocode/evolution/patterns/.
type ErrorLearner struct {
	mu       sync.RWMutex
	dir      string
	patterns map[string]*ErrorPattern // keyed by ID
}

// PromoteThreshold is the number of occurrences before a pattern becomes an
// active correction rule injected into the system prompt.
const PromoteThreshold = 3

// NewErrorLearner creates or loads an error learner from the given base
// directory (typically .mocode/evolution/patterns/).
func NewErrorLearner(dir string) (*ErrorLearner, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create patterns dir: %w", err)
	}
	l := &ErrorLearner{
		dir:      dir,
		patterns: make(map[string]*ErrorPattern),
	}
	if err := l.load(); err != nil {
		return nil, fmt.Errorf("load patterns: %w", err)
	}
	return l, nil
}

// Record records a tool execution error. If the same pattern has been seen
// enough times (>= PromoteThreshold), it is automatically promoted to an
// active correction rule. Returns the pattern (which may be newly promoted).
func (l *ErrorLearner) Record(provider, model, tool, errorMsg string) *ErrorPattern {
	pattern := extractPattern(errorMsg)
	id := patternID(provider, model, tool, pattern)

	l.mu.Lock()
	defer l.mu.Unlock()

	p, ok := l.patterns[id]
	if !ok {
		p = &ErrorPattern{
			ID:         id,
			Provider:   provider,
			Model:      model,
			Tool:       tool,
			Pattern:    pattern,
			Correction: buildCorrection(tool, pattern, errorMsg),
		}
		l.patterns[id] = p
	}

	p.Occurrences++
	p.LastSeen = time.Now()

	if p.Occurrences >= PromoteThreshold && !p.IsPromoted {
		p.IsPromoted = true
		p.PromotedAt = time.Now()
	}

	_ = l.savePattern(p)
	return p
}

// ActiveCorrections returns all promoted correction rules for the given
// provider and model. These should be injected into the system prompt.
func (l *ErrorLearner) ActiveCorrections(provider, model string) []ErrorPattern {
	l.mu.RLock()
	defer l.mu.RUnlock()

	var result []ErrorPattern
	for _, p := range l.patterns {
		if !p.IsPromoted {
			continue
		}
		if p.Provider != provider {
			continue
		}
		if p.Model != "" && p.Model != model {
			continue
		}
		result = append(result, *p)
	}
	return result
}

// CorrectionContext builds a Markdown block suitable for injecting into the
// system prompt as a provider-specific correction layer.
func (l *ErrorLearner) CorrectionContext(provider, model string) string {
	corrections := l.ActiveCorrections(provider, model)
	if len(corrections) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## Learned Corrections\n")
	sb.WriteString("The following issues have been encountered repeatedly. Avoid these mistakes:\n\n")
	for _, c := range corrections {
		sb.WriteString(fmt.Sprintf("- **%s tool**: %s (seen %d times)\n", c.Tool, c.Correction, c.Occurrences))
	}
	return sb.String()
}

// AllPatterns returns all known error patterns for inspection.
func (l *ErrorLearner) AllPatterns() []ErrorPattern {
	l.mu.RLock()
	defer l.mu.RUnlock()

	result := make([]ErrorPattern, 0, len(l.patterns))
	for _, p := range l.patterns {
		result = append(result, *p)
	}
	return result
}

// load reads all pattern files from disk.
func (l *ErrorLearner) load() error {
	entries, err := os.ReadDir(l.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(l.dir, entry.Name()))
		if err != nil {
			continue
		}
		p, err := parsePatternFile(data)
		if err != nil {
			continue
		}
		l.patterns[p.ID] = p
	}
	return nil
}

// savePattern persists a single pattern as an Obsidian-flavored Markdown file
// with YAML frontmatter.
func (l *ErrorLearner) savePattern(p *ErrorPattern) error {
	var sb strings.Builder

	// YAML frontmatter.
	sb.WriteString("---\n")
	sb.WriteString(fmt.Sprintf("id: %q\n", p.ID))
	sb.WriteString(fmt.Sprintf("provider: %q\n", p.Provider))
	sb.WriteString(fmt.Sprintf("model: %q\n", p.Model))
	sb.WriteString(fmt.Sprintf("tool: %q\n", p.Tool))
	sb.WriteString(fmt.Sprintf("pattern: %q\n", p.Pattern))
	sb.WriteString(fmt.Sprintf("occurrences: %d\n", p.Occurrences))
	sb.WriteString(fmt.Sprintf("last_seen: %q\n", p.LastSeen.Format(time.RFC3339)))
	sb.WriteString(fmt.Sprintf("is_promoted: %v\n", p.IsPromoted))
	if p.IsPromoted {
		sb.WriteString(fmt.Sprintf("promoted_at: %q\n", p.PromotedAt.Format(time.RFC3339)))
	}
	sb.WriteString("---\n\n")

	// Human-readable body.
	sb.WriteString(fmt.Sprintf("# Error Pattern: %s\n\n", truncate(p.Pattern, 60)))
	sb.WriteString(fmt.Sprintf("**Tool**: `%s`  |  **Provider**: `%s`", p.Tool, p.Provider))
	if p.Model != "" {
		sb.WriteString(fmt.Sprintf("  |  **Model**: `%s`", p.Model))
	}
	sb.WriteString(fmt.Sprintf("\n\n**Seen**: %d times\n\n", p.Occurrences))
	sb.WriteString("## Correction\n\n")
	sb.WriteString(p.Correction)
	sb.WriteString("\n")

	return os.WriteFile(filepath.Join(l.dir, p.ID+".md"), []byte(sb.String()), 0o644)
}

// parsePatternFile reads a YAML-frontmatter Markdown file back into an ErrorPattern.
func parsePatternFile(data []byte) (*ErrorPattern, error) {
	content := string(data)

	// Extract YAML frontmatter between --- markers.
	if !strings.HasPrefix(content, "---\n") {
		return nil, fmt.Errorf("missing YAML frontmatter")
	}
	end := strings.Index(content[4:], "\n---\n")
	if end == -1 {
		return nil, fmt.Errorf("unclosed YAML frontmatter")
	}
	yamlBlock := content[4 : end+4]

	p := &ErrorPattern{}
	// Simple YAML key-value parsing.
	lines := strings.Split(yamlBlock, "\n")
	for _, line := range lines {
		kv := strings.SplitN(line, ":", 2)
		if len(kv) != 2 {
			continue
		}
		key := strings.TrimSpace(kv[0])
		val := strings.TrimSpace(kv[1])
		val = strings.Trim(val, "\"")

		switch key {
		case "id":
			p.ID = val
		case "provider":
			p.Provider = val
		case "model":
			p.Model = val
		case "tool":
			p.Tool = val
		case "pattern":
			p.Pattern = val
		case "occurrences":
			fmt.Sscanf(val, "%d", &p.Occurrences)
		case "last_seen":
			p.LastSeen, _ = time.Parse(time.RFC3339, val)
		case "is_promoted":
			p.IsPromoted = val == "true"
		case "promoted_at":
			p.PromotedAt, _ = time.Parse(time.RFC3339, val)
		}
	}

	if p.Correction == "" {
		p.Correction = buildCorrection(p.Tool, p.Pattern, "")
	}
	return p, nil
}

// extractPattern extracts the stable error pattern from a raw error message.
// It normalizes file paths, line numbers, and other variable parts.
func extractPattern(msg string) string {
	// Remove specific file paths.
	s := ansiStripPath.ReplaceAllString(msg, "<path>")
	// Remove specific line numbers.
	s = ansiStripLineNum.ReplaceAllString(s, "<line>")
	// Remove timestamps.
	s = ansiStripTime.ReplaceAllString(s, "<time>")
	// Truncate to first 200 chars for stable matching.
	if len(s) > 200 {
		s = s[:200]
	}
	return strings.TrimSpace(s)
}

// buildCorrection generates a correction instruction from the error context.
func buildCorrection(tool, pattern, original string) string {
	// Check known patterns for common corrections.
	lower := strings.ToLower(pattern)

	if tool == "bash" {
		if strings.Contains(lower, "command not found") {
			// Extract the command name.
			cmd := extractCommandName(original)
			if cmd != "" {
				return fmt.Sprintf("The command `%s` is not available on this system. Use an alternative tool (e.g., `view` for viewing files, `grep` for searching). Do not attempt to call `%s` again.", cmd, cmd)
			}
		}
		if strings.Contains(lower, "permission denied") {
			return "Permission was denied. Do not retry the same command. Ask the user or use a different approach."
		}
		if strings.Contains(lower, "no such file") {
			return "The file does not exist. Verify the path with `glob` or `ls` before attempting to read or edit it."
		}
	}

	if tool == "edit" || tool == "write" {
		if strings.Contains(lower, "old_string not found") {
			return "The text to replace was not found in the file. Re-read the file first to get the exact current content, then retry the edit with the correct text."
		}
	}

	// Generic correction.
	if original != "" {
		return fmt.Sprintf("This tool call tends to fail with: %s. Adjust your approach to avoid this error.", truncate(pattern, 100))
	}
	return fmt.Sprintf("Avoid repeating the same failing pattern with %s.", tool)
}

func extractCommandName(msg string) string {
	if idx := strings.Index(msg, "command not found"); idx != -1 {
		after := msg[idx+len("command not found"):]
		after = strings.TrimLeft(after, ": ")
		if cmd := strings.Fields(after); len(cmd) > 0 {
			return strings.TrimRight(cmd[0], ":")
		}
		before := msg[:idx]
		fields := strings.Fields(before)
		if len(fields) > 0 {
			return strings.TrimRight(fields[len(fields)-1], ":")
		}
	}
	return ""
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func patternID(provider, model, tool, pattern string) string {
	key := provider + "|" + model + "|" + tool + "|" + pattern
	h := sha256.Sum256([]byte(key))
	return hex.EncodeToString(h[:])[:12]
}

var (
	ansiStripPath    = regexp.MustCompile(`(/[^\s:]+|/[^\s]+)`)
	ansiStripLineNum = regexp.MustCompile(`:\d+`)
	ansiStripTime    = regexp.MustCompile(`\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}`)
)

// Ensure ErrorLearner is stored as JSONL-compatible.
var _ json.Marshaler = (*ErrorPattern)(nil)

// MarshalJSON implements json.Marshaler.
func (p *ErrorPattern) MarshalJSON() ([]byte, error) {
	type alias ErrorPattern
	return json.Marshal((*alias)(p))
}
