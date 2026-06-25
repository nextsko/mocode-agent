package extension

import (
	"context"

	"charm.land/fantasy"

	"github.com/package-register/mocode/internal/domain/session/message"
)

// Event marks a discrete point in the agent lifecycle. Extensions observe and
// optionally mutate the flow at these points.
type Event string

const (
	// EventBeforeRun fires before an agent begins processing a prompt.
	EventBeforeRun Event = "before_run"
	// EventAfterRun fires after an agent finishes, successfully or not.
	EventAfterRun Event = "after_run"
	// EventBeforeToolCall fires before a tool executes.
	EventBeforeToolCall Event = "before_tool_call"
	// EventAfterToolCall fires after a tool returns its result.
	EventAfterToolCall Event = "after_tool_call"
	// EventOnMessage fires when a message is appended to the session.
	EventOnMessage Event = "on_message"
	// EventOnError fires when the agent or a tool errors.
	EventOnError Event = "on_error"
)

// Context carries the run-scoped state passed to every callback. Fields are
// pointers so an extension may enrich them (for example, annotating Result or
// swapping in a different ToolCall); nil fields mean "not applicable for this
// event".
type Context struct {
	// SessionID is the active session.
	SessionID string
	// AgentName is the agent handling the run (coder, task, a subagent, ...).
	AgentName string
	// Prompt is the user prompt for BeforeRun/AfterRun.
	Prompt string
	// Messages is the message trace, available to observers.
	Messages []message.Message
	// Response is the agent's final textual response for AfterRun/AfterRun
	// observers (e.g. the evo distiller that turns a successful turn into a
	// generalized principle). May be empty for non-text responses.
	Response string
	// ToolCall is the tool call for BeforeToolCall/AfterToolCall.
	ToolCall *fantasy.ToolCall
	// ToolResult is the tool result for AfterToolCall.
	ToolResult *message.ToolResult
	// Err is the error for OnError.
	Err error
}

// Callback is a single on_xxx handler. It receives the dispatch context and
// the firing event. Returning an error logs it but does not abort the run;
// returning a non-nil *Decision can short-circuit a tool call.
type Callback func(ctx context.Context, ev Event, ec *Context) (*Decision, error)

// Decision lets a callback influence control flow. It is intentionally small:
// most callbacks are pure observers and return nil.
type Decision struct {
	// SkipTool, when true on BeforeToolCall, skips the tool execution and
	// returns ToolResult (if set) in its place. Used by policy extensions to
	// block a tool without raising an error.
	SkipTool bool
	// ToolResult is the substitute result when SkipTool is true.
	ToolResult *message.ToolResult
	// AbortRun, when true, requests the coordinator stop the run after the
	// current callback. Used by guardrails (budget limits, safety stops).
	AbortRun bool
	// AbortReason explains an abort for logging.
	AbortReason string
}

// Extension is a named bundle of callbacks plus a lifecycle. Names let the
// Manager dedupe, disable, and inspect extensions at runtime (the evo mode
// uses this to toggle observability on entry/exit).
type Extension interface {
	// Name is a unique, stable identifier (e.g. "evo.observability").
	Name() string
	// Callbacks returns the callbacks this extension registers. The Manager
	// fires each on its matching event.
	Callbacks() []Callback
	// OnEvent is the catch-all dispatcher for events without a structured
	// callback, mirroring trpc's PluginManager.OnEvent. May mutate ec in place.
	OnEvent(ctx context.Context, ev Event, ec *Context)
	// Close releases resources held by the extension.
	Close(ctx context.Context) error
}
