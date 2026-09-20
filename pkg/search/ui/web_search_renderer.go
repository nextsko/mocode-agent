// WebSearchRenderer: the web search tool rendering entry point. It maps
// call args and SearchResponse values onto the OutputBlock, TreeList,
// Markdown, and status line components, mirroring the TS
// webSearchToolRenderer (render.ts).
package ui

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/nextsko/mocode-agent/pkg/search"
	"github.com/nextsko/mocode-agent/pkg/search/query"
	"github.com/nextsko/mocode-agent/pkg/search/ui/theme"
)

// searchTitle is the tool title shown in status lines.
const searchTitle = "Web Search"

// fallbackCollapsedLines caps fallback raw-text rows when collapsed.
const fallbackCollapsedLines = 6

// queryPreviewWidth bounds the query preview fragment.
const queryPreviewWidth = 80

// providerLabels mirrors the TS SEARCH_PROVIDER_LABELS table; unknown ids
// fall back to the raw id.
var providerLabels = map[string]string{
	"perplexity": "Perplexity",
	"gemini":     "Gemini",
	"anthropic":  "Anthropic",
	"codex":      "OpenAI",
	"xai":        "xAI",
	"zai":        "Z.AI",
	"exa":        "Exa",
	"tinyfish":   "TinyFish",
	"jina":       "Jina",
	"kagi":       "Kagi",
	"tavily":     "Tavily",
	"firecrawl":  "Firecrawl",
	"brave":      "Brave",
	"kimi":       "Kimi",
	"parallel":   "Parallel",
	"synthetic":  "Synthetic",
	"searxng":    "SearXNG",
	"startpage":  "Startpage",
	"duckduckgo": "DuckDuckGo",
	"ecosia":     "Ecosia",
	"google":     "Google",
	"mojeek":     "Mojeek",
	"public":     "Public Web",
}

// providerLabel resolves a provider id to its display label.
func providerLabel(id string) string {
	if label, ok := providerLabels[id]; ok {
		return label
	}
	return id
}

// authShort renders the authentication mode suffix ("oauth" → "OAuth",
// "api_key" → "API", anything else verbatim).
func authShort(mode string) string {
	switch mode {
	case "oauth":
		return "OAuth"
	case "api_key":
		return "API"
	default:
		return mode
	}
}

// RendererOptions configures a WebSearchRenderer; zero values fall back
// to the package defaults.
type RendererOptions struct {
	// MaxAnswerLines caps the rendered answer in compact (collapsed)
	// mode; 0 means DefaultMaxAnswerLines.
	MaxAnswerLines int
	// MaxCollapsedItems is the collapsed tree row budget; 0 means
	// theme.MaxCollapsedItems.
	MaxCollapsedItems int
	// MarkdownStyle is the glamour standard style name; "" means dark.
	MarkdownStyle string
}

// RenderOptions configures one RenderResult call.
type RenderOptions struct {
	// Width is the outer block width in terminal cells.
	Width int
	// Expanded reports whether the user expanded the block.
	Expanded bool
	// Query is the optional query preview from the call args.
	Query string
}

// WebSearchRenderer renders web search tool calls and results. It holds
// the Theme plus tunable limits, and reuses one cached OutputBlock across
// renders. Instances are safe for concurrent use.
type WebSearchRenderer struct {
	theme theme.Theme
	opts  RendererOptions
	block *OutputBlock
	tree  *TreeList
}

// New returns a WebSearchRenderer bound to t; nil options fields take
// their defaults.
func New(t theme.Theme, opts RendererOptions) *WebSearchRenderer {
	if opts.MaxAnswerLines < 1 {
		opts.MaxAnswerLines = DefaultMaxAnswerLines
	}
	if opts.MaxCollapsedItems < 1 {
		opts.MaxCollapsedItems = theme.MaxCollapsedItems
	}
	return &WebSearchRenderer{
		theme: t,
		opts:  opts,
		block: NewOutputBlock(t),
		tree: NewTreeList(t,
			WithMaxCollapsedItems(opts.MaxCollapsedItems),
			WithTreeItemType("source")),
	}
}

// MergeCallAndResult reports that call and result render in one merged
// block, matching the TS webSearchToolRenderer.
func (r *WebSearchRenderer) MergeCallAndResult() bool { return true }

// Invalidate drops the cached framed renders (e.g. after a theme switch).
func (r *WebSearchRenderer) Invalidate() { r.block.Invalidate() }

// RenderCall renders the pre-execution call line: a pending status line
// showing the query (parsed to surface directive counts) plus key
// parameters like limit. Unparseable args degrade to a raw JSON fragment.
func (r *WebSearchRenderer) RenderCall(args map[string]any) []string {
	rawQuery, ok := args["query"].(string)
	if !ok {
		return []string{theme.RenderStatusLine(r.theme, theme.StatePending,
			searchTitle, r.rawArgsPreview(args), "")}
	}

	sq := query.ParseSearchQuery(rawQuery)
	desc := theme.TruncateToWidth(sq.Raw, queryPreviewWidth, theme.DefaultEllipsis)
	meta := r.callMeta(args, &sq)
	return []string{theme.RenderStatusLine(r.theme, theme.StatePending,
		searchTitle, desc, meta)}
}

// rawArgsPreview degrades unparseable call args to a compact JSON
// fragment.
func (r *WebSearchRenderer) rawArgsPreview(args map[string]any) string {
	fragment := "{}"
	if len(args) > 0 {
		if b, err := json.Marshal(args); err == nil {
			fragment = string(b)
		}
	}
	return theme.TruncateToWidth(fragment, queryPreviewWidth, theme.DefaultEllipsis)
}

// callMeta assembles the dim meta fragments after the query: limit,
// recency, and a directive count when the query embeds operators.
func (r *WebSearchRenderer) callMeta(args map[string]any, sq *query.StructuredQuery) string {
	var parts []string
	if n, ok := numericArg(args["limit"]); ok && n > 0 {
		parts = append(parts, fmt.Sprintf("limit %d", n))
	}
	if s, ok := args["recency"].(string); ok && s != "" {
		parts = append(parts, "recency "+s)
	}
	if n := countDirectives(sq); n > 0 {
		parts = append(parts, FormatCount("directive", n))
	}
	return strings.Join(parts, " ")
}

// numericArg extracts an integer from a JSON-decoded number.
func numericArg(v any) (int64, bool) {
	switch n := v.(type) {
	case float64:
		return int64(n), true
	case int:
		return int64(n), true
	case int64:
		return n, true
	case json.Number:
		if i, err := n.Int64(); err == nil {
			return i, true
		}
	}
	return 0, false
}

// countDirectives totals every parsed directive constraint.
func countDirectives(sq *query.StructuredQuery) int {
	n := len(sq.Sites) + len(sq.ExcludedSites) +
		len(sq.InURL) + len(sq.ExcludedInURL) +
		len(sq.InTitle) + len(sq.ExcludedInTitle) +
		len(sq.InText) + len(sq.ExcludedInText) +
		len(sq.Filetypes) + len(sq.ExcludedFiletypes)
	if sq.After != nil {
		n++
	}
	if sq.Before != nil {
		n++
	}
	if sq.Lang != nil {
		n++
	}
	return n
}

// RenderResult renders a successful provider response as a framed block:
// optional Query section, glamour Answer section, collapsible Sources
// tree, and Metadata section. A nil response degrades to the plain
// fallback tree.
func (r *WebSearchRenderer) RenderResult(resp *search.SearchResponse, opts RenderOptions) []string {
	if resp == nil {
		return r.renderFallbackLines(opts.Query, opts.Expanded)
	}

	t := r.theme
	sourceCount := len(resp.Sources)
	label := providerLabel(resp.Provider)
	if resp.Provider == "none" {
		label = "None"
	}

	var header string
	if sourceCount > 0 {
		header = RenderStatusLineWithIcon(t,
			t.StyledSymbol("tool.webSearch", "⌕"),
			searchTitle, label, FormatCount("source", sourceCount))
	} else {
		header = theme.RenderStatusLine(t, theme.StateWarning,
			searchTitle, label, FormatCount("source", sourceCount))
	}

	preview := opts.Query
	if preview == "" && len(resp.SearchQueries) > 0 {
		preview = resp.SearchQueries[0]
	}
	preview = theme.TruncateToWidth(preview, queryPreviewWidth, theme.DefaultEllipsis)

	answerLines := RenderMarkdown(resp.Answer,
		OutputBlockContentWidth(opts.Width),
		MarkdownOptions{
			Theme:          t,
			Compact:        !opts.Expanded,
			MaxAnswerLines: r.opts.MaxAnswerLines,
			Style:          r.opts.MarkdownStyle,
		})

	sourceLines := r.renderSources(resp.Sources, opts)
	metaLines := r.metadataLines(resp, label)

	sections := make([]Section, 0, 4)
	if preview != "" {
		sections = append(sections, Section{Lines: []string{
			t.FG(theme.ColorMuted, "Query:") + " " + t.FG(theme.ColorText, preview),
		}})
	}
	sections = append(sections,
		Section{Label: t.FG(theme.ColorToolTitle, "Answer"), Lines: answerLines},
		Section{Label: t.FG(theme.ColorToolTitle, "Sources"), Lines: sourceLines},
		Section{Label: t.FG(theme.ColorToolTitle, "Metadata"), Lines: metaLines},
	)

	state := theme.StateSuccess
	if sourceCount == 0 {
		state = theme.StateWarning
	}
	return r.block.Render(OutputBlockOptions{
		Header:   header,
		State:    state,
		Sections: sections,
		Width:    opts.Width,
		ApplyBG:  true,
	})
}

// renderSources maps response sources onto tree rows, substituting the
// "No sources returned" degradation line for empty lists.
func (r *WebSearchRenderer) renderSources(sources []search.SearchSource, opts RenderOptions) []string {
	t := r.theme
	if len(sources) == 0 {
		return []string{t.FG(theme.ColorMuted, "No sources returned")}
	}
	items := make([]TreeItem, 0, len(sources))
	for _, src := range sources {
		items = append(items, sourceTreeItem(t, src))
	}
	return r.tree.RenderItems(items, opts.Expanded, opts.Width)
}

// sourceTreeItem converts one SearchSource into a TreeItem: linked
// accent title, dim domain, muted age.
func sourceTreeItem(t theme.Theme, src search.SearchSource) TreeItem {
	title := strings.TrimSpace(src.Title)
	if title == "" {
		title = strings.TrimSpace(src.URL)
	}
	if title == "" {
		title = "Untitled"
	}

	var metaParts []string
	if domain := theme.GetDomain(src.URL); domain != "" {
		metaParts = append(metaParts, t.FG(theme.ColorDim, "("+domain+")"))
	}
	if age := theme.FormatAgeOrDate(src.AgeSeconds, src.PublishedDate); age != "" {
		metaParts = append(metaParts, t.FG(theme.ColorMuted, age))
	}
	// The TS sep.dot glyph is " · "; the Go theme stores the bare "·", so
	// the join re-adds the surrounding spaces for the same visual.
	sep := t.FG(theme.ColorDim, " "+t.Sep().Dot+" ")
	meta := strings.Join(metaParts, sep)

	return TreeItem{Title: title, Meta: meta, Link: src.URL}
}

// metadataLines builds the Metadata section: provider identity plus usage
// metrics joined by the dim separator dot.
func (r *WebSearchRenderer) metadataLines(resp *search.SearchResponse, label string) []string {
	t := r.theme

	providerInfo := label
	if resp.Model != "" {
		providerInfo = resp.Model + " @ " + label
	}
	if short := authShort(resp.AuthMode); short != "" {
		providerInfo += " (" + short + ")"
	}
	lines := []string{
		t.FG(theme.ColorMuted, "Provider:") + " " + t.FG(theme.ColorText, providerInfo),
	}

	if resp.Usage == nil {
		return lines
	}
	var usageParts []string
	if resp.Usage.InputTokens > 0 {
		usageParts = append(usageParts, fmt.Sprintf("in %d", resp.Usage.InputTokens))
	}
	if resp.Usage.OutputTokens > 0 {
		usageParts = append(usageParts, fmt.Sprintf("out %d", resp.Usage.OutputTokens))
	}
	if resp.Usage.TotalTokens > 0 {
		usageParts = append(usageParts, fmt.Sprintf("total %d", resp.Usage.TotalTokens))
	}
	if resp.Usage.SearchRequests > 0 {
		usageParts = append(usageParts, fmt.Sprintf("search %d", resp.Usage.SearchRequests))
	}
	if len(usageParts) > 0 {
		sep := t.FG(theme.ColorDim, " "+t.Sep().Dot+" ")
		lines = append(lines,
			t.FG(theme.ColorMuted, "Usage:")+" "+t.FG(theme.ColorText, strings.Join(usageParts, sep)))
	}
	return lines
}

// RenderError renders a failed search as a framed error panel reusing the
// success layout (error-state border, "Error:" body, embedded
// classification for SearchProviderError). Widths below one cell degrade
// to a single red line.
func (r *WebSearchRenderer) RenderError(err error, width int) []string {
	message := "unknown error"
	if err != nil {
		message = err.Error()
	}

	if width < 1 {
		return []string{r.theme.FG(theme.ColorError,
			"Error: "+replaceTabs(message))}
	}

	desc := ""
	var spe *search.SearchProviderError
	if errors.As(err, &spe) && spe.Provider != "" && spe.Provider != "none" {
		desc = providerLabel(spe.Provider)
	}
	header := theme.RenderStatusLine(r.theme, theme.StateError, searchTitle, desc, "")

	body := []string{
		r.theme.FG(theme.ColorError, "Error: "+replaceTabs(message)),
	}
	if spe != nil {
		var parts []string
		if spe.Code != "" {
			parts = append(parts, "code "+spe.Code)
		}
		if spe.HTTPStatus > 0 {
			parts = append(parts, fmt.Sprintf("http %d", spe.HTTPStatus))
		}
		if len(parts) > 0 {
			sep := r.theme.FG(theme.ColorDim, " "+r.theme.Sep().Dot+" ")
			body = append(body,
				r.theme.FG(theme.ColorDim, strings.Join(parts, sep)))
		}
	}

	return r.block.Render(OutputBlockOptions{
		Header:   header,
		State:    theme.StateError,
		Sections: []Section{{Lines: body}},
		Width:    width,
		ApplyBG:  true,
	})
}

// renderFallbackLines renders the plain (frame-less) fallback tree for a
// missing structured response, mirroring the TS renderFallbackText.
func (r *WebSearchRenderer) renderFallbackLines(contentText string, expanded bool) []string {
	t := r.theme
	var content []string
	for _, line := range strings.Split(contentText, "\n") {
		if strings.TrimSpace(line) != "" {
			content = append(content, strings.TrimSpace(line))
		}
	}
	maxLines := len(content)
	if !expanded && maxLines > fallbackCollapsedLines {
		maxLines = fallbackCollapsedLines
	}
	display := content[:maxLines]
	remaining := len(content) - len(display)

	lines := []string{
		t.StatusIcon(theme.StateWarning) + " " +
			t.Dim("Response") + theme.FormatExpandHint(expanded, remaining > 0),
	}
	if len(display) == 0 {
		return append(lines, " "+t.Dim(t.Tree().Last)+" "+
			t.FG(theme.ColorMuted, "No response data"))
	}
	for i, line := range display {
		branch := t.Tree().Branch
		if i == len(display)-1 && remaining == 0 {
			branch = t.Tree().Last
		}
		lines = append(lines, " "+t.Dim(branch)+" "+
			t.Dim(theme.TruncateToWidth(line, 110, theme.DefaultEllipsis)))
	}
	if !expanded && remaining > 0 {
		lines = append(lines, " "+t.Dim(t.Tree().Last)+" "+
			t.FG(theme.ColorMuted, theme.FormatMoreItems(remaining, "line")))
	}
	return lines
}

// replaceTabs swaps tabs for spaces so they cannot break block layout.
func replaceTabs(s string) string {
	return strings.ReplaceAll(s, "\t", "    ")
}
