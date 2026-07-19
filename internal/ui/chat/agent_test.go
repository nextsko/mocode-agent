package chat

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nextsko/mocode-agent/internal/core/agent"
	"github.com/nextsko/mocode-agent/internal/domain/session/message"
)

type fakeToolMessageItem struct {
	id     string
	render string
}

func (f *fakeToolMessageItem) ID() string                        { return f.id }
func (f *fakeToolMessageItem) Render(width int) string           { return f.render }
func (f *fakeToolMessageItem) RawRender(width int) string        { return f.render }
func (f *fakeToolMessageItem) ToolCall() message.ToolCall        { return message.ToolCall{ID: f.id} }
func (f *fakeToolMessageItem) SetToolCall(tc message.ToolCall)   { f.id = tc.ID }
func (f *fakeToolMessageItem) Result() *message.ToolResult       { return nil }
func (f *fakeToolMessageItem) SetResult(res *message.ToolResult) {}
func (f *fakeToolMessageItem) MessageID() string                 { return "" }
func (f *fakeToolMessageItem) SetMessageID(id string)            {}
func (f *fakeToolMessageItem) SetStatus(status ToolStatus)       {}
func (f *fakeToolMessageItem) Status() ToolStatus                { return ToolStatusRunning }

func TestBuildAgentTaskPanelsGroupsNestedToolsByTaskID(t *testing.T) {
	t.Parallel()

	params := agent.AgentParams{Tasks: []agent.AgentTaskParams{{ID: "task-a"}, {ID: "task-b"}}}
	nestedTools := []ToolMessageItem{
		&fakeToolMessageItem{id: "tool-1", render: "alpha"},
		&fakeToolMessageItem{id: "tool-2", render: "beta"},
		&fakeToolMessageItem{id: "tool-3", render: "gamma"},
	}

	panels := BuildAgentTaskPanels("parent", params, nestedTools, func(toolCallID string) string {
		switch toolCallID {
		case "tool-1", "tool-2":
			return "task-a"
		case "tool-3":
			return "task-b"
		default:
			return ""
		}
	}, nil)

	require.Len(t, panels, 2)
	require.Equal(t, "task-a", panels[0].ID)
	require.Equal(t, "alpha\n\nbeta", panels[0].Content)
	require.Equal(t, "task-b", panels[1].ID)
	require.Equal(t, "gamma", panels[1].Content)
}

func TestBuildAgentTaskPanelsFallsBackToTaskSummary(t *testing.T) {
	t.Parallel()

	params := agent.AgentParams{Tasks: []agent.AgentTaskParams{{ID: "task-a"}}}

	panels := BuildAgentTaskPanels("parent", params, nil, func(toolCallID string) string {
		return ""
	}, func(taskID string) string {
		if taskID == "task-a" {
			return "text-only child summary"
		}
		return ""
	})

	require.Len(t, panels, 1)
	require.Equal(t, "text-only child summary", panels[0].Content)
}
