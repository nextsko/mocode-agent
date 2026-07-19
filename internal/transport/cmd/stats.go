package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/package-register/mocode/internal/core/config"
	"github.com/package-register/mocode/internal/store"
)

func init() {
	rootCmd.AddCommand(statsCmd)
}

var statsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show usage statistics",
	Long:  "Display usage statistics including sessions, tokens, and cost.",
	RunE: func(cmd *cobra.Command, _ []string) error {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("get working directory: %w", err)
		}

		cfg, err := config.Init(cwd, false)
		if err != nil {
			return fmt.Errorf("init config: %w", err)
		}

		st, err := store.New(cwd, cfg)
		if err != nil {
			return fmt.Errorf("open store: %w", err)
		}
		defer st.Close()

		total := st.Stats().TotalStats()
		if total.Sessions == 0 {
			return fmt.Errorf("no sessions found")
		}

		fmt.Println()
		fmt.Printf("  Total sessions:    %d\n", total.Sessions)
		fmt.Printf("  Total messages:    %d\n", total.Messages)
		fmt.Printf("  Prompt tokens:     %d\n", total.PromptTokens)
		fmt.Printf("  Completion tokens:  %d\n", total.CompletionTokens)
		fmt.Printf("  Total tokens:      %d\n", total.PromptTokens+total.CompletionTokens)
		fmt.Printf("  Total cost:        $%.4f\n", total.Cost)
		if total.Sessions > 0 {
			fmt.Printf("  Avg msg/session:   %.1f\n", float64(total.Messages)/float64(total.Sessions))
		}
		fmt.Println()

		return nil
	},
}
