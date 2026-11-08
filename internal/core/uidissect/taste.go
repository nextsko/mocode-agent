package uidissect

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Recurring is a finding that keeps appearing across pages/rounds.
type Recurring struct {
	Category   string    `json:"category"`
	Message    string    `json:"message"`
	Suggestion string    `json:"suggestion,omitempty"`
	Count      int       `json:"count"`
	Pages      int       `json:"pages"`
	FirstSeen  time.Time `json:"first_seen"`
	LastSeen   time.Time `json:"last_seen"`
}

// ScorePoint is one page's aesthetic score in the trend.
type ScorePoint struct {
	URL   string    `json:"url"`
	Score int       `json:"score"`
	At    time.Time `json:"at"`
}

// Taste is the retrospective: what keeps going wrong, how the score moves,
// and which rules the project should adopt next.
type Taste struct {
	GeneratedAt time.Time    `json:"generated_at"`
	Pages       int          `json:"pages"`
	AvgScore    float64      `json:"avg_score"`
	ScoreTrend  []ScorePoint `json:"score_trend"`
	Recurring   []Recurring  `json:"recurring"`
	Lessons     []string     `json:"lessons"`
}

var numberRE = regexp.MustCompile(`[0-9]+(?:\.[0-9]+)?`)

// findingKey normalises a message so "3.2:1" and "4.8:1" count as the same
// recurring issue rather than two unrelated ones.
func findingKey(f Finding) string {
	m := numberRE.ReplaceAllString(f.Message, "#")
	return f.Category + "|" + m
}

// BuildTaste aggregates saved maps into a retrospective.
func BuildTaste(maps []Map) Taste {
	t := Taste{GeneratedAt: time.Now().UTC(), Pages: len(maps)}
	type agg struct {
		Recurring
		pages map[string]bool
	}
	groups := map[string]*agg{}
	total := 0
	for _, m := range maps {
		total += m.Score
		t.ScoreTrend = append(t.ScoreTrend, ScorePoint{URL: m.URL, Score: m.Score, At: m.GeneratedAt})
		for _, f := range m.Findings {
			k := findingKey(f)
			a := groups[k]
			if a == nil {
				a = &agg{Recurring: Recurring{Category: f.Category, Message: f.Message, Suggestion: f.Suggestion}, pages: map[string]bool{}}
				a.FirstSeen = m.GeneratedAt
				groups[k] = a
			}
			a.Count++
			a.pages[firstNonEmpty(m.URL, m.Title)] = true
			if m.GeneratedAt.Before(a.FirstSeen) || a.FirstSeen.IsZero() {
				a.FirstSeen = m.GeneratedAt
			}
			if m.GeneratedAt.After(a.LastSeen) {
				a.LastSeen = m.GeneratedAt
			}
		}
	}
	if len(maps) > 0 {
		t.AvgScore = round1(float64(total) / float64(len(maps)))
	}
	for _, a := range groups {
		a.Pages = len(a.pages)
		t.Recurring = append(t.Recurring, a.Recurring)
	}
	sort.SliceStable(t.Recurring, func(i, j int) bool {
		if t.Recurring[i].Count != t.Recurring[j].Count {
			return t.Recurring[i].Count > t.Recurring[j].Count
		}
		return t.Recurring[i].Category < t.Recurring[j].Category
	})
	t.Lessons = deriveLessons(t)
	return t
}

// lessonRule is the durable "taste" advice for a finding category.
var lessonRule = map[string]string{
	"contrast":     "把 WCAG AA 4.5:1 设为发布门禁；文字/背景用令牌，而不是临时裸值。",
	"spacing":      "建立 4/8 网格，间距只允许来自令牌；出现新值先问是不是设计意图。",
	"typography":   "字号阶梯收敛到 4–6 级；正文不小于 12–14px。",
	"color":        "色板收敛为中性与 accent 两类；语义色只用于状态，不做装饰。",
	"touch-target": "交互目标至少 44x44px；小图标用 padding 或伪元素扩大热区。",
	"overflow":     "长内容允许换行/省略号/响应式布局，禁止横向裁切。",
	"a11y":         "图片 alt、按钮可访问标签、表单 label 纳入验收清单。",
	"hierarchy":    "单页一个 h1；标题层级按 h1→h6 连续使用。",
	"layout":       "单列布局要确认是设计意图；否则用网格或 max-width 容器。",
}

func deriveLessons(t Taste) []string {
	var out []string
	for _, r := range t.Recurring {
		if r.Pages < 2 && r.Count < 3 {
			continue
		}
		rule := lessonRule[r.Category]
		if rule == "" {
			rule = "重复出现的问题要升级为规则或检查项。"
		}
		out = append(out, fmt.Sprintf("%s：出现 %d 次 / 覆盖 %d 页。%s", r.Category, r.Count, r.Pages, rule))
	}
	if t.AvgScore < 70 {
		out = append(out, fmt.Sprintf("平均审美分 %.1f 低于 70：先修 error 级问题，再处理 warn。", t.AvgScore))
	}
	if len(out) == 0 {
		out = append(out, "暂无重复问题；继续扩大采样面（更多页面/更多状态）再复盘。")
	}
	return out
}

// RenderTaste renders the retrospective.
func RenderTaste(t Taste) string {
	var b strings.Builder
	b.WriteString("# 审美复盘\n\n")
	fmt.Fprintf(&b, "- Pages: %d\n- Average score: %.1f\n- Recurring issues: %d\n\n", t.Pages, t.AvgScore, len(t.Recurring))

	b.WriteString("## 分数趋势\n\n")
	if len(t.ScoreTrend) == 0 {
		b.WriteString("_No samples yet._\n")
	} else {
		b.WriteString("| URL | Score | At |\n|---|---|---|\n")
		for _, p := range t.ScoreTrend {
			fmt.Fprintf(&b, "| %s | %d | %s |\n", truncate(p.URL, 60), p.Score, p.At.Format("2006-01-02 15:04"))
		}
	}
	b.WriteString("\n## 重复问题\n\n")
	if len(t.Recurring) == 0 {
		b.WriteString("_None._\n")
	} else {
		b.WriteString("| Category | Count | Pages | Message |\n|---|---|---|---|\n")
		for _, r := range t.Recurring {
			fmt.Fprintf(&b, "| %s | %d | %d | %s |\n", r.Category, r.Count, r.Pages, truncate(strings.ReplaceAll(r.Message, "|", "\\|"), 70))
		}
	}
	b.WriteString("\n## 沉淀出的规则\n\n")
	for _, l := range t.Lessons {
		fmt.Fprintf(&b, "- %s\n", l)
	}
	return b.String()
}

// SaveTaste persists the retrospective and appends a dated snapshot, so the
// taste model has a history instead of only a latest state.
func SaveTaste(t Taste, workingDir string) (string, error) {
	dir := Dir(workingDir)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", fmt.Errorf("create ui dir: %w", err)
	}
	raw, err := jsonMarshalIndent(t)
	if err != nil {
		return "", fmt.Errorf("marshal taste: %w", err)
	}
	base := filepath.Join(dir, "taste.json")
	if err := os.WriteFile(base, raw, 0o600); err != nil {
		return "", fmt.Errorf("write taste: %w", err)
	}
	md := filepath.Join(dir, "taste.md")
	if err := os.WriteFile(md, []byte(RenderTaste(t)), 0o600); err != nil {
		return "", fmt.Errorf("write taste markdown: %w", err)
	}
	snapDir := filepath.Join(dir, "taste")
	if err := os.MkdirAll(snapDir, 0o750); err != nil {
		return "", fmt.Errorf("create taste snapshots: %w", err)
	}
	snap := filepath.Join(snapDir, time.Now().UTC().Format("20060102T150405")+"-taste.md")
	if err := os.WriteFile(snap, []byte(RenderTaste(t)), 0o600); err != nil {
		return "", fmt.Errorf("write taste snapshot: %w", err)
	}
	return md, nil
}

// LoadTaste reads the latest persisted retrospective, if any.
func LoadTaste(workingDir string) (Taste, bool) {
	raw, err := os.ReadFile(filepath.Join(Dir(workingDir), "taste.json"))
	if err != nil {
		return Taste{}, false
	}
	var t Taste
	if err := jsonUnmarshal(raw, &t); err != nil {
		return Taste{}, false
	}
	return t, true
}
