package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/spf13/cobra"

	"github.com/package-register/mocode/internal/integration/authhandler"
	"github.com/package-register/mocode/internal/transport/workspace"
)

var loginCmd = &cobra.Command{
	Aliases: []string{"login"},
	Use:     "auth [wechat]",
	Short:   "Authenticate mocode integrations",
	Long: `Authenticate mocode with a supported integration.
Available platforms are: wechat.`,
	Example: `
# Authenticate WeChat bot
mocode auth wechat
  `,
	ValidArgsFunction: func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return authhandler.IDs(), cobra.ShellCompDirectiveNoFileComp
	},
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) == 1 {
			return nil
		}
		_ = cmd.Help()
		return fmt.Errorf("platform required: %s", strings.Join(authhandler.IDs(), ", "))
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		ws, cleanup, err := setupLocalWorkspace(cmd)
		if err != nil {
			return err
		}
		defer cleanup()

		appWs, ok := ws.(*workspace.AppWorkspace)
		if !ok {
			return fmt.Errorf("unexpected workspace type")
		}

		progressEnabled := ws.Config().Options.Progress == nil || *ws.Config().Options.Progress
		if progressEnabled && supportsProgressBar() {
			_, _ = fmt.Fprintf(os.Stderr, ansi.SetIndeterminateProgressBar)
			defer func() { _, _ = fmt.Fprintf(os.Stderr, ansi.ResetProgressBar) }()
		}

		provider := args[0]

		handler, ok := authhandler.Get(provider)
		if !ok {
			return fmt.Errorf("unknown platform: %s (available: %v)", provider, authhandler.IDs())
		}

		return handler.Login(getLoginContext(), authhandler.Env{
			Store:     appWs.Store(),
			Workspace: ws,
			Stdout:    os.Stdout,
			Stderr:    os.Stderr,
		})
	},
}

func getLoginContext() context.Context {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, os.Kill)
	go func() {
		<-ctx.Done()
		cancel()
		os.Exit(1)
	}()
	return ctx
}

func waitEnter() {
	_, _ = fmt.Scanln()
}
