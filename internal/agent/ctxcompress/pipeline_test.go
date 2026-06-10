package ctxcompress

import (
	"strings"
	"testing"
)

func TestDefaultPolicy_Levels(t *testing.T) {
	p := DefaultPolicy()

	tests := []struct {
		distance int
		want     CompressionLevel
	}{
		{0, LevelFull},
		{9, LevelFull},
		{10, LevelTrim},
		{29, LevelTrim},
		{30, LevelCompact},
		{59, LevelCompact},
		{60, LevelSummary},
	}
	for _, tt := range tests {
		got := p.LevelForIndex(tt.distance)
		if got != tt.want {
			t.Errorf("LevelForIndex(%d) = %v, want %v", tt.distance, got, tt.want)
		}
	}
}

func TestPipeline_TrimBashOutput(t *testing.T) {
	pipeline := NewPipeline(DefaultPolicy())

	// Generate 200 lines of bash output.
	var lines []string
	for i := 0; i < 200; i++ {
		lines = append(lines, "output line "+strings.Repeat("x", 30))
	}
	content := strings.Join(lines, "\n")

	msg := CompressedMessage{
		Role:     "tool",
		ToolName: "bash",
		Content:  content,
	}

	result := pipeline.Compress(msg, LevelTrim)
	if result.Level != LevelTrim {
		t.Fatalf("expected LevelTrim, got %v", result.Level)
	}
	if strings.Count(result.Content, "\n") >= 200 {
		t.Fatal("expected bash output to be trimmed")
	}
	if !strings.Contains(result.Content, "truncated") {
		t.Fatal("expected truncation marker in output")
	}
}

func TestPipeline_ErrorLinesPreserved(t *testing.T) {
	pipeline := NewPipeline(DefaultPolicy())

	var lines []string
	for i := 0; i < 150; i++ {
		if i == 50 {
			lines = append(lines, "Error: something failed badly")
		}
		lines = append(lines, "normal output line")
	}
	content := strings.Join(lines, "\n")

	msg := CompressedMessage{
		Role:     "tool",
		ToolName: "bash",
		Content:  content,
	}

	result := pipeline.Compress(msg, LevelTrim)
	if !strings.Contains(result.Content, "Error: something failed badly") {
		t.Fatal("expected error line to be preserved by KeepPatterns")
	}
}

func TestPipeline_CompactExtractsKeyInfo(t *testing.T) {
	pipeline := NewPipeline(DefaultPolicy())

	var lines []string
	for i := 0; i < 100; i++ {
		lines = append(lines, "line "+strings.Repeat("y", 20))
	}
	content := strings.Join(lines, "\n")

	msg := CompressedMessage{
		Role:     "tool",
		ToolName: "bash",
		Content:  content,
	}

	result := pipeline.Compress(msg, LevelCompact)
	if result.Level != LevelCompact {
		t.Fatalf("expected LevelCompact, got %v", result.Level)
	}
	// Compact should be shorter than original.
	if len(result.Content) >= len(content) {
		t.Fatal("expected compact result to be shorter than original")
	}
}

func TestPipeline_FullLevelNoCompression(t *testing.T) {
	pipeline := NewPipeline(DefaultPolicy())

	content := strings.Repeat("this is a long message that should not be compressed at all ", 100)
	msg := CompressedMessage{
		Role:    "assistant",
		Content: content,
	}

	result := pipeline.Compress(msg, LevelFull)
	if result.Content != content {
		t.Fatal("LevelFull should preserve content verbatim")
	}
}

func TestPipeline_ShortContentUnchanged(t *testing.T) {
	pipeline := NewPipeline(DefaultPolicy())

	msg := CompressedMessage{
		Role:     "tool",
		ToolName: "bash",
		Content:  "short output\nonly 2 lines",
	}

	result := pipeline.Compress(msg, LevelTrim)
	if result.Content != msg.Content {
		t.Fatal("short content should not be trimmed")
	}
}

func TestPipeline_ViewToolTrim(t *testing.T) {
	pipeline := NewPipeline(DefaultPolicy())

	var lines []string
	for i := 0; i < 80; i++ {
		lines = append(lines, "file content line")
	}
	content := strings.Join(lines, "\n")

	msg := CompressedMessage{
		Role:     "tool",
		ToolName: "view",
		Content:  content,
	}

	result := pipeline.Compress(msg, LevelTrim)
	if len(result.Content) >= len(content) {
		t.Fatal("expected view output to be trimmed")
	}
}

func TestPipeline_GrepToolTrim(t *testing.T) {
	pipeline := NewPipeline(DefaultPolicy())

	var lines []string
	for i := 0; i < 50; i++ {
		lines = append(lines, "file.go:10: match found here")
	}
	content := strings.Join(lines, "\n")

	msg := CompressedMessage{
		Role:     "tool",
		ToolName: "grep",
		Content:  content,
	}

	result := pipeline.Compress(msg, LevelTrim)
	if !strings.Contains(result.Content, "truncated") {
		t.Fatal("expected grep output to be truncated")
	}
}
