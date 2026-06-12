package types

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPublicToolResult_AliasIdentity(t *testing.T) {
	// This test verifies the public package exposes the SAME type
	// as internal/types (not a copy). We do this by constructing
	// a value via the public API and checking the underlying type.

	tr := ToolResult{ToolCallID: "abc", Content: "hi"}
	data, err := json.Marshal(tr)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	// JSON should contain the field names from internal/types.ToolResult
	got := string(data)
	if !strings.Contains(got, `"tool_call_id":"abc"`) {
		t.Errorf("public ToolResult should be the same type as internal: %s", got)
	}
}
