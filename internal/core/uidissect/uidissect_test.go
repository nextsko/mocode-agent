package uidissect

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

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
