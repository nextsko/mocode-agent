package uidissect

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// FlowStep is one interaction in a dynamic capture.
type FlowStep struct {
	Action string `json:"action"` // click | fill | press | hover | scroll | wait | hover
	Target string `json:"target,omitempty"`
	Text   string `json:"text,omitempty"`
	Key    string `json:"key,omitempty"`
	Ms     int    `json:"ms,omitempty"`
}

// FlowState is one measured state of a dynamic flow.
type FlowState struct {
	Step  int    `json:"step"`
	Label string `json:"label"`
	Map   Map    `json:"map"`
	Diff  Diff   `json:"diff,omitempty"`
}

// Flow is a sequence of interaction states with the regression between them.
type Flow struct {
	URL         string      `json:"url"`
	GeneratedAt time.Time   `json:"generated_at"`
	Steps       []FlowStep  `json:"steps"`
	States      []FlowState `json:"states"`
}

// RecordFlow drives any executor through steps, measuring a state after each
// one and diffing consecutive states. It stays framework-free so tests can
// supply fake executors while the browser tool supplies real CDP actions.
func RecordFlow(
	url string,
	steps []FlowStep,
	exec func(step FlowStep) error,
	measure func() (Map, error),
) (Flow, error) {
	flow := Flow{URL: url, GeneratedAt: time.Now().UTC(), Steps: steps}
	initial, err := measure()
	if err != nil {
		return Flow{}, fmt.Errorf("measure initial state: %w", err)
	}
	flow.States = append(flow.States, FlowState{Step: 0, Label: "initial", Map: initial})

	prev := initial
	for i, step := range steps {
		if err := exec(step); err != nil {
			return flow, fmt.Errorf("step %d (%s): %w", i+1, step.Action, err)
		}
		cur, err := measure()
		if err != nil {
			return flow, fmt.Errorf("measure after step %d: %w", i+1, err)
		}
		flow.States = append(flow.States, FlowState{
			Step:  i + 1,
			Label: stepLabel(step),
			Map:   cur,
			Diff:  Compare(prev, cur),
		})
		prev = cur
	}
	return flow, nil
}

func stepLabel(step FlowStep) string {
	if step.Target != "" {
		return fmt.Sprintf("%s %s", step.Action, truncate(step.Target, 48))
	}
	if step.Text != "" {
		return fmt.Sprintf("%s %q", step.Action, truncate(step.Text, 32))
	}
	return step.Action
}

// RenderFlow renders the dynamic flow as a report.
func RenderFlow(f Flow) string {
	var b strings.Builder
	b.WriteString("# 动态 UI 流\n\n")
	fmt.Fprintf(&b, "- URL: %s\n- Steps: %d\n- States: %d\n- Generated: %s\n\n", f.URL, len(f.Steps), len(f.States), f.GeneratedAt.Format(time.RFC3339))
	if len(f.Steps) > 0 {
		b.WriteString("## 交互步骤\n\n")
		for i, s := range f.Steps {
			fmt.Fprintf(&b, "%d. %s\n", i+1, stepLabel(s))
		}
		b.WriteString("\n")
	}
	b.WriteString("## 状态轨迹\n\n")
	b.WriteString("| Step | Label | Score | Nodes | Components | Diff |\n|---|---|---|---|---|---|\n")
	for _, st := range f.States {
		diff := "—"
		if st.Step > 0 {
			diff = fmt.Sprintf("%d", len(st.Diff.Changes))
		}
		fmt.Fprintf(&b, "| %d | %s | %d | %d | %d | %s |\n", st.Step, truncate(st.Label, 40), st.Map.Score, st.Map.Stats.Nodes, st.Map.Stats.Components, diff)
	}
	b.WriteString("\n## 状态间变化\n\n")
	for _, st := range f.States {
		if st.Step == 0 || len(st.Diff.Changes) == 0 {
			continue
		}
		fmt.Fprintf(&b, "### step %d → %s\n\n", st.Step, st.Label)
		for i, c := range st.Diff.Changes {
			if i >= 12 {
				b.WriteString("- …\n")
				break
			}
			fmt.Fprintf(&b, "- **%s** %s\n", c.Kind, truncate(c.Detail, 100))
		}
		b.WriteString("\n")
	}
	return b.String()
}

// SaveFlow persists a dynamic capture under .mocode/ui/flows/.
func SaveFlow(f Flow, workingDir string) (string, error) {
	dir := filepath.Join(Dir(workingDir), "flows")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", fmt.Errorf("create flows dir: %w", err)
	}
	stamp := time.Now().UTC().Format("20060102T150405")
	raw, err := jsonMarshalIndent(f)
	if err != nil {
		return "", fmt.Errorf("marshal flow: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, stamp+"-flow.json"), raw, 0o600); err != nil {
		return "", fmt.Errorf("write flow json: %w", err)
	}
	path := filepath.Join(dir, stamp+"-flow.md")
	if err := os.WriteFile(path, []byte(RenderFlow(f)), 0o600); err != nil {
		return "", fmt.Errorf("write flow markdown: %w", err)
	}
	return path, nil
}
