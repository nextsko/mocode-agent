package agent

import (
	"strings"
	"testing"

	"charm.land/fantasy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/package-register/mocode/internal/core/agent/ctxcompress"
)

// helper: build a fantasy.Message containing a single ToolResultPart with
// the given tool call ID and text content.
func toolMsg(toolCallID, content string) fantasy.Message {
	return fantasy.Message{
		Role: fantasy.MessageRoleTool,
		Content: []fantasy.MessagePart{
			fantasy.ToolResultPart{
				ToolCallID: toolCallID,
				Output:     fantasy.ToolResultOutputContentText{Text: content},
			},
		},
	}
}

// helper: extract the text of the first ToolResultPart in a message.
func firstToolResultText(msg fantasy.Message) string {
	for _, part := range msg.Content {
		if tr, ok := fantasy.AsMessagePart[fantasy.ToolResultPart](part); ok {
			return extractToolResultText(tr)
		}
	}
	return ""
}

func TestCompressToolMessage_NilCompressor(t *testing.T) {
	t.Parallel()

	a := &sessionAgent{} // compressor is nil
	original := toolMsg("call_1", "some content")
	result := a.compressToolMessage(original, 50, nil)

	require.Equal(t, original, result, "nil compressor should return msg unchanged")
}

func TestCompressToolMessage_LevelFull(t *testing.T) {
	t.Parallel()

	a := &sessionAgent{
		compressor: ctxcompress.NewPipeline(ctxcompress.DefaultPolicy()),
	}

	content := strings.Repeat("this is a line of output\n", 200)
	original := toolMsg("call_1", content)

	// distance 5 → within RecentWindow(10) → LevelFull
	result := a.compressToolMessage(original, 5, map[string]string{"call_1": "bash"})
	require.Equal(t, content, firstToolResultText(result), "LevelFull should preserve content")
}

func TestCompressToolMessage_LevelTrim_BashTool(t *testing.T) {
	t.Parallel()

	a := &sessionAgent{
		compressor: ctxcompress.NewPipeline(ctxcompress.DefaultPolicy()),
	}

	// 200 lines — exceeds bash MaxLines(80) so will be trimmed.
	var lines []string
	for i := 0; i < 200; i++ {
		lines = append(lines, "output line "+strings.Repeat("x", 20))
	}
	content := strings.Join(lines, "\n")
	original := toolMsg("call_1", content)

	// distance 15 → LevelTrim, toolName=bash
	result := a.compressToolMessage(original, 15, map[string]string{"call_1": "bash"})
	compressed := firstToolResultText(result)

	require.NotEqual(t, content, compressed, "LevelTrim should modify long bash output")
	assert.Less(t, len(compressed), len(content), "trimmed content should be shorter")
	assert.Contains(t, compressed, "truncated", "should contain truncation marker")
}

func TestCompressToolMessage_LevelTrim_NoToolName(t *testing.T) {
	t.Parallel()

	a := &sessionAgent{
		compressor: ctxcompress.NewPipeline(ctxcompress.DefaultPolicy()),
	}

	// 200 lines without a tool name — pipeline's heuristicCompress skips
	// trimToolOutput when ToolName is empty, so LevelTrim is a no-op.
	// This verifies that tool name propagation matters for trimming to fire.
	var lines []string
	for i := 0; i < 200; i++ {
		lines = append(lines, "output line "+strings.Repeat("x", 20))
	}
	content := strings.Join(lines, "\n")
	original := toolMsg("call_1", content)

	// distance 15 → LevelTrim, no tool name in map
	result := a.compressToolMessage(original, 15, nil)
	compressed := firstToolResultText(result)

	assert.Equal(t, content, compressed,
		"without tool name, LevelTrim should be a no-op (heuristicCompress skips trimToolOutput)")
}

func TestCompressToolMessage_LevelTrim_ErrorLinesPreserved(t *testing.T) {
	t.Parallel()

	a := &sessionAgent{
		compressor: ctxcompress.NewPipeline(ctxcompress.DefaultPolicy()),
	}

	var lines []string
	for i := 0; i < 200; i++ {
		if i == 100 {
			lines = append(lines, "Error: something failed badly")
		}
		lines = append(lines, "normal output line")
	}
	content := strings.Join(lines, "\n")
	original := toolMsg("call_1", content)

	// distance 15 → LevelTrim, toolName=bash (has Error KeepPattern)
	result := a.compressToolMessage(original, 15, map[string]string{"call_1": "bash"})
	compressed := firstToolResultText(result)

	assert.Contains(t, compressed, "Error: something failed badly",
		"error lines should be preserved by bash KeepPatterns")
}

func TestCompressToolMessage_LevelCompact(t *testing.T) {
	t.Parallel()

	a := &sessionAgent{
		compressor: ctxcompress.NewPipeline(ctxcompress.DefaultPolicy()),
	}

	var lines []string
	for i := 0; i < 150; i++ {
		lines = append(lines, "line "+strings.Repeat("y", 30))
	}
	content := strings.Join(lines, "\n")
	original := toolMsg("call_1", content)

	// distance 40 → LevelCompact (30..59)
	result := a.compressToolMessage(original, 40, map[string]string{"call_1": "bash"})
	compressed := firstToolResultText(result)

	assert.Less(t, len(compressed), len(content), "LevelCompact should be shorter than original")
}

func TestCompressToolMessage_LevelSummary(t *testing.T) {
	t.Parallel()

	a := &sessionAgent{
		compressor: ctxcompress.NewPipeline(ctxcompress.DefaultPolicy()),
	}

	content := strings.Repeat("historical content line\n", 150)
	original := toolMsg("call_1", content)

	// distance 70 → LevelSummary (60+)
	result := a.compressToolMessage(original, 70, map[string]string{"call_1": "bash"})
	compressed := firstToolResultText(result)

	assert.NotEqual(t, content, compressed, "LevelSummary should modify long content")
}

func TestCompressToolMessage_ShortContentUnchanged(t *testing.T) {
	t.Parallel()

	a := &sessionAgent{
		compressor: ctxcompress.NewPipeline(ctxcompress.DefaultPolicy()),
	}

	content := "short output\nonly a few lines"
	original := toolMsg("call_1", content)

	// Even at LevelTrim distance, short content should be unchanged.
	result := a.compressToolMessage(original, 15, map[string]string{"call_1": "bash"})
	assert.Equal(t, content, firstToolResultText(result), "short content should not be trimmed")
}

func TestCompressToolMessage_PreservesNonToolResultParts(t *testing.T) {
	t.Parallel()

	a := &sessionAgent{
		compressor: ctxcompress.NewPipeline(ctxcompress.DefaultPolicy()),
	}

	textPart := fantasy.TextPart{Text: "not a tool result"}
	toolPart := fantasy.ToolResultPart{
		ToolCallID: "call_1",
		Output:     fantasy.ToolResultOutputContentText{Text: strings.Repeat("compress me\n", 150)},
	}

	msg := fantasy.Message{
		Role:    fantasy.MessageRoleTool,
		Content: []fantasy.MessagePart{textPart, toolPart},
	}

	result := a.compressToolMessage(msg, 40, map[string]string{"call_1": "bash"})

	require.Len(t, result.Content, 2, "should preserve both parts")

	// Non-ToolResult part should be unchanged.
	if tp, ok := fantasy.AsMessagePart[fantasy.TextPart](result.Content[0]); ok {
		assert.Equal(t, "not a tool result", tp.Text)
	} else {
		t.Fatal("first part should still be a TextPart")
	}

	// ToolResult part should have been compressed.
	if tr, ok := fantasy.AsMessagePart[fantasy.ToolResultPart](result.Content[1]); ok {
		text := extractToolResultText(tr)
		assert.Less(t, len(text), 150*len("compress me\n"),
			"tool result should have been compressed at distance 40")
	} else {
		t.Fatal("second part should still be a ToolResultPart")
	}
}

func TestCompressToolMessage_ErrorOutput(t *testing.T) {
	t.Parallel()

	a := &sessionAgent{
		compressor: ctxcompress.NewPipeline(ctxcompress.DefaultPolicy()),
	}

	// Long error output — LevelCompact at distance 40 should compress it
	// while preserving error information.
	var lines []string
	for i := 0; i < 100; i++ {
		lines = append(lines, "Error: something failed at step "+strings.Repeat("x", 20))
	}
	content := strings.Join(lines, "\n")

	msg := fantasy.Message{
		Role: fantasy.MessageRoleTool,
		Content: []fantasy.MessagePart{
			fantasy.ToolResultPart{
				ToolCallID: "call_err",
				Output: fantasy.ToolResultOutputContentError{
					Error: &strError{msg: content},
				},
			},
		},
	}

	// distance 40 → LevelCompact
	result := a.compressToolMessage(msg, 40, map[string]string{"call_err": "bash"})
	compressed := firstToolResultText(result)

	assert.Less(t, len(compressed), len(content),
		"error output should be compressed at LevelCompact")
}

// strError is a simple error implementation for testing.
type strError struct{ msg string }

func (e *strError) Error() string { return e.msg }
