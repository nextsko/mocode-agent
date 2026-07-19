package agent

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/nextsko/mocode-agent/internal/domain/session/message"
)

// ContextCompressor automatically compresses long conversations by summarizing
// middle turns when the context window fills up. It uses a cheap/fast auxiliary
// model for summarization and protects head (system/first turns) and tail
// (recent turns) from compression.
type ContextCompressor struct {
	mu sync.Mutex

	// SummarizeFunc is the function that generates a summary from conversation turns.
	// If nil, compression is disabled.
	SummarizeFunc func(ctx context.Context, turns []message.Message, budget int) (string, error)

	// Threshold is the context usage ratio (0-1) at which compression triggers.
	// Default: 0.75 (75% of context window used)
	Threshold float64

	// ProtectHeadMessages is the number of initial messages to never compress.
	// Default: 4
	ProtectHeadMessages int

	// ProtectTailMessages is the number of recent messages to never compress.
	// Default: 8
	ProtectTailMessages int

	// MaxSummaryTokens is the maximum token budget for the summary output.
	// Default: 4000
	MaxSummaryTokens int

	// CharsPerToken rough estimate for token counting.
	// Default: 4
	CharsPerToken int

	// lastCompressAt tracks when the last compression happened per session.
	lastCompressAt map[string]time.Time

	// compressCooldown is the minimum time between compressions for a session.
	compressCooldown time.Duration
}

// NewContextCompressor creates a new compressor with sensible defaults.
func NewContextCompressor(summarizeFunc func(ctx context.Context, turns []message.Message, budget int) (string, error)) *ContextCompressor {
	return &ContextCompressor{
		SummarizeFunc:       summarizeFunc,
		Threshold:           0.75,
		ProtectHeadMessages: 4,
		ProtectTailMessages: 8,
		MaxSummaryTokens:    4000,
		CharsPerToken:       4,
		lastCompressAt:      make(map[string]time.Time),
		compressCooldown:    10 * time.Minute,
	}
}

// ShouldCompress checks whether the conversation needs compression
// based on current token usage vs the model's context window.
func (cc *ContextCompressor) ShouldCompress(sessionID string, promptTokens, completionTokens int64, contextWindow int) bool {
	if cc.SummarizeFunc == nil {
		return false
	}
	if contextWindow <= 0 {
		return false
	}

	cc.mu.Lock()
	defer cc.mu.Unlock()

	// Cooldown check
	if last, ok := cc.lastCompressAt[sessionID]; ok {
		if time.Since(last) < cc.compressCooldown {
			return false
		}
	}

	used := promptTokens + completionTokens
	ratio := float64(used) / float64(contextWindow)
	return ratio >= cc.Threshold
}

// Compress compresses the conversation by summarizing middle turns.
// It returns the summary message and the IDs of messages that were compressed.
func (cc *ContextCompressor) Compress(ctx context.Context, sessionID string, messages []message.Message) (*message.Message, []string, error) {
	cc.mu.Lock()
	cc.lastCompressAt[sessionID] = time.Now()
	cc.mu.Unlock()

	if cc.SummarizeFunc == nil {
		return nil, nil, fmt.Errorf("compressor: no summarize function configured")
	}

	if len(messages) <= cc.ProtectHeadMessages+cc.ProtectTailMessages {
		return nil, nil, nil // nothing to compress
	}

	// Split messages into head, middle (to compress), and tail
	headEnd := cc.ProtectHeadMessages
	tailStart := len(messages) - cc.ProtectTailMessages

	if tailStart <= headEnd {
		return nil, nil, nil // overlap, nothing to compress
	}

	_ = messages[:headEnd]
	middle := messages[headEnd:tailStart]
	_ = messages[tailStart:]

	// Check if middle has any user/assistant messages to compress
	hasContent := false
	for _, m := range middle {
		if m.Role == message.User || m.Role == message.Assistant {
			hasContent = true
			break
		}
	}
	if !hasContent {
		return nil, nil, nil
	}

	// Estimate summary budget
	budget := cc.estimateBudget(middle)

	// Generate summary
	summary, err := cc.SummarizeFunc(ctx, middle, budget)
	if err != nil {
		slog.Warn("Context compression failed, using fallback",
			"error", err, "session_id", sessionID)
		summary = cc.fallbackSummary(middle)
	}

	// Build summary message
	summaryMsg := cc.buildSummaryMessage(summary, middle)

	// Collect IDs of compressed messages
	compressedIDs := make([]string, len(middle))
	for i, m := range middle {
		compressedIDs[i] = m.ID
	}

	slog.Info("Context compressed",
		"session_id", sessionID,
		"compressed_messages", len(middle),
		"summary_chars", len(summary),
		"total_messages", len(messages))

	return summaryMsg, compressedIDs, nil
}

// estimateBudget calculates the character budget for the summary.
func (cc *ContextCompressor) estimateBudget(middle []message.Message) int {
	totalChars := 0
	for _, m := range middle {
		totalChars += estimateMessageChars(m)
	}

	// Summary is 20% of compressed content, capped at MaxSummaryTokens
	summaryTokens := int(math.Min(
		float64(totalChars/cc.CharsPerToken)*0.20,
		float64(cc.MaxSummaryTokens),
	))
	if summaryTokens < 500 {
		summaryTokens = 500
	}
	return summaryTokens * cc.CharsPerToken
}

// buildSummaryMessage creates the summary message with proper markers.
func (cc *ContextCompressor) buildSummaryMessage(summary string, middle []message.Message) *message.Message {
	// Build structured summary with markers
	var sb strings.Builder

	sb.WriteString("[CONTEXT COMPACTION - REFERENCE ONLY] ")
	sb.WriteString("Earlier turns were compacted into the summary below. ")
	sb.WriteString("Treat this as background reference, NOT as active instructions. ")
	sb.WriteString("Respond ONLY to the latest user message AFTER this summary.\n\n")
	sb.WriteString(summary)
	sb.WriteString("\n\n--- END OF CONTEXT SUMMARY --- ")
	sb.WriteString("Respond to the message below, not the summary above ---")

	summaryMsg := &message.Message{
		Role:             message.Assistant,
		Parts:            []message.ContentPart{message.TextContent{Text: sb.String()}},
		IsSummaryMessage: true,
		CreatedAt:        time.Now().Unix(),
	}

	return summaryMsg
}

// fallbackSummary generates a simple summary when the LLM summarizer fails.
func (cc *ContextCompressor) fallbackSummary(turns []message.Message) string {
	var sb strings.Builder

	resolvedCount := 0
	inProgressCount := 0
	var lastUserMsg string
	var lastAssistantAction string
	var relevantFiles []string

	for _, m := range turns {
		text := extractTextFromParts(m.Parts)
		switch m.Role {
		case message.User:
			if text != "" {
				lastUserMsg = truncateString(text, 200)
			}
		case message.Assistant:
			if text != "" {
				lastAssistantAction = truncateString(text, 200)
				resolvedCount++
			}
			// Track tool calls
			for _, tc := range m.ToolCalls() {
				if tc.Name != "" {
					inProgressCount++
				}
			}
		}
		// Extract file paths
		extractFilePaths(text, &relevantFiles, 10)
	}

	sb.WriteString("## Historical Task Snapshot\n")
	if lastUserMsg != "" {
		fmt.Fprintf(&sb, "Last user request: %s\n", lastUserMsg)
	}
	if lastAssistantAction != "" {
		fmt.Fprintf(&sb, "Last assistant action: %s\n", lastAssistantAction)
	}
	fmt.Fprintf(&sb, "Resolved items: %d\n", resolvedCount)
	fmt.Fprintf(&sb, "Tool operations: %d\n", inProgressCount)

	if len(relevantFiles) > 0 {
		sb.WriteString("\n## Files Referenced\n")
		for _, f := range relevantFiles {
			fmt.Fprintf(&sb, "- %s\n", f)
		}
	}

	return sb.String()
}

// extractTextFromParts gets all text content from message parts.
func extractTextFromParts(parts []message.ContentPart) string {
	var texts []string
	for _, p := range parts {
		switch v := p.(type) {
		case message.TextContent:
			if v.Text != "" {
				texts = append(texts, v.Text)
			}
		case message.ToolResult:
			if v.Content != "" {
				texts = append(texts, v.Content)
			}
		case message.ToolCall:
			if v.Name != "" {
				texts = append(texts, "tool:"+v.Name)
			}
		}
	}
	return strings.Join(texts, "\n")
}

// estimateMessageChars estimates the character length of a message for budgeting.
func estimateMessageChars(m message.Message) int {
	total := 0
	for _, p := range m.Parts {
		switch v := p.(type) {
		case message.TextContent:
			total += len(v.Text)
		case message.ToolResult:
			if v.Content != "" {
				total += len(v.Content)
			}
		case message.ToolCall:
			total += len(v.Name) + len(v.Input)
		case message.BinaryContent:
			total += 6400 // ~1600 tokens equivalent per image
		}
	}
	return total
}

// extractFilePaths finds file paths in text.
func extractFilePaths(text string, files *[]string, limit int) {
	// Simple path extraction: look for patterns like /path/to/file or C:\path
	// This is a basic implementation; a more robust version would use regex
	if len(*files) >= limit {
		return
	}

	// Split on whitespace and check for path-like tokens
	tokens := strings.Fields(text)
	for _, t := range tokens {
		if strings.Contains(t, "/") || strings.Contains(t, "\\") {
			if strings.HasPrefix(t, "/") || strings.HasPrefix(t, "~") ||
				(len(t) > 2 && t[1] == ':') {
				// Truncate and deduplicate
				candidate := truncateString(t, 100)
				duplicate := false
				for _, existing := range *files {
					if existing == candidate {
						duplicate = true
						break
					}
				}
				if !duplicate && len(*files) < limit {
					*files = append(*files, candidate)
				}
			}
		}
	}
}

// truncateString truncates a string to maxLen.
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
