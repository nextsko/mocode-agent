// Package llmconv also owns the low-level fantasy message helpers used while
// preparing a step: tool-result text access, orphan filtering, and retry log
// fields. They live here (not in agent/messages) so the domain-facing helper
// package stays free of the LLM framework.
package llmconv

import (
	"errors"
	"log/slog"
	"time"

	"charm.land/fantasy"

	"github.com/nextsko/mocode-agent/internal/domain/session/message"
)

func ExtractToolResultText(tr fantasy.ToolResultPart) string {
	if r, ok := fantasy.AsToolResultOutputType[fantasy.ToolResultOutputContentText](tr.Output); ok {
		return r.Text
	}
	if r, ok := fantasy.AsToolResultOutputType[fantasy.ToolResultOutputContentError](tr.Output); ok {
		return r.Error.Error()
	}
	return ""
}

func IsToolResultError(tr fantasy.ToolResultPart) bool {
	_, ok := fantasy.AsToolResultOutputType[fantasy.ToolResultOutputContentError](tr.Output)
	return ok
}

func SetToolResultText(tr *fantasy.ToolResultPart, text string) {
	tr.Output = fantasy.ToolResultOutputContentText{Text: text}
}

func FilterImagePartsFromMessages(msgs []fantasy.Message) []fantasy.Message {
	var result []fantasy.Message
	for _, msg := range msgs {
		var filteredParts []fantasy.MessagePart
		for _, part := range msg.Content {
			if _, isFile := fantasy.AsMessagePart[fantasy.FilePart](part); isFile {
				continue
			}
			filteredParts = append(filteredParts, part)
		}
		if len(filteredParts) > 0 {
			msg.Content = filteredParts
			result = append(result, msg)
		}
	}
	return result
}

// FilterEmptyContentMessages drops any messages with empty content arrays.
// Sending {"content": []} to LLM APIs causes "messages.content.type is
// invalid" errors. System messages are preserved (they use a string content
// field, not an array).
func FilterEmptyContentMessages(msgs []fantasy.Message) []fantasy.Message {
	filtered := msgs[:0]
	for _, msg := range msgs {
		if msg.Role != fantasy.MessageRoleSystem && len(msg.Content) == 0 {
			slog.Warn(
				"Dropping message with empty content in PrepareStep",
				"role", msg.Role,
			)
			continue
		}
		filtered = append(filtered, msg)
	}
	return filtered
}

func FilterOrphanedToolResults(m message.Message, knownToolCallIDs map[string]struct{}) (fantasy.Message, bool) {
	aiMsgs := ToAIMessage(m)
	if len(aiMsgs) == 0 {
		return fantasy.Message{}, false
	}
	var validParts []fantasy.MessagePart
	for _, part := range aiMsgs[0].Content {
		tr, ok := fantasy.AsMessagePart[fantasy.ToolResultPart](part)
		if !ok {
			validParts = append(validParts, part)
			continue
		}
		if _, known := knownToolCallIDs[tr.ToolCallID]; known {
			validParts = append(validParts, part)
		} else {
			slog.Warn(
				"Dropping orphaned tool result with no matching tool call",
				"tool_call_id", tr.ToolCallID,
			)
		}
	}
	if len(validParts) == 0 {
		return fantasy.Message{}, false
	}
	msg := aiMsgs[0]
	msg.Content = validParts
	return msg, true
}

func SyntheticToolResultsForOrphanedCalls(m message.Message, knownToolResultIDs map[string]struct{}) (fantasy.Message, bool) {
	var syntheticParts []fantasy.MessagePart
	for _, tc := range m.ToolCalls() {
		if _, hasResult := knownToolResultIDs[tc.ID]; hasResult {
			continue
		}
		slog.Warn(
			"Injecting synthetic tool result for orphaned tool call",
			"tool_call_id", tc.ID,
			"tool_name", tc.Name,
		)
		syntheticParts = append(syntheticParts, fantasy.ToolResultPart{
			ToolCallID: tc.ID,
			Output: fantasy.ToolResultOutputContentError{
				Error: errors.New("tool call was interrupted and did not produce a result, you may retry this call if the result is still needed"),
			},
		})
	}
	if len(syntheticParts) == 0 {
		return fantasy.Message{}, false
	}
	return fantasy.Message{
		Role:    fantasy.MessageRoleTool,
		Content: syntheticParts,
	}, true
}

func ProviderRetryLogFields(err *fantasy.ProviderError, delay time.Duration) []any {
	fields := []any{
		"retry_delay", delay.String(),
	}
	if err == nil {
		return fields
	}
	fields = append(fields, "status_code", err.StatusCode)
	if err.Title != "" {
		fields = append(fields, "title", err.Title)
	}
	if err.Message != "" {
		fields = append(fields, "message", err.Message)
	}
	return fields
}
