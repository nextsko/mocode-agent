package agent

import (
	"encoding/json"
	"testing"

	"charm.land/fantasy"

	"github.com/package-register/mocode/internal/agent/notify"
)

// TestTaskResult_OmitsEmptyFields 验证空字段不进入 JSON 输出，避免前端
// 看到一堆零值。
func TestTaskResult_OmitsEmptyFields(t *testing.T) {
	tr := TaskResult{Status: TaskResultStatusSuccess, Summary: "ok"}

	data, err := json.Marshal(tr)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := raw["duration_ms"]; ok {
		t.Errorf("did not expect duration_ms in zero envelope, got %#v", raw)
	}
	if _, ok := raw["usage"]; ok {
		t.Errorf("did not expect usage in zero envelope, got %#v", raw)
	}
	if _, ok := raw["error"]; ok {
		t.Errorf("did not expect error in success envelope, got %#v", raw)
	}
}

// TestTaskResult_RoundTripsMetadata 验证 duration_ms / usage 字段往返不丢。
func TestTaskResult_RoundTripsMetadata(t *testing.T) {
	tr := TaskResult{
		Status:     TaskResultStatusSuccess,
		Summary:    "ok",
		DurationMs: 1234,
		Usage: &TaskUsage{
			Input:         10,
			Output:        5,
			CacheRead:     1,
			CacheCreation: 2,
			Total:         18,
		},
	}
	data, err := json.Marshal(tr)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded TaskResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.DurationMs != 1234 {
		t.Errorf("expected duration_ms 1234, got %d", decoded.DurationMs)
	}
	if decoded.Usage == nil {
		t.Fatal("expected usage to round-trip")
	}
	if decoded.Usage.Total != 18 {
		t.Errorf("expected total 18, got %d", decoded.Usage.Total)
	}
}

// TestTaskResult_BlockedStatus 验证 blocked 状态作为合法值能序列化。
func TestTaskResult_BlockedStatus(t *testing.T) {
	tr := TaskResult{Status: TaskResultStatusBlocked, Error: "upstream dependency failed"}
	data, err := json.Marshal(tr)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded TaskResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Status != TaskResultStatusBlocked {
		t.Errorf("expected status %q, got %q", TaskResultStatusBlocked, decoded.Status)
	}
	if decoded.Error != "upstream dependency failed" {
		t.Errorf("expected error to round-trip, got %q", decoded.Error)
	}
}

// TestTaskResultStatusConstants 验证状态枚举值稳定，前端在消费 TaskResult
// JSON 时依赖这些字符串。
func TestTaskResultStatusConstants(t *testing.T) {
	cases := map[string]string{
		TaskResultStatusSuccess:   "success",
		TaskResultStatusError:     "error",
		TaskResultStatusBlocked:   "blocked",
		TaskResultStatusCancelled: "cancelled",
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("TaskResultStatus enum drift: got %q want %q", got, want)
		}
	}
}

// TestTaskUsage_ToNotifyUsage 验证双向转换无损。
func TestTaskUsage_ToNotifyUsage(t *testing.T) {
	u := &TaskUsage{Input: 10, Output: 5, CacheRead: 1, CacheCreation: 2, Total: 18}
	got := u.ToNotifyUsage()
	want := notify.SubagentTokenUsage{Input: 10, Output: 5, CacheRead: 1, CacheCreation: 2, Total: 18}
	if got != want {
		t.Errorf("ToNotifyUsage mismatch: got %#v want %#v", got, want)
	}
}

// TestTaskUsage_ToNotifyUsage_NilSafe 验证 nil 接收器不 panic。
func TestTaskUsage_ToNotifyUsage_NilSafe(t *testing.T) {
	var u *TaskUsage
	if got := u.ToNotifyUsage(); got != (notify.SubagentTokenUsage{}) {
		t.Errorf("expected zero value on nil receiver, got %#v", got)
	}
}

// TestFromFantasyUsage 验证 fantasy.Usage -> TaskUsage 的转换。
func TestFromFantasyUsage(t *testing.T) {
	got := FromFantasyUsage(fantasy.Usage{
		InputTokens:         10,
		OutputTokens:        5,
		CacheReadTokens:     1,
		CacheCreationTokens: 2,
		TotalTokens:         18,
	})
	if got == nil {
		t.Fatal("expected non-nil conversion for non-zero usage")
	}
	if got.Total != 18 {
		t.Errorf("expected total 18, got %d", got.Total)
	}
}

// TestFromFantasyUsage_EmptyReturnsNil 验证空 usage 不产出 TaskUsage，避免
// 在 envelope 里出现一堆零值。
func TestFromFantasyUsage_EmptyReturnsNil(t *testing.T) {
	if got := FromFantasyUsage(fantasy.Usage{}); got != nil {
		t.Errorf("expected nil for empty usage, got %#v", got)
	}
}
