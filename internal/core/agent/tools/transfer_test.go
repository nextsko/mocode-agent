package tools_test

import (
	"context"
	"fmt"
	"testing"

	"charm.land/fantasy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/package-register/mocode/internal/core/agent/tools"
)

// ─── TransferTracker — basic counter ─────────────────────────────────────────

func TestTransferTracker_DefaultCanTransfer(t *testing.T) {
	t.Parallel()
	tt := tools.NewTransferTracker(5)
	assert.True(t, tt.CanTransfer())
	assert.Equal(t, 0, tt.Count())
}

func TestTransferTracker_ExhaustMaxHandoffs(t *testing.T) {
	t.Parallel()
	tt := tools.NewTransferTracker(3)
	for i := 0; i < 3; i++ {
		require.True(t, tt.CanTransfer(), "should allow transfer %d", i+1)
		tt.RecordTransfer("agent-a")
	}
	assert.False(t, tt.CanTransfer(), "must block after max handoffs")
	assert.Equal(t, 3, tt.Count())
}

func TestTransferTracker_CounterIncrementsOnRecord(t *testing.T) {
	t.Parallel()
	tt := tools.NewTransferTracker(10)
	tt.RecordTransfer("a")
	tt.RecordTransfer("b")
	assert.Equal(t, 2, tt.Count())
}

// ─── TransferTracker — sliding-window loop detection ─────────────────────────

func TestTransferTracker_NoRepetitionUntilWindowFull(t *testing.T) {
	t.Parallel()
	// window=4, minUnique=2 → needs 4 transfers before checking.
	tt := tools.NewTransferTrackerCustom(20, 4, 2)
	tt.RecordTransfer("a")
	tt.RecordTransfer("a")
	tt.RecordTransfer("a")
	// window not yet full (only 3 entries) → not repetitive.
	assert.False(t, tt.IsRepetitive())
	assert.True(t, tt.CanTransfer())
}

func TestTransferTracker_DetectsRepetitiveLoop(t *testing.T) {
	t.Parallel()
	// window=4, minUnique=3 → need ≥3 unique in any window of 4.
	tt := tools.NewTransferTrackerCustom(20, 4, 3)
	// Fill window with only 2 unique agents.
	tt.RecordTransfer("alice")
	tt.RecordTransfer("bob")
	tt.RecordTransfer("alice")
	tt.RecordTransfer("bob") // window now full: [alice, bob, alice, bob] → 2 unique < 3
	assert.True(t, tt.IsRepetitive())
	assert.False(t, tt.CanTransfer())
}

func TestTransferTracker_SufficientUniqueness_NotRepetitive(t *testing.T) {
	t.Parallel()
	// window=4, minUnique=2 → 2 unique in window is enough.
	tt := tools.NewTransferTrackerCustom(20, 4, 2)
	tt.RecordTransfer("alice")
	tt.RecordTransfer("bob")
	tt.RecordTransfer("alice")
	tt.RecordTransfer("bob")
	assert.False(t, tt.IsRepetitive(), "2 unique >= minUnique(2): should not flag")
	assert.True(t, tt.CanTransfer())
}

func TestTransferTracker_WindowSlides(t *testing.T) {
	t.Parallel()
	// window=4, minUnique=3. Start with a loop, then inject a new agent to break it.
	tt := tools.NewTransferTrackerCustom(20, 4, 3)
	tt.RecordTransfer("a")
	tt.RecordTransfer("b")
	tt.RecordTransfer("a")
	tt.RecordTransfer("b") // window: [a,b,a,b] → 2 unique → repetitive
	assert.True(t, tt.IsRepetitive())

	// Inject 3 new unique agents to push old entries out of the window.
	tt.RecordTransfer("c")
	tt.RecordTransfer("d")
	tt.RecordTransfer("e") // window: [b,c,d,e] → 4 unique → not repetitive
	assert.False(t, tt.IsRepetitive(), "window should slide and recover")
	assert.True(t, tt.CanTransfer())
}

func TestTransferTracker_DisableRepetitionCheck_ZeroWindow(t *testing.T) {
	t.Parallel()
	// windowSize=0 disables the repetition check entirely.
	tt := tools.NewTransferTrackerCustom(20, 0, 3)
	for i := 0; i < 10; i++ {
		tt.RecordTransfer("same-agent")
	}
	assert.False(t, tt.IsRepetitive(), "repetition check disabled when windowSize=0")
	assert.True(t, tt.CanTransfer())
}

func TestTransferTracker_DisableRepetitionCheck_ZeroMinUnique(t *testing.T) {
	t.Parallel()
	tt := tools.NewTransferTrackerCustom(20, 4, 0)
	for i := 0; i < 4; i++ {
		tt.RecordTransfer("same-agent")
	}
	assert.False(t, tt.IsRepetitive(), "repetition check disabled when minUnique=0")
}

func TestTransferTracker_DefaultSettings_MatchConstants(t *testing.T) {
	t.Parallel()
	tt := tools.NewTransferTracker(tools.MaxTransfers)
	// Verify the window starts empty and is not repetitive.
	assert.False(t, tt.IsRepetitive())
	assert.True(t, tt.CanTransfer())
}

// ─── TransferController interface ────────────────────────────────────────────

// controllerFunc wraps a func as a TransferController for testing.
type controllerFunc struct {
	fn func(ctx context.Context, from, to string) (tools.TransferDecision, error)
}

func (c *controllerFunc) OnTransfer(ctx context.Context, from, to string) (tools.TransferDecision, error) {
	return c.fn(ctx, from, to)
}

func TestNewTransferTool_Controller_BlocksTransfer(t *testing.T) {
	t.Parallel()
	ctrl := &controllerFunc{
		fn: func(_ context.Context, _, to string) (tools.TransferDecision, error) {
			if to == "blocked-agent" {
				return tools.TransferDecision{}, fmt.Errorf("agent %q is blocked", to)
			}
			return tools.TransferDecision{}, nil
		},
	}
	tool := tools.NewTransferToolWithController([]string{"blocked-agent", "allowed-agent"}, ctrl)
	resp, err := tool.Run(context.Background(), fantasy.ToolCall{
		ID: "x", Name: tools.TransferToolName,
		Input: `{"agent_name":"blocked-agent"}`,
	})
	require.NoError(t, err)
	assert.True(t, resp.IsError)
	assert.Contains(t, resp.Content, "blocked")
}

func TestNewTransferTool_Controller_AllowsTransfer(t *testing.T) {
	t.Parallel()
	called := false
	ctrl := &controllerFunc{
		fn: func(_ context.Context, _, _ string) (tools.TransferDecision, error) {
			called = true
			return tools.TransferDecision{}, nil
		},
	}
	tool := tools.NewTransferToolWithController([]string{"allowed-agent"}, ctrl)
	resp, err := tool.Run(context.Background(), fantasy.ToolCall{
		ID: "x", Name: tools.TransferToolName,
		Input: `{"agent_name":"allowed-agent"}`,
	})
	require.NoError(t, err)
	assert.False(t, resp.IsError)
	assert.True(t, called, "controller.OnTransfer must have been called")
}

func TestNewTransferTool_NilController_StillWorks(t *testing.T) {
	t.Parallel()
	tool := tools.NewTransferToolWithController([]string{"agent-x"}, nil)
	resp, err := tool.Run(context.Background(), fantasy.ToolCall{
		ID: "x", Name: tools.TransferToolName,
		Input: `{"agent_name":"agent-x"}`,
	})
	require.NoError(t, err)
	assert.False(t, resp.IsError)
}

func TestNewTransferToolWithCallback_Shim_BackwardCompat(t *testing.T) {
	t.Parallel()
	var gotFrom, gotTo, gotMsg string
	cb := tools.TransferCallback(func(_ context.Context, from, to, msg string) error {
		gotFrom, gotTo, gotMsg = from, to, msg
		return nil
	})
	tool := tools.NewTransferToolWithCallback([]string{"agent-y"}, cb)
	resp, err := tool.Run(context.Background(), fantasy.ToolCall{
		ID: "x", Name: tools.TransferToolName,
		Input: `{"agent_name":"agent-y","message":"hello"}`,
	})
	require.NoError(t, err)
	assert.False(t, resp.IsError)
	assert.Equal(t, "", gotFrom)
	assert.Equal(t, "agent-y", gotTo)
	assert.Equal(t, "hello", gotMsg)
}

// ─── TransferTracker — combined: absolute cap beats repetition check ──────────

func TestTransferTracker_MaxHandoffsBeatsWindow(t *testing.T) {
	t.Parallel()
	// maxHandoffs=2, window=8. The absolute cap should trigger first.
	tt := tools.NewTransferTrackerCustom(2, 8, 3)
	tt.RecordTransfer("a")
	tt.RecordTransfer("b")
	// count == maxHandoffs (2); window not yet full (only 2 of 8) → still blocked by cap.
	assert.False(t, tt.CanTransfer())
}
