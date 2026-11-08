package browser

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"

	corebrowser "github.com/nextsko/mocode-agent/internal/core/browser"
	"github.com/nextsko/mocode-agent/internal/core/tools/contract"
	"github.com/nextsko/mocode-agent/internal/core/uidissect"
)

//go:embed ui_dissect.md
var uiDissectDescription []byte

// UIDissectToolName is the name of the UI dissection tool.
const UIDissectToolName = "ui_dissect"

// UIDissectParams controls the dissection actions.
type UIDissectParams struct {
	Action    string `json:"action" enum:"map,review,replicate,list" description:"map builds the full structural map; review returns only aesthetic findings; replicate emits a skeleton for one component; list shows saved maps."`
	URL       string `json:"url,omitempty" description:"Optional URL to navigate to before measuring; omit to use the current page."`
	Component string `json:"component,omitempty" description:"Component id (c1, c2, …) from a map, required for action=replicate."`
	Save      *bool  `json:"save,omitempty" description:"Persist the map/skeleton under .mocode/ui (default true)."`
}

// NewUIDissectPortTool builds the UI dissection tool. It shares the browser
// Manager with the browser tool so a session can be reused across calls.
func NewUIDissectPortTool(manager *corebrowser.Manager, workingDir string) contract.Tool {
	return contract.NewFunc(
		UIDissectToolName,
		"Dissect a live page into a structural map (components, aesthetics, design tokens) for UI review and replication.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"action": map[string]any{
					"type":        "string",
					"enum":        []string{"map", "review", "replicate", subActionList},
					"description": "map builds the full structural map; review returns only aesthetic findings; replicate emits a skeleton for one component; list shows saved maps.",
				},
				"url": map[string]any{
					"type":        "string",
					"description": "Optional URL to navigate to before measuring; omit to use the current page.",
				},
				"component": map[string]any{
					"type":        "string",
					"description": "Component id (c1, c2, …) from a map, required for action=replicate.",
				},
				"save": map[string]any{
					"type":        "boolean",
					"description": "Persist the map/skeleton under .mocode/ui (default true).",
				},
			},
			"required": []string{"action"},
		},
		func(ctx context.Context, _ contract.ToolContext, args json.RawMessage) (contract.ToolResult, error) {
			var params UIDissectParams
			if len(args) > 0 {
				if err := json.Unmarshal(args, &params); err != nil {
					return contract.ToolResult{}, fmt.Errorf("invalid parameters: %w", err)
				}
			}
			return runUIDissect(ctx, manager, workingDir, params)
		},
	)
}

func runUIDissect(ctx context.Context, manager *corebrowser.Manager, workingDir string, params UIDissectParams) (contract.ToolResult, error) {
	switch strings.ToLower(strings.TrimSpace(params.Action)) {
	case subActionList:
		entries, err := uidissect.List(workingDir)
		if err != nil {
			return contract.Error(err.Error()), nil
		}
		if len(entries) == 0 {
			return contract.Text("No UI maps saved yet. Run action=map first."), nil
		}
		return contract.Text("Saved UI maps:\n- " + strings.Join(entries, "\n- ")), nil
	case "map", "review", "replicate":
	default:
		return contract.Error(`action must be one of: map, review, replicate, list`), nil
	}

	if manager == nil {
		return contract.Error("browser manager is not configured"), nil
	}
	session, err := manager.Ensure(ctx, corebrowser.EnsureOptions{URL: params.URL})
	if err != nil {
		return contract.Error("failed to start browser: " + err.Error()), nil //nolint:nilerr // tool-level error is reported as text to the model.
	}
	if params.URL != "" {
		if err := session.Navigate(ctx, params.URL); err != nil {
			return contract.Error("failed to navigate: " + err.Error()), nil //nolint:nilerr // tool-level error is reported as text to the model.
		}
	}

	var raw uidissect.Raw
	if err := session.Evaluate(ctx, uidissect.CollectorScript, &raw); err != nil {
		return contract.Error("failed to measure page: " + err.Error()), nil //nolint:nilerr // tool-level error is reported as text to the model.
	}
	m := uidissect.Build(raw)

	save := params.Save == nil || *params.Save
	if strings.EqualFold(params.Action, "review") {
		return contract.Text(renderReview(m, save, workingDir)), nil
	}
	if strings.EqualFold(params.Action, "replicate") {
		return replicateComponent(m, params.Component, workingDir, save)
	}

	var saved uidissect.Saved
	if save {
		saved, err = uidissect.Save(m, workingDir)
		if err != nil {
			return contract.Error("failed to save map: " + err.Error()), nil //nolint:nilerr // tool-level error is reported as text to the model.
		}
	}
	text := uidissect.RenderMarkdown(m)
	if saved.Markdown != "" {
		text += fmt.Sprintf("\n\nSaved map: `%s`\nSaved JSON: `%s`", saved.Markdown, saved.JSONPath)
	}
	return contract.Text(truncateForModel(text, 12000)), nil
}

func renderReview(m uidissect.Map, save bool, workingDir string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# UI Review · %s\n\n", firstNonEmpty(m.Title, m.URL))
	fmt.Fprintf(&b, "%d nodes · %d components · %d findings\n\n", m.Stats.Nodes, m.Stats.Components, m.Stats.Findings)
	if len(m.Findings) == 0 {
		b.WriteString("No findings — the measured surface is consistent.\n")
	}
	for _, f := range m.Findings {
		icon := map[string]string{"error": "🔴", "warn": "🟠", "info": "🔵"}[f.Severity]
		fmt.Fprintf(&b, "- %s [%s] %s", icon, f.Category, f.Message)
		if f.Suggestion != "" {
			fmt.Fprintf(&b, " — %s", f.Suggestion)
		}
		b.WriteString("\n")
	}
	if save {
		if saved, err := uidissect.Save(m, workingDir); err == nil {
			fmt.Fprintf(&b, "\nSaved: `%s`", saved.Markdown)
		}
	}
	return b.String()
}

func replicateComponent(m uidissect.Map, id, workingDir string, save bool) (contract.ToolResult, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return contract.Error("component is required for action=replicate (for example c1)"), nil
	}
	var comp *uidissect.Component
	for i := range m.Components {
		if strings.EqualFold(m.Components[i].ID, id) {
			comp = &m.Components[i]
			break
		}
	}
	if comp == nil {
		ids := make([]string, 0, len(m.Components))
		for _, c := range m.Components {
			ids = append(ids, c.ID+"("+c.Kind+")")
		}
		return contract.Error(fmt.Sprintf("component %q not found; available: %s", id, strings.Join(ids, ", "))), nil
	}
	htmlOut, cssOut := uidissect.Skeleton(m, *comp)
	path := ""
	if save {
		p, err := uidissect.SaveComponent(m, *comp, htmlOut, cssOut, workingDir)
		if err != nil {
			return contract.Error("failed to save component: " + err.Error()), nil //nolint:nilerr // tool-level error is reported as text to the model.
		}
		path = p
	}
	text := fmt.Sprintf("Component %s (%s) skeleton:\n\n```html\n%s\n```\n\n```css\n%s\n```", comp.ID, comp.Kind, htmlOut, cssOut)
	if path != "" {
		text += fmt.Sprintf("\n\nSaved: `%s`", path)
	}
	return contract.Text(text), nil
}

func truncateForModel(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "\n\n… (map truncated; full markdown saved on disk)"
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
