package model

import (
	"context"
	"image"
	"testing"
	"time"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/catwalk/pkg/catwalk"
	"charm.land/lipgloss/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	agentcore "github.com/nextsko/mocode-agent/internal/core/agent"
	"github.com/nextsko/mocode-agent/internal/core/agent/notify"
	"github.com/nextsko/mocode-agent/internal/core/config"
	"github.com/nextsko/mocode-agent/internal/domain/session"
	"github.com/nextsko/mocode-agent/internal/domain/session/message"
	"github.com/nextsko/mocode-agent/internal/transport/workspace"
	"github.com/nextsko/mocode-agent/internal/ui/attachments"
	"github.com/nextsko/mocode-agent/internal/ui/chat"
	"github.com/nextsko/mocode-agent/internal/ui/common"
	"github.com/nextsko/mocode-agent/internal/ui/completions"
	"github.com/nextsko/mocode-agent/internal/ui/dialog"
	"github.com/nextsko/mocode-agent/internal/ui/styles"
	"github.com/nextsko/mocode-agent/internal/ui/util"
	"github.com/nextsko/mocode-agent/internal/util/csync"
	"github.com/nextsko/mocode-agent/internal/util/pubsub"
)

func TestCurrentModelSupportsImages(t *testing.T) {
	t.Parallel()

	t.Run("returns false when config is nil", func(t *testing.T) {
		t.Parallel()

		ui := newTestUIWithConfig(t, nil)
		require.False(t, ui.currentModelSupportsImages())
	})

	t.Run("returns false when coder agent is missing", func(t *testing.T) {
		t.Parallel()

		cfg := &config.Config{
			Providers: csync.NewMap[string, config.ProviderConfig](),
			Agents:    map[string]config.Agent{},
		}
		ui := newTestUIWithConfig(t, cfg)
		require.False(t, ui.currentModelSupportsImages())
	})

	t.Run("returns false when model is not found", func(t *testing.T) {
		t.Parallel()

		cfg := &config.Config{
			Providers: csync.NewMap[string, config.ProviderConfig](),
			Agents: map[string]config.Agent{
				config.AgentCoder: {Model: config.SelectedModelTypeLarge},
			},
		}
		ui := newTestUIWithConfig(t, cfg)
		require.False(t, ui.currentModelSupportsImages())
	})

	t.Run("returns true when current model supports images", func(t *testing.T) {
		t.Parallel()

		providers := csync.NewMap[string, config.ProviderConfig]()
		providers.Set("test-provider", config.ProviderConfig{
			ID: "test-provider",
			Models: []catwalk.Model{
				{ID: "test-model", SupportsImages: true},
			},
		})

		cfg := &config.Config{
			Models: map[config.SelectedModelType]config.SelectedModel{
				config.SelectedModelTypeLarge: {
					Provider: "test-provider",
					Model:    "test-model",
				},
			},
			Providers: providers,
			Agents: map[string]config.Agent{
				config.AgentCoder: {Model: config.SelectedModelTypeLarge},
			},
		}

		ui := newTestUIWithConfig(t, cfg)
		require.True(t, ui.currentModelSupportsImages())
	})
}

func TestSplitSlashCommand(t *testing.T) {
	t.Parallel()

	t.Run("splits command and args", func(t *testing.T) {
		t.Parallel()
		cmd, args := splitSlashCommand("/plan start")
		require.Equal(t, "/plan", cmd)
		require.Equal(t, "start", args)
	})

	t.Run("handles command without args", func(t *testing.T) {
		t.Parallel()
		cmd, args := splitSlashCommand("/code")
		require.Equal(t, "/code", cmd)
		require.Equal(t, "", args)
	})

	t.Run("handles multi-word args", func(t *testing.T) {
		t.Parallel()
		cmd, args := splitSlashCommand("/wechat switch myid")
		require.Equal(t, "/wechat", cmd)
		require.Equal(t, "switch myid", args)
	})
}

func TestHandleDialogActionSelectMode(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Providers: csync.NewMap[string, config.ProviderConfig](),
		Options:   &config.Options{},
	}
	ui := newTestUIWithConfig(t, cfg)
	ui.dialog = dialog.NewOverlay()
	ws := ui.com.Workspace.(*testWorkspace)
	ws.currentAgentID = config.AgentCoder

	cmd := ui.handleDialogAction(dialog.ActionSelectMode{ModeID: config.AgentPlan})
	require.NotNil(t, cmd)

	msg := cmd()
	require.NotNil(t, msg)
	require.Equal(t, config.AgentPlan, cfg.Options.ActiveMode)
	require.Equal(t, config.AgentPlan, ws.currentAgentID)
	require.Equal(t, 1, ws.switchAgentCalls)
}

func TestHandleSlashCommandSelectMode(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Providers: csync.NewMap[string, config.ProviderConfig](),
		Options:   &config.Options{},
	}
	ui := newTestUIWithConfig(t, cfg)
	ui.dialog = dialog.NewOverlay()
	ws := ui.com.Workspace.(*testWorkspace)
	ws.currentAgentID = config.AgentCoder

	cmd, handled := ui.handleSlashCommand("/plan")
	require.True(t, handled)
	require.NotNil(t, cmd)

	msg := cmd()
	require.NotNil(t, msg)
	require.Equal(t, config.AgentPlan, cfg.Options.ActiveMode)
	require.Equal(t, config.AgentPlan, ws.currentAgentID)
	require.Equal(t, 1, ws.switchAgentCalls)
}

func TestHandleSlashCommandInitKnowledge(t *testing.T) {
	t.Parallel()

	ui := newTestUIWithConfig(t, &config.Config{
		Providers: csync.NewMap[string, config.ProviderConfig](),
		Options:   &config.Options{},
	})
	ui.dialog = dialog.NewOverlay()

	cmd, handled := ui.handleSlashCommand("/init_kng")
	require.True(t, handled)
	require.NotNil(t, cmd)

	msg := cmd()
	require.NotNil(t, msg)
	require.Equal(t, 1, ui.com.Workspace.(*testWorkspace).initKnowledgeCalls)
}

func TestHandleKeyPressCtrlPOpensSlashCompletion(t *testing.T) {
	t.Parallel()

	ui := newTestUIWithConfig(t, &config.Config{
		Providers: csync.NewMap[string, config.ProviderConfig](),
		Options:   &config.Options{},
	})
	ui.dialog = dialog.NewOverlay()
	ui.keyMap = DefaultKeyMap()
	ui.focus = uiFocusEditor
	ui.state = uiLanding

	cmd := ui.handleKeyPressMsg(tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
	require.NotNil(t, cmd)
	require.Equal(t, "/", ui.textarea.Value())
	require.True(t, ui.completionsOpen)
	require.True(t, ui.completionsSlashMode)
	require.False(t, ui.dialog.ContainsDialog(dialog.CommandsID))
}

func TestHandleSlashCommandWeChatOpensManagerDialog(t *testing.T) {
	t.Parallel()

	ui := newTestUIWithConfig(t, &config.Config{
		Providers: csync.NewMap[string, config.ProviderConfig](),
		Options:   &config.Options{},
	})
	ui.dialog = dialog.NewOverlay()

	cmd, handled := ui.handleSlashCommand("/wechat")
	require.True(t, handled)
	require.True(t, ui.dialog.ContainsDialog(dialog.WeChatManagerID))
	require.NotNil(t, cmd)
}

func TestHandleDialogActionOpenWeChatManagerDialog(t *testing.T) {
	t.Parallel()

	ui := newTestUIWithConfig(t, &config.Config{
		Providers: csync.NewMap[string, config.ProviderConfig](),
		Options:   &config.Options{},
	})
	ui.dialog = dialog.NewOverlay()

	cmd := ui.handleDialogAction(dialog.ActionOpenDialog{DialogID: dialog.WeChatManagerID})
	require.True(t, ui.dialog.ContainsDialog(dialog.WeChatManagerID))
	require.NotNil(t, cmd)
}

func TestHandleAgentNotificationTracksSessionScopedRuntime(t *testing.T) {
	t.Parallel()

	ui := newTestUIWithConfig(t, &config.Config{
		Providers: csync.NewMap[string, config.ProviderConfig](),
		Options:   &config.Options{},
	})
	ws := ui.com.Workspace.(*testWorkspace)
	ws.currentAgentID = config.AgentCoder
	ws.availableAgents = []workspace.AgentInfo{{ID: config.AgentCoder, Name: "Coder"}}
	ui.session = &session.Session{ID: "session-a", Title: "A"}
	ui.agentRuntimes = make(map[string]*sessionAgentRuntimeState)

	require.Nil(t, ui.handleAgentNotification(notify.Notification{
		SessionID: "session-a",
		Type:      notify.TypeAgentThinking,
	}))
	require.Equal(t, "thinking...", ui.agentStatus)

	require.Nil(t, ui.handleAgentNotification(notify.Notification{
		SessionID: "session-b",
		Type:      notify.TypeAgentToolExecuting,
		ToolName:  "bash",
	}))
	require.Equal(t, "thinking...", ui.agentStatus)

	aEntries := ui.agentRuntimes["session-a"].entries
	require.Len(t, aEntries, 1)
	require.Equal(t, agentRuntimeThinking, aEntries[config.AgentCoder].Status)

	bEntries := ui.agentRuntimes["session-b"].entries
	require.Len(t, bEntries, 1)
	require.Equal(t, agentRuntimeExecuting, bEntries[config.AgentCoder].Status)
	require.Equal(t, "bash", bEntries[config.AgentCoder].ToolName)

	_ = ui.handleAgentNotification(notify.Notification{
		SessionID:    "session-a",
		SessionTitle: "A",
		Type:         notify.TypeAgentFinished,
	})
	require.Empty(t, ui.agentStatus)
	require.Equal(t, agentRuntimeStopped, ui.agentRuntimes["session-a"].entries[config.AgentCoder].Status)
}

func TestUpdateAgentRuntimeKeepsFirstSeenOrderStable(t *testing.T) {
	t.Parallel()

	ui := newTestUIWithConfig(t, &config.Config{
		Providers: csync.NewMap[string, config.ProviderConfig](),
		Options:   &config.Options{},
	})

	now := time.Now()
	ui.updateAgentRuntime("session-a", "alpha", "Alpha", agentRuntimeThinking, "", now)
	ui.updateAgentRuntime("session-a", "beta", "Beta", agentRuntimeExecuting, "bash", now.Add(time.Second))
	ui.updateAgentRuntime("session-a", "alpha", "Alpha", agentRuntimeStopped, "", now.Add(2*time.Second))

	state := ui.agentRuntimes["session-a"]
	require.NotNil(t, state)
	require.Equal(t, []string{"alpha", "beta"}, state.order)
	require.Equal(t, 0, state.entries["alpha"].FirstSeenOrder)
	require.Equal(t, 1, state.entries["beta"].FirstSeenOrder)
	require.Equal(t, agentRuntimeStopped, state.entries["alpha"].Status)
}

func TestHandleChildSessionMessage_AssociatesBatchSubAgentSessionWithParentAgentTool(t *testing.T) {
	t.Parallel()

	ui := newTestUIWithConfig(t, &config.Config{
		Providers: csync.NewMap[string, config.ProviderConfig](),
		Options:   &config.Options{},
	})
	ui.session = &session.Session{ID: "session-parent", Title: "Parent"}

	parentMsg := message.Message{
		ID:        "msg-parent",
		Role:      message.Assistant,
		SessionID: "session-parent",
		Parts: []message.ContentPart{
			message.ToolCall{
				ID:    "agent-call",
				Name:  agentcore.AgentToolName,
				Input: `{"tasks":[{"prompt":"Inspect UI"},{"prompt":"Patch UI"}]}`,
			},
		},
	}

	_ = ui.updateSessionMessage(parentMsg)

	childSessionID := ui.com.Workspace.CreateAgentToolSessionID(parentMsg.ID, "agent-call-1")
	childMsg := message.Message{
		ID:        "child-msg-1",
		Role:      message.Assistant,
		SessionID: childSessionID,
		Parts: []message.ContentPart{
			message.ToolCall{
				ID:    "bash-call-1",
				Name:  "bash",
				Input: `echo hi`,
			},
		},
	}

	_ = ui.handleChildSessionMessage(pubsub.Event[message.Message]{
		Type:    pubsub.UpdatedEvent,
		Payload: childMsg,
	})

	toolItem, ok := ui.chat.MessageItem("agent-call").(chat.NestedToolContainer)
	require.True(t, ok)
	require.Len(t, toolItem.NestedTools(), 1)
	require.Equal(t, "bash-call-1", toolItem.NestedTools()[0].ToolCall().ID)
}

func TestHandleChildSessionMessage_TracksTextOnlySubAgentRuntime(t *testing.T) {
	t.Parallel()

	ui := newTestUIWithConfig(t, &config.Config{
		Providers: csync.NewMap[string, config.ProviderConfig](),
		Options:   &config.Options{},
	})
	ui.session = &session.Session{ID: "session-parent", Title: "Parent"}

	parentMsg := message.Message{
		ID:        "msg-parent",
		Role:      message.Assistant,
		SessionID: "session-parent",
		Parts: []message.ContentPart{
			message.ToolCall{
				ID:    "agent-call",
				Name:  agentcore.AgentToolName,
				Input: `{"prompt":"Analyze the failing test"}`,
			},
		},
	}

	_ = ui.updateSessionMessage(parentMsg)

	childSessionID := ui.com.Workspace.CreateAgentToolSessionID(parentMsg.ID, "agent-call")
	_ = ui.handleChildSessionMessage(pubsub.Event[message.Message]{
		Type: pubsub.UpdatedEvent,
		Payload: message.Message{
			ID:        "child-text-1",
			Role:      message.Assistant,
			SessionID: childSessionID,
			Parts: []message.ContentPart{
				message.TextContent{Text: "Analyzed the failure path."},
			},
		},
	})

	state := ui.agentRuntimes["session-parent"]
	require.NotNil(t, state)
	entry := state.entries[childSessionID]
	require.NotNil(t, entry)
	require.Equal(t, config.AgentTask, entry.DisplayName)
	require.Equal(t, agentRuntimeExecuting, entry.Status)
	require.Equal(t, "Analyzed the failure path.", entry.Summary)
}

func TestSetSessionMessages_LoadsNestedBatchSubAgentHistory(t *testing.T) {
	t.Parallel()

	ui := newTestUIWithConfig(t, &config.Config{
		Providers: csync.NewMap[string, config.ProviderConfig](),
		Options:   &config.Options{},
	})
	ui.session = &session.Session{ID: "session-parent", Title: "Parent"}

	parentMsg := message.Message{
		ID:        "msg-parent",
		Role:      message.Assistant,
		SessionID: "session-parent",
		Parts: []message.ContentPart{
			message.ToolCall{
				ID:    "agent-call",
				Name:  agentcore.AgentToolName,
				Input: `{"tasks":[{"prompt":"Inspect UI"},{"prompt":"Patch UI"}]}`,
			},
		},
	}

	childSessionID := ui.com.Workspace.CreateAgentToolSessionID(parentMsg.ID, "agent-call-1")
	ui.com.Workspace.(*testWorkspace).messagesBySession = map[string][]message.Message{
		childSessionID: {
			{
				ID:        "child-msg-1",
				Role:      message.Assistant,
				SessionID: childSessionID,
				Parts: []message.ContentPart{
					message.ToolCall{
						ID:       "bash-call-1",
						Name:     "bash",
						Input:    `echo hi`,
						Finished: true,
					},
				},
			},
			{
				ID:        "child-result-1",
				Role:      message.Tool,
				SessionID: childSessionID,
				Parts: []message.ContentPart{
					message.ToolResult{
						ToolCallID: "bash-call-1",
						Name:       "bash",
						Content:    "ok",
					},
				},
			},
		},
	}

	_ = ui.setSessionMessages([]message.Message{parentMsg})

	toolItem, ok := ui.chat.MessageItem("agent-call").(chat.NestedToolContainer)
	require.True(t, ok)
	require.Len(t, toolItem.NestedTools(), 1)
	require.Equal(t, "bash-call-1", toolItem.NestedTools()[0].ToolCall().ID)
}

func TestHandleChildSessionMessage_RendersNestedToolTreeForSingleSubAgent(t *testing.T) {
	t.Parallel()

	ui := newTestUIWithConfig(t, &config.Config{
		Providers: csync.NewMap[string, config.ProviderConfig](),
		Options:   &config.Options{},
	})
	ui.session = &session.Session{ID: "session-parent", Title: "Parent"}

	parentMsg := message.Message{
		ID:        "msg-parent",
		Role:      message.Assistant,
		SessionID: "session-parent",
		Parts: []message.ContentPart{
			message.ToolCall{
				ID:    "agent-call",
				Name:  agentcore.AgentToolName,
				Input: `{"prompt":"Analyze the failing test"}`,
			},
		},
	}

	_ = ui.updateSessionMessage(parentMsg)

	childSessionID := ui.com.Workspace.CreateAgentToolSessionID(parentMsg.ID, "agent-call")
	childMsg := message.Message{
		ID:        "child-msg-1",
		Role:      message.Assistant,
		SessionID: childSessionID,
		Parts: []message.ContentPart{
			message.ToolCall{
				ID:    "bash-call-1",
				Name:  "bash",
				Input: `{"command":"echo hi"}`,
			},
		},
	}

	_ = ui.handleChildSessionMessage(pubsub.Event[message.Message]{
		Type:    pubsub.UpdatedEvent,
		Payload: childMsg,
	})

	toolItem, ok := ui.chat.MessageItem("agent-call").(chat.NestedToolContainer)
	require.True(t, ok)
	require.Len(t, toolItem.NestedTools(), 1)

	rendered := ui.chat.MessageItem("agent-call").Render(80)
	require.Contains(t, rendered, "Bash")
	require.Contains(t, rendered, "echo hi")
}

func TestHandleChildSessionMessage_RendersNestedToolTreeForBatchSubAgent(t *testing.T) {
	t.Parallel()

	ui := newTestUIWithConfig(t, &config.Config{
		Providers: csync.NewMap[string, config.ProviderConfig](),
		Options:   &config.Options{},
	})
	ui.session = &session.Session{ID: "session-parent", Title: "Parent"}

	parentMsg := message.Message{
		ID:        "msg-parent",
		Role:      message.Assistant,
		SessionID: "session-parent",
		Parts: []message.ContentPart{
			message.ToolCall{
				ID:    "agent-call",
				Name:  agentcore.AgentToolName,
				Input: `{"tasks":[{"prompt":"Inspect UI"},{"prompt":"Patch UI"}]}`,
			},
		},
	}

	_ = ui.updateSessionMessage(parentMsg)

	childSessionID := ui.com.Workspace.CreateAgentToolSessionID(parentMsg.ID, "agent-call-1")
	childMsg := message.Message{
		ID:        "child-msg-1",
		Role:      message.Assistant,
		SessionID: childSessionID,
		Parts: []message.ContentPart{
			message.ToolCall{
				ID:    "bash-call-1",
				Name:  "bash",
				Input: `{"command":"echo hi"}`,
			},
		},
	}

	_ = ui.handleChildSessionMessage(pubsub.Event[message.Message]{
		Type:    pubsub.UpdatedEvent,
		Payload: childMsg,
	})

	toolItem, ok := ui.chat.MessageItem("agent-call").(chat.NestedToolContainer)
	require.True(t, ok)
	require.Len(t, toolItem.NestedTools(), 1)

	rendered := ui.chat.MessageItem("agent-call").Render(80)
	require.Contains(t, rendered, "Bash")
	require.Contains(t, rendered, "echo hi")
}

func TestUISubAgentTreeViaUpdateLoop(t *testing.T) {
	t.Parallel()

	ui := newTestUIWithConfig(t, &config.Config{
		Providers: csync.NewMap[string, config.ProviderConfig](),
		Options:   &config.Options{},
	})
	ui.session = &session.Session{ID: "session-parent", Title: "Parent"}

	parentMsg := message.Message{
		ID:        "msg-parent",
		Role:      message.Assistant,
		SessionID: "session-parent",
		Parts: []message.ContentPart{
			message.ToolCall{
				ID:    "agent-call",
				Name:  agentcore.AgentToolName,
				Input: `{"prompt":"Analyze the failing test"}`,
			},
		},
	}

	// Simulate the real message event flow through UI.Update.
	_, _ = ui.Update(pubsub.Event[message.Message]{
		Type:    pubsub.CreatedEvent,
		Payload: parentMsg,
	})

	childSessionID := ui.com.Workspace.CreateAgentToolSessionID(parentMsg.ID, "agent-call")

	_, _ = ui.Update(pubsub.Event[message.Message]{
		Type: pubsub.CreatedEvent,
		Payload: message.Message{
			ID:        "child-msg-1",
			Role:      message.Assistant,
			SessionID: childSessionID,
			Parts:     []message.ContentPart{},
		},
	})

	_, _ = ui.Update(pubsub.Event[message.Message]{
		Type: pubsub.UpdatedEvent,
		Payload: message.Message{
			ID:        "child-msg-1",
			Role:      message.Assistant,
			SessionID: childSessionID,
			Parts: []message.ContentPart{
				message.ToolCall{
					ID:    "bash-call-1",
					Name:  "bash",
					Input: `{"command":"echo hi"}`,
				},
			},
		},
	})

	toolItem, ok := ui.chat.MessageItem("agent-call").(chat.NestedToolContainer)
	require.True(t, ok)
	require.Len(t, toolItem.NestedTools(), 1)

	rendered := ui.chat.MessageItem("agent-call").Render(80)
	require.Contains(t, rendered, "Bash")
}

func TestPillClickTargetAt(t *testing.T) {
	t.Parallel()

	ui := newTestUIWithConfig(t, &config.Config{
		Providers: csync.NewMap[string, config.ProviderConfig](),
		Options:   &config.Options{},
	})
	ui.session = &session.Session{
		ID: "session-a",
		Todos: []session.Todo{
			{Content: "done", Status: session.TodoStatusCompleted},
			{Content: "working", Status: session.TodoStatusInProgress},
			{Content: "next", Status: session.TodoStatusPending},
		},
	}
	ui.layout.pills = image.Rect(0, 0, 100, 8)

	target := ui.pillClickTargetAt(4, 1)
	require.Equal(t, pillClickTodoSummary, target.kind)
	require.Equal(t, -1, target.todoIndex)

	ui.promptQueue = 2
	todoWidth := lipgloss.Width(todoPill(ui.session.Todos, ui.com.Styles.Tool.TodoInProgressIcon.Render(styles.SpinnerIcon), false, false, ui.com.Styles))
	queueTarget := ui.pillClickTargetAt(3+todoWidth+2, 1)
	require.Equal(t, pillClickQueue, queueTarget.kind)

	ui.pillsExpanded = true
	ui.focusedPillSection = pillSectionTodos
	rowTarget := ui.pillClickTargetAt(4, 3)
	require.Equal(t, pillClickTodoRow, rowTarget.kind)
	require.Equal(t, 0, rowTarget.todoIndex)

	rowTarget = ui.pillClickTargetAt(4, 4)
	require.Equal(t, pillClickTodoRow, rowTarget.kind)
	require.Equal(t, 1, rowTarget.todoIndex)
}

func TestHandlePillsMouseClickRequiresSameTargetForDoubleClick(t *testing.T) {
	t.Parallel()

	ui := newTestUIWithConfig(t, &config.Config{
		Providers: csync.NewMap[string, config.ProviderConfig](),
		Options:   &config.Options{},
	})
	ui.dialog = dialog.NewOverlay()
	ui.session = &session.Session{
		ID:    "session-a",
		Title: "A",
		Todos: []session.Todo{{Content: "next", Status: session.TodoStatusPending}},
	}
	ui.layout.pills = image.Rect(0, 0, 100, 6)
	ui.promptQueue = 1

	require.Nil(t, ui.handlePillsMouseClick(tea.MouseClickMsg{X: 4, Y: 1, Button: tea.MouseLeft}))

	cmd := ui.handlePillsMouseClick(tea.MouseClickMsg{X: 20, Y: 1, Button: tea.MouseLeft})
	require.Nil(t, cmd)
	require.False(t, ui.dialog.ContainsDialog(dialog.TodoID))

	ui.lastPillClick = time.Now()
	ui.lastPillTarget = pillClickTarget{kind: pillClickTodoSummary, todoIndex: -1}
	cmd = ui.handlePillsMouseClick(tea.MouseClickMsg{X: 4, Y: 1, Button: tea.MouseLeft})
	require.Nil(t, cmd)
	require.True(t, ui.dialog.ContainsDialog(dialog.TodoID))
}

func TestSaveSessionTodosPersistsAndRefreshesState(t *testing.T) {
	t.Parallel()

	ui := newTestUIWithConfig(t, &config.Config{
		Providers: csync.NewMap[string, config.ProviderConfig](),
		Options:   &config.Options{},
	})
	ws := ui.com.Workspace.(*testWorkspace)
	ui.session = &session.Session{
		ID:    "session-a",
		Title: "A",
		Todos: []session.Todo{{Content: "old", Status: session.TodoStatusPending}},
	}
	ui.layout.pills = image.Rect(0, 0, 80, 6)

	todos := []session.Todo{{Content: "new", Status: session.TodoStatusInProgress, ActiveForm: "doing new"}}
	cmd := ui.saveSessionTodos(todos)
	require.NotNil(t, cmd)
	require.Equal(t, todos, ui.session.Todos)
	require.Equal(t, todoFingerprint(todos), ui.todoContinuations["session-a"].LastFingerprint)

	msg := cmd()
	info, ok := msg.(util.InfoMsg)
	require.True(t, ok)
	require.Equal(t, "Todos updated", info.Msg)
	require.Equal(t, todos, ws.savedSession.Todos)
	require.Equal(t, "session-a", ws.savedSession.ID)
}

func TestMaybeAutoContinueTodosRunsWhenIncomplete(t *testing.T) {
	t.Parallel()

	ui := newTestUIWithConfig(t, &config.Config{
		Providers: csync.NewMap[string, config.ProviderConfig](),
		Options:   &config.Options{},
	})
	ws := ui.com.Workspace.(*testWorkspace)
	ws.agentReady = true
	ws.sessionByID = map[string]session.Session{
		"session-a": {
			ID:    "session-a",
			Title: "A",
			Todos: []session.Todo{{Content: "next", Status: session.TodoStatusPending}},
		},
	}

	cmd := ui.maybeAutoContinueTodos("session-a")
	require.NotNil(t, cmd)
	msg := cmd()
	info, ok := msg.(util.InfoMsg)
	require.True(t, ok)
	require.Equal(t, "Auto-continuing remaining Todo items", info.Msg)
	require.Equal(t, "session-a", ws.lastRunSessionID)
	require.Contains(t, ws.lastRunPrompt, "Continue executing the remaining Todo items")
	require.True(t, ui.todoContinuations["session-a"].InFlight)
}

func TestMaybeAutoContinueTodosStopsWhenStalled(t *testing.T) {
	t.Parallel()

	ui := newTestUIWithConfig(t, &config.Config{
		Providers: csync.NewMap[string, config.ProviderConfig](),
		Options:   &config.Options{},
	})
	ws := ui.com.Workspace.(*testWorkspace)
	ws.agentReady = true
	ws.sessionByID = map[string]session.Session{
		"session-a": {
			ID:    "session-a",
			Todos: []session.Todo{{Content: "next", Status: session.TodoStatusPending}},
		},
	}
	state := ui.ensureTodoContinuationState("session-a")
	state.ConsecutiveNoChanges = 3
	ui.session = &session.Session{ID: "session-a", Todos: ws.sessionByID["session-a"].Todos}
	ui.layout.pills = image.Rect(0, 0, 80, 8)

	cmd := ui.maybeAutoContinueTodos("session-a")
	require.NotNil(t, cmd)
	msg := cmd()
	info, ok := msg.(util.InfoMsg)
	require.True(t, ok)
	require.Equal(t, util.InfoTypeWarn, info.Type)
	require.Equal(t, "Todo stalled, manual intervention required", info.Msg)
	require.True(t, state.Stalled)
	require.Empty(t, ws.lastRunSessionID)
}

func TestMaybeAutoContinueTodosRespectsBlocks(t *testing.T) {
	t.Parallel()

	ui := newTestUIWithConfig(t, &config.Config{
		Providers: csync.NewMap[string, config.ProviderConfig](),
		Options:   &config.Options{},
	})
	ws := ui.com.Workspace.(*testWorkspace)
	ws.agentReady = true
	ws.sessionByID = map[string]session.Session{
		"session-a": {
			ID:    "session-a",
			Todos: []session.Todo{{Content: "next", Status: session.TodoStatusPending}},
		},
	}

	state := ui.ensureTodoContinuationState("session-a")
	state.PendingPermission = true
	require.Nil(t, ui.maybeAutoContinueTodos("session-a"))
	state.PendingPermission = false

	state.ReauthBlocked = true
	require.Nil(t, ui.maybeAutoContinueTodos("session-a"))
	state.ReauthBlocked = false

	ws.busySessions["session-a"] = true
	require.Nil(t, ui.maybeAutoContinueTodos("session-a"))
	ws.busySessions["session-a"] = false

	ws.queuedPrompts["session-a"] = 1
	require.Nil(t, ui.maybeAutoContinueTodos("session-a"))
	ws.queuedPrompts["session-a"] = 0

	ui.isCanceling = true
	require.Nil(t, ui.maybeAutoContinueTodos("session-a"))
}

func newTestUIWithConfig(t *testing.T, cfg *config.Config) *UI {
	t.Helper()

	ws := &testWorkspace{
		cfg:           cfg,
		busySessions:  make(map[string]bool),
		queuedPrompts: make(map[string]int),
		sessionByID:   make(map[string]session.Session),
	}
	com := common.DefaultCommon(ws)
	ta := textarea.New()
	ta.SetStyles(com.Styles.Editor.Textarea)
	ta.ShowLineNumbers = false
	ta.CharLimit = -1
	ta.SetVirtualCursor(false)
	ta.DynamicHeight = true
	ta.MinHeight = TextareaMinHeight
	ta.MaxHeight = TextareaMaxHeight
	ta.Focus()

	ui := &UI{
		com:         com,
		textarea:    ta,
		chat:        NewChat(com),
		completions: completions.New(com.Styles.Completions.Normal, com.Styles.Completions.Focused, com.Styles.Completions.Match),
		attachments: attachments.New(
			attachments.NewRenderer(
				com.Styles.Attachments.Normal,
				com.Styles.Attachments.Deleting,
				com.Styles.Attachments.Image,
				com.Styles.Attachments.Text,
			),
			attachments.Keymap{
				DeleteMode: DefaultKeyMap().Editor.AttachmentDeleteMode,
				DeleteAll:  DefaultKeyMap().Editor.DeleteAllAttachments,
				Escape:     DefaultKeyMap().Editor.Escape,
			},
		),
		keyMap:             DefaultKeyMap(),
		agentRuntimes:      make(map[string]*sessionAgentRuntimeState),
		agentToolParents:   make(map[string]string),
		agentToolChildren:  make(map[string]string),
		agentToolTaskIDs:   make(map[string]string),
		agentToolSummaries: make(map[string]map[string]string),
		todoContinuations:  make(map[string]*todoAutoContinueState),
		width:              120,
		height:             40,
	}
	chat.SetAgentPanelResolver(ui.agentTaskPanelsForRender)
	return ui
}

// testWorkspace is a minimal [workspace.Workspace] stub for unit tests.
type testWorkspace struct {
	workspace.Workspace
	cfg                *config.Config
	initKnowledgeCalls int
	currentAgentID     string
	switchAgentCalls   int
	availableAgents    []workspace.AgentInfo
	agentReady         bool
	busySessions       map[string]bool
	queuedPrompts      map[string]int
	sessionByID        map[string]session.Session
	messagesBySession  map[string][]message.Message
	lastRunSessionID   string
	lastRunPrompt      string
	savedSession       session.Session
}

func (w *testWorkspace) Config() *config.Config {
	return w.cfg
}

func (w *testWorkspace) UpdateAgentModel(_ context.Context) error {
	return nil
}

func (w *testWorkspace) AgentIsReady() bool {
	return w.agentReady
}

func (w *testWorkspace) AgentIsBusy() bool {
	return false
}

func (w *testWorkspace) AgentIsSessionBusy(sessionID string) bool {
	return w.busySessions[sessionID]
}

func (w *testWorkspace) AgentQueuedPrompts(sessionID string) int {
	return w.queuedPrompts[sessionID]
}

func (w *testWorkspace) GetSession(_ context.Context, sessionID string) (session.Session, error) {
	if sess, ok := w.sessionByID[sessionID]; ok {
		return sess, nil
	}
	return session.Session{}, nil
}

func (w *testWorkspace) CreateAgentToolSessionID(messageID, toolCallID string) string {
	return messageID + "$$" + toolCallID
}

func (w *testWorkspace) ParseAgentToolSessionID(sessionID string) (string, string, bool) {
	messageID, toolCallID, ok := parseChildSessionID(sessionID)
	return messageID, toolCallID, ok
}

func (w *testWorkspace) SaveSession(_ context.Context, sess session.Session) (session.Session, error) {
	w.savedSession = sess
	if w.sessionByID == nil {
		w.sessionByID = make(map[string]session.Session)
	}
	w.sessionByID[sess.ID] = sess
	return sess, nil
}

func (w *testWorkspace) ListMessages(_ context.Context, sessionID string) ([]message.Message, error) {
	if w.messagesBySession == nil {
		return nil, nil
	}
	return append([]message.Message(nil), w.messagesBySession[sessionID]...), nil
}

func (w *testWorkspace) AgentRun(_ context.Context, sessionID, prompt string, _ ...message.Attachment) error {
	w.lastRunSessionID = sessionID
	w.lastRunPrompt = prompt
	return nil
}

func (w *testWorkspace) CurrentAgentID() string {
	return w.currentAgentID
}

func (w *testWorkspace) SwitchAgent(_ context.Context, agentID string) error {
	w.switchAgentCalls++
	w.currentAgentID = agentID
	if w.cfg != nil && w.cfg.Options != nil {
		w.cfg.Options.ActiveMode = agentID
	}
	return nil
}

func (w *testWorkspace) AvailableAgents() []workspace.AgentInfo {
	return w.availableAgents
}

func (w *testWorkspace) InitKnowledge(_ context.Context) ([]string, error) {
	w.initKnowledgeCalls++
	return []string{"written.md"}, nil
}
