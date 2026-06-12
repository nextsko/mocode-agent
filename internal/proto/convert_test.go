package proto

import (
	"testing"

	"github.com/package-register/mocode/internal/session/message"
)

func TestToMessageToolResult_AllFields(t *testing.T) {
	p := ToolResult{
		ToolCallID: "call_abc",
		Name:       "bash",
		AgentName:  "coder",
		Content:    "hello world",
		Data:       "aGVsbG8=",
		MIMEType:   "text/plain",
		Metadata:   `{"exit_code":0}`,
		IsError:    false,
	}

	m := ToMessageToolResult(p)

	if m.ToolCallID != "call_abc" {
		t.Errorf("ToolCallID: got %q, want %q", m.ToolCallID, "call_abc")
	}
	if m.Name != "bash" {
		t.Errorf("Name: got %q, want %q", m.Name, "bash")
	}
	if m.AgentName != "coder" {
		t.Errorf("AgentName: got %q, want %q", m.AgentName, "coder")
	}
	if m.Content != "hello world" {
		t.Errorf("Content: got %q, want %q", m.Content, "hello world")
	}
	if m.Data != "aGVsbG8=" {
		t.Errorf("Data: got %q, want %q", m.Data, "aGVsbG8=")
	}
	if m.MIMEType != "text/plain" {
		t.Errorf("MIMEType: got %q, want %q", m.MIMEType, "text/plain")
	}
	if m.Metadata != `{"exit_code":0}` {
		t.Errorf("Metadata: got %q, want %q", m.Metadata, `{"exit_code":0}`)
	}
	if m.IsError {
		t.Errorf("IsError: got true, want false")
	}
}

func TestFromMessageToolResult_AllFields(t *testing.T) {
	m := message.ToolResult{
		ToolCallID: "call_xyz",
		Name:       "read",
		AgentName:  "explorer",
		Content:    "file content",
		Data:       "ZmlsZSBiYXNlNjQ=",
		MIMEType:   "image/png",
		Metadata:   `{"size":1024}`,
		IsError:    true,
	}

	p := FromMessageToolResult(m)

	if p.ToolCallID != "call_xyz" {
		t.Errorf("ToolCallID: got %q, want %q", p.ToolCallID, "call_xyz")
	}
	if p.Name != "read" {
		t.Errorf("Name: got %q, want %q", p.Name, "read")
	}
	if p.AgentName != "explorer" {
		t.Errorf("AgentName: got %q, want %q", p.AgentName, "explorer")
	}
	if p.Content != "file content" {
		t.Errorf("Content: got %q, want %q", p.Content, "file content")
	}
	if p.Data != "ZmlsZSBiYXNlNjQ=" {
		t.Errorf("Data: got %q, want %q", p.Data, "ZmlsZSBiYXNlNjQ=")
	}
	if p.MIMEType != "image/png" {
		t.Errorf("MIMEType: got %q, want %q", p.MIMEType, "image/png")
	}
	if p.Metadata != `{"size":1024}` {
		t.Errorf("Metadata: got %q, want %q", p.Metadata, `{"size":1024}`)
	}
	if !p.IsError {
		t.Errorf("IsError: got false, want true")
	}
}

func TestToMessageToolResult_RoundTrip(t *testing.T) {
	original := message.ToolResult{
		ToolCallID: "rt1",
		Name:       "tool",
		AgentName:  "agent1",
		Content:    "x",
		Data:       "eA==",
		MIMEType:   "application/octet-stream",
		Metadata:   `{"k":"v"}`,
		IsError:    false,
	}

	roundTripped := ToMessageToolResult(FromMessageToolResult(original))

	if roundTripped != original {
		t.Errorf("round-trip mismatch:\nwant %+v\ngot  %+v", original, roundTripped)
	}
}

func TestToMessageToolResult_BugFixRegression(t *testing.T) {
	// Before the bug fix, callers manually copied only 4 fields:
	// ToolCallID, Name, Content, IsError.
	// This test ensures ALL fields (including AgentName, Data, MIMEType,
	// Metadata) are preserved by the new conversion function.

	p := ToolResult{
		ToolCallID: "bug_test",
		Name:       "n",
		AgentName:  "a",
		Content:    "c",
		Data:       "d",
		MIMEType:   "mt",
		Metadata:   "md",
		IsError:    false,
	}
	m := ToMessageToolResult(p)

	// These were the 4 fields previously preserved:
	if m.ToolCallID != "bug_test" || m.Name != "n" || m.Content != "c" || m.IsError {
		t.Error("core 4 fields lost in conversion")
	}

	// These were the 4 fields that would have been lost before the fix:
	if m.AgentName != "a" {
		t.Error("AgentName lost — bug fix regression")
	}
	if m.Data != "d" {
		t.Error("Data lost — bug fix regression")
	}
	if m.MIMEType != "mt" {
		t.Error("MIMEType lost — bug fix regression")
	}
	if m.Metadata != "md" {
		t.Error("Metadata lost — bug fix regression")
	}
}
