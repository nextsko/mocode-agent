package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/package-register/mocode/internal/evolution"
	"github.com/package-register/mocode/internal/config"
	"github.com/spf13/cobra"
)

 var evolveMinRepeats int

 var evolveCmd = &cobra.Command{
 	Use:   "evolve",
 	Short: "Produce evolution patches from session error logs",
 	Long: `Scan session bug logs for recurring tool errors and produce rule patches
 into .mocode/patches/. Produced patches are injected into future agent runs
 via injectEvolutionContext. Safe to re-run: existing patches are not duplicated.`,
 	RunE: runEvolve,
 }

 func init() {
 	evolveCmd.Flags().IntVar(&evolveMinRepeats, "min-repeats", 3, "Minimum occurrences of an error before a patch is produced")
 	rootCmd.AddCommand(evolveCmd)
 }

func runEvolve(cmd *cobra.Command, _ []string) error {
	dataDir, _ := cmd.Flags().GetString("data-dir")
	cwd, err := filepath.Abs(".")
	if err != nil {
		return fmt.Errorf("resolve working directory: %w", err)
	}

	cfg, err := config.Init(cwd, dataDir, false)
	if err != nil {
		return err
	}
 	if dataDir == "" {
 		dataDir = cfg.Config().Options.DataDirectory
 	}

 	store, err := evolution.NewPatchStore(dataDir)
 	if err != nil {
 		return fmt.Errorf("open patch store: %w", err)
 	}
 	sessionsDir := filepath.Join(dataDir, "sessions")
 	prod, err := evolution.NewProducer(store, sessionsDir, evolution.WithMinRepeats(evolveMinRepeats))
 	if err != nil {
 		return fmt.Errorf("build producer: %w", err)
 	}

 	created, err := prod.Produce()
 	if err != nil {
 		return fmt.Errorf("produce patches: %w", err)
 	}

 	out := cmd.OutOrStdout()
 	if len(created) == 0 {
 		fmt.Fprintln(out, "No new patches produced.")
 		return nil
 	}
 	fmt.Fprintf(out, "Produced %d patch(es):\n", len(created))
 	for _, id := range created {
 		fmt.Fprintf(out, "  - %s\n", id)
 	}
	fmt.Fprintln(out, "\nThese will be injected into future agent runs until applied.")
	return nil
}
