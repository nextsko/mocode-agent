package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"

	"charm.land/fantasy"

	"github.com/spf13/cobra"

	"github.com/package-register/mocode/internal/core/agent"
	"github.com/package-register/mocode/internal/core/evaluation"
	"github.com/package-register/mocode/internal/core/evaluation/criterion"
	"github.com/package-register/mocode/internal/core/evaluation/evalset"
	"github.com/package-register/mocode/internal/core/evaluation/evalset/inmemory"
	"github.com/package-register/mocode/internal/core/evaluation/metric"
	"github.com/package-register/mocode/internal/core/evaluation/result"
	"github.com/package-register/mocode/internal/domain/session/message"
	"github.com/package-register/mocode/internal/transport/workspace"
)

var (
	evalRuns int
	evalJSON bool
)

var evalCmd = &cobra.Command{
	Use:   "eval <eval-set.json>",
	Short: "Run an evaluation set against the configured agent",
	Long: `Run a JSON evaluation set against the configured agent and print per-case
pass/fail plus an aggregate summary.

Each case may assert expected_text (exact match) and/or expected_tools
(in-order tool calls). Cases asserting neither are still executed and report
inference success/failure. Costs and token usage come from the inference
sessions.`,
	Args: cobra.ExactArgs(1),
	RunE: runEval,
}

func init() {
	evalCmd.Flags().IntVarP(&evalRuns, "runs", "n", 1, "Number of inference runs per case")
	evalCmd.Flags().BoolVar(&evalJSON, "json", false, "Output results as JSON")
	rootCmd.AddCommand(evalCmd)
}

func runEval(cmd *cobra.Command, args []string) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, os.Kill)
	defer cancel()

	es, err := loadEvalSet(args[0])
	if err != nil {
		return err
	}

	ws, cleanup, err := setupLocalWorkspace(cmd)
	if err != nil {
		return err
	}
	defer cleanup()

	if !ws.Config().IsConfigured() {
		return fmt.Errorf("no providers configured - run 'Mocode' to set up a provider interactively")
	}

	appWs, ok := ws.(*workspace.AppWorkspace)
	if !ok {
		return fmt.Errorf("eval requires a local workspace")
	}
	app := appWs.App()
	if app.AgentCoordinator == nil {
		return fmt.Errorf("no agent configured - check your mocode configuration")
	}

	sets := inmemory.New()
	sets.Put(es)

	ev, err := evaluation.New(
		&coordinatorAgent{c: app.AgentCoordinator},
		evaluation.WithEvalSetManager(sets),
		evaluation.WithSessions(app.Sessions),
		evaluation.WithMessages(app.Messages),
		evaluation.WithMetrics(defaultMetrics()...),
		evaluation.WithNumRuns(evalRuns),
	)
	if err != nil {
		return fmt.Errorf("build evaluator: %w", err)
	}
	defer ev.Close()

	res, err := ev.Evaluate(ctx, es.ID)
	if err != nil {
		return fmt.Errorf("evaluation failed: %w", err)
	}
	return printEvalResult(cmd, es, res)
}

func loadEvalSet(path string) (*evalset.EvalSet, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open eval set: %w", err)
	}
	defer f.Close()
	es, err := evalset.LoadSet(f)
	if err != nil {
		return nil, fmt.Errorf("decode eval set %q: %w", path, err)
	}
	if es.ID == "" {
		return nil, fmt.Errorf("eval set %q has no id", path)
	}
	if len(es.Cases) == 0 {
		return nil, fmt.Errorf("eval set %q has no cases", path)
	}
	return es, nil
}

// defaultMetrics: final-response exact match and tool-trajectory exact.
// FinalResponse falls back to the case ExpectedText when the criterion has none,
// and a case with no expected tools scores 1.0 vacuously, so both metrics are
// safe to apply to every case.
func defaultMetrics() []*metric.EvalMetric {
	return []*metric.EvalMetric{
		{Name: "final-response", Threshold: 1.0, Criterion: &criterion.Criterion{FinalResponse: &criterion.FinalResponse{Mode: criterion.MatchExact}}},
		{Name: "tool-trajectory", Threshold: 1.0, Criterion: &criterion.Criterion{ToolTrajectory: &criterion.ToolTrajectory{Exact: true}}},
	}
}

type evalOutputCase struct {
	ID      string             `json:"id"`
	Status  string             `json:"status"`
	Error   string             `json:"error,omitempty"`
	Metrics []evalOutputMetric `json:"metrics,omitempty"`
}

type evalOutputMetric struct {
	Name   string  `json:"name"`
	Score  float64 `json:"score"`
	Passed bool    `json:"passed"`
	Reason string  `json:"reason,omitempty"`
}

type evalOutput struct {
	EvalSetID string           `json:"eval_set_id"`
	NumRuns   int              `json:"num_runs"`
	Cases     []evalOutputCase `json:"cases"`
	Summary   *result.Summary  `json:"summary,omitempty"`
}

func printEvalResult(cmd *cobra.Command, es *evalset.EvalSet, res *result.EvalSetResult) error {
	out := cmd.OutOrStdout()
	if evalJSON {
		return encodeEvalJSON(out, es, res)
	}
	return printEvalHuman(out, es, res)
}

func encodeEvalJSON(w io.Writer, es *evalset.EvalSet, res *result.EvalSetResult) error {
	cases := make([]evalOutputCase, 0, len(res.CaseResults))
	for _, cr := range res.CaseResults {
		c := evalOutputCase{ID: cr.EvalID, Status: string(cr.Status), Error: cr.ErrorMessage}
		for _, mr := range cr.MetricResults {
			c.Metrics = append(c.Metrics, evalOutputMetric{Name: mr.MetricName, Score: mr.Score, Passed: mr.Passed, Reason: mr.Reason})
		}
		cases = append(cases, c)
	}
	o := evalOutput{EvalSetID: res.EvalSetID, Cases: cases, Summary: res.Summary}
	if res.Summary != nil {
		o.NumRuns = res.Summary.NumRuns
	}
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(o)
}

func printEvalHuman(w io.Writer, es *evalset.EvalSet, res *result.EvalSetResult) error {
	fmt.Fprintf(w, "Evaluation: %s (%d cases)\n\n", es.ID, len(es.Cases))
	for _, cr := range res.CaseResults {
		fmt.Fprintf(w, "  [%s] %s\n", statusGlyph(cr.Status), cr.EvalID)
		if cr.ErrorMessage != "" {
			fmt.Fprintf(w, "      error: %s\n", cr.ErrorMessage)
		}
		for _, mr := range cr.MetricResults {
			fmt.Fprintf(w, "      %-16s score=%.2f passed=%v", mr.MetricName, mr.Score, mr.Passed)
			if !mr.Passed && mr.Reason != "" {
				fmt.Fprintf(w, " (%s)", mr.Reason)
			}
			fmt.Fprintln(w)
		}
	}
	if res.Summary != nil {
		fmt.Fprintln(w)
		fmt.Fprintf(w, "Summary: runs=%d pass_rate=%.0f%% avg_score=%.2f\n",
			res.Summary.NumRuns, res.Summary.PassRate*100, res.Summary.AvgScore)
	}
	return nil
}

func statusGlyph(s result.Status) string {
	switch s {
	case result.StatusPassed:
		return "PASS"
	case result.StatusFailed:
		return "FAIL"
	case result.StatusError:
		return "ERR "
	default:
		return "????"
	}
}

// coordinatorAgent adapts an agent.Coordinator to the agent.SessionAgent
// interface expected by the evaluator. The evaluator only drives Run and
// SetSystemPrompt for per-case overrides; the remaining SessionAgent methods
// are satisfied by delegating to the coordinator where it has a matching
// method, or as no-ops otherwise.
type coordinatorAgent struct {
	c         agent.Coordinator
	sysprompt string
}

func (a *coordinatorAgent) Run(ctx context.Context, call agent.SessionAgentCall) (*fantasy.AgentResult, error) {
	prompt := call.Prompt
	if a.sysprompt != "" {
		// Surface the per-case system prompt as an explicit instruction prefix
		// so a case can override behavior without mutating global agent state.
		prompt = "<system_instruction>" + a.sysprompt + "</system_instruction>\n\n" + prompt
	}
	var attachments []message.Attachment
	if len(call.Attachments) > 0 {
		attachments = call.Attachments
	}
	return a.c.Run(ctx, call.SessionID, prompt, attachments...)
}

func (a *coordinatorAgent) SetSystemPrompt(s string) { a.sysprompt = s }

func (a *coordinatorAgent) SystemPrompt() string { return a.sysprompt }

func (a *coordinatorAgent) Cancel(sessionID string)             { a.c.Cancel(sessionID) }
func (a *coordinatorAgent) CancelAll()                          { a.c.CancelAll() }
func (a *coordinatorAgent) IsSessionBusy(sessionID string) bool { return a.c.IsSessionBusy(sessionID) }

// The following SessionAgent members are not exercised by the evaluator and are
// intentionally no-ops; models/tools/summarize are owned by the coordinator's
// configured agent.
func (a *coordinatorAgent) SetModels(agent.Model, agent.Model) {}
func (a *coordinatorAgent) SetTools([]fantasy.AgentTool)       {}
func (a *coordinatorAgent) IsBusy() bool                       { return false }
func (a *coordinatorAgent) QueuedPrompts(string) int           { return 0 }
func (a *coordinatorAgent) QueuedPromptsList(string) []string  { return nil }
func (a *coordinatorAgent) ClearQueue(string)                  {}
func (a *coordinatorAgent) Summarize(context.Context, string, fantasy.ProviderOptions) (string, error) {
	return "", nil
}
func (a *coordinatorAgent) Model() agent.Model      { return agent.Model{} }
func (a *coordinatorAgent) SmallModel() agent.Model { return agent.Model{} }

var _ agent.SessionAgent = (*coordinatorAgent)(nil)
