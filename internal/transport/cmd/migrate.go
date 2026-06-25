package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/package-register/mocode/internal/store"
)

func init() {
	rootCmd.AddCommand(migrateCmd)
}

var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Migrate from old SQLite database to file-based storage",
	Long: `Migrate all sessions, messages, and file history from the old 
SQLite database (.mocode/mocode.db) to the new file-based JSONL format.

Usage:
  mocode migrate                          # migrate from default location
  mocode migrate --from /path/to/mocode.db   # specify old database path
  mocode migrate --dry-run                   # preview without writing`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		fromPath, _ := cmd.Flags().GetString("from")

		ctx := cmd.Context()

		if fromPath == "" {
			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("get working directory: %w", err)
			}
			fromPath = filepath.Join(cwd, ".mocode", "mocode.db")
		}

		if _, err := os.Stat(fromPath); os.IsNotExist(err) {
			return fmt.Errorf("SQLite database not found at: %s\n  Use --from to specify the path to the old database", fromPath)
		}

		if dryRun {
			fmt.Printf("Dry-run mode: would migrate from %s\n", fromPath)
			fmt.Println("Use 'mocode migrate' without --dry-run to perform the actual migration.")
			return nil
		}

		cwd, _ := os.Getwd()
		_, err := store.MigrateFromSQLite(ctx, fromPath, cwd)
		return err
	},
}

func init() {
	migrateCmd.Flags().Bool("dry-run", false, "Preview migration without writing any files")
	migrateCmd.Flags().String("from", "", "Path to the old SQLite database (default: ./.mocode/mocode.db)")
}
