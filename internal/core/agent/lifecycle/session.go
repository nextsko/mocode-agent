// Package agent is the core orchestration layer for Mocode AI agents.
//
// It provides session-based AI agent functionality for managing
// conversations, tool execution, and message handling. It coordinates
// interactions between language models, messages, sessions, and tools while
// handling features like automatic summarization, queuing, and token
// management.
package lifecycle

import (
	"context"
	_ "embed"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"charm.land/catwalk/pkg/catwalk"
	"charm.land/fantasy"

	"github.com/nextsko/mocode-agent/internal/core/agent/ctxcompress"
	"github.com/nextsko/mocode-agent/internal/core/agent/notify"
	"github.com/nextsko/mocode-agent/internal/core/config"
	"github.com/nextsko/mocode-agent/internal/domain/session"
	"github.com/nextsko/mocode-agent/internal/domain/session/message"
	"github.com/nextsko/mocode-agent/internal/util/csync"
	"github.com/nextsko/mocode-agent/internal/util/errcoll"
	"github.com/nextsko/mocode-agent/internal/util/pubsub"
	"github.com/nextsko/mocode-agent/internal/util/version"
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
	workingDir           string
	notify               pubsub.Publisher[notify.Notification]
	callbacks            *AgentCallbacks

	errorCollector *errcoll.Collector
	compressor     *ctxcompress.Pipeline

	messageQueue   *csync.Map[string, []SessionAgentCall]
	activeRequests *csync.Map[string, context.CancelFunc]

	// runMu guards busy-entry and queue transitions in the Run dispatcher
	// loop. It is only held across map operations — never across model/IO
	// calls — to make enqueue-vs-pop atomic and eliminate the check-then-set
	// and release-then-pop races the previous recursive dispatch had.
	runMu sync.Mutex
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
	WorkingDir           string
	Notify               pubsub.Publisher[notify.Notification]
	Callbacks            *AgentCallbacks
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
		workingDir:           opts.WorkingDir,
		notify:               opts.Notify,
		callbacks:            opts.Callbacks,
		errorCollector:       opts.ErrorCollector,
		compressor:           ctxcompress.NewPipeline(ctxcompress.DefaultPolicy()),
		messageQueue:         csync.NewMap[string, []SessionAgentCall](),
		activeRequests:       csync.NewMap[string, context.CancelFunc](),
	}
}

// compressToolMessage applies the multi-level compression pipeline to a tool
// result message. Tool results are often the biggest consumers of context
