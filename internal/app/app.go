// Package app wires together services, coordinates agents, and manages
// application lifecycle.
package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/catwalk/pkg/catwalk"
	"charm.land/fantasy"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/exp/charmtone"
	"github.com/charmbracelet/x/term"
	"github.com/google/uuid"
	"github.com/package-register/mocode/internal/agent"
	"github.com/package-register/mocode/internal/agent/notify"
	"github.com/package-register/mocode/internal/agent/tools/mcp"
	"github.com/package-register/mocode/internal/config"
	"github.com/package-register/mocode/internal/errcoll"
	"github.com/package-register/mocode/internal/filetracker"
	"github.com/package-register/mocode/internal/history"
	"github.com/package-register/mocode/internal/knowledge/memory"
	"github.com/package-register/mocode/internal/log"
	"github.com/package-register/mocode/internal/agent/tools/lsp"
	"github.com/package-register/mocode/internal/permission"
	"github.com/package-register/mocode/internal/pubsub"
	"github.com/package-register/mocode/internal/session"
	"github.com/package-register/mocode/internal/session/message"
	"github.com/package-register/mocode/internal/shellruntime/shell"
	"github.com/package-register/mocode/internal/skills"
	"github.com/package-register/mocode/internal/store"
	"github.com/package-register/mocode/internal/ui/anim"
	"github.com/package-register/mocode/internal/ui/styles"
)

type App struct {
	Sessions    session.Service
	Messages    message.Service
	History     history.Service
	Permissions permission.Service
	FileTracker filetracker.Service

	AgentCoordinator agent.Coordinator

	LSPManager *lsp.Manager
	Memory     memory.Service

	config *config.ConfigStore

	serviceEventsWG *sync.WaitGroup
	eventsCtx       context.Context
	events          *pubsub.Broker[tea.Msg]
	tuiWG           *sync.WaitGroup

	// global context and cleanup functions
	globalCtx          context.Context
	cleanupFuncs       []func(context.Context) error
	agentNotifications *pubsub.Broker[notify.Notification]

	// database dependencies exposed for snapshot/backup services
	store *store.Store

	sessionStoreID  string
	currentServices *SessionServices

	// ErrorCollector records tool execution failures for later analysis.
	ErrorCollector *errcoll.Collector
}

type SessionServices struct {
	Sessions    session.Service
	Messages    message.Service
	History     history.Service
	FileTracker filetracker.Service
	Memory      memory.Service
}

// New initializes a new application instance with file-based storage.
func New(ctx context.Context, storeCfg *config.ConfigStore) (*App, error) {
	cfg := storeCfg.Config()
	skipPermissionsRequests := storeCfg.Overrides().SkipPermissionRequests
	var allowedTools []string
	if cfg.Permissions != nil && cfg.Permissions.AllowedTools != nil {
		allowedTools = cfg.Permissions.AllowedTools
	}

	st, err := store.New(storeCfg.WorkingDir(), storeCfg)
	if err != nil {
		return nil, fmt.Errorf("init store: %w", err)
	}

	app := &App{
		Permissions: permission.NewPermissionService(storeCfg.WorkingDir(), skipPermissionsRequests, allowedTools),
		LSPManager:  lsp.NewManager(storeCfg),

		globalCtx: ctx,

		config: storeCfg,
		store:  st,

		events:             pubsub.NewBroker[tea.Msg](),
		serviceEventsWG:    &sync.WaitGroup{},
		tuiWG:              &sync.WaitGroup{},
		agentNotifications: pubsub.NewBroker[notify.Notification](),
	}

	// Initialize the project-local error collector.
	// cfg.Options.DataDirectory is always resolved to an absolute path by
	// config.setDefaults (see internal/config/load.go), so it is safe to use
	// it directly here. Joining it with storeCfg.WorkingDir() would produce
	// an invalid path with two drive letters on Windows (e.g.
	// "C:\wd\C:\wd\.mocode\errors"), which os.MkdirAll rejects.
	dataDir := cfg.Options.DataDirectory
	if dataDir == "" {
		dataDir = filepath.Join(storeCfg.WorkingDir(), ".mocode")
	}
	errorsDir := filepath.Join(dataDir, "errors")
	collector, err := errcoll.New(errorsDir)
	if err != nil {
		return nil, fmt.Errorf("init error collector: %w", err)
	}
	app.ErrorCollector = collector
	app.cleanupFuncs = append(app.cleanupFuncs, func(context.Context) error {
		collector.Stop()
		return nil
	})

	if err := app.useStoreServices(); err != nil {
		return nil, err
	}

	app.setupEvents()

	// Update providers in the background without blocking startup.
	config.UpdateProvidersAsync(storeCfg.Config())

	go mcp.Initialize(ctx, app.Permissions, storeCfg)

	// cleanup database upon app shutdown
	app.cleanupFuncs = append(
		app.cleanupFuncs,
		func(context.Context) error { return st.Close() },
		func(ctx context.Context) error { return mcp.Close(ctx) },
	)

	// TODO: remove the concept of agent config, most likely.
	if !cfg.IsConfigured() {
		slog.Warn("No agent configuration found")
		return app, nil
	}
	if err := app.InitCoderAgent(ctx); err != nil {
		return nil, fmt.Errorf("failed to initialize coder agent: %w", err)
	}

	// Set up callback for LSP state updates.
	app.LSPManager.SetCallback(func(name string, client *lsp.Client) {
		if client == nil {
			updateLSPState(name, lsp.StateUnstarted, nil, nil, 0)
			return
		}
		client.SetDiagnosticsCallback(updateLSPDiagnostics)
		updateLSPState(name, client.GetServerState(), nil, client, 0)
	})
	go app.LSPManager.TrackConfigured()

	return app, nil
}

// Config returns the pure-data configuration.
func (app *App) Config() *config.Config {
	return app.config.Config()
}

// Store returns the config store.
func (app *App) Store() *config.ConfigStore {
	return app.config
}

// DataStore returns the file-based data store.
func (app *App) DataStore() *store.Store {
	return app.store
}

// ErrorCollectorService returns the project-local asynchronous error
// collector used by tools.
func (app *App) ErrorCollectorService() *errcoll.Collector {
	return app.ErrorCollector
}

// Events returns a per-caller subscription channel for application events.
// Each caller receives its own channel; all callers receive every event.
func (app *App) Events(ctx context.Context) <-chan pubsub.Event[tea.Msg] {
	return app.events.Subscribe(ctx)
}

// SendEvent publishes a message to all event subscribers.
func (app *App) SendEvent(msg tea.Msg) {
	app.events.Publish(pubsub.UpdatedEvent, msg)
}

// AgentNotifications returns the broker for agent notification events.
func (app *App) AgentNotifications() *pubsub.Broker[notify.Notification] {
	return app.agentNotifications
}

// resolveSession resolves which session to use for a non-interactive run
// If continueSessionID is set, it looks up that session by ID
// If useLast is set, it returns the most recently updated top-level session
// Otherwise, it creates a new session
func (app *App) resolveSession(ctx context.Context, continueSessionID string, useLast bool) (session.Session, error) {
	var sess session.Session
	var err error
	switch {
	case continueSessionID != "":
		if app.Sessions.IsAgentToolSession(continueSessionID) {
			return session.Session{}, fmt.Errorf("cannot continue an agent tool session: %s", continueSessionID)
		}
		sess, err = app.GetSession(ctx, continueSessionID)
		if err != nil {
			return session.Session{}, fmt.Errorf("session not found: %s", continueSessionID)
		}
		if sess.ParentSessionID != "" {
			return session.Session{}, fmt.Errorf("cannot continue a child session: %s", continueSessionID)
		}

	case useLast:
		sess, err = app.databaseSessionService().GetLast(ctx)
		if err != nil {
			return session.Session{}, fmt.Errorf("no sessions found to continue")
		}

	default:
		sess, err = app.databaseSessionService().Create(ctx, agent.DefaultSessionName)
		if err != nil {
			return session.Session{}, err
		}
	}

	if err := app.useSessionStore(ctx, sess); err != nil {
		return session.Session{}, err
	}
	return sess, nil
}

// RunNonInteractive runs the application in non-interactive mode with the
// given prompt, printing to stdout.
func (app *App) RunNonInteractive(ctx context.Context, output io.Writer, prompt, largeModel, smallModel string, hideSpinner bool, continueSessionID string, useLast bool) error {
	slog.Info("Running in non-interactive mode")

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	if largeModel != "" || smallModel != "" {
		if err := app.overrideModelsForNonInteractive(ctx, largeModel, smallModel); err != nil {
			return fmt.Errorf("failed to override models: %w", err)
		}
	}

	var (
		spinner   *anim.Spinner
		stdoutTTY bool
		stderrTTY bool
		stdinTTY  bool
		progress  bool
	)

	if f, ok := output.(*os.File); ok {
		stdoutTTY = term.IsTerminal(f.Fd())
	}
	stderrTTY = term.IsTerminal(os.Stderr.Fd())
	stdinTTY = term.IsTerminal(os.Stdin.Fd())
	progress = app.config.Config().Options.Progress == nil || *app.config.Config().Options.Progress

	if !hideSpinner && stderrTTY {
		t := styles.ThemeForProvider(app.config.Config().Models[config.SelectedModelTypeLarge].Provider)

		// Detect background color to set the appropriate color for the
		// spinner's 'Generating...' text. Without this, that text would be
		// unreadable in light terminals.
		hasDarkBG := true
		if f, ok := output.(*os.File); ok && stdinTTY && stdoutTTY {
			hasDarkBG = lipgloss.HasDarkBackground(os.Stdin, f)
		}
		defaultFG := lipgloss.LightDark(hasDarkBG)(charmtone.Pepper, t.WorkingLabelColor)

		spinner = anim.NewSpinner(ctx, cancel, anim.Settings{
			Size:        10,
			Label:       "Generating",
			LabelColor:  defaultFG,
			GradColorA:  t.WorkingGradFromColor,
			GradColorB:  t.WorkingGradToColor,
			CycleColors: true,
		})
		spinner.Start()
	}

	// Helper function to stop spinner once.
	stopSpinner := func() {
		if !hideSpinner && spinner != nil {
			spinner.Stop()
			spinner = nil
		}
	}

	// force update of agent models before running so mcp tools are loaded
	app.AgentCoordinator.UpdateModels(ctx)

	defer stopSpinner()

	sess, err := app.resolveSession(ctx, continueSessionID, useLast)
	if err != nil {
		return fmt.Errorf("failed to create session for non-interactive mode: %w", err)
	}

	if continueSessionID != "" || useLast {
		slog.Info("Continuing session for non-interactive run", "session_id", sess.ID)
	} else {
		slog.Info("Created session for non-interactive run", "session_id", sess.ID)
	}

	// Automatically approve all permission requests for this non-interactive
	// session.
	app.Permissions.AutoApproveSession(sess.ID)

	type response struct {
		result *fantasy.AgentResult
		err    error
	}
	done := make(chan response, 1)

	go func(ctx context.Context, sessionID, prompt string) {
		result, err := app.AgentCoordinator.Run(ctx, sess.ID, prompt)
		if err != nil {
			done <- response{
				err: fmt.Errorf("failed to start agent processing stream: %w", err),
			}
			return
		}
		done <- response{
			result: result,
		}
	}(ctx, sess.ID, prompt)

	messageEvents := app.Messages.Subscribe(ctx)
	messageReadBytes := make(map[string]int)
	var printed bool

	defer func() {
		if progress && stderrTTY {
			_, _ = fmt.Fprintf(os.Stderr, ansi.ResetProgressBar)
		}

		// Always print a newline at the end. If output is a TTY this will
		// prevent the prompt from overwriting the last line of output.
		_, _ = fmt.Fprintln(output)
	}()

	for {
		if progress && stderrTTY {
			// HACK: Reinitialize the terminal progress bar on every iteration
			// so it doesn't get hidden by the terminal due to inactivity.
			_, _ = fmt.Fprintf(os.Stderr, ansi.SetIndeterminateProgressBar)
		}

		select {
		case result := <-done:
			stopSpinner()
			if result.err != nil {
				if errors.Is(result.err, context.Canceled) || errors.Is(result.err, agent.ErrRequestCancelled) {
					slog.Debug("Non-interactive: agent processing cancelled", "session_id", sess.ID)
					return nil
				}
				return fmt.Errorf("agent processing failed: %w", result.err)
			}
			return nil

		case event := <-messageEvents:
			msg := event.Payload
			if msg.SessionID == sess.ID && msg.Role == message.Assistant && len(msg.Parts) > 0 {
				stopSpinner()

				content := msg.Content().String()
				readBytes := messageReadBytes[msg.ID]

				if len(content) < readBytes {
					slog.Error("Non-interactive: message content is shorter than read bytes", "message_length", len(content), "read_bytes", readBytes)
					return fmt.Errorf("message content is shorter than read bytes: %d < %d", len(content), readBytes)
				}

				part := content[readBytes:]
				// Trim leading whitespace. Sometimes the LLM includes leading
				// formatting and intentation, which we don't want here.
				if readBytes == 0 {
					part = strings.TrimLeft(part, " \t")
				}
				// Ignore initial whitespace-only messages.
				if printed || strings.TrimSpace(part) != "" {
					printed = true
					fmt.Fprint(output, part)
				}
				messageReadBytes[msg.ID] = len(content)
			}

		case <-ctx.Done():
			stopSpinner()
			return ctx.Err()
		}
	}
}

func (app *App) UpdateAgentModel(ctx context.Context) error {
	if app.AgentCoordinator == nil {
		return fmt.Errorf("agent configuration is missing")
	}
	return app.AgentCoordinator.UpdateModels(ctx)
}

// overrideModelsForNonInteractive parses the model strings and temporarily
// overrides the model configurations, then rebuilds the agent.
// Format: "model-name" (searches all providers) or "provider/model-name".
// Model matching is case-insensitive.
// If largeModel is provided but smallModel is not, the small model defaults to
// the provider's default small model.
func (app *App) overrideModelsForNonInteractive(ctx context.Context, largeModel, smallModel string) error {
	providers := app.config.Config().Providers.Copy()

	largeMatches, smallMatches, err := findModels(providers, largeModel, smallModel)
	if err != nil {
		return err
	}

	var largeProviderID string

	// Override large model.
	if largeModel != "" {
		found, err := validateMatches(largeMatches, largeModel, "large")
		if err != nil {
			return err
		}
		largeProviderID = found.provider
		slog.Info("Overriding large model for non-interactive run", "provider", found.provider, "model", found.modelID)
		app.config.Config().Models[config.SelectedModelTypeLarge] = config.SelectedModel{
			Provider: found.provider,
			Model:    found.modelID,
		}
	}

	// Override small model.
	switch {
	case smallModel != "":
		found, err := validateMatches(smallMatches, smallModel, "small")
		if err != nil {
			return err
		}
		slog.Info("Overriding small model for non-interactive run", "provider", found.provider, "model", found.modelID)
		app.config.Config().Models[config.SelectedModelTypeSmall] = config.SelectedModel{
			Provider: found.provider,
			Model:    found.modelID,
		}

	case largeModel != "":
		// No small model specified, but large model was - use provider's default.
		smallCfg := app.GetDefaultSmallModel(largeProviderID)
		app.config.Config().Models[config.SelectedModelTypeSmall] = smallCfg
	}

	return app.AgentCoordinator.UpdateModels(ctx)
}

// GetDefaultSmallModel returns the default small model for the given
// provider. Falls back to the large model if no default is found.
func (app *App) GetDefaultSmallModel(providerID string) config.SelectedModel {
	cfg := app.config.Config()
	largeModelCfg := cfg.Models[config.SelectedModelTypeLarge]

	// Find the provider in the known providers list to get its default small model.
	knownProviders, _ := config.Providers(cfg)
	var knownProvider *catwalk.Provider
	for _, p := range knownProviders {
		if string(p.ID) == providerID {
			knownProvider = &p
			break
		}
	}

	// For unknown/local providers, use the large model as small.
	if knownProvider == nil {
		slog.Warn("Using large model as small model for unknown provider", "provider", providerID, "model", largeModelCfg.Model)
		return largeModelCfg
	}

	defaultSmallModelID := knownProvider.DefaultSmallModelID
	model := cfg.GetModel(providerID, defaultSmallModelID)
	if model == nil {
		slog.Warn("Default small model not found, using large model", "provider", providerID, "model", largeModelCfg.Model)
		return largeModelCfg
	}

	slog.Info("Using provider default small model", "provider", providerID, "model", defaultSmallModelID)
	return config.SelectedModel{
		Provider:        providerID,
		Model:           defaultSmallModelID,
		MaxTokens:       model.DefaultMaxTokens,
		ReasoningEffort: model.DefaultReasoningEffort,
	}
}

func (app *App) setupEvents() {
	ctx, cancel := context.WithCancel(app.globalCtx)
	app.eventsCtx = ctx
	app.subscribeSessionServices(ctx)
	setupSubscriber(ctx, app.serviceEventsWG, "permissions", app.Permissions.Subscribe, app.events)
	setupSubscriber(ctx, app.serviceEventsWG, "permissions-notifications", app.Permissions.SubscribeNotifications, app.events)
	setupSubscriber(ctx, app.serviceEventsWG, "agent-notifications", app.agentNotifications.Subscribe, app.events)
	setupSubscriber(ctx, app.serviceEventsWG, "mcp", mcp.SubscribeEvents, app.events)
	setupSubscriber(ctx, app.serviceEventsWG, "lsp", SubscribeLSPEvents, app.events)
	setupSubscriber(ctx, app.serviceEventsWG, "skills", skills.SubscribeEvents, app.events)
	cleanupFunc := func(context.Context) error {
		cancel()
		app.serviceEventsWG.Wait()
		app.events.Shutdown()
		return nil
	}
	app.cleanupFuncs = append(app.cleanupFuncs, cleanupFunc)
}

func (app *App) subscribeSessionServices(ctx context.Context) {
	setupSubscriber(ctx, app.serviceEventsWG, "sessions", app.Sessions.Subscribe, app.events)
	setupSubscriber(ctx, app.serviceEventsWG, "messages", app.Messages.Subscribe, app.events)
	setupSubscriber(ctx, app.serviceEventsWG, "history", app.History.Subscribe, app.events)
}

func setupSubscriber[T any](
	ctx context.Context,
	wg *sync.WaitGroup,
	name string,
	subscriber func(context.Context) <-chan pubsub.Event[T],
	broker *pubsub.Broker[tea.Msg],
) {
	wg.Go(func() {
		subCh := subscriber(ctx)
		for {
			select {
			case event, ok := <-subCh:
				if !ok {
					slog.Debug("Subscription channel closed", "name", name)
					return
				}
				broker.Publish(pubsub.UpdatedEvent, tea.Msg(event))
			case <-ctx.Done():
				slog.Debug("Subscription cancelled", "name", name)
				return
			}
		}
	})
}

func (app *App) InitCoderAgent(ctx context.Context) error {
	coderAgentCfg := app.config.Config().Agents[config.AgentCoder]
	if coderAgentCfg.ID == "" {
		return fmt.Errorf("coder agent configuration is missing")
	}
	var err error
	app.AgentCoordinator, err = agent.NewCoordinator(
		ctx,
		app.config,
		app.Sessions,
		app.Messages,
		app.Permissions,
		app.History,
		app.FileTracker,
		app.LSPManager,
		app.Memory,
		app.agentNotifications,
		app.ErrorCollector,
		app.store.Search(),
	)
	if err != nil {
		slog.Error("Failed to create coder agent", "err", err)
		return err
	}
	return nil
}

// Subscribe sends events to the TUI as tea.Msgs.
func (app *App) Subscribe(program *tea.Program) {
	defer log.RecoverPanic("app.Subscribe", func() {
		slog.Info("TUI subscription panic: attempting graceful shutdown")
		program.Quit()
	})

	app.tuiWG.Add(1)
	tuiCtx, tuiCancel := context.WithCancel(app.globalCtx)
	app.cleanupFuncs = append(app.cleanupFuncs, func(context.Context) error {
		slog.Debug("Cancelling TUI message handler")
		tuiCancel()
		app.tuiWG.Wait()
		return nil
	})
	defer app.tuiWG.Done()

	events := app.events.Subscribe(tuiCtx)
	for {
		select {
		case <-tuiCtx.Done():
			slog.Debug("TUI message handler shutting down")
			return
		case ev, ok := <-events:
			if !ok {
				slog.Debug("TUI message channel closed")
				return
			}
			program.Send(ev.Payload)
		}
	}
}

// Shutdown performs a graceful shutdown of the application.
func (app *App) Shutdown() {
	start := time.Now()
	defer func() { slog.Debug("Shutdown took " + time.Since(start).String()) }()

	// First, cancel all agents and wait for them to finish. This must complete
	// before closing the DB so agents can finish writing their state.
	if app.AgentCoordinator != nil {
		app.AgentCoordinator.CancelAll()
	}

	// Now run remaining cleanup tasks in parallel.
	var wg sync.WaitGroup

	// Shared shutdown context for all timeout-bounded cleanup.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Kill all background shells.
	wg.Go(func() {
		shell.GetBackgroundShellManager().KillAll(shutdownCtx)
	})

	// Shutdown all LSP clients.
	wg.Go(func() {
		app.LSPManager.KillAll(shutdownCtx)
	})

	// Call all cleanup functions.
	for _, cleanup := range app.cleanupFuncs {
		if cleanup != nil {
			wg.Go(func() {
				if err := cleanup(shutdownCtx); err != nil {
					slog.Error("Failed to cleanup app properly on shutdown", "error", err)
				}
			})
		}
	}
	wg.Wait()

	// Flush and close log file to ensure all logs are written to disk
	_ = log.Close()
}

// --- Store-backed service adapters ---

// storeSessionService wraps *store.SessionStore and adds pubsub to satisfy session.Service.
type storeSessionService struct {
	*pubsub.Broker[session.Session]
	store *store.SessionStore
}

func newStoreSessionService(st *store.Store) *storeSessionService {
	return &storeSessionService{
		Broker: pubsub.NewBroker[session.Session](),
		store:  st.Sessions(),
	}
}

func (s *storeSessionService) Create(ctx context.Context, title string) (session.Session, error) {
	sess, err := s.store.Create(ctx, title)
	if err != nil {
		return session.Session{}, err
	}
	s.Publish(pubsub.CreatedEvent, sess)
	return sess, nil
}

func (s *storeSessionService) Get(ctx context.Context, id string) (session.Session, error) {
	return s.store.Get(ctx, id)
}

func (s *storeSessionService) GetLast(ctx context.Context) (session.Session, error) {
	return s.store.GetLast(ctx)
}

func (s *storeSessionService) List(ctx context.Context) ([]session.Session, error) {
	return s.store.List(ctx)
}

func (s *storeSessionService) Save(ctx context.Context, sess session.Session) (session.Session, error) {
	if err := s.store.Save(ctx, sess); err != nil {
		return session.Session{}, err
	}
	s.Publish(pubsub.UpdatedEvent, sess)
	return sess, nil
}

func (s *storeSessionService) UpdateTitleAndUsage(ctx context.Context, id, title string, p, c, cacheRead, cacheCreation int64, cost float64) error {
	return s.store.UpdateTitleAndUsage(ctx, id, title, p, c, cacheRead, cacheCreation, cost)
}

func (s *storeSessionService) Rename(ctx context.Context, id, title string) error {
	return s.store.Rename(ctx, id, title)
}

func (s *storeSessionService) Delete(ctx context.Context, id string) error {
	sess, _ := s.store.Get(ctx, id)
	if err := s.store.Delete(ctx, id); err != nil {
		return err
	}
	if sess.ID != "" {
		s.Publish(pubsub.DeletedEvent, sess)
	}
	return nil
}

func (s *storeSessionService) CreateTitleSession(ctx context.Context, pid string) (session.Session, error) {
	now := time.Now().Unix()
	sess := session.Session{ID: "title-" + pid, ParentSessionID: pid, Title: "Generate a title", CreatedAt: now, UpdatedAt: now}
	s.store.Save(ctx, sess)
	s.Publish(pubsub.CreatedEvent, sess)
	return sess, nil
}

func (s *storeSessionService) CreateTaskSession(ctx context.Context, tool, pid, title string) (session.Session, error) {
	now := time.Now().Unix()
	// Generate a unique session ID using UUID, but keep the toolCallID as metadata
	// for traceability. Using UUID prevents collisions when the same toolCallID
	// is reused across retries or different messages.
	sess := session.Session{
		ID:              fmt.Sprintf("%s-%s", tool, uuid.New().String()),
		ParentSessionID: pid,
		Title:           title,
		CreatedAt:       now,
		UpdatedAt:       now,
		AgentToolCallID: tool, // Store original toolCallID for traceability
	}
	if err := s.store.Save(ctx, sess); err != nil {
		return session.Session{}, err
	}
	s.Publish(pubsub.CreatedEvent, sess)
	return sess, nil
}

func (s *storeSessionService) CreateAgentToolSessionID(mID, tcID string) string {
	return fmt.Sprintf("%s$$%s", mID, tcID)
}

func (s *storeSessionService) ParseAgentToolSessionID(id string) (string, string, bool) {
	parts := strings.Split(id, "$$")
	if len(parts) != 2 {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func (s *storeSessionService) IsAgentToolSession(id string) bool {
	_, _, ok := s.ParseAgentToolSessionID(id)
	return ok
}

func (s *storeSessionService) IncrementCost(ctx context.Context, id string, delta float64) error {
	return s.store.IncrementCost(ctx, id, delta)
}

// storeMessageService wraps *store.MessageStore.
type storeMessageService struct {
	*pubsub.Broker[message.Message]
	store     *store.MessageStore
	debouncer *messageUpdateDebouncer
}

func newStoreMessageService(st *store.Store) *storeMessageService {
	svc := &storeMessageService{
		Broker: pubsub.NewBroker[message.Message](),
		store:  st.Messages(),
	}
	svc.debouncer = newMessageUpdateDebouncer(svc)
	return svc
}

func (s *storeMessageService) Create(ctx context.Context, sid string, params message.CreateMessageParams) (message.Message, error) {
	msg, err := s.store.Create(ctx, sid, params)
	if err != nil {
		return message.Message{}, err
	}
	s.Publish(pubsub.CreatedEvent, msg.Clone())
	return msg, nil
}

func (s *storeMessageService) Update(ctx context.Context, msg message.Message) error {
	return s.debouncer.Update(ctx, msg)
}

func (s *storeMessageService) Get(ctx context.Context, id string) (message.Message, error) {
	return s.store.Get(ctx, id)
}

func (s *storeMessageService) List(ctx context.Context, sid string) ([]message.Message, error) {
	return s.store.List(ctx, sid)
}

func (s *storeMessageService) ListUserMessages(ctx context.Context, sid string) ([]message.Message, error) {
	return s.store.ListUserMessages(ctx, sid)
}

func (s *storeMessageService) ListAllUserMessages(ctx context.Context) ([]message.Message, error) {
	return s.store.ListAllUserMessages(ctx)
}

func (s *storeMessageService) Delete(ctx context.Context, id string) error {
	msg, _ := s.store.Get(ctx, id)
	if err := s.store.Delete(ctx, id); err != nil {
		return err
	}
	if msg.ID != "" {
		s.Publish(pubsub.DeletedEvent, msg.Clone())
	}
	return nil
}

func (s *storeMessageService) DeleteSessionMessages(ctx context.Context, sid string) error {
	return s.store.DeleteSessionMessages(ctx, sid)
}

// storeHistoryService wraps *store.FileHistoryStore.
type storeHistoryService struct {
	*pubsub.Broker[history.File]
	store *store.FileHistoryStore
}

func newStoreHistoryService(st *store.Store) *storeHistoryService {
	return &storeHistoryService{
		Broker: pubsub.NewBroker[history.File](),
		store:  st.Files(),
	}
}

func (s *storeHistoryService) Create(ctx context.Context, sid, path, content string) (history.File, error) {
	f, err := s.store.Create(ctx, sid, path, content)
	if err != nil {
		return history.File{}, err
	}
	s.Publish(pubsub.CreatedEvent, f)
	return f, nil
}

func (s *storeHistoryService) CreateVersion(ctx context.Context, sid, path, content string) (history.File, error) {
	f, err := s.store.CreateVersion(ctx, sid, path, content)
	if err != nil {
		return history.File{}, err
	}
	s.Publish(pubsub.CreatedEvent, f)
	return f, nil
}

func (s *storeHistoryService) Get(ctx context.Context, id string) (history.File, error) {
	return s.store.Get(ctx, id)
}

func (s *storeHistoryService) GetByPathAndSession(ctx context.Context, path, sid string) (history.File, error) {
	return s.store.GetByPathAndSession(ctx, path, sid)
}

func (s *storeHistoryService) ListBySession(ctx context.Context, sid string) ([]history.File, error) {
	return s.store.ListBySession(ctx, sid)
}

func (s *storeHistoryService) ListLatestSessionFiles(ctx context.Context, sid string) ([]history.File, error) {
	return s.store.ListLatestSessionFiles(ctx, sid)
}

func (s *storeHistoryService) Delete(ctx context.Context, id string) error {
	f, _ := s.store.Get(ctx, id)
	if err := s.store.Delete(ctx, id); err != nil {
		return err
	}
	if f.ID != "" {
		s.Publish(pubsub.DeletedEvent, f)
	}
	return nil
}

func (s *storeHistoryService) DeleteSessionFiles(ctx context.Context, sid string) error {
	return s.store.DeleteSessionFiles(ctx, sid)
}

// storeFileTrackerService wraps *store.FileTrackerStore.
type storeFileTrackerService struct {
	store *store.FileTrackerStore
}

func newStoreFileTrackerService(st *store.Store) *storeFileTrackerService {
	return &storeFileTrackerService{store: st.Tracker()}
}

func (s *storeFileTrackerService) RecordRead(ctx context.Context, sid, path string) {
	s.store.RecordRead(ctx, sid, path)
}

func (s *storeFileTrackerService) LastReadTime(ctx context.Context, sid, path string) time.Time {
	return s.store.LastReadTime(ctx, sid, path)
}

func (s *storeFileTrackerService) ListReadFiles(ctx context.Context, sid string) ([]string, error) {
	return s.store.ListReadFiles(ctx, sid)
}

func (s *storeFileTrackerService) DeleteSession(ctx context.Context, sid string) {
	s.store.DeleteSession(ctx, sid)
}

// storeMemoryService wraps *store.MemoryStore.
type storeMemoryService struct {
	store *store.MemoryStore
}

func newStoreMemoryService(st *store.Store) *storeMemoryService {
	return &storeMemoryService{store: st.Memories()}
}

func (s *storeMemoryService) AddMemory(ctx context.Context, app, user, mem string, topics []string, kind memory.Kind, eventTime *time.Time, participants []string, location string) error {
	return s.store.AddMemory(ctx, app, user, mem, topics, kind, eventTime, participants, location)
}

func (s *storeMemoryService) UpdateMemory(ctx context.Context, app, user, mid, mem string, topics []string, kind memory.Kind, eventTime *time.Time, participants []string, location string) error {
	return s.store.UpdateMemory(ctx, app, user, mid, mem, topics, kind, eventTime, participants, location)
}

func (s *storeMemoryService) DeleteMemory(ctx context.Context, app, user, mid string) error {
	return s.store.DeleteMemory(ctx, app, user, mid)
}

func (s *storeMemoryService) ClearMemories(ctx context.Context, app, user string) error {
	return s.store.ClearMemories(ctx, app, user)
}

func (s *storeMemoryService) ReadMemories(ctx context.Context, app, user string, limit int) ([]*memory.Entry, error) {
	return s.store.ReadMemories(ctx, app, user, limit)
}

func (s *storeMemoryService) SearchMemories(ctx context.Context, app, user, query string, limit int) ([]*memory.Entry, error) {
	return s.store.SearchMemories(ctx, app, user, query, limit)
}
func (s *storeMemoryService) Tools() []fantasy.AgentTool {
	return memory.ToolsFor(s, memory.DefaultToolOptions())
}
func (s *storeMemoryService) Close() error { return s.store.Close() }
