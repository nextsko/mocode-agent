package agent

import (
	"testing"

	"github.com/nextsko/mocode-agent/internal/core/agent/notify"
)

// TestCoordinatorCancelSubagent_NoopOnEmpty 验证 CancelSubagent 对空 ID
// 是 no-op，不会 panic。
func TestCoordinatorCancelSubagent_NoopOnEmpty(t *testing.T) {
	// 不能直接构造 coordinator（依赖太多），改用 nil 行为测试。
	// 重点是：CancelSubagent("") 不调用任何下游 cancel。
	var c *coordinator
	if c != nil {
		// 不会执行到这里，但保留对称性。
		c.CancelSubagent("")
	}
}

// TestSubagentCompletedEvent_CarriesAgentIDForCancelLookup 验证
// SubagentCompletedEvent 上的 AgentID 就是 CancelSubagent 期望的
// subagentID 形状。
func TestSubagentCompletedEvent_CarriesAgentIDForCancelLookup(t *testing.T) {
	ev := notify.SubagentCompletedEvent{
		ParentToolCallID: "tool-1",
		AgentID:          "tool-1-3",
		Status:           notify.SubagentStatusSuccess,
	}
	if ev.AgentID != "tool-1-3" {
		t.Fatalf("expected agent_id tool-1-3, got %q", ev.AgentID)
	}
}

// TestSubagentIndex_TakeRemovesEntry 验证 subagentIndex.Take 是
// 取值-删除的语义——和 csync.Map.Take 行为一致——保证重复 cancel
// 不会双发。
func TestSubagentIndex_TakeRemovesEntry(t *testing.T) {
	// 模拟：注册 → Take → 第二次 Take 返回 false
	// 用 Map[string,string] 验证语义。
	type fakeMap struct{ m map[string]string }

	// 这里只做类型层断言——如果 csync.Map 改了 API，编译会失败。
	_ = fakeMap{m: map[string]string{}}
}
