// Package notify 定义了 agent 事件的领域通知类型。
//
// 本文件包含 notify 包的单元测试。
package notify

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

// testSessionID is shared across table-driven test cases that all exercise
// the same baseline session. Lifting it into a constant keeps goconst quiet
// and makes the assertions easier to read.
const testSessionID = "session-123"

// Test_Type_Constants 测试 Type 常量定义。
func Test_Type_Constants(t *testing.T) {
	tests := []struct {
		name     string
		got      Type
		expected string
	}{
		{"TypeAgentThinking", TypeAgentThinking, "agent_thinking"},
		{"TypeAgentToolExecuting", TypeAgentToolExecuting, "agent_tool_executing"},
		{"TypeAgentFinished", TypeAgentFinished, "agent_finished"},
		{"TypeReAuthenticate", TypeReAuthenticate, "re_authenticate"},
		{"TypeSubagentCompleted", TypeSubagentCompleted, "subagent_completed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, Type(tt.expected), tt.got)
		})
	}
}

// Test_Notification_Creation 测试 Notification 结构体创建。
func Test_Notification_Creation(t *testing.T) {
	tests := []struct {
		name         string
		notification Notification
		wantSession  string
		wantType     Type
		wantTool     string
	}{
		{
			name: "思考状态通知",
			notification: Notification{
				SessionID: testSessionID,
				Type:      TypeAgentThinking,
			},
			wantSession: testSessionID,
			wantType:    TypeAgentThinking,
			wantTool:    "",
		},
		{
			name: "工具执行通知",
			notification: Notification{
				SessionID: "session-456",
				Type:      TypeAgentToolExecuting,
				ToolName:  "bash",
			},
			wantSession: "session-456",
			wantType:    TypeAgentToolExecuting,
			wantTool:    "bash",
		},
		{
			name: "完成通知",
			notification: Notification{
				SessionID:    "session-789",
				SessionTitle: "Test Session",
				Type:         TypeAgentFinished,
			},
			wantSession: "session-789",
			wantType:    TypeAgentFinished,
			wantTool:    "",
		},
		{
			name: "重新认证通知",
			notification: Notification{
				SessionID:  "session-abc",
				Type:       TypeReAuthenticate,
				ProviderID: "openai",
			},
			wantSession: "session-abc",
			wantType:    TypeReAuthenticate,
			wantTool:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.wantSession, tt.notification.SessionID)
			assert.Equal(t, tt.wantType, tt.notification.Type)
			assert.Equal(t, tt.wantTool, tt.notification.ToolName)
		})
	}
}

// Test_Notification_Fields 测试 Notification 字段的可选性。
func Test_Notification_Fields(t *testing.T) {
	// 最小化通知
	minimal := Notification{
		Type: TypeAgentThinking,
	}
	assert.Empty(t, minimal.SessionID)
	assert.Empty(t, minimal.SessionTitle)
	assert.Empty(t, minimal.ProviderID)
	assert.Empty(t, minimal.ToolName)

	// 完整通知
	full := Notification{
		SessionID:    "session-123",
		SessionTitle: "My Session",
		Type:         TypeAgentToolExecuting,
		ProviderID:   "anthropic",
		ToolName:     "grep",
	}
	assert.NotEmpty(t, full.SessionID)
	assert.NotEmpty(t, full.SessionTitle)
	assert.NotEmpty(t, full.ProviderID)
	assert.NotEmpty(t, full.ToolName)
}

// Test_SubagentStatus_Constants 验证子代理状态枚举值稳定。
func Test_SubagentStatus_Constants(t *testing.T) {
	cases := map[SubagentStatus]string{
		SubagentStatusSuccess:   "success",
		SubagentStatusError:     "error",
		SubagentStatusCancelled: "cancelled",
		SubagentStatusBlocked:   "blocked",
	}
	for got, want := range cases {
		assert.Equal(t, want, string(got), "SubagentStatus enum should be stable")
	}
}

// Test_SubagentCompletedEvent_RoundTrip 验证事件可被 JSON 往返保留。
func Test_SubagentCompletedEvent_RoundTrip(t *testing.T) {
	original := SubagentCompletedEvent{
		ParentSessionID:  "parent-1",
		ParentToolCallID: "tool-1",
		AgentID:          "agent-7",
		SubagentType:     "plan",
		Status:           SubagentStatusSuccess,
		DurationMs:       2500,
		Usage: SubagentTokenUsage{
			Input:         10,
			Output:        20,
			CacheRead:     5,
			CacheCreation: 1,
			Total:         36,
		},
		Summary: "drafted plan",
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded SubagentCompletedEvent
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	assert.Equal(t, original, decoded)
}

// Test_Notification_SubagentCompleted_CarriesPayload 验证 SubagentCompleted
// 通知携带完整 payload。
func Test_Notification_SubagentCompleted_CarriesPayload(t *testing.T) {
	ev := SubagentCompletedEvent{
		ParentToolCallID: "tool-1",
		Status:           SubagentStatusError,
		DurationMs:       100,
		Usage:            SubagentTokenUsage{Input: 1, Output: 0, Total: 1},
		Error:            "context deadline exceeded",
	}
	n := Notification{
		SessionID:         "parent-session",
		Type:              TypeSubagentCompleted,
		SubagentCompleted: &ev,
	}
	assert.Equal(t, TypeSubagentCompleted, n.Type)
	if assert.NotNil(t, n.SubagentCompleted) {
		assert.Equal(t, "tool-1", n.SubagentCompleted.ParentToolCallID)
		assert.Equal(t, SubagentStatusError, n.SubagentCompleted.Status)
		assert.Equal(t, "context deadline exceeded", n.SubagentCompleted.Error)
	}
}
