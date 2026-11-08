package uidissect

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func fixture() Raw {
	raw := Raw{URL: "https://example.com/pricing", Title: "Pricing"}
	raw.Viewport.W, raw.Viewport.H = 1280, 800
	raw.Nodes = []Node{
		{Path: "body", Tag: "body", Depth: 0, Rect: Rect{W: 1280, H: 2400}, Styles: Styles{Background: "rgb(255,255,255)"}},
		{Path: "header", Tag: "header", Depth: 1, Rect: Rect{W: 1280, H: 64}, Styles: Styles{Background: "rgb(20,20,20)"}},
		{Path: "h1", Tag: "h1", Depth: 2, Text: "Plans", Rect: Rect{W: 300, H: 40}, Styles: Styles{FontSize: 32, Color: "rgb(255,255,255)"}},
		{Path: "card1", Tag: "div", Depth: 1, Classes: []string{"card"}, Rect: Rect{W: 320, H: 200}, Styles: Styles{Background: "rgb(245,245,245)"}},
		{Path: "card1 > h2", Tag: "h2", Depth: 2, Text: "Starter", Rect: Rect{W: 200, H: 24}, Styles: Styles{FontSize: 20, Color: "rgb(120,120,120)"}},
		{Path: "card2", Tag: "div", Depth: 1, Classes: []string{"card"}, Rect: Rect{W: 320, H: 200}, Styles: Styles{Background: "rgb(245,245,245)"}},
		{Path: "card2 > h2", Tag: "h2", Depth: 2, Text: "Team", Rect: Rect{W: 200, H: 24}, Styles: Styles{FontSize: 20, Color: "rgb(120,120,120)"}},
		{Path: "card3", Tag: "div", Depth: 1, Classes: []string{"card"}, Rect: Rect{W: 320, H: 200}, Styles: Styles{Background: "rgb(245,245,245)"}},
		{Path: "cta", Tag: "button", Depth: 1, Text: "Buy", Rect: Rect{W: 24, H: 24}, Interactive: true, Styles: Styles{FontSize: 11, Color: "rgb(200,200,200)", Background: "rgb(255,255,255)"}},
		{Path: "logo", Tag: "img", Depth: 1, ImgAlt: "", Rect: Rect{W: 40, H: 40}},
	}
	return raw
}

func TestBuildSegmentsRepeatedCardsAndFindsIssues(t *testing.T) {
	t.Parallel()
	m := Build(fixture())

	require.Equal(t, 10, m.Stats.Nodes)
	require.NotEmpty(t, m.Components)

	var cards *Component
	for i := range m.Components {
		if m.Components[i].Kind == "card" {
			cards = &m.Components[i]
			break
		}
	}
	require.NotNil(t, cards, "three similar .card siblings should segment as one repeated component")
	assert.Equal(t, 3, cards.Repeat)

	var hasContrast, hasTarget, hasAlt bool
	for _, f := range m.Findings {
		switch f.Category {
		case "contrast":
			hasContrast = true
		case "touch-target":
			hasTarget = true
		case "a11y":
			if strings.Contains(f.Message, "alt") {
				hasAlt = true
			}
		}
	}
	assert.True(t, hasContrast, "low-contrast text must be reported")
	assert.True(t, hasTarget, "small button must be reported")
	assert.True(t, hasAlt, "img without alt must be reported")

	assert.NotEmpty(t, m.Tokens.FontSizes)
	assert.Contains(t, m.Tokens.FontSizes, 20.0)
}

func TestRenderMarkdownHasAllSections(t *testing.T) {
	t.Parallel()
	md := RenderMarkdown(Build(fixture()))
	for _, want := range []string{"# 庖丁解牛", "## 结构地图", "## 组件切片", "## 审美审查", "## 设计令牌", "## 复刻入口"} {
		assert.Contains(t, md, want)
	}
	assert.Contains(t, md, "card")
}

func TestSkeletonProducesHTMLAndCSS(t *testing.T) {
	t.Parallel()
	m := Build(fixture())
	require.NotEmpty(t, m.Components)
	html, css := Skeleton(m, m.Components[0])
	assert.Contains(t, html, "<section")
	assert.Contains(t, css, ".c-")
}

func TestSaveWritesArtifactsAndIndex(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	saved, err := Save(Build(fixture()), dir)
	require.NoError(t, err)
	_, err = os.Stat(saved.JSONPath)
	require.NoError(t, err)
	_, err = os.Stat(saved.Markdown)
	require.NoError(t, err)

	index, err := os.ReadFile(filepath.Join(Dir(dir), "index.md"))
	require.NoError(t, err)
	assert.Contains(t, string(index), "Pricing")

	entries, err := List(dir)
	require.NoError(t, err)
	assert.NotEmpty(t, entries)
}

func TestSystemFromClustersAcrossMaps(t *testing.T) {
	t.Parallel()
	m1 := Build(fixture())
	raw2 := fixture()
	raw2.URL = "https://example.com/about"
	raw2.Title = "About"
	raw2.Nodes[3].Text = "Different copy"
	m2 := Build(raw2)

	sys := SystemFrom([]Map{m1, m2})
	require.Equal(t, 2, sys.Pages)
	require.NotEmpty(t, sys.Clusters)

	var cards *Cluster
	for i := range sys.Clusters {
		if sys.Clusters[i].Kind == "card" {
			cards = &sys.Clusters[i]
			break
		}
	}
	require.NotNil(t, cards, "card shape should cluster across pages")
	assert.Equal(t, 2, cards.Count)
	assert.Len(t, cards.Pages, 2)
	assert.Contains(t, RenderSystem(sys), "站点设计系统")
}

func TestFromHTMLBuildsStaticMap(t *testing.T) {
	t.Parallel()
	html := `<!doctype html><html><head><title>Demo</title></head><body>
<header><h1>Demo</h1></header>
<main><div class="card"><h2>A</h2><button>Go</button></div>
<div class="card"><h2>B</h2><button>Go</button></div>
<div class="card"><h2>C</h2><button>Go</button></div></main>
<footer><img src="x.png"></footer></body></html>`
	raw, err := FromHTML(html, "https://example.com/demo")
	require.NoError(t, err)
	require.NotEmpty(t, raw.Nodes)
	m := Build(raw)
	require.NotEmpty(t, m.Components)
	assert.Contains(t, RenderMarkdown(m), "Demo")
}

func TestCompareReportsBoxAndStyleChanges(t *testing.T) {
	t.Parallel()
	base := Build(fixture())
	raw := fixture()
	raw.Nodes[3].Rect.X = 40
	raw.Nodes[3].Rect.W = 360
	raw.Nodes[2].Styles.Color = "rgb(0,0,0)"
	raw.Nodes = append(raw.Nodes, Node{Path: "extra", Tag: "p", Depth: 1, Text: "new", Rect: Rect{W: 100, H: 20}})
	cur := Build(raw)

	diff := Compare(base, cur)
	require.NotEmpty(t, diff.Changes)
	assert.Greater(t, diff.Summary["moved"], 0)
	assert.Greater(t, diff.Summary["resized"], 0)
	assert.Greater(t, diff.Summary["restyled"], 0)
	assert.Greater(t, diff.Summary["added"], 0)
	assert.Contains(t, RenderDiff(diff), "UI 视觉回归")
}

func TestCompareIdenticalMapsIsClean(t *testing.T) {
	t.Parallel()
	m := Build(fixture())
	diff := Compare(m, m)
	assert.Empty(t, diff.Changes)
	assert.Contains(t, RenderDiff(diff), "No differences")
}

func TestCompareTokensReportsDrift(t *testing.T) {
	t.Parallel()
	brand := Tokens{
		FontSizes: []float64{12, 14, 20, 32},
		Spacing:   []float64{4, 8, 16},
		Colors:    []string{"rgb(20,20,20)", "rgb(255,255,255)"},
		Radii:     []string{"8px"},
	}
	cur := Tokens{
		FontSizes: []float64{13, 20, 36},
		Spacing:   []float64{4, 12},
		Colors:    []string{"rgb(20,20,20)", "rgb(255,0,0)"},
		Radii:     []string{"8px"},
	}
	drifts := CompareTokens(brand, cur)
	require.NotEmpty(t, drifts)
	var offBrand, unused int
	for _, d := range drifts {
		switch d.Status {
		case "off-brand":
			offBrand++
		case "unused":
			unused++
		}
	}
	assert.Greater(t, offBrand, 0)
	assert.Greater(t, unused, 0)
	assert.Contains(t, RenderTokenDrift(Brand{Name: "brand"}, cur, drifts), "品牌令牌对比")
}

func TestSkeletonForTargets(t *testing.T) {
	t.Parallel()
	m := Build(fixture())
	require.NotEmpty(t, m.Components)
	comp := m.Components[0]

	react, _ := SkeletonFor(m, comp, TargetReact)
	assert.Contains(t, react, "className=")
	assert.NotContains(t, react, `class="`)

	vue, _ := SkeletonFor(m, comp, TargetVue)
	assert.Contains(t, vue, "<template>")
	assert.Contains(t, vue, "<style scoped>")

	svelte, _ := SkeletonFor(m, comp, TargetSvelte)
	assert.Contains(t, svelte, "<style>")

	html, _ := SkeletonFor(m, comp, TargetHTML)
	assert.Contains(t, html, `<section`)

	assert.Equal(t, ".jsx", ExtensionFor(TargetReact))
	assert.Equal(t, ".vue", ExtensionFor(TargetVue))
	assert.Equal(t, ".svelte", ExtensionFor(TargetSvelte))
	assert.Equal(t, ".html", ExtensionFor(TargetHTML))
}

func TestSkillDraftIsLearnReady(t *testing.T) {
	t.Parallel()
	m := Build(fixture())
	name, desc, body := SkillDraft(m, nil)
	assert.True(t, strings.HasPrefix(desc, "Use when"), "skill descriptions must start with 'Use when'")
	assert.NotEmpty(t, name)
	doc := RenderSkillMarkdown(name, desc, body)
	assert.Contains(t, doc, "---")
	assert.Contains(t, doc, "name: "+name)
	assert.Contains(t, doc, "## 组件清单")
	assert.Contains(t, doc, "## 设计令牌")
}

func TestRecordFlowCapturesStatesAndDiffs(t *testing.T) {
	t.Parallel()
	base := Build(fixture())
	mutated := fixture()
	mutated.Nodes[3].Rect.X = 80
	second := Build(mutated)

	calls := 0
	flow, err := RecordFlow("https://example.com", []FlowStep{{Action: "click", Target: "#buy"}},
		func(FlowStep) error { calls++; return nil },
		func() (Map, error) {
			if calls == 0 {
				return base, nil
			}
			return second, nil
		})
	require.NoError(t, err)
	assert.Equal(t, 1, calls)
	require.Len(t, flow.States, 2)
	assert.NotEmpty(t, flow.States[1].Diff.Changes)
	assert.Contains(t, RenderFlow(flow), "动态 UI 流")
}

func TestBuildTasteFindsRecurringLessons(t *testing.T) {
	t.Parallel()
	m1 := Build(fixture())
	raw2 := fixture()
	raw2.URL = "https://example.com/about"
	raw2.Title = "About"
	m2 := Build(raw2)

	taste := BuildTaste([]Map{m1, m2})
	require.Equal(t, 2, taste.Pages)
	assert.Len(t, taste.ScoreTrend, 2)
	require.NotEmpty(t, taste.Recurring)
	require.NotEmpty(t, taste.Lessons)
	joined := strings.Join(taste.Lessons, "\n")
	assert.Contains(t, joined, "contrast")
	assert.Contains(t, RenderTaste(taste), "审美复盘")
}

func TestEvolutionAndRulesSkill(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	base := Build(fixture())
	cur := Build(fixture())
	cur.Score = base.Score - 6
	d := Compare(base, cur)
	require.NotZero(t, d.BaseScore)

	require.NoError(t, AppendEvolution(EvolutionEntry{
		At: time.Now().UTC(), BaseURL: base.URL, CurrentURL: cur.URL,
		BaseScore: base.Score, CurScore: cur.Score, Delta: cur.Score - base.Score, Changes: len(d.Changes),
	}, dir))
	require.NoError(t, AppendEvolution(EvolutionEntry{
		At: time.Now().UTC(), BaseURL: cur.URL, CurrentURL: base.URL,
		BaseScore: cur.Score, CurScore: base.Score, Delta: base.Score - cur.Score, Changes: 2,
	}, dir))

	entries, err := LoadEvolution(dir)
	require.NoError(t, err)
	require.Len(t, entries, 2)
	stats := SummarizeEvolution(entries)
	assert.Equal(t, 2, stats.Runs)
	assert.Equal(t, 1, stats.Improved)
	assert.Equal(t, 1, stats.Degraded)

	taste := BuildTaste([]Map{base, cur}, entries...)
	assert.NotEmpty(t, taste.Evolution)
	assert.Contains(t, RenderTaste(taste), "进化记录")

	name, desc, body := RulesSkillDraft(taste)
	assert.Equal(t, "ui-taste-rules", name)
	assert.True(t, strings.HasPrefix(desc, "Use when"))
	assert.Contains(t, body, "回归纪律")
	assert.Contains(t, body, "规则")
}
