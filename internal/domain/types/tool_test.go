package types

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestToolResult_JSON_RoundTrip(t *testing.T) {
	original := ToolResult{
		ToolCallID: "call_abc123",
		Name:       "bash",
		AgentName:  "coder",
		Type:       "text",
		Content:    "hello world",
		Data:       "aGVsbG8=",
		MIMEType:   "text/plain",
		Metadata:   `{"exit_code":0}`,
		IsError:    false,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var decoded ToolResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if decoded != original {
		t.Errorf("round-trip mismatch:\nwant %+v\ngot  %+v", original, decoded)
	}
}

func TestToolResult_ZeroValue_OmitsEmptyFields(t *testing.T) {
	tr := ToolResult{ToolCallID: "x", Content: "hi"}

	data, err := json.Marshal(tr)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	got := string(data)
	// Required fields present
	if !strings.Contains(got, `"tool_call_id":"x"`) {
		t.Errorf("missing tool_call_id: %s", got)
	}
	if !strings.Contains(got, `"content":"hi"`) {
		t.Errorf("missing content: %s", got)
	}
	// Optional fields omitted
	for _, field := range []string{`"name"`, `"agent_name"`, `"type"`, `"data"`, `"mime_type"`, `"metadata"`, `"is_error"`} {
		if strings.Contains(got, field) {
			t.Errorf("expected %s to be omitted, got: %s", field, got)
		}
	}
}

func TestToolResult_IsError_VisibleInJSON(t *testing.T) {
	tr := ToolResult{ToolCallID: "x", Content: "boom", IsError: true}
	data, _ := json.Marshal(tr)
	if !strings.Contains(string(data), `"is_error":true`) {
		t.Errorf("expected is_error:true in JSON, got: %s", data)
	}
}

func TestToolResult_TypeAlias_Compatibility(t *testing.T) {
	// Sanity check: ToolResult identity is preserved through any.
	var a ToolResult = ToolResult{ToolCallID: "x", Content: "y"}
	var b any = a
	if _, ok := b.(ToolResult); !ok {
		t.Fatal("ToolResult identity broken")
	}
}
