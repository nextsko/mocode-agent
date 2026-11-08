package browser

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

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
	Action    string `json:"action" enum:"map,review,replicate,diff,system,brand,tokens,skill,list" description:"map builds the full structural map; review returns only aesthetic findings; replicate emits a skeleton for one component; list shows saved maps."`
	URL       string `json:"url,omitempty" description:"Optional URL to navigate to before measuring; omit to use the current page."`
	Component string `json:"component,omitempty" description:"Component id (c1, c2, …) from a map, required for action=replicate."`
	Baseline  string `json:"baseline,omitempty" description:"For action=diff: path to a saved map JSON, or 'latest' (default)."`
	Target    string `json:"target,omitempty" enum:"html,react,vue,svelte" description:"For action=replicate: output framework (default html)."`
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
					"enum":        []string{"map", "review", "replicate", "diff", "system", "brand", "tokens", "skill", subActionList},
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
				"baseline": map[string]any{
					"type":        "string",
					"description": "For action=diff: path to a saved map JSON, or 'latest' (default).",
				},
				"target": map[string]any{
					"type":        "string",
					"enum":        []string{uidissect.TargetHTML, uidissect.TargetReact, uidissect.TargetVue, uidissect.TargetSvelte},
					"description": "For action=replicate: output framework (default html).",
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
	case "diff":
		return runDiff(ctx, manager, workingDir, params)
	case "system":
		return renderSystem(workingDir)
	case subActionList:
		entries, err := uidissect.List(workingDir)
		if err != nil {
			return contract.Error(err.Error()), nil
		}
		if len(entries) == 0 {
			return contract.Text("No UI maps saved yet. Run action=map first."), nil
		}
		return contract.Text("Saved UI maps:\n- " + strings.Join(entries, "\n- ")), nil
	case "map", "review", "replicate", "brand", "tokens", "skill":
	default:
		return contract.Error(`action must be one of: map, review, replicate, diff, system, brand, tokens, skill, list`), nil
	}

	raw, mode, err := measurePage(ctx, manager, params.URL)
	if err != nil {
		return contract.Error(err.Error()), nil
	}
	m := uidissect.Build(raw)

	if strings.EqualFold(params.Action, "skill") {
		return runSkill(m, params, workingDir), nil
	}
	if strings.EqualFold(params.Action, "brand") {
		brand := uidissect.Brand{
			Name:       firstNonEmpty(m.Title, m.URL),
			SourceURL:  m.URL,
			CapturedAt: time.Now().UTC(),
			Tokens:     m.Tokens,
		}
		path, err := uidissect.SaveBrand(brand, workingDir)
		if err != nil {
			return contract.Error("failed to save brand: " + err.Error()), nil //nolint:nilerr // tool-level error is reported as text to the model.
		}
		return contract.Text(fmt.Sprintf("Brand captured from %s: %d font sizes, %d colors, %d spacing values.\nSaved: `%s`", firstNonEmpty(m.Title, m.URL), len(m.Tokens.FontSizes), len(m.Tokens.Colors), len(m.Tokens.Spacing), path)), nil
	}
	if strings.EqualFold(params.Action, "tokens") {
		brand, ok := uidissect.LoadBrand(workingDir)
		if !ok {
			return contract.Text("No brand baseline yet. Run action=brand on a reference page first."), nil
		}
		drifts := uidissect.CompareTokens(brand.Tokens, m.Tokens)
		return contract.Text(uidissect.RenderTokenDrift(brand, m.Tokens, drifts)), nil
	}

	save := params.Save == nil || *params.Save
	if strings.EqualFold(params.Action, "review") {
		return contract.Text(renderReview(m, save, workingDir)), nil
	}
	if strings.EqualFold(params.Action, "replicate") {
		return replicateComponent(m, params.Component, params.Target, workingDir, save)
	}

	var saved uidissect.Saved
	if save {
		saved, err = uidissect.Save(m, workingDir)
		if err != nil {
			return contract.Error("failed to save map: " + err.Error()), nil //nolint:nilerr // tool-level error is reported as text to the model.
		}
	}
	text := uidissect.RenderMarkdown(m)
	if mode == "static" {
		text += "\n\n> static HTML mode: no browser was available, so boxes and computed styles are empty and layout/contrast findings are intentionally skipped."
	}
	if saved.Markdown != "" {
		text += fmt.Sprintf("\n\nSaved map: `%s`\nSaved JSON: `%s`", saved.Markdown, saved.JSONPath)
		if sysPath, err := updateSystem(workingDir); err == nil && sysPath != "" {
			text += fmt.Sprintf("\nDesign system updated: `%s`", sysPath)
		}
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

func replicateComponent(m uidissect.Map, id, target, workingDir string, save bool) (contract.ToolResult, error) {
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
	htmlOut, cssOut := uidissect.SkeletonFor(m, *comp, target)
	if target == "" {
		target = uidissect.TargetHTML
	}
	label := target
	if target == uidissect.TargetHTML {
		label = "html"
	}
	path := ""
	if save {
		p, err := uidissect.SaveComponent(m, *comp, htmlOut, cssOut, uidissect.ExtensionFor(target), workingDir)
		if err != nil {
			return contract.Error("failed to save component: " + err.Error()), nil //nolint:nilerr // tool-level error is reported as text to the model.
		}
		path = p
	}
	text := fmt.Sprintf("Component %s (%s) %s skeleton:\n\n```%s\n%s\n```\n\n```css\n%s\n```", comp.ID, comp.Kind, label, label, htmlOut, cssOut)
	if path != "" {
		text += fmt.Sprintf("\n\nSaved: `%s`", path)
	}
	return contract.Text(text), nil
}

// measurePage measures the live page through the browser; when no browser can
// be started but a URL was given, it falls back to static HTML so structural
// review still works on server-rendered pages.
func measurePage(ctx context.Context, manager *corebrowser.Manager, url string) (uidissect.Raw, string, error) {
	if manager != nil {
		raw, err := measureWithBrowser(ctx, manager, url)
		if err == nil {
			return raw, "browser", nil
		}
		if url == "" {
			return uidissect.Raw{}, "", err
		}
	} else if url == "" {
		return uidissect.Raw{}, "", fmt.Errorf("browser manager is not configured")
	}

	raw, err := fetchStatic(ctx, url)
	if err != nil {
		return uidissect.Raw{}, "", err
	}
	return raw, "static", nil
}

// measureWithBrowser attaches/launches and measures through CDP.
func measureWithBrowser(ctx context.Context, manager *corebrowser.Manager, url string) (uidissect.Raw, error) {
	session, err := manager.Ensure(ctx, corebrowser.EnsureOptions{URL: url})
	if err != nil {
		return uidissect.Raw{}, fmt.Errorf("failed to start browser: %w", err)
	}
	if url != "" {
		if err := session.Navigate(ctx, url); err != nil {
			return uidissect.Raw{}, fmt.Errorf("failed to navigate: %w", err)
		}
	}
	var raw uidissect.Raw
	if err := session.Evaluate(ctx, uidissect.CollectorScript, &raw); err != nil {
		return uidissect.Raw{}, fmt.Errorf("failed to measure page: %w", err)
	}
	return raw, nil
}

// fetchStatic downloads a page and parses its server-rendered HTML.
func fetchStatic(ctx context.Context, url string) (uidissect.Raw, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return uidissect.Raw{}, fmt.Errorf("invalid url: %w", err)
	}
	req.Header.Set("User-Agent", "mocode-ui-dissect/1.0")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return uidissect.Raw{}, fmt.Errorf("failed to fetch page: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return uidissect.Raw{}, fmt.Errorf("fetch returned status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return uidissect.Raw{}, fmt.Errorf("failed to read page: %w", err)
	}
	return uidissect.FromHTML(string(body), url)
}

// runDiff compares two dissections: the live page (when url is given) against
// a stored baseline, or the two most recent saved maps.
func runDiff(ctx context.Context, manager *corebrowser.Manager, workingDir string, params UIDissectParams) (contract.ToolResult, error) {
	maps, err := uidissect.LoadMaps(workingDir)
	if err != nil {
		return contract.Error("failed to load maps: " + err.Error()), nil //nolint:nilerr // tool-level error is reported as text to the model.
	}

	var base uidissect.Map
	if params.URL != "" {
		raw, _, err := measurePage(ctx, manager, params.URL)
		if err != nil {
			return contract.Error(err.Error()), nil
		}
		cur := uidissect.Build(raw)
		switch {
		case params.Baseline != "" && params.Baseline != "latest":
			base, err = uidissect.LoadMapFile(params.Baseline)
			if err != nil {
				return contract.Error(err.Error()), nil
			}
		case len(maps) > 0:
			base = maps[len(maps)-1]
		default:
			return contract.Text("No baseline map yet. Run action=map first, then diff again."), nil
		}
		return finishDiff(uidissect.Compare(base, cur), workingDir, params.Save), nil
	}

	if len(maps) < 2 {
		return contract.Text("Need at least two saved maps to diff. Run action=map on another page (or pass url)."), nil
	}
	base, cur := maps[len(maps)-2], maps[len(maps)-1]
	if params.Baseline != "" && params.Baseline != "latest" {
		base, err = uidissect.LoadMapFile(params.Baseline)
		if err != nil {
			return contract.Error(err.Error()), nil
		}
	}
	return finishDiff(uidissect.Compare(base, cur), workingDir, params.Save), nil
}

func finishDiff(d uidissect.Diff, workingDir string, save *bool) contract.ToolResult {
	text := uidissect.RenderDiff(d)
	if save == nil || *save {
		if path, err := uidissect.SaveDiff(d, workingDir); err == nil {
			text += "\n\nSaved: `" + path + "`"
		}
	}
	return contract.Text(text)
}

// runSkill stages a learn-ready skill draft for the page or one component.
func runSkill(m uidissect.Map, params UIDissectParams, workingDir string) contract.ToolResult {
	var comp *uidissect.Component
	if params.Component != "" {
		for i := range m.Components {
			if strings.EqualFold(m.Components[i].ID, params.Component) {
				comp = &m.Components[i]
				break
			}
		}
		if comp == nil {
			return contract.Error(fmt.Sprintf("component %q not found", params.Component))
		}
	}
	path, err := uidissect.SaveSkillDraft(m, comp, workingDir)
	if err != nil {
		return contract.Error("failed to stage skill draft: " + err.Error())
	}
	name, desc, body := uidissect.SkillDraft(m, comp)
	text := fmt.Sprintf("Skill draft staged: `%s`\n\nTo persist it in the skill library, call `learn` with:\n- name: %s\n- description: %s\n\n---\n\n%s", path, name, desc, uidissect.RenderSkillMarkdown(name, desc, body))
	return contract.Text(truncateForModel(text, 12000))
}

// updateSystem re-mines the site design system from all saved maps.
func updateSystem(workingDir string) (string, error) {
	maps, err := uidissect.LoadMaps(workingDir)
	if err != nil {
		return "", err
	}
	return uidissect.SaveSystem(uidissect.SystemFrom(maps), workingDir)
}

// renderSystem returns the persisted (or freshly mined) design system.
func renderSystem(workingDir string) (contract.ToolResult, error) {
	maps, err := uidissect.LoadMaps(workingDir)
	if err != nil {
		return contract.Error("failed to load maps: " + err.Error()), nil //nolint:nilerr // tool-level error is reported as text to the model.
	}
	if len(maps) == 0 {
		return contract.Text("No maps saved yet. Run action=map on a page first."), nil
	}
	sys := uidissect.SystemFrom(maps)
	path, err := uidissect.SaveSystem(sys, workingDir)
	if err != nil {
		return contract.Error("failed to save design system: " + err.Error()), nil //nolint:nilerr // tool-level error is reported as text to the model.
	}
	return contract.Text(uidissect.RenderSystem(sys) + "\n\nSaved: `" + path + "`"), nil
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
