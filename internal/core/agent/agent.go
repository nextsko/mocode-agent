// Package agent is the core orchestration layer for Mocode AI agents.
//
// It provides session-based AI agent functionality for managing
// conversations, tool execution, and message handling. It coordinates
// interactions between language models, messages, sessions, and tools while
// handling features like automatic summarization, queuing, and token
// management.
package agent

import (
	"context"
	_ "embed"
	"fmt"
	"regexp"
	"strings"

	"charm.land/catwalk/pkg/catwalk"
	"charm.land/fantasy"

	"github.com/package-register/mocode/internal/core/agent/ctxcompress"
	"github.com/package-register/mocode/internal/core/agent/evolution"
	"github.com/package-register/mocode/internal/core/agent/notify"
	"github.com/package-register/mocode/internal/core/config"
	"github.com/package-register/mocode/internal/core/knowledge/memory"
	"github.com/package-register/mocode/internal/domain/session"
	"github.com/package-register/mocode/internal/domain/session/message"
	"github.com/package-register/mocode/internal/util/csync"
	"github.com/package-register/mocode/internal/util/errcoll"
	"github.com/package-register/mocode/internal/util/pubsub"
	"github.com/package-register/mocode/internal/util/version"
)

const (
	DefaultSessionName = "Untitled Session"

	// Constants for auto-summarization thresholds
	largeContextWindowThreshold = 200_000
	largeContextWindowBuffer    = 20_000
	smallContextWindowRatio     = 0.2
)

var userAgent = fmt.Sprintf("%s/%s", strings.ReplaceAll(config.AppName(), " ", ""), version.Version)

//go:embed templates/title.md
var titlePrompt []byte

//go:embed templates/summary.md
var summaryPrompt []byte

// Used to remove <think> tags from generated titles.
var (
	thinkTagRegex       = regexp.MustCompile(`(?s)<think>.*?</think>`)
	orphanThinkTagRegex = regexp.MustCompile(`</?think>`)
)

type SessionAgentCall struct {
	SessionID        string
	Prompt           string
	ProviderOptions  fantasy.ProviderOptions
	Attachments      []message.Attachment
	MaxOutputTokens  int64
	Temperature      *float64
	TopP             *float64
	TopK             *int64
	FrequencyPenalty *float64
	PresencePenalty  *float64
	NonInteractive   bool
}

type SessionAgent interface {
	Run(context.Context, SessionAgentCall) (*fantasy.AgentResult, error)
	SetModels(large Model, small Model)
	SetTools(tools []fantasy.AgentTool)
	SetSystemPrompt(systemPrompt string)
	SystemPrompt() string
	Cancel(sessionID string)
	CancelAll()
	IsSessionBusy(sessionID string) bool
	IsBusy() bool
	QueuedPrompts(sessionID string) int
	QueuedPromptsList(sessionID string) []string
	ClearQueue(sessionID string)
	Summarize(context.Context, string, fantasy.ProviderOptions) (string, error)
	Model() Model
	// SmallModel returns the configured small model (cheap auxiliary calls).
	SmallModel() Model
}

type Model struct {
	Model      fantasy.LanguageModel
	CatwalkCfg catwalk.Model
	ModelCfg   config.SelectedModel
}

type sessionAgent struct {
	largeModel         *csync.Value[Model]
	smallModel         *csync.Value[Model]
	systemPromptPrefix *csync.Value[string]
	systemPrompt       *csync.Value[string]
	tools              *csync.Slice[fantasy.AgentTool]

	isSubAgent           bool
	sessions             session.Service
	messages             message.Service
	disableAutoSummarize bool
	isYolo               bool
	memory               memory.Service
	workingDir           string
	notify               pubsub.Publisher[notify.Notification]
	callbacks            *AgentCallbacks

	errorLearner   *evolution.ErrorLearner
	errorCollector *errcoll.Collector
	compressor     *ctxcompress.Pipeline

	messageQueue   *csync.Map[string, []SessionAgentCall]
	activeRequests *csync.Map[string, context.CancelFunc]
}

type SessionAgentOptions struct {
	LargeModel           Model
	SmallModel           Model
	SystemPromptPrefix   string
	SystemPrompt         string
	IsSubAgent           bool
	DisableAutoSummarize bool
	IsYolo               bool
	Sessions             session.Service
	Messages             message.Service
	Tools                []fantasy.AgentTool
	Memory               memory.Service
	WorkingDir           string
	Notify               pubsub.Publisher[notify.Notification]
	Callbacks            *AgentCallbacks
	ErrorLearner         *evolution.ErrorLearner
	ErrorCollector       *errcoll.Collector
}

func NewSessionAgent(
	opts SessionAgentOptions,
) SessionAgent {
	return &sessionAgent{
		largeModel:           csync.NewValue(opts.LargeModel),
		smallModel:           csync.NewValue(opts.SmallModel),
		systemPromptPrefix:   csync.NewValue(opts.SystemPromptPrefix),
		systemPrompt:         csync.NewValue(opts.SystemPrompt),
		isSubAgent:           opts.IsSubAgent,
		sessions:             opts.Sessions,
		messages:             opts.Messages,
		disableAutoSummarize: opts.DisableAutoSummarize,
		tools:                csync.NewSliceFrom(opts.Tools),
		isYolo:               opts.IsYolo,
		memory:               opts.Memory,
		workingDir:           opts.WorkingDir,
		notify:               opts.Notify,
		callbacks:            opts.Callbacks,
		errorLearner:         opts.ErrorLearner,
		errorCollector:       opts.ErrorCollector,
		compressor:           ctxcompress.NewPipeline(ctxcompress.DefaultPolicy()),
		messageQueue:         csync.NewMap[string, []SessionAgentCall](),
		activeRequests:       csync.NewMap[string, context.CancelFunc](),
	}
}

// compressToolMessage applies the multi-level compression pipeline to a tool
// result message. Tool results are often the biggest consumers of context
// window tokens, so compressing older ones yields significant savings.

// extractToolResultText extracts the text content from a ToolResultPart.

// isToolResultError returns true if the tool result is an error.

// setToolResultText replaces the text content in a ToolResultPart.
// model does not support images. This prevents API errors when switching from
// an image-capable model to one that doesn't support images.

// filterOrphanedToolResults converts a tool message to a fantasy.Message,
// dropping any tool result parts whose tool_call_id has no matching tool call
// in the known set. An orphaned result causes API validation to fail on every
// subsequent turn, permanently locking the session. Returns the filtered
// message and true if at least one valid part remains.

// syntheticToolResultsForOrphanedCalls returns a tool message containing
// synthetic tool results for any tool calls in the assistant message that
// have no matching result in knownToolResultIDs. LLM APIs require every
// tool_use to be immediately followed by a tool_result; an interrupted
// session can leave orphaned tool_use blocks that permanently lock the
// conversation. Returns the message and true if any synthetic results were
// produced.

// generateTitle generates a session titled based on the initial prompt.

// convertToToolResult converts a fantasy tool result to a message tool result.

// workaroundProviderMediaLimitations converts media content in tool results to
// user messages for providers that don't natively support images in tool results.
//
// Problem: OpenAI, Google, OpenRouter, and other OpenAI-compatible providers
// don't support sending images/media in tool result messages - they only accept
// text in tool results. However, they DO support images in user messages.
//
// If we send media in tool results to these providers, the API returns an error.
//
// Solution: For these providers, we:
//  1. Replace the media in the tool result with a text placeholder
//  2. Inject a user message immediately after with the image as a file attachment
//  3. This maintains the tool execution flow while working around API limitations
//
// Anthropic and Bedrock support images natively in tool results, so we skip
// this workaround for them.
//
// Example transformation:
//
//	BEFORE: [tool result: image data]
//	AFTER:  [tool result: "Image loaded - see attached"], [user: image attachment]

// buildSummaryPrompt constructs the prompt text for session summarization.
// The output follows the Obsidian + YAML frontmatter format defined in
// templates/summary.md for both human readability and machine parsing.
