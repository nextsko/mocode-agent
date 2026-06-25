package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"

	"github.com/package-register/mocode/internal/wechat/gateway"
	"github.com/spf13/cobra"
)

var gatewayCmd = &cobra.Command{
	Use:   "gateway",
	Short: "Run mocode as a long-running bot gateway",
	Long:  "Run mocode as a persistent WeChat bot gateway with yolo permissions enabled.",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, os.Kill)
		defer cancel()
		cmd.SetContext(ctx)

		if err := cmd.Flags().Set("yolo", "true"); err != nil {
			return err
		}

		ws, cleanup, err := setupLocalWorkspace(cmd)
		if err != nil {
			return err
		}
		defer cleanup()

		if !ws.Config().IsConfigured() {
			return fmt.Errorf("no providers configured - please run 'mocode' to set up a provider interactively")
		}

		gw := gateway.NewWeChatGateway(gateway.Options{
			Workspace: ws,
			Stdout:    os.Stdout,
			Stderr:    os.Stderr,
		})
		if err := gw.Start(ctx); err != nil && !errors.Is(err, context.Canceled) {
			return err
		}
		return nil
	},
}
