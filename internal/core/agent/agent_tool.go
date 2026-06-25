package agent

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"charm.land/fantasy"

	"golang.org/x/sync/errgroup"

	"github.com/package-register/mocode/internal/core/agent/notify"
	"github.com/package-register/mocode/internal/core/agent/prompt"
	"github.com/package-register/mocode/internal/core/agent/tools"
	"github.com/package-register/mocode/internal/core/config"
	"github.com/package-register/mocode/internal/core/knowledge/memory"
)

//go:embed templates/agent_tool.md
var agentToolDescription []byte

type AgentParams struct {
	Prompt string            `json:"prompt" description:"The task for the agent to perform"`
	Tasks  []AgentTaskParams `json:"tasks,omitempty" description:"Optional batch of tasks to run in parallel"`
}

type AgentTaskParams struct {
	// ID is an optional identifier for the task, used for dependency tracking
	// and structured result attribution.
	ID string `json:"id,omitempty" description:"Optional unique ID for DAG ordering and result attribution"`
	// Prompt is the task description for the sub-agent.
	Prompt string `json:"prompt" description:"The task for the agent to perform"`
	// AgentName is the target agent/mode ID. Defaults to "task".
	AgentName string `json:"agent_name,omitempty" description:"Optional target agent/mode ID. Defaults to task"`
	// AllowEdit enables write tools for this sub-agent.
	AllowEdit bool `json:"allow_edit,omitempty" description:"Allow this sub-agent to use edit/write tools when the selected agent supports them"`
	// DependsOn lists IDs of tasks that must complete before this one starts.
	DependsOn []string `json:"depends_on,omitempty" description:"Optional list of task IDs this task depends on"`
}

// TaskResultStatus enumerates the possible terminal status values reported
// on a TaskResult. Keeping them as named constants lets the goconst lint
// rule recognise them, and gives the frontend a single source of truth.
const (
	TaskResultStatusSuccess   = "success"
	TaskResultStatusError     = "error"
	TaskResultStatusBlocked   = "blocked"
	TaskResultStatusCancelled = "cancelled"
)

// TaskResult is the structured output envelope returned by sub-agent execution.
// It provides machine-parseable status/summary/artifacts alongside the raw text.
type TaskResult struct {
	Status      string     `json:"status"`                 // TaskResultStatusSuccess, ...Error, ...Blocked, ...Cancelled
	Summary     string     `json:"summary,omitempty"`      // one-line summary of the sub-agent's work
	Artifacts   []string   `json:"artifacts,omitempty"`    // files created or modified
	Risks       []string   `json:"risks,omitempty"`        // risks or concerns found
	Evidence    []string   `json:"evidence,omitempty"`     // evidence refs (e.g. "file:line")
	NextActions []string   `json:"next_actions,omitempty"` // suggested follow-up actions
	Error       string     `json:"error,omitempty"`        // error message if status == "error"
	DurationMs  int64      `json:"duration_ms,omitempty"`  // wall-clock duration of the sub-agent run
	Usage       *TaskUsage `json:"usage,omitempty"`        // aggregated token usage of the run
}

// TaskUsage is the wire-level token usage reported on each TaskResult. It
// mirrors fantasy.Usage but is kept here so the Agent tool's output envelope
// is self-contained and decoupled from the model layer.
type TaskUsage struct {
	Input         int64 `json:"input"`
	Output        int64 `json:"output"`
	CacheRead     int64 `json:"cache_read"`
	CacheCreation int64 `json:"cache_creation"`
	Total         int64 `json:"total"`
}

// ToNotifyUsage converts the wire-level TaskUsage into the notify package's
// sub-agent token usage representation.
func (u *TaskUsage) ToNotifyUsage() notify.SubagentTokenUsage {
	if u == nil {
		return notify.SubagentTokenUsage{}
	}
	return notify.SubagentTokenUsage{
		Input:         u.Input,
		Output:        u.Output,
		CacheRead:     u.CacheRead,
		CacheCreation: u.CacheCreation,
		Total:         u.Total,
	}
}

// FromFantasyUsage converts a fantasy.Usage into the wire-level TaskUsage
// representation. Returns nil when every token bucket is zero, so the wire
// envelope (which uses omitempty) can suppress the "usage" key entirely
// for runs that produced no measurable usage. The check covers all five
// buckets — Total, Input, Output, CacheRead, CacheCreation — because some
// providers can bill cache-only traffic that leaves Total/Input/Output at
// zero but reports non-zero cache reads or creations.
func FromFantasyUsage(u fantasy.Usage) *TaskUsage {
	if u.TotalTokens == 0 &&
		u.InputTokens == 0 &&
		u.OutputTokens == 0 &&
		u.CacheReadTokens == 0 &&
		u.CacheCreationTokens == 0 {
		return nil
	}
	return &TaskUsage{
		Input:         u.InputTokens,
		Output:        u.OutputTokens,
		CacheRead:     u.CacheReadTokens,
		CacheCreation: u.CacheCreationTokens,
		Total:         u.TotalTokens,
	}
}

const (
	AgentToolName = "agent"
)

func (c *coordinator) agentTool(ctx context.Context) (fantasy.AgentTool, error) {
	defaultAgent, err := c.buildSubAgent(ctx, config.AgentTask, false)
	if err != nil {
		return nil, err
	}
	return fantasy.NewParallelAgentTool(
		AgentToolName,
		tools.FirstLineDescription(agentToolDescription),
		func(ctx context.Context, params AgentParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if params.Prompt == "" && len(params.Tasks) == 0 {
				return fantasy.NewTextErrorResponse("prompt or tasks is required"), nil
			}

			sessionID := tools.GetSessionFromContext(ctx)
			if sessionID == "" {
				return fantasy.ToolResponse{}, errors.New("session id missing from context")
			}

			agentMessageID := tools.GetMessageFromContext(ctx)
			if agentMessageID == "" {
				return fantasy.ToolResponse{}, errors.New("agent message id missing from context")
			}

			if len(params.Tasks) > 0 {
				return c.runSubAgentBatch(ctx, subAgentBatchParams{
					SessionID:      sessionID,
					AgentMessageID: agentMessageID,
					ToolCallID:     call.ID,
					Tasks:          params.Tasks,
				})
			}

			return c.runSubAgent(ctx, subAgentParams{
				Agent:          defaultAgent,
				SessionID:      sessionID,
				AgentMessageID: agentMessageID,
				ToolCallID:     call.ID,
				Prompt:         params.Prompt,
				SessionTitle:   "New Agent Session",
				AgentID:        call.ID,
				SubagentType:   config.AgentTask,
			})
		}), nil
}

func (c *coordinator) buildSubAgent(ctx context.Context, agentID string, allowEdit bool) (SessionAgent, error) {
	if agentID == "" {
		agentID = config.AgentTask
	}
	agentCfg, ok := c.cfg.Config().Agents[agentID]
	if !ok {
		return nil, fmt.Errorf("agent %q not configured", agentID)
	}
	if !allowEdit {
		agentCfg.AllowedTools = readOnlyAgentTools(agentCfg.AllowedTools)
	} else {
		agentCfg.AllowedTools = withEditAgentTools(agentCfg.AllowedTools)
	}
	agentPrompt, err := promptForAgent(agentCfg, prompt.WithWorkingDir(c.cfg.WorkingDir()))
	if err != nil {
		return nil, err
	}
	return c.buildAgent(ctx, agentPrompt, agentCfg, true)
}

type subAgentBatchParams struct {
	SessionID      string
	AgentMessageID string
	ToolCallID     string
	Tasks          []AgentTaskParams
}

func (c *coordinator) runSubAgentBatch(ctx context.Context, params subAgentBatchParams) (fantasy.ToolResponse, error) {
	// If any task has dependencies, use DAG scheduler.
	hasDeps := false
	for _, t := range params.Tasks {
		if len(t.DependsOn) > 0 {
			hasDeps = true
			break
		}
	}
	if hasDeps {
		return c.runSubAgentDAG(ctx, params)
	}
	return c.runSubAgentParallel(ctx, params)
}

// runSubAgentParallel runs all tasks concurrently (no dependencies).
func (c *coordinator) runSubAgentParallel(ctx context.Context, params subAgentBatchParams) (fantasy.ToolResponse, error) {
	type batchEntry struct {
		idx      int
		task     AgentTaskParams
		result   string
		err      string
		duration int64
		usage    *TaskUsage
	}
	entries := make([]batchEntry, len(params.Tasks))

	group, ctx := errgroup.WithContext(ctx)
	for i, task := range params.Tasks {
		entries[i] = batchEntry{idx: i, task: task}
		e := &entries[i]
		group.Go(func() error {
			if strings.TrimSpace(e.task.Prompt) == "" {
				e.err = "prompt is required"
				return nil
			}
			agentID := strings.TrimSpace(e.task.AgentName)
			if agentID == "" {
				agentID = config.AgentTask
			}
			agent, err := c.buildSubAgent(ctx, agentID, e.task.AllowEdit)
			if err != nil {
				e.err = err.Error()
				return nil
			}
			resp, meta, callErr := c.runSubAgentWithMeta(ctx, subAgentParams{
				Agent:          agent,
				SessionID:      params.SessionID,
				AgentMessageID: params.AgentMessageID,
				ToolCallID:     fmt.Sprintf("%s-%d", params.ToolCallID, e.idx+1),
				Prompt:         e.task.Prompt,
				SessionTitle:   fmt.Sprintf("Agent %s: %s", agentID, e.task.Prompt),
				AgentID:        fmt.Sprintf("%s-%d", params.ToolCallID, e.idx+1),
				SubagentType:   agentID,
			})
			e.duration = meta.DurationMs
			e.usage = FromFantasyUsage(meta.Usage)
			if callErr != nil {
				e.err = callErr.Error()
				return nil
			}
			e.result = strings.TrimSpace(resp.Content)
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return fantasy.ToolResponse{}, err
	}

	// Build structured results array.
	taskResults := make([]TaskResult, len(entries))
	for _, e := range entries {
		tr := TaskResult{DurationMs: e.duration, Usage: e.usage}
		if e.err != "" {
			tr.Status = TaskResultStatusError
			tr.Error = e.err
		} else if e.result == "" {
			tr.Status = TaskResultStatusError
			tr.Error = "empty result"
		} else {
			tr.Status = TaskResultStatusSuccess
			tr.Summary = firstLine(e.result)
		}
		taskResults[e.idx] = tr
	}

	// Text output: legacy + structured JSON appendix.
	var out strings.Builder
	out.WriteString("Batch agent results:\n")
	for _, e := range entries {
		label := e.task.ID
		if label == "" {
			label = fmt.Sprintf("Agent %d", e.idx+1)
		}
		agentName := e.task.AgentName
		if agentName == "" {
			agentName = config.AgentTask
		}
		out.WriteString(fmt.Sprintf("\n## %s [%s]\n\n", label, agentName))
		if e.err != "" {
			out.WriteString(fmt.Sprintf("**error**: %s\n", e.err))
		} else if e.result != "" {
			out.WriteString(e.result)
			out.WriteString("\n")
		} else {
			out.WriteString("(empty result)\n")
		}
	}

	// Append structured envelope.
	envJSON, err := json.Marshal(taskResults)
	if err == nil {
		out.WriteString("\n\n<task_results>\n")
		out.WriteString(string(envJSON))
		out.WriteString("\n</task_results>")
	}

	return fantasy.NewTextResponse(out.String()), nil
}

// firstLine returns the first non-empty line of s, truncated to 200 chars.
func firstLine(s string) string {
	lines := strings.SplitN(strings.TrimSpace(s), "\n", 2)
	l := strings.TrimSpace(lines[0])
	if len(l) > 200 {
		l = l[:200] + "..."
	}
	return l
}

// runSubAgentDAG executes tasks respecting the DependsOn DAG.
// It runs tasks in phases: each phase executes all ready tasks in parallel,
// then advances to the next phase. Failed/errored tasks block their dependents.
func (c *coordinator) runSubAgentDAG(ctx context.Context, params subAgentBatchParams) (fantasy.ToolResponse, error) {
	n := len(params.Tasks)
	if n == 0 {
		return fantasy.NewTextResponse("No tasks to run."), nil
	}

	// Assign default IDs if not provided.
	for i := range params.Tasks {
		if params.Tasks[i].ID == "" {
			params.Tasks[i].ID = fmt.Sprintf("task-%d", i+1)
		}
	}

	// Build ID index.
	byID := make(map[string]int, n)
	for i, t := range params.Tasks {
		if _, dup := byID[t.ID]; dup {
			return fantasy.NewTextErrorResponse(fmt.Sprintf("Duplicate task ID: %q", t.ID)), nil
		}
		byID[t.ID] = i
	}

	// Validate all DependsOn references exist.
	for _, t := range params.Tasks {
		for _, dep := range t.DependsOn {
			if _, ok := byID[dep]; !ok {
				return fantasy.NewTextErrorResponse(
					fmt.Sprintf("Task %q depends on unknown task %q", t.ID, dep),
				), nil
			}
		}
	}

	// Cycle detection via Kahn's algorithm on a copy.
	inDegree := make([]int, n)
	for i, t := range params.Tasks {
		inDegree[i] = len(t.DependsOn)
	}
	{
		queue := make([]int, 0, n)
		for i, d := range inDegree {
			if d == 0 {
				queue = append(queue, i)
			}
		}
		visited := 0
		for len(queue) > 0 {
			idx := queue[0]
			queue = queue[1:]
			visited++
			for j, t := range params.Tasks {
				for _, dep := range t.DependsOn {
					if dep == params.Tasks[idx].ID {
						inDegree[j]--
						if inDegree[j] == 0 {
							queue = append(queue, j)
						}
						break
					}
				}
			}
		}
		if visited != n {
			return fantasy.NewTextErrorResponse("Cycle detected in task dependencies"), nil
		}
	}

	// Run in phases.
	done := make(map[string]bool, n)
	failed := make(map[string]bool, n)
	blocked := make(map[string]bool, n)
	results := make([]string, n)
	durations := make([]int64, n)
	usages := make([]*TaskUsage, n)

	for len(done)+len(failed)+len(blocked) < n {
		// Find ready tasks: all deps done, not yet processed, and no dep failed.
		var ready []int
		for i, t := range params.Tasks {
			if done[t.ID] || failed[t.ID] || blocked[t.ID] {
				continue
			}
			isReady := true
			for _, dep := range t.DependsOn {
				if failed[dep] {
					// Dependency failed or blocked: this task is blocked.
					isReady = false
					blocked[t.ID] = true
					results[byID[t.ID]] = "blocked: upstream dependency failed"
					break
				}
				if !done[dep] {
					isReady = false
					break
				}
			}
			if isReady {
				ready = append(ready, i)
			}
		}

		if len(ready) == 0 {
			// Remaining tasks are all blocked or have unmet deps.
			for _, t := range params.Tasks {
				if done[t.ID] || failed[t.ID] || blocked[t.ID] {
					continue
				}
				results[byID[t.ID]] = "blocked: no ready tasks"
				blocked[t.ID] = true
			}
			break
		}

		// Run ready tasks in parallel.
		group, gctx := errgroup.WithContext(ctx)
		for _, idx := range ready {
			i := idx
			group.Go(func() error {
				task := params.Tasks[i]
				if strings.TrimSpace(task.Prompt) == "" {
					results[i] = "error: prompt is required"
					failed[task.ID] = true
					return nil
				}
				agentID := strings.TrimSpace(task.AgentName)
				if agentID == "" {
					agentID = config.AgentTask
				}
				agent, err := c.buildSubAgent(gctx, agentID, task.AllowEdit)
				if err != nil {
					results[i] = "error: " + err.Error()
					failed[task.ID] = true
					return nil
				}
				resp, meta, callErr := c.runSubAgentWithMeta(gctx, subAgentParams{
					Agent:          agent,
					SessionID:      params.SessionID,
					AgentMessageID: params.AgentMessageID,
					ToolCallID:     fmt.Sprintf("%s-%s", params.ToolCallID, task.ID),
					Prompt:         task.Prompt,
					SessionTitle:   fmt.Sprintf("Agent %s: %s", agentID, task.Prompt),
					AgentID:        fmt.Sprintf("%s-%s", params.ToolCallID, task.ID),
					SubagentType:   agentID,
				})
				durations[i] = meta.DurationMs
				usages[i] = FromFantasyUsage(meta.Usage)
				if callErr != nil {
					results[i] = "error: " + callErr.Error()
					failed[task.ID] = true
					return nil
				}
				results[i] = strings.TrimSpace(resp.Content)
				done[task.ID] = true
				return nil
			})
		}
		if err := group.Wait(); err != nil {
			return fantasy.ToolResponse{}, err
		}
	}

	// Build output.
	var out strings.Builder
	out.WriteString("DAG batch results:\n")
	for i, t := range params.Tasks {
		agentName := t.AgentName
		if agentName == "" {
			agentName = config.AgentTask
		}
		var status string
		switch {
		case failed[t.ID]:
			status = "❌"
		case blocked[t.ID]:
			status = "⏸"
		default:
			status = "✅"
		}
		out.WriteString(fmt.Sprintf("\n## %s %s [%s]\n\n", status, t.ID, agentName))
		if results[i] != "" {
			out.WriteString(results[i])
			out.WriteString("\n")
		} else {
			out.WriteString("(empty result)\n")
		}
	}

	// Structured envelope.
	taskResults := make([]TaskResult, n)
	for i := range params.Tasks {
		t := params.Tasks[i]
		tr := TaskResult{DurationMs: durations[i], Usage: usages[i]}
		switch {
		case results[i] == "":
			// Empty result wins over status flags: a failed/blocked task
			// with no captured message is more usefully described as
			// "empty result" than as an empty Error string.
			tr.Status = TaskResultStatusError
			tr.Error = "empty result"
		case blocked[t.ID]:
			tr.Status = TaskResultStatusBlocked
			tr.Error = strings.TrimPrefix(results[i], "blocked: ")
		case failed[t.ID] || strings.HasPrefix(results[i], "error:"):
			tr.Status = TaskResultStatusError
			tr.Error = strings.TrimPrefix(results[i], "error: ")
		default:
			tr.Status = TaskResultStatusSuccess
			tr.Summary = firstLine(results[i])
		}
		taskResults[i] = tr
	}
	envJSON, err := json.Marshal(taskResults)
	if err == nil {
		out.WriteString("\n\n<task_results>\n")
		out.WriteString(string(envJSON))
		out.WriteString("\n</task_results>")
	}

	return fantasy.NewTextResponse(out.String()), nil
}

func readOnlyAgentTools(allowed []string) []string {
	readOnly := []string{"glob", "grep", "ls", "sourcegraph", "view", "crawl", "download_docs", tools.SessionSummaryToolName, tools.SessionExportToolName, memory.SearchToolName, memory.LoadToolName}
	if allowed == nil {
		return readOnly
	}
	filtered := make([]string, 0, len(allowed))
	for _, name := range allowed {
		if slices.Contains(readOnly, name) {
			filtered = append(filtered, name)
		}
	}
	return filtered
}

func withEditAgentTools(allowed []string) []string {
	next := append([]string{}, readOnlyAgentTools(allowed)...)
	for _, name := range []string{"edit", "write", "multiedit"} {
		if !slices.Contains(next, name) {
			next = append(next, name)
		}
	}
	return next
}
