package tools

import (
	"context"
	_ "embed"
	"fmt"
	"strings"

	"charm.land/fantasy"
)

const (
	TransferToolName = "transfer_to_agent"
	// MaxTransfers is the default ceiling on total handoffs per run.
	MaxTransfers = 5
	// DefaultRepetitionWindow is the default sliding-window size for loop detection.
	DefaultRepetitionWindow = 8
	// DefaultRepetitionMinUnique is the minimum unique agents required in the window
	// before a repetition is flagged.
	DefaultRepetitionMinUnique = 3
)

//go:embed transfer.md
var transferDescription []byte

type TransferParams struct {
	AgentName string `json:"agent_name" description:"Name of the agent to transfer control to"`
	Message   string `json:"message,omitempty" description:"Optional message with context for the target agent"`
}

// TransferCallback is invoked when a transfer is requested.
// It should return an error if the transfer is not allowed.
// Deprecated: use TransferController for new code.
type TransferCallback func(ctx context.Context, fromAgent, toAgent, message string) error

// TransferDecision is returned by TransferController.OnTransfer.
// It may carry optional metadata about the approved transfer.
// Inspired by trpc-agent-go/agent/transfer_controller.go.
type TransferDecision struct {
	// Reserved for future fields (e.g. target timeout, priority, metadata).
}

// TransferController can enforce policies and limits for agent transfers.
// If a controller returns a non-nil error from OnTransfer, the transfer is
// rejected and the error message is returned to the model.
// Inspired by trpc-agent-go/agent/transfer_controller.go.
type TransferController interface {
	// OnTransfer is called right before the transfer executes.
	// Return a non-nil error to reject the transfer.
	OnTransfer(ctx context.Context, fromAgent, toAgent string) (TransferDecision, error)
}

// funcTransferController adapts a TransferCallback into a TransferController.
type funcTransferController struct {
	cb TransferCallback
	// message is captured per-call via a closure in NewTransferToolWithCallback.
}

func (f *funcTransferController) OnTransfer(ctx context.Context, from, to string) (TransferDecision, error) {
	return TransferDecision{}, f.cb(ctx, from, to, "")
}

// TransferTracker tracks handoff counts and detects repetitive-loop patterns.
// Inspired by SwarmConfig in trpc-agent-go/team/swarm.go.
type TransferTracker struct {
	count       int
	maxHandoffs int

	// Sliding-window loop detection (borrowed from SwarmConfig).
	windowSize   int      // number of recent transfers to examine; 0 disables
	minUnique    int      // minimum unique agents required in window; 0 disables
	recentAgents []string // ring-buffer of agent names, newest last
}

// NewTransferTracker creates a new transfer tracker with default loop-detection
// settings.
func NewTransferTracker(maxHandoffs int) *TransferTracker {
	if maxHandoffs <= 0 {
		maxHandoffs = MaxTransfers
	}
	return &TransferTracker{
		maxHandoffs: maxHandoffs,
		windowSize:  DefaultRepetitionWindow,
		minUnique:   DefaultRepetitionMinUnique,
	}
}

// NewTransferTrackerCustom creates a tracker with explicit loop-detection
// parameters.  Set windowSize or minUnique to 0 to disable that check.
func NewTransferTrackerCustom(maxHandoffs, windowSize, minUnique int) *TransferTracker {
	if maxHandoffs <= 0 {
		maxHandoffs = MaxTransfers
	}
	return &TransferTracker{
		maxHandoffs: maxHandoffs,
		windowSize:  windowSize,
		minUnique:   minUnique,
	}
}

// CanTransfer returns true if another transfer is allowed.
// It enforces both the absolute cap and the sliding-window uniqueness check.
func (t *TransferTracker) CanTransfer() bool {
	return t.count < t.maxHandoffs && !t.isRepetitive()
}

// RecordTransfer increments the transfer count and appends the target agent
// name to the sliding window.
func (t *TransferTracker) RecordTransfer(toAgent string) {
	t.count++
	if t.windowSize > 0 {
		t.recentAgents = append(t.recentAgents, toAgent)
		if len(t.recentAgents) > t.windowSize {
			t.recentAgents = t.recentAgents[len(t.recentAgents)-t.windowSize:]
		}
	}
}

// Count returns the total number of completed transfers.
func (t *TransferTracker) Count() int {
	return t.count
}

// IsRepetitive returns true when the sliding window is full and contains
// fewer unique agents than minUnique.  Exposed for testing.
func (t *TransferTracker) IsRepetitive() bool {
	return t.isRepetitive()
}

// isRepetitive is the internal implementation.
func (t *TransferTracker) isRepetitive() bool {
	if t.windowSize <= 0 || t.minUnique <= 0 {
		return false
	}
	if len(t.recentAgents) < t.windowSize {
		return false // window not yet full
	}
	return t.countUnique() < t.minUnique
}

// countUnique returns the number of distinct agent names in the recent window.
func (t *TransferTracker) countUnique() int {
	seen := make(map[string]struct{}, len(t.recentAgents))
	for _, a := range t.recentAgents {
		seen[a] = struct{}{}
	}
	return len(seen)
}

// NewTransferTool creates a transfer_to_agent tool with the legacy callback
// signature.  New code should prefer NewTransferToolWithController.
func NewTransferTool(availableAgents []string, onTransfer TransferCallback) fantasy.AgentTool {
	return NewTransferToolWithCallback(availableAgents, onTransfer)
}

// NewTransferToolWithCallback is the explicit backward-compat entry point.
// The legacy callback receives the message field directly via the else-if
// path in newTransferToolCore; do not wrap it as a controller.
func NewTransferToolWithCallback(availableAgents []string, onTransfer TransferCallback) fantasy.AgentTool {
	return newTransferToolCore(availableAgents, nil, onTransfer)
}

// NewTransferToolWithController creates a transfer_to_agent tool using the
// structured TransferController interface.  Pass nil to skip policy enforcement.
// Inspired by trpc-agent-go/agent/transfer_controller.go.
func NewTransferToolWithController(availableAgents []string, ctrl TransferController) fantasy.AgentTool {
	return newTransferToolCore(availableAgents, ctrl, nil)
}

// newTransferToolCore is the shared implementation.
// cb is only non-nil in the legacy path so we can forward the message field.
func newTransferToolCore(
	availableAgents []string,
	ctrl TransferController,
	legacyCb TransferCallback,
) fantasy.AgentTool {
	tracker := NewTransferTracker(MaxTransfers)

	return fantasy.NewParallelAgentTool(
		TransferToolName,
		FirstLineDescription(transferDescription),
		func(ctx context.Context, params TransferParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if params.AgentName == "" {
				return fantasy.NewTextErrorResponse("agent_name is required"), nil
			}

			// Check if transfer is allowed (absolute cap + repetition check).
			if !tracker.CanTransfer() {
				msg := fmt.Sprintf("Transfer limit reached (%d/%d). Cannot transfer anymore.",
					tracker.Count(), tracker.maxHandoffs)
				if tracker.isRepetitive() {
					msg = fmt.Sprintf("Transfer loop detected: only %d unique agent(s) in the last %d transfers. "+
						"Refusing further transfers to prevent infinite loops.",
						tracker.countUnique(), tracker.windowSize)
				}
				return fantasy.NewTextErrorResponse(msg), nil
			}

			// Validate target agent exists.
			target := strings.TrimSpace(params.AgentName)
			found := false
			for _, a := range availableAgents {
				if a == target {
					found = true
					break
				}
			}
			if !found {
				return fantasy.NewTextErrorResponse(
					fmt.Sprintf("Agent %q is not available. Available agents: %s",
						target, strings.Join(availableAgents, ", ")),
				), nil
			}

			// Execute controller (structured) or legacy callback.
			if ctrl != nil {
				if _, err := ctrl.OnTransfer(ctx, "", target); err != nil {
					return fantasy.NewTextErrorResponse(fmt.Sprintf("Transfer failed: %v", err)), nil
				}
			} else if legacyCb != nil {
				if err := legacyCb(ctx, "", target, params.Message); err != nil {
					return fantasy.NewTextErrorResponse(fmt.Sprintf("Transfer failed: %v", err)), nil
				}
			}

			tracker.RecordTransfer(target)

			msg := fmt.Sprintf("Control transferred to agent %q.", target)
			if params.Message != "" {
				msg += fmt.Sprintf(" Context: %s", params.Message)
			}
			msg += fmt.Sprintf(" (transfer %d/%d)", tracker.Count(), tracker.maxHandoffs)

			return fantasy.NewTextResponse(msg), nil
		})
}
