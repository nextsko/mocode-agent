package cmd

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	fang "charm.land/fang/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/exp/charmtone"
	xstrings "github.com/charmbracelet/x/exp/strings"
	"github.com/charmbracelet/x/term"
	"github.com/package-register/mocode/internal/app"
	"github.com/package-register/mocode/internal/client"
	"github.com/package-register/mocode/internal/config"
	"github.com/package-register/mocode/internal/config/projects"
	mocodelog "github.com/package-register/mocode/internal/log"
	"github.com/package-register/mocode/internal/proto"
	"github.com/package-register/mocode/internal/server"
	"github.com/package-register/mocode/internal/session"
	"github.com/package-register/mocode/internal/ui/common"
	ui "github.com/package-register/mocode/internal/ui/model"
	"github.com/package-register/mocode/internal/version"
	"github.com/package-register/mocode/internal/workspace"
	"github.com/spf13/cobra"
)

var clientHost string

func init() {
	rootCmd.CompletionOptions.DisableDefaultCmd = true
	rootCmd.SetHelpFunc(printHelp)
	rootCmd.PersistentFlags().StringP("cwd", "c", "", "Current working directory")
	rootCmd.PersistentFlags().StringP("data-dir", "D", "", "Custom mocode data directory")
	rootCmd.PersistentFlags().BoolP("debug", "d", false, "Debug")
	rootCmd.PersistentFlags().StringVarP(&clientHost, "host", "H", server.DefaultHost(), "Connect to a specific mocode server host (for advanced users)")
	rootCmd.Flags().BoolP("help", "h", false, "Help")
	rootCmd.Flags().StringP("session", "s", "", "Continue a previous session by ID")
	rootCmd.Flags().BoolP("continue", "C", false, "Continue the most recent session")
	rootCmd.MarkFlagsMutuallyExclusive("session", "continue")
	rootCmd.PersistentFlags().BoolP("yolo", "y", false, "Automatically accept all permissions (dangerous mode)")

	rootCmd.AddCommand(
		runCmd,
		doctorCmd,
		dirsCmd,
		projectsCmd,
		updateProvidersCmd,
		gatewayCmd,
		logsCmd,
		schemaCmd,
		loginCmd,
		sessionCmd,
		migrateCmd,
	)
}

var rootCmd = &cobra.Command{
	Use:   "mocode",
	Short: "Terminal AI coding assistant",
	Long:  "Terminal AI coding assistant for coding, automation, and agent workflows.",
	Example: `
# Run in interactive mode
mocode

# Run non-interactively
mocode run "Guess my 5 favorite Pokemon"

# Run a non-interactively with pipes and redirection
cat README.md | mocode run "make this more glamorous" > GLAMOROUS_README.md

# Run with debug logging in a specific directory
mocode --debug --cwd /path/to/project

# Run in yolo mode (auto-accept all permissions; use with care)
mocode --yolo

# Run with custom data directory
mocode --data-dir /path/to/custom/.mocode

# Continue a previous session
mocode --session {session-id}

# Continue the most recent session
mocode --continue
  `,
	RunE: func(cmd *cobra.Command, args []string) error {
		sessionID, _ := cmd.Flags().GetString("session")
		continueLast, _ := cmd.Flags().GetBool("continue")

		ws, cleanup, err := setupWorkspaceWithProgressBar(cmd)
		if err != nil {
			return err
		}
		defer cleanup()

		if sessionID != "" {
			sess, err := resolveWorkspaceSessionID(cmd.Context(), ws, sessionID)
			if err != nil {
				return err
			}
			sessionID = sess.ID
		}

		com := common.DefaultCommon(ws)
		model := ui.New(com, sessionID, continueLast)

		var env uv.Environ = os.Environ()
		program := tea.NewProgram(
			model,
			tea.WithEnvironment(env),
			tea.WithContext(cmd.Context()),
			tea.WithFilter(ui.MouseEventFilter),
		)
		go ws.Subscribe(program)

		if finalModel, runErr := program.Run(); runErr != nil {
			slog.Error("TUI run error", "error", runErr)
			return errors.New("mocode crashed. If metrics are enabled, we were notified about it. If you'd like to report it, please copy the stacktrace above and open an issue at https://github.com/package-register/mocode/issues/new?template=bug.yml")
		} else if m, ok := finalModel.(*ui.UI); ok {
			if summary := m.QuitSummary(); summary != "" {
				fmt.Println(summary)
			}
		}
		return nil
	},
}

func printHelp(cmd *cobra.Command, _ []string) {
	if cmd == rootCmd {
		fmt.Fprint(cmd.OutOrStdout(), rootHelp())
		return
	}
	printCommandHelp(cmd)
}

func rootHelp() string {
	return `MOCODE
  Terminal AI coding assistant.

USAGE
  mocode [flags]
  mocode <command> [args]

START
  mocode                         Open the interactive TUI
  mocode --continue              Continue the latest session
  mocode --session <id>          Continue a specific session
  mocode run "fix this bug"      Run one prompt and exit

COMMANDS
  run        Run a non-interactive prompt
  auth       Authenticate wechat or minimax
  gateway    Run the WeChat bot gateway
  quota      Show MiniMax quota usage
  doctor     Run local environment diagnostics
  session    List, show, rename, or delete sessions
  models     List configured models
  projects   List known project directories
  dirs       Print config/data directories
  stats      Show usage statistics
  logs       View debug logs

FLAGS
  -c, --cwd <dir>          Run from another working directory
  -D, --data-dir <dir>     Use another mocode data directory
  -s, --session <id>       Continue a session by ID
  -C, --continue           Continue the most recent session
  -y, --yolo               Auto-accept permissions
  -H, --host <addr>        Connect to server host (advanced)
  -d, --debug              Enable debug logs
  -v, --version            Show version
  -h, --help               Show this help

MORE
  mocode <command> --help  Show detailed command help
  In the TUI: /help /models /context /quit

`
}

func printCommandHelp(cmd *cobra.Command) {
	out := cmd.OutOrStdout()
	if cmd.Long != "" {
		fmt.Fprintln(out, strings.TrimSpace(cmd.Long))
	} else if cmd.Short != "" {
		fmt.Fprintln(out, strings.TrimSpace(cmd.Short))
	}
	fmt.Fprintf(out, "\nUSAGE\n  %s\n", cmd.UseLine())

	if cmd.HasAvailableSubCommands() {
		fmt.Fprintln(out, "\nCOMMANDS")
		for _, c := range cmd.Commands() {
			if !c.IsAvailableCommand() && c.Name() != "help" {
				continue
			}
			fmt.Fprintf(out, "  %-12s %s\n", c.Name(), c.Short)
		}
	}

	if cmd.HasExample() {
		fmt.Fprintf(out, "\nEXAMPLES\n%s\n", strings.TrimSpace(cmd.Example))
	}

	if cmd.HasAvailableLocalFlags() {
		fmt.Fprintln(out, "\nFLAGS")
		fmt.Fprint(out, cmd.LocalFlags().FlagUsagesWrapped(88))
	}

	if cmd.HasAvailableInheritedFlags() {
		fmt.Fprintln(out, "\nGLOBAL FLAGS")
		fmt.Fprint(out, cmd.InheritedFlags().FlagUsagesWrapped(88))
	}
}

var heartbit = lipgloss.NewStyle().Foreground(charmtone.Dolly).SetString(`
    閳诲嫧鏉介埢鍕ㄦ澖閳诲嫧鏉介埢鍕ㄦ澖    閳诲嫧鏉介埢鍕ㄦ澖閳诲嫧鏉介埢鍕ㄦ澖
  閳诲牃鏋呴埢鍫氭瀰閳诲牃鏋呴埢鍫氭瀰閳诲牃鏋呴埢? 閳诲牃鏋呴埢鍫氭瀰閳诲牃鏋呴埢鍫氭瀰閳诲牃鏋呴埢?閳诲牃鏋呴埢鍫氭瀰閳诲牃鏋呴埢鍫氭瀰閳诲牃鏋呴埢鍫氭瀰閳诲牃鏋呴埢鍫氭瀰閳诲牃鏋呴埢鍫氭瀰閳诲牃鏋呴埢鍫氭瀰閳诲牃鏋呴埢鍫氭瀰
閳诲牃鏋呴埢鍫氭瀰閳诲牃鏋呴埢鍫氭瀰閳诲牃鏋呴埢鍫氭瀰閳诲牃鏋呴埢鍫氭瀰閳诲牃鏋呴埢鍫氭瀰閳诲牃鏋呴埢鍫氭瀰閳诲牃鏋呴埢鍫氭瀰
閳诲牃鏋呴埢鍫氭瀰閳诲牃鏋呴埢鍫氭瀰閳诲牃鏋呴埢鈧埢鍫氭瀰閳诲牃鏋呴埢鍫氭瀰閳烩偓閳诲牃鏋呴埢鍫氭瀰閳诲牃鏋呴埢鍫氭瀰閳诲牃鏋?閳诲牃鏋呴埢鍫氭瀰閳诲牃鏋呴埢鍫氭瀰閳诲牃鏋?閳诲牃鏋呴埢鍫氭瀰閳诲牃鏋?閳诲牃鏋呴埢鍫氭瀰閳诲牃鏋呴埢鍫氭瀰閳诲牃鏋?閳烩偓閳烩偓閳诲牃鏋呴埢鍫氭瀰閳诲牃鏋呴埢鍕ㄦ瀰閳诲牃鏋呴埢鍫氭澖閳诲嫧鏋呴埢鍫氭瀰閳诲牃鏉介埢鍫氭瀰閳诲牃鏋呴埢鍫氭瀰閳烩偓閳烩偓
  閳诲牃鏋呴埢鍫氭瀰閳诲牃鏋呴埢鍫氭瀰閳诲牃鏋呴埢鍫氭瀰閳诲牃鏋呴埢鍫氭瀰閳诲牃鏋呴埢鍫氭瀰閳诲牃鏋呴埢鍫氭瀰
    閳诲牃鏋呴埢鍫氭瀰閳诲牃鏋呴埢鍫氭瀰閳诲牃鏋呴埢鍫氭瀰閳诲牃鏋呴埢鍫氭瀰閳诲牃鏋呴埢鍫氭瀰
       閳烩偓閳烩偓閳诲牃鏋呴埢鍫氭瀰閳诲牃鏋呴埢鍫氭瀰閳诲牃鏋呴埢鈧埢鈧?           閳烩偓閳烩偓閳烩偓閳烩偓閳烩偓閳烩偓
`)

// copied from cobra:
const defaultVersionTemplate = `{{with .DisplayName}}{{printf "%s " .}}{{end}}{{printf "version %s" .Version}}
`

func Execute() {
	args := os.Args[1:]
	if wantsRootHelp(args) {
		fmt.Fprint(os.Stdout, rootHelp())
		return
	}
	if wantsCommandHelp(args) {
		executePlainHelp(args)
		return
	}

	// FIXME: config.Load uses slog internally during provider resolution,
	// but the file-based logger isn't set up until after config is loaded
	// (because the log path depends on the data directory from config).
	// This creates a window where slog calls in config.Load leak to
	// stderr. We discard early logs here as a workaround. The proper
	// fix is to remove slog calls from config.Load and have it return
	// warnings/diagnostics instead of logging them as a side effect.
	slog.SetDefault(slog.New(slog.DiscardHandler))

	// NOTE: very hacky: we create a colorprofile writer with STDOUT, then make
	// it forward to a bytes.Buffer, write the colored heartbit to it, and then
	// finally prepend it in the version template.
	// Unfortunately cobra doesn't give us a way to set a function to handle
	// printing the version, and PreRunE runs after the version is already
	// handled, so that doesn't work either.
	// This is the only way I could find that works relatively well.
	if term.IsTerminal(os.Stdout.Fd()) {
		var b bytes.Buffer
		w := colorprofile.NewWriter(os.Stdout, os.Environ())
		w.Forward = &b
		_, _ = w.WriteString(heartbit.String())
		rootCmd.SetVersionTemplate(b.String() + "\n" + defaultVersionTemplate)
	}
	if err := fang.Execute(
		context.Background(),
		rootCmd,
		fang.WithVersion(version.Version),
		fang.WithNotifySignal(os.Interrupt),
		fang.WithoutCompletions(),
	); err != nil {
		os.Exit(1)
	}
}

func executePlainHelp(args []string) {
	rootCmd.SetArgs(args)
	rootCmd.SetHelpFunc(printHelp)
	rootCmd.SilenceUsage = true
	rootCmd.SilenceErrors = true
	rootCmd.Version = version.Version
	if err := rootCmd.ExecuteContext(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func wantsRootHelp(args []string) bool {
	if len(args) == 0 {
		return false
	}

	skipNext := false
	for i, arg := range args {
		if skipNext {
			skipNext = false
			continue
		}

		switch arg {
		case "--":
			return false
		case "help":
			return i == 0 && len(args) == 1
		case "-h", "--help":
			return true
		}

		if strings.HasPrefix(arg, "-") {
			flagName := strings.TrimLeft(arg, "-")
			if name, _, ok := strings.Cut(flagName, "="); ok {
				flagName = name
			}
			switch flagName {
			case "c", "cwd", "D", "data-dir", "H", "host", "s", "session":
				if !strings.Contains(arg, "=") {
					skipNext = true
				}
			}
			continue
		}

		return false
	}

	return false
}

func wantsCommandHelp(args []string) bool {
	if len(args) < 2 {
		return false
	}
	last := args[len(args)-1]
	if last != "-h" && last != "--help" {
		return false
	}
	first := args[0]
	return first != "-h" && first != "--help" && first != "help" && !strings.HasPrefix(first, "-")
}

// supportsProgressBar tries to determine whether the current terminal supports
// progress bars by looking into environment variables.
func supportsProgressBar() bool {
	if !term.IsTerminal(os.Stderr.Fd()) {
		return false
	}
	termProg := os.Getenv("TERM_PROGRAM")
	_, isWindowsTerminal := os.LookupEnv("WT_SESSION")

	return isWindowsTerminal || xstrings.ContainsAnyOf(strings.ToLower(termProg), "ghostty", "iterm2", "rio")
}

// useClientServer returns true when the client/server architecture is
// enabled via the MOCODE_CLIENT_SERVER environment variable.
func useClientServer() bool {
	v, _ := strconv.ParseBool(os.Getenv("MOCODE_CLIENT_SERVER"))
	return v
}

// setupWorkspaceWithProgressBar wraps setupWorkspace with an optional
// terminal progress bar shown during initialization.
func setupWorkspaceWithProgressBar(cmd *cobra.Command) (workspace.Workspace, func(), error) {
	showProgress := supportsProgressBar()
	if showProgress {
		_, _ = fmt.Fprintf(os.Stderr, ansi.SetIndeterminateProgressBar)
	}

	ws, cleanup, err := setupWorkspace(cmd)

	if showProgress {
		_, _ = fmt.Fprintf(os.Stderr, ansi.ResetProgressBar)
	}

	return ws, cleanup, err
}

// setupWorkspace returns a Workspace and cleanup function. When
// MOCODE_CLIENT_SERVER=1, it connects to a server process and returns a
// ClientWorkspace. Otherwise it creates an in-process app.App and
// returns an AppWorkspace.
func setupWorkspace(cmd *cobra.Command) (workspace.Workspace, func(), error) {
	if useClientServer() {
		return setupClientServerWorkspace(cmd)
	}
	return setupLocalWorkspace(cmd)
}

// setupLocalWorkspace creates an in-process app.App and wraps it in an
// AppWorkspace.
func setupLocalWorkspace(cmd *cobra.Command) (workspace.Workspace, func(), error) {
	debug, _ := cmd.Flags().GetBool("debug")
	yolo, _ := cmd.Flags().GetBool("yolo")
	dataDir, _ := cmd.Flags().GetString("data-dir")
	ctx := cmd.Context()

	cwd, err := ResolveCwd(cmd)
	if err != nil {
		return nil, nil, err
	}

	store, err := config.Init(cwd, dataDir, debug)
	if err != nil {
		return nil, nil, err
	}

	cfg := store.Config()
	store.Overrides().SkipPermissionRequests = yolo

	if err := os.MkdirAll(cfg.Options.DataDirectory, 0o700); err != nil {
		return nil, nil, fmt.Errorf("failed to create data directory: %q %w", cfg.Options.DataDirectory, err)
	}

	gitIgnorePath := filepath.Join(cfg.Options.DataDirectory, ".gitignore")
	if _, err := os.Stat(gitIgnorePath); os.IsNotExist(err) {
		if err := os.WriteFile(gitIgnorePath, []byte("*\n"), 0o600); err != nil {
			return nil, nil, fmt.Errorf("failed to create .gitignore file: %q %w", gitIgnorePath, err)
		}
	}

	if err := projects.Register(cwd, cfg.Options.DataDirectory); err != nil {
		slog.Warn("Failed to register project", "error", err)
	}

	logFile := mocodelog.MainLogPath(cfg.Options.DataDirectory)
	mocodelog.Setup(logFile, debug)

	appInstance, err := app.New(ctx, store)
	if err != nil {
		slog.Error("Failed to create app instance", "error", err)
		return nil, nil, err
	}

	ws := workspace.NewAppWorkspace(appInstance, store)
	cleanup := func() { appInstance.Shutdown() }
	return ws, cleanup, nil
}

// setupClientServerWorkspace connects to a server process and wraps the
// result in a ClientWorkspace.
func setupClientServerWorkspace(cmd *cobra.Command) (workspace.Workspace, func(), error) {
	c, protoWs, cleanupServer, err := connectToServer(cmd)
	if err != nil {
		return nil, nil, err
	}

	clientWs := workspace.NewClientWorkspace(c, *protoWs)

	if protoWs.Config.IsConfigured() {
		if err := clientWs.InitCoderAgent(cmd.Context()); err != nil {
			slog.Error("Failed to initialize coder agent", "error", err)
		}
	}

	return clientWs, cleanupServer, nil
}

// connectToServer ensures the server is running, creates a client and
// workspace, and returns a cleanup function that deletes the workspace.
func connectToServer(cmd *cobra.Command) (*client.Client, *proto.Workspace, func(), error) {
	hostURL, err := server.ParseHostURL(clientHost)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("invalid host URL: %v", err)
	}

	if err := ensureServer(cmd, hostURL); err != nil {
		return nil, nil, nil, err
	}

	debug, _ := cmd.Flags().GetBool("debug")
	yolo, _ := cmd.Flags().GetBool("yolo")
	dataDir, _ := cmd.Flags().GetString("data-dir")
	ctx := cmd.Context()

	cwd, err := ResolveCwd(cmd)
	if err != nil {
		return nil, nil, nil, err
	}

	c, err := client.NewClient(cwd, hostURL.Scheme, hostURL.Host)
	if err != nil {
		return nil, nil, nil, err
	}

	wsReq := proto.Workspace{
		Path:    cwd,
		DataDir: dataDir,
		Debug:   debug,
		YOLO:    yolo,
		Version: version.Version,
		Env:     os.Environ(),
	}

	ws, err := c.CreateWorkspace(ctx, wsReq)
	if err != nil {
		// The server socket may exist before the HTTP handler is ready.
		// Retry a few times with a short backoff.
		for range 5 {
			select {
			case <-ctx.Done():
				return nil, nil, nil, ctx.Err()
			case <-time.After(200 * time.Millisecond):
			}
			ws, err = c.CreateWorkspace(ctx, wsReq)
			if err == nil {
				break
			}
		}
		if err != nil {
			return nil, nil, nil, fmt.Errorf("failed to create workspace: %v", err)
		}
	}

	if ws.Config != nil {
		logFile := mocodelog.MainLogPath(ws.Config.Options.DataDirectory)
		mocodelog.Setup(logFile, debug)
	}

	cleanup := func() { _ = c.DeleteWorkspace(context.Background(), ws.ID) }
	return c, ws, cleanup, nil
}

// ensureServer auto-starts a detached server if the socket file does not
// exist. When the socket exists, it verifies that the running server
// version matches the client; on mismatch it shuts down the old server
// and starts a fresh one.
func ensureServer(cmd *cobra.Command, hostURL *url.URL) error {
	switch hostURL.Scheme {
	case "unix", "npipe":
		needsStart := false
		if _, err := os.Stat(hostURL.Host); err != nil && errors.Is(err, fs.ErrNotExist) { //nolint:gosec // User-selected local socket path is intentional.
			needsStart = true
		} else if err == nil {
			if err := restartIfStale(cmd, hostURL); err != nil {
				slog.Warn("Failed to check server version, restarting", "error", err)
				needsStart = true
			}
		}

		if needsStart {
			if err := startDetachedServer(cmd); err != nil {
				return err
			}
		}

		var err error
		for range 10 {
			_, err = os.Stat(hostURL.Host) //nolint:gosec // User-selected local socket path is intentional.
			if err == nil {
				break
			}
			select {
			case <-cmd.Context().Done():
				return cmd.Context().Err()
			case <-time.After(100 * time.Millisecond):
			}
		}
		if err != nil {
			return fmt.Errorf("failed to initialize mocode server: %v", err)
		}
	}

	return nil
}

// restartIfStale checks whether the running server matches the current
// client version. When they differ, it sends a shutdown command and
// removes the stale socket so the caller can start a fresh server.
func restartIfStale(cmd *cobra.Command, hostURL *url.URL) error {
	c, err := client.NewClient("", hostURL.Scheme, hostURL.Host)
	if err != nil {
		return err
	}
	vi, err := c.VersionInfo(cmd.Context())
	if err != nil {
		return err
	}
	if vi.Version == version.Version {
		return nil
	}
	slog.Info(
		"Server version mismatch, restarting",
		"server", vi.Version,
		"client", version.Version,
	)
	_ = c.ShutdownServer(cmd.Context())
	// Give the old process a moment to release the socket.
	for range 20 {
		if _, err := os.Stat(hostURL.Host); errors.Is(err, fs.ErrNotExist) { //nolint:gosec // User-selected local socket path is intentional.
			break
		}
		select {
		case <-cmd.Context().Done():
			return cmd.Context().Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	// Force-remove if the socket is still lingering.
	_ = os.Remove(hostURL.Host) //nolint:gosec // User-selected local socket path is intentional.
	return nil
}

var safeNameRegexp = regexp.MustCompile(`[^a-zA-Z0-9._-]`)

func startDetachedServer(cmd *cobra.Command) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %v", err)
	}

	safeClientHost := safeNameRegexp.ReplaceAllString(clientHost, "_")
	chDir := filepath.Join(config.GlobalCacheDir(), "server-"+safeClientHost)
	if err := os.MkdirAll(chDir, 0o700); err != nil {
		return fmt.Errorf("failed to create server working directory: %v", err)
	}

	cmdArgs := []string{"server"}
	if clientHost != server.DefaultHost() {
		cmdArgs = append(cmdArgs, "--host", clientHost)
	}

	c := exec.CommandContext(cmd.Context(), exe, cmdArgs...)
	stdoutPath := filepath.Join(chDir, "stdout.log")
	stderrPath := filepath.Join(chDir, "stderr.log")
	detachProcess(c)

	stdout, err := os.Create(stdoutPath)
	if err != nil {
		return fmt.Errorf("failed to create stdout log file: %v", err)
	}
	defer stdout.Close()
	c.Stdout = stdout

	stderr, err := os.Create(stderrPath)
	if err != nil {
		return fmt.Errorf("failed to create stderr log file: %v", err)
	}
	defer stderr.Close()
	c.Stderr = stderr

	if err := c.Start(); err != nil {
		return fmt.Errorf("failed to start mocode server: %v", err)
	}

	if err := c.Process.Release(); err != nil {
		return fmt.Errorf("failed to detach mocode server process: %v", err)
	}

	return nil
}

func MaybePrependStdin(prompt string) (string, error) {
	if term.IsTerminal(os.Stdin.Fd()) {
		return prompt, nil
	}
	fi, err := os.Stdin.Stat()
	if err != nil {
		return prompt, err
	}
	// Check if stdin is a named pipe ( | ) or regular file ( < ).
	if fi.Mode()&os.ModeNamedPipe == 0 && !fi.Mode().IsRegular() {
		return prompt, nil
	}
	bts, err := io.ReadAll(os.Stdin)
	if err != nil {
		return prompt, err
	}
	return string(bts) + "\n\n" + prompt, nil
}

// resolveWorkspaceSessionID resolves a session ID that may be a full
// UUID, full hash, or hash prefix. Works against the Workspace
// interface so both local and client/server paths get hash prefix
// support.
func resolveWorkspaceSessionID(ctx context.Context, ws workspace.Workspace, id string) (session.Session, error) {
	if sess, err := ws.GetSession(ctx, id); err == nil {
		return sess, nil
	}

	sessions, err := ws.ListSessions(ctx)
	if err != nil {
		return session.Session{}, err
	}

	var matches []session.Session
	for _, s := range sessions {
		hash := session.HashID(s.ID)
		if hash == id || strings.HasPrefix(hash, id) {
			matches = append(matches, s)
		}
	}

	switch len(matches) {
	case 0:
		return session.Session{}, fmt.Errorf("session not found: %s", id)
	case 1:
		return matches[0], nil
	default:
		return session.Session{}, fmt.Errorf("session ID %q is ambiguous (%d matches)", id, len(matches))
	}
}

func ResolveCwd(cmd *cobra.Command) (string, error) {
	cwd, _ := cmd.Flags().GetString("cwd")
	if cwd != "" {
		err := os.Chdir(cwd)
		if err != nil {
			return "", fmt.Errorf("failed to change directory: %v", err)
		}
		return cwd, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get current working directory: %v", err)
	}
	return cwd, nil
}

func createDotmocodeDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("failed to create data directory: %q %w", dir, err)
	}

	gitIgnorePath := filepath.Join(dir, ".gitignore")
	content, err := os.ReadFile(gitIgnorePath)

	// create or update if old version
	if os.IsNotExist(err) || string(content) == oldGitIgnore {
		if err := os.WriteFile(gitIgnorePath, []byte(defaultGitIgnore), 0o600); err != nil {
			return fmt.Errorf("failed to create .gitignore file: %q %w", gitIgnorePath, err)
		}
	}

	return nil
}

//go:embed gitignore/old
var oldGitIgnore string

//go:embed gitignore/default
var defaultGitIgnore string
