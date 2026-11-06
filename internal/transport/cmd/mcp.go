package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/spf13/cobra"

	"github.com/nextsko/mocode-agent/internal/transport/mcp"
	"github.com/nextsko/mocode-agent/internal/transport/workspace"
	"github.com/nextsko/mocode-agent/internal/util/version"
)

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Serve mocode over the Model Context Protocol",
	Long:  `Expose mocode to MCP clients (editors, agent harnesses, automation).`,
}

var mcpServeCmd = &cobra.Command{
	Use:   "serve",
	Short: "Serve mocode as an MCP server over stdio",
	Long: `Serve mocode as a Model Context Protocol server on stdio.

The server speaks newline-delimited JSON-RPC 2.0 and exposes two tools:

  run     run one non-interactive prompt and return its final answer
  models  list the provider/model IDs mocode has configured

stdout carries protocol frames only; diagnostics go to stderr. The server runs
with the working directory and provider configuration of the directory mocode
was started in. A "cwd" argument on the run tool switches to another project.

Register it in an MCP client with command "mocode" and arguments "mcp", "serve".`,
	Example: `
# Start the server (usually spawned by an MCP client, not by hand)
mocode mcp serve

# MCP client configuration (DeepSeek Harness mcp-client, Claude Desktop, ...)
#   command: mocode
#   args:    ["mcp", "serve"]

# A one-shot smoke test: send the handshake and the tool list
printf '%s\n' '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}' \
  '{"jsonrpc":"2.0","id":2,"method":"tools/list"}' | mocode mcp serve`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		// cobra must never write usage or errors to stdout: it is the
		// protocol channel.
		cmd.SilenceUsage = true
		cmd.SilenceErrors = true

		cwd, err := ResolveCwd(cmd)
		if err != nil {
			return err
		}
		debug, _ := cmd.Flags().GetBool("debug")
		yolo, _ := cmd.Flags().GetBool("yolo")

		runner := newMCPRunner(cmd.Context(), cwd, debug, yolo)
		defer runner.close()

		return mcp.New(runner).Serve(cmd.Context(), os.Stdin, os.Stdout)
	},
}

func init() {
	mcpCmd.AddCommand(mcpServeCmd)
	rootCmd.AddCommand(mcpCmd)
}

// mcpRunner implements mcp.Runner by driving one workspace per project
// directory. Workspaces are created lazily and cached so repeated tool calls in
// the same project reuse the same in-process application.
type mcpRunner struct {
	ctx     context.Context
	baseCWD string
	debug   bool
	yolo    bool

	mu    sync.Mutex
	cache map[string]*mcpWorkspace
	order []string
}

// mcpWorkspace is one cached workspace plus its cleanup function.
type mcpWorkspace struct {
	ws      workspace.Workspace
	cleanup func()
}

// newMCPRunner creates a runner rooted at baseCWD.
func newMCPRunner(ctx context.Context, baseCWD string, debug, yolo bool) *mcpRunner {
	return &mcpRunner{
		ctx:     ctx,
		baseCWD: baseCWD,
		debug:   debug,
		yolo:    yolo,
		cache:   map[string]*mcpWorkspace{},
	}
}

// Version reports the mocode version.
func (r *mcpRunner) Version() string {
	return version.Version
}

// workspaceFor returns (creating on first use) the workspace for cwd.
func (r *mcpRunner) workspaceFor(cwd string) (workspace.Workspace, error) {
	if cwd == "" {
		cwd = r.baseCWD
	}
	abs, err := filepath.Abs(cwd)
	if err != nil {
		return nil, fmt.Errorf("resolve cwd %q: %w", cwd, err)
	}
	if info, err := os.Stat(abs); err != nil || !info.IsDir() {
		return nil, fmt.Errorf("cwd %q is not a directory", abs)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if cached, ok := r.cache[abs]; ok {
		return cached.ws, nil
	}

	ws, cleanup, err := setupLocalWorkspaceAt(r.ctx, abs, r.debug, r.yolo)
	if err != nil {
		return nil, fmt.Errorf("open workspace %q: %w", abs, err)
	}
	r.cache[abs] = &mcpWorkspace{ws: ws, cleanup: cleanup}
	r.order = append(r.order, abs)
	return ws, nil
}

// RunPrompt implements mcp.Runner.
func (r *mcpRunner) RunPrompt(ctx context.Context, prompt, cwd string, out io.Writer) error {
	ws, err := r.workspaceFor(cwd)
	if err != nil {
		return err
	}
	appWs, ok := ws.(*workspace.AppWorkspace)
	if !ok {
		return errors.New("workspace does not support non-interactive runs")
	}
	if !appWs.Config().IsConfigured() {
		return errors.New("no providers configured - please run 'mocode' to set up a provider interactively")
	}
	return appWs.App().RunNonInteractive(ctx, out, prompt, "", "", true, "", false)
}

// ListModels implements mcp.Runner.
func (r *mcpRunner) ListModels(_ context.Context, out io.Writer) error {
	ws, err := r.workspaceFor("")
	if err != nil {
		return err
	}
	view := ws.Config().View()
	if !view.Configured() {
		return errors.New("no providers configured - please run 'mocode' to set up a provider interactively")
	}
	for _, id := range view.ModelIDs() {
		if _, err := fmt.Fprintln(out, id); err != nil {
			return fmt.Errorf("write model line: %w", err)
		}
	}
	return nil
}

// close shuts down every workspace the runner created.
func (r *mcpRunner) close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := len(r.order) - 1; i >= 0; i-- {
		if ws, ok := r.cache[r.order[i]]; ok {
			ws.cleanup()
			delete(r.cache, r.order[i])
		}
	}
	r.order = nil
}
