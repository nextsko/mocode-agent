package agenttools

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/nextsko/mocode-agent/internal/core/agent/toolutil"
	"github.com/nextsko/mocode-agent/internal/core/tools/contract"
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

// transferPort is the framework-agnostic form of transfer_to_agent. It owns
// the per-tool tracker so handoff limits survive across calls.
type transferPort struct {
	availableAgents []string
	ctrl            TransferController
	legacyCb        TransferCallback
	tracker         *TransferTracker
}

func newTransferPort(availableAgents []string, ctrl TransferController, legacyCb TransferCallback) *transferPort {
	return &transferPort{
		availableAgents: availableAgents,
		ctrl:            ctrl,
		legacyCb:        legacyCb,
		tracker:         NewTransferTracker(MaxTransfers),
	}
}

// NewTransferPortTool is the framework-agnostic constructor for the legacy
// callback signature.
func NewTransferPortTool(availableAgents []string, onTransfer TransferCallback) contract.Tool {
	return newTransferPort(availableAgents, nil, onTransfer)
}

// NewTransferPortToolWithController is the framework-agnostic constructor for
// the structured controller interface.
func NewTransferPortToolWithController(availableAgents []string, ctrl TransferController) contract.Tool {
	return newTransferPort(availableAgents, ctrl, nil)
}

func (t *transferPort) Name() string { return TransferToolName }

func (t *transferPort) Description() string {
	return toolutil.FirstLineDescription(transferDescription)
}

func (t *transferPort) Schema() contract.Schema {
	return contract.Schema{
		Name:        TransferToolName,
		Description: t.Description(),
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"agent_name": map[string]any{
					"type":        "string",
					"description": "Name of the agent to transfer control to",
				},
				"message": map[string]any{
					"type":        "string",
					"description": "Optional message with context for the target agent",
				},
			},
			"required": []string{"agent_name"},
		},
	}
}

// Parallel implements contract.ParallelTool: transfers never touch shared files.
func (t *transferPort) Parallel() bool { return true }

func (t *transferPort) Execute(ctx context.Context, _ contract.ToolContext, args json.RawMessage) (contract.ToolResult, error) {
	var params TransferParams
	if len(args) > 0 {
		if err := json.Unmarshal(args, &params); err != nil {
			return contract.ToolResult{}, fmt.Errorf("invalid parameters: %w", err)
		}
	}
	text, isErr := t.run(ctx, params)
	if isErr {
		return contract.ToolResult{Error: fmt.Errorf("%s", text)}, nil
	}
	return contract.ToolResult{Content: text}, nil
}

// run executes one transfer and reports whether the text is an error message.
func (t *transferPort) run(ctx context.Context, params TransferParams) (string, bool) {
	if params.AgentName == "" {
		return "agent_name is required", true
	}

	if !t.tracker.CanTransfer() {
		msg := fmt.Sprintf("Transfer limit reached (%d/%d). Cannot transfer anymore.",
			t.tracker.Count(), t.tracker.maxHandoffs)
		if t.tracker.isRepetitive() {
			msg = fmt.Sprintf("Transfer loop detected: only %d unique agent(s) in the last %d transfers. "+
				"Refusing further transfers to prevent infinite loops.",
				t.tracker.countUnique(), t.tracker.windowSize)
		}
		return msg, true
	}

	target := strings.TrimSpace(params.AgentName)
	found := false
	for _, a := range t.availableAgents {
		if a == target {
			found = true
			break
		}
	}
	if !found {
		return fmt.Sprintf("Agent %q is not available. Available agents: %s",
			target, strings.Join(t.availableAgents, ", ")), true
	}

	if t.ctrl != nil {
		if _, err := t.ctrl.OnTransfer(ctx, "", target); err != nil {
			return fmt.Sprintf("Transfer failed: %v", err), true
		}
	} else if t.legacyCb != nil {
		if err := t.legacyCb(ctx, "", target, params.Message); err != nil {
			return fmt.Sprintf("Transfer failed: %v", err), true
		}
	}

	t.tracker.RecordTransfer(target)

	msg := fmt.Sprintf("Control transferred to agent %q.", target)
	if params.Message != "" {
		msg += fmt.Sprintf(" Context: %s", params.Message)
	}
	msg += fmt.Sprintf(" (transfer %d/%d)", t.tracker.Count(), t.tracker.maxHandoffs)
	return msg, false
}
