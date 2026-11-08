package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/nextsko/mocode-agent/internal/transport/acp"
	"github.com/nextsko/mocode-agent/internal/transport/workspace"
	"github.com/nextsko/mocode-agent/internal/util/version"
)

var acpCmd = &cobra.Command{
	Use:   "acp",
	Short: "Serve mocode over the Agent Client Protocol",
	Long: `Serve mocode as an ACP (Agent Client Protocol) agent on stdio.

ACP is JSON-RPC 2.0 with newline-delimited frames. The server implements the
v1 session surface:

  initialize, authenticate, logout
  session/new, session/load, session/resume, session/list,
  session/delete, session/close
  session/set_mode, session/set_config_option
  session/prompt, session/cancel
  session/update notifications (message/reasoning chunks, tool calls,
  plans, available commands, mode/config updates)

Permission requests are forwarded to the client as session/request_permission,
and the usual client capabilities (fs, terminal, elicitation) are available to
callers that need them. Diagnostics go to stderr; stdout carries frames only.

Register it in an ACP client with command "mocode" and arguments "acp".`,
	Example: `
# Run the agent (usually spawned by the client)
mocode acp

# Smoke test: initialize and open a session
printf '%s\n' \
  '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":1,"clientCapabilities":{"fs":{"readTextFile":false,"writeTextFile":false},"terminal":false}}}' \
  '{"jsonrpc":"2.0","id":2,"method":"session/new","params":{"cwd":"/tmp","mcpServers":[]}}' \
  | mocode acp`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		cmd.SilenceUsage = true
		cmd.SilenceErrors = true

		cwd, err := ResolveCwd(cmd)
		if err != nil {
			return err
		}
		debug, _ := cmd.Flags().GetBool("debug")
		yolo, _ := cmd.Flags().GetBool("yolo")

		ws, cleanup, err := setupLocalWorkspaceAt(cmd.Context(), cwd, debug, yolo)
		if err != nil {
			return err
		}
		defer cleanup()
		appWs, ok := ws.(*workspace.AppWorkspace)
		if !ok {
			return fmt.Errorf("workspace does not support ACP serving")
		}
		if !appWs.Config().IsConfigured() {
			return fmt.Errorf("no providers configured - run 'mocode' once to set one up")
		}
		return serveACP(cmd.Context(), appWs, yolo)
	},
}

func serveACP(ctx context.Context, ws *workspace.AppWorkspace, yolo bool) error {
	peer := acp.NewPeer(os.Stdin, os.Stdout)
	runner := newACPAppRunner(ws)
	server := acp.NewServer(peer, runner, acp.Implementation{
		Name: "mocode", Title: "mocode", Version: version.Version,
	})

	if yolo {
		ws.PermissionSetSkipRequests(true)
	} else {
		server.SetPermissionRequester(func(ctx context.Context, sessionID string, toolCall json.RawMessage, options []acp.PermissionOption) (string, bool) {
			raw, err := peer.Call(ctx, "session/request_permission", acp.RequestPermissionRequest{
				SessionID: sessionID,
				ToolCall:  toolCall,
				Options:   options,
			})
			if err != nil {
				return "", true
			}
			var resp acp.RequestPermissionResponse
			if err := json.Unmarshal(raw, &resp); err != nil {
				return "", true
			}
			if resp.Outcome.Outcome != "selected" {
				return "", true
			}
			return resp.Outcome.OptionID, false
		})
		runner.setPermit(func(ctx context.Context, sessionID string, toolCall json.RawMessage) (string, bool, error) {
			option, cancelled := server.RequestPermissionSync(ctx, sessionID, toolCall)
			return option, cancelled, nil
		})
	}

	peer.OnRequest(func(ctx context.Context, method string, params json.RawMessage) (any, *acp.Error) {
		return server.Handle(ctx, method, params)
	})
	peer.OnNotification(server.HandleNotification)

	if err := peer.Serve(ctx); err != nil {
		return fmt.Errorf("acp server: %w", err)
	}
	return nil
}

func init() {
	acpCmd.Flags().Bool("debug", false, "enable debug logging on stderr")
	acpCmd.Flags().Bool("yolo", false, "auto-approve every permission request")
	rootCmd.AddCommand(acpCmd)
}
