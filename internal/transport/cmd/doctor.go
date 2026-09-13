package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/nextsko/mocode-agent/internal/core/config"
	"github.com/nextsko/mocode-agent/internal/util/version"
)

type doctorStatus string

const (
	doctorStatusOK   doctorStatus = "ok"
	doctorStatusWarn doctorStatus = "warn"
	doctorStatusFail doctorStatus = "fail"
	doctorStatusSkip doctorStatus = "skip"
)

type doctorCheck struct {
	Name    string       `json:"name"`
	Status  doctorStatus `json:"status"`
	Summary string       `json:"summary"`
	Details []string     `json:"details,omitempty"`
}

type doctorReport struct {
	Version string        `json:"version"`
	CWD     string        `json:"cwd"`
	Checks  []doctorCheck `json:"checks"`
}

type doctorOptions struct {
	JSON  bool
	Build bool
}

type doctorPathState struct {
	Path            string
	Exists          bool
	Readable        bool
	Writable        bool
	NearestExisting string
	Err             error
}

type doctorWorkspaceFacts struct {
	HasGoMod      bool
	ModulePath    string
	HasTaskfile   bool
	HasAgents     bool
	HasProjectCfg bool
	GitWorktree   bool
}

type doctorLoadedConfig struct {
	cfg         *config.Config
	resolver    config.VariableResolver
	loadedPaths []string
}

type doctorProviderIssue struct {
	Status doctorStatus
	Code   string
	Label  string
}

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Run local environment diagnostics",
	Long: `Run local diagnostics for mocode.

This command checks the local runtime, config/data directories, config files,
common provider environment variables, source-checkout toolchain, and optional
build health.`,
	Example: `
# Run standard local diagnostics
mocode doctor

# Include a source build check when running inside the mocode repository
mocode doctor --build

# Emit machine-readable JSON
mocode doctor --json
`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		opts := doctorOptions{}
		opts.JSON, _ = cmd.Flags().GetBool("json")
		opts.Build, _ = cmd.Flags().GetBool("build")
		return runDoctor(cmd, opts)
	},
}

func init() {
	doctorCmd.Flags().Bool("json", false, "Emit JSON diagnostics")
	doctorCmd.Flags().Bool("build", false, "Run an additional source build check when applicable")
	rootCmd.AddCommand(doctorCmd)
}

func runDoctor(cmd *cobra.Command, opts doctorOptions) error {
	report, err := collectDoctorReport(cmd.Context(), cmd, opts)
	if err != nil {
		return err
	}

	if opts.JSON {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(report)
	}

	okCount, warnCount, failCount, skipCount := doctorStatusCounts(report.Checks)
	fmt.Fprintln(cmd.OutOrStdout(), "mocode doctor")
	fmt.Fprintln(cmd.OutOrStdout(), "=============")
	fmt.Fprintf(cmd.OutOrStdout(), "Version: %s\n", report.Version)
	fmt.Fprintf(cmd.OutOrStdout(), "CWD: %s\n\n", report.CWD)

	for _, check := range report.Checks {
		fmt.Fprintf(cmd.OutOrStdout(), "[%s] %s: %s\n", strings.ToUpper(string(check.Status)), check.Name, check.Summary)
		for _, detail := range check.Details {
			fmt.Fprintf(cmd.OutOrStdout(), "      - %s\n", detail)
		}
	}

	fmt.Fprintf(
		cmd.OutOrStdout(),
		"\nSummary: %d passed, %d warnings, %d failed, %d skipped\n",
		okCount, warnCount, failCount, skipCount,
	)
	if failCount > 0 {
		return fmt.Errorf("doctor found %d failing check(s)", failCount)
	}
	return nil
}

func collectDoctorReport(ctx context.Context, cmd *cobra.Command, opts doctorOptions) (doctorReport, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	cwd, err := ResolveCwd(cmd)
	if err != nil {
		return doctorReport{}, err
	}

	report := doctorReport{
		Version: version.Version,
		CWD:     cwd,
	}

	facts := collectDoctorWorkspaceFacts(ctx, cwd)
	loadedCfg, storeErr := loadDoctorConfigStore(cwd, cmd)
	report.Checks = append(
		report.Checks,
		doctorBinaryCheck(),
		doctorRuntimeCheck(cwd, facts),
		doctorConfigDirCheck(filepath.Dir(config.GlobalConfig())),
		doctorDataDirCheck(filepath.Dir(config.GlobalConfigData())),
		doctorConfigFilesCheck(cwd),
		doctorMergedConfigCheck(loadedCfg, storeErr),
		doctorEnvVarsCheck(),
		doctorProviderConnectivityCheck(loadedCfg, storeErr),
		doctorModelConfigCheck(loadedCfg, storeErr),
		doctorSubagentTUICheck(loadedCfg, storeErr),
		doctorSubagentSourceCheck(cwd, facts),
		doctorToolchainCheck(cwd, facts),
	)

	if opts.Build {
		report.Checks = append(report.Checks, doctorBuildCheck(ctx, cwd, facts))
	}

	return report, nil
}

func loadDoctorConfigStore(cwd string, cmd *cobra.Command) (*doctorLoadedConfig, error) {
	debug, _ := cmd.Flags().GetBool("debug")
	store, err := config.LoadReadOnly(cwd, debug)
	if err != nil {
		return nil, err
	}
	return &doctorLoadedConfig{
		cfg:         store.Config(),
		resolver:    store.Resolver(),
		loadedPaths: store.LoadedPaths(),
	}, nil
}
