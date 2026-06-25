package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"

	"charm.land/log/v2"
	"github.com/package-register/mocode/internal/workspace"
	"github.com/spf13/cobra"
)

var runCmd = &cobra.Command{
	Aliases: []string{"r"},
	Use:     "run [prompt...]",
	Short:   "Run a single non-interactive prompt",
	Long: `Run a single prompt in non-interactive mode and exit.
The prompt can be provided as arguments or piped from stdin.`,
	Example: `
# Run a simple prompt
Mocode run "Guess my 5 favorite Pok茅mon"

# Pipe input from stdin
curl https://charm.land | Mocode run "Summarize this website"

# Read from a file
Mocode run "What is this code doing?" <<< prrr.go

# Redirect output to a file
Mocode run "Generate a hot README for this project" > MY_HOT_README.md

# Run in quiet mode (hide the spinner)
Mocode run --quiet "Generate a README for this project"

# Run in verbose mode (show logs)
Mocode run --verbose "Generate a README for this project"

# Continue a previous session
Mocode run --session {session-id} "Follow up on your last response"

# Continue the most recent session
Mocode run --continue "Follow up on your last response"

  `,
	RunE: func(cmd *cobra.Command, args []string) error {
		var (
			quiet, _      = cmd.Flags().GetBool("quiet")
			verbose, _    = cmd.Flags().GetBool("verbose")
			largeModel, _ = cmd.Flags().GetString("model")
			smallModel, _ = cmd.Flags().GetString("small-model")
			sessionID, _  = cmd.Flags().GetString("session")
			useLast, _    = cmd.Flags().GetBool("continue")
		)

		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, os.Kill)
		defer cancel()

		prompt := strings.Join(args, " ")

		prompt, err := MaybePrependStdin(prompt)
		if err != nil {
			slog.Error("Failed to read from stdin", "error", err)
			return err
		}

		if prompt == "" {
			return fmt.Errorf("no prompt provided")
		}

		ws, cleanup, err := setupLocalWorkspace(cmd)
		if err != nil {
			return err
		}
		defer cleanup()

		if !ws.Config().IsConfigured() {
			return fmt.Errorf("no providers configured - please run 'Mocode' to set up a provider interactively")
		}

		if verbose {
			slog.SetDefault(slog.New(log.New(os.Stderr)))
		}

		appWs := ws.(*workspace.AppWorkspace)
		return appWs.App().RunNonInteractive(ctx, os.Stdout, prompt, largeModel, smallModel, quiet || verbose, sessionID, useLast)
	},
}

func init() {
	runCmd.Flags().BoolP("quiet", "q", false, "Hide spinner")
	runCmd.Flags().BoolP("verbose", "v", false, "Show logs")
	runCmd.Flags().StringP("model", "m", "", "Model to use. Accepts 'model' or 'provider/model' to disambiguate models with the same name across providers")
	runCmd.Flags().String("small-model", "", "Small model to use. If not provided, uses the default small model for the provider")
	runCmd.Flags().StringP("session", "s", "", "Continue a previous session by ID")
	runCmd.Flags().BoolP("continue", "C", false, "Continue the most recent session")
	runCmd.MarkFlagsMutuallyExclusive("session", "continue")
}
