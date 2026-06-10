// Package context implements a multi-level compression pipeline for managing
// conversation context windows. It combines heuristic, rule-based, and LLM
// strategies to progressively compress message history while preserving
// critical information.
package ctxcompress

import (
	"fmt"
	"regexp"
	"strings"
)

// CompressionLevel defines how aggressively a message should be compressed.
type CompressionLevel int

const (
	// LevelFull preserves the message verbatim — no compression.
	LevelFull CompressionLevel = iota
	// LevelTrim removes verbose tool output but keeps structure.
	LevelTrim
	// LevelCompact reduces to key facts only (1-2 sentences).
	LevelCompact
	// LevelSummary replaces with a generated summary reference.
	LevelSummary
)

// CompressionPolicy determines the compression level for a message based on
// its position in the conversation, role, and content characteristics.
type CompressionPolicy struct {
	// RecentWindow is the number of most-recent messages to keep at LevelFull.
	RecentWindow int
	// MidWindow is the number of messages before RecentWindow to keep at LevelTrim.
	MidWindow int
	// FarWindow messages beyond Recent+Mid get LevelCompact.
	// Everything beyond that gets LevelSummary.
}

// DefaultPolicy returns a sensible default policy.
func DefaultPolicy() CompressionPolicy {
	return CompressionPolicy{
		RecentWindow: 10,
		MidWindow:    20,
	}
}

// LevelForIndex returns the compression level for a message at the given
// distance from the end of the conversation.
func (p CompressionPolicy) LevelForIndex(distanceFromEnd int) CompressionLevel {
	if distanceFromEnd < p.RecentWindow {
		return LevelFull
	}
	if distanceFromEnd < p.RecentWindow+p.MidWindow {
		return LevelTrim
	}
	if distanceFromEnd < p.RecentWindow+p.MidWindow+30 {
		return LevelCompact
	}
	return LevelSummary
}

// CompressedMessage holds a message that has been through the compression
// pipeline.
type CompressedMessage struct {
	Role       string
	Content    string
	ToolName   string
	ToolCallID string
	IsError    bool
	Level      CompressionLevel
}

// Pipeline processes raw message content through multiple compression stages.
type Pipeline struct {
	policy    CompressionPolicy
	trimRules map[string]TrimRule
}

// TrimRule defines how to trim tool output for a specific tool.
type TrimRule struct {
	// MaxLines is the maximum number of output lines to keep.
	MaxLines int
	// HeadLines is the number of lines to keep from the start.
	HeadLines int
	// TailLines is the number of lines to keep from the end.
	TailLines int
	// KeepPatterns are regex patterns whose matching lines are always kept.
	KeepPatterns []*regexp.Regexp
}

// NewPipeline creates a new compression pipeline with the given policy.
func NewPipeline(policy CompressionPolicy) *Pipeline {
	return &Pipeline{
		policy:    policy,
		trimRules: defaultTrimRules(),
	}
}

// Policy returns the active compression policy.
func (p *Pipeline) Policy() CompressionPolicy {
	return p.policy
}

// Compress applies the appropriate compression level to a message.
func (p *Pipeline) Compress(msg CompressedMessage, level CompressionLevel) CompressedMessage {
	if level == LevelFull {
		msg.Level = LevelFull
		return msg
	}

	// Stage 1: Heuristic compression (no LLM, instant).
	msg = p.heuristicCompress(msg)

	if level == LevelTrim {
		msg.Level = LevelTrim
		return msg
	}

	// Stage 2: Rule-based compression (no LLM, instant).
	msg = p.ruleBasedCompress(msg)

	if level == LevelCompact {
		msg.Level = LevelCompact
		return msg
	}

	// LevelSummary is handled externally by the LLM summarizer.
	msg.Level = LevelSummary
	return msg
}

// heuristicCompress applies fast, pattern-based compression.
func (p *Pipeline) heuristicCompress(msg CompressedMessage) CompressedMessage {
	if msg.Role == "tool" && msg.ToolName != "" {
		msg.Content = p.trimToolOutput(msg.ToolName, msg.Content)
	}
	if msg.Role == "assistant" {
		msg.Content = p.trimAssistantContent(msg.Content)
	}
	return msg
}

// ruleBasedCompress applies semantic rules to extract key information.
func (p *Pipeline) ruleBasedCompress(msg CompressedMessage) CompressedMessage {
	if msg.Role == "tool" {
		msg.Content = p.extractKeyInfo(msg.ToolName, msg.Content, msg.IsError)
		return msg
	}
	if msg.Role == "user" {
		msg.Content = p.compactUserMessage(msg.Content)
		return msg
	}
	return msg
}

// trimToolOutput applies tool-specific trimming rules.
func (p *Pipeline) trimToolOutput(toolName, content string) string {
	rule, ok := p.trimRules[toolName]
	if !ok {
		// Default: trim to 100 lines.
		rule = TrimRule{MaxLines: 100, HeadLines: 30, TailLines: 30}
	}

	lines := strings.Split(content, "\n")
	if len(lines) <= rule.MaxLines {
		return content
	}

	// Always keep lines matching KeepPatterns.
	var kept []string
	var rest []string
	for _, line := range lines {
		matched := false
		for _, pat := range rule.KeepPatterns {
			if pat.MatchString(line) {
				kept = append(kept, line)
				matched = true
				break
			}
		}
		if !matched {
			rest = append(rest, line)
		}
	}

	if len(rest) <= rule.MaxLines {
		return content
	}

	var result []string
	result = append(result, rest[:rule.HeadLines]...)
	result = append(result, fmt.Sprintf("  ... [%d lines truncated] ...", len(rest)-rule.HeadLines-rule.TailLines))
	result = append(result, rest[len(rest)-rule.TailLines:]...)
	if len(kept) > 0 {
		result = append(result, "", "--- matched patterns ---")
		result = append(result, kept...)
	}
	return strings.Join(result, "\n")
}

// trimAssistantContent removes verbose reasoning dumps from assistant messages.
func (p *Pipeline) trimAssistantContent(content string) string {
	// Keep content as-is for LevelTrim — only truncate if extremely long.
	if len(content) > 4000 {
		return content[:2000] + "\n... [truncated] ...\n" + content[len(content)-1500:]
	}
	return content
}

// extractKeyInfo extracts the most important parts of a tool result.
func (p *Pipeline) extractKeyInfo(toolName, content string, isError bool) string {
	if isError {
		return p.extractErrorInfo(content)
	}

	switch toolName {
	case "bash":
		return p.extractBashKeyInfo(content)
	case "view":
		return p.extractViewKeyInfo(content)
	case "grep":
		return p.extractGrepKeyInfo(content)
	default:
		if len(content) > 500 {
			return content[:250] + "\n... [compressed] ...\n" + content[len(content)-200:]
		}
		return content
	}
}

// extractBashKeyInfo keeps exit code, error lines, and last few output lines.
func (p *Pipeline) extractBashKeyInfo(content string) string {
	lines := strings.Split(content, "\n")

	var errorLines []string
	var outputLines []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "error") || strings.Contains(trimmed, "Error") ||
			strings.Contains(trimmed, "fatal") || strings.Contains(trimmed, "FAIL") ||
			strings.HasPrefix(trimmed, "exit code") {
			errorLines = append(errorLines, line)
		}
	}

	// Keep last 10 lines of output.
	tailStart := len(lines) - 10
	if tailStart < 0 {
		tailStart = 0
	}
	outputLines = lines[tailStart:]

	var result []string
	if len(errorLines) > 0 {
		result = append(result, "[errors]")
		result = append(result, errorLines...)
		result = append(result, "")
	}
	if len(outputLines) > 0 {
		if len(result) > 0 {
			result = append(result, "[output tail]")
		}
		result = append(result, outputLines...)
	}
	return strings.Join(result, "\n")
}

// extractViewKeyInfo keeps first few and last few lines of file content.
func (p *Pipeline) extractViewKeyInfo(content string) string {
	lines := strings.Split(content, "\n")
	if len(lines) <= 20 {
		return content
	}
	result := []string{lines[0], lines[1], lines[2]}
	result = append(result, fmt.Sprintf("  ... [%d lines omitted] ...", len(lines)-6))
	result = append(result, lines[len(lines)-3:]...)
	return strings.Join(result, "\n")
}

// extractGrepKeyInfo keeps match count and first few matches.
func (p *Pipeline) extractGrepKeyInfo(content string) string {
	lines := strings.Split(content, "\n")
	if len(lines) <= 10 {
		return content
	}
	result := lines[:8]
	result = append(result, fmt.Sprintf("  ... [%d more matches] ...", len(lines)-8))
	return strings.Join(result, "\n")
}

// extractErrorInfo compresses error messages to essential info.
func (p *Pipeline) extractErrorInfo(content string) string {
	lines := strings.Split(content, "\n")
	if len(lines) <= 5 {
		return content
	}
	// Keep first 3 lines (usually the actual error) + last 2 lines (context).
	result := lines[:3]
	result = append(result, fmt.Sprintf("  ... [%d lines omitted] ...", len(lines)-5))
	result = append(result, lines[len(lines)-2:]...)
	return strings.Join(result, "\n")
}

// compactUserMessage reduces verbose user messages to key intent.
func (p *Pipeline) compactUserMessage(content string) string {
	if len(content) <= 200 {
		return content
	}
	// Keep first 150 chars and last 50 chars.
	return content[:150] + " ... " + content[len(content)-50:]
}

// defaultTrimRules returns built-in trimming rules per tool.
func defaultTrimRules() map[string]TrimRule {
	return map[string]TrimRule{
		"bash": {
			MaxLines: 80, HeadLines: 20, TailLines: 20,
			KeepPatterns: []*regexp.Regexp{
				regexp.MustCompile(`(?i)(error|fatal|panic|fail)`),
				regexp.MustCompile(`exit code`),
			},
		},
		"view": {
			MaxLines: 60, HeadLines: 15, TailLines: 15,
		},
		"grep": {
			MaxLines: 30, HeadLines: 15, TailLines: 10,
		},
		"glob": {
			MaxLines: 50, HeadLines: 25, TailLines: 20,
		},
		"fetch": {
			MaxLines: 80, HeadLines: 20, TailLines: 20,
		},
		"crawl": {
			MaxLines: 60, HeadLines: 15, TailLines: 15,
		},
	}
}
