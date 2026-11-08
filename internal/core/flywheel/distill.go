package flywheel

import (
	"fmt"
	"strings"
)

// Distill keeps the shortest successful path: failed attempts, retries and
// repeated identical actions are dropped, order is preserved. The result is
// what a future run should try first.
func Distill(steps []Step) Path {
	type key struct{ action, target, command string }
	seen := map[key]bool{}
	var out []Step
	failed, retries := 0, 0
	for _, s := range steps {
		retries += s.Retries
		if s.Failed {
			failed++
			continue
		}
		k := key{s.Action, s.Target, s.Command}
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, s)
	}
	total := len(steps)
	kept := len(out)
	dropped := total - kept
	savings := 0.0
	if total > 0 {
		savings = float64(dropped) / float64(total) * 100
	}
	return Path{Steps: out, Dropped: dropped, Retries: retries + failed, SavingsPct: savings}
}

// DraftFromTask turns a run into a skill proposal.
func DraftFromTask(t TaskRun) Draft {
	path := Distill(t.Steps)
	name := slug(t.Type + "-" + goalKey(t.Goal))
	if name == "" {
		name = "task-skill"
	}
	desc := "Use when " + strings.ToLower(firstNonEmpty(t.Goal, t.Type, "a similar task")) + "."
	triggers := triggersOf(t)

	var b strings.Builder
	b.WriteString("## 何时使用\n\n")
	fmt.Fprintf(&b, "当任务类型为 `%s`，目标类似「%s」时使用。\n\n", t.Type, t.Goal)
	b.WriteString("## 最优路径\n\n")
	for i, s := range path.Steps {
		label := s.Action
		if s.Target != "" {
			label += " " + s.Target
		}
		if s.Command != "" {
			label += " `" + s.Command + "`"
		}
		fmt.Fprintf(&b, "%d. %s\n", i+1, label)
	}
	if path.Dropped > 0 {
		fmt.Fprintf(&b, "\n> 初始路径 %d 步，去掉失败/重试后 %d 步（−%.0f%%）。\n", len(t.Steps), len(path.Steps), path.SavingsPct)
	}
	if pitfalls := failedSteps(t.Steps); len(pitfalls) > 0 {
		b.WriteString("\n## 坑\n\n")
		for _, s := range pitfalls {
			fmt.Fprintf(&b, "- %s %s", s.Action, s.Target)
			if s.Result != "" {
				fmt.Fprintf(&b, " → %s", truncate(s.Result, 100))
			}
			b.WriteString("\n")
		}
	}
	if len(t.Lessons) > 0 {
		b.WriteString("\n## 经验\n\n")
		for _, l := range t.Lessons {
			fmt.Fprintf(&b, "- %s\n", l)
		}
	}
	b.WriteString("\n## 验收\n\n")
	fmt.Fprintf(&b, "本次结果：%s。复用后先验证同一路径是否仍成立，再按需调整。\n", t.Outcome)
	return Draft{
		Name: name, Type: t.Type, Description: desc, Triggers: triggers,
		Instructions: b.String(), Path: path.Steps, Lessons: t.Lessons,
	}
}

func failedSteps(steps []Step) []Step {
	var out []Step
	for _, s := range steps {
		if s.Failed {
			out = append(out, s)
		}
	}
	if len(out) > 8 {
		out = out[:8]
	}
	return out
}

func triggersOf(t TaskRun) []string {
	set := map[string]bool{}
	add := func(s string) {
		for _, w := range strings.Fields(strings.ToLower(s)) {
			w = strings.Trim(w, "`.,:;()[]{}'\"")
			if len(w) >= 4 && !stopwords[w] {
				set[w] = true
			}
		}
	}
	add(t.Type)
	add(t.Goal)
	var out []string
	for w := range set {
		out = append(out, w)
	}
	if len(out) > 8 {
		out = out[:8]
	}
	return out
}

var stopwords = map[string]bool{
	"this": true, "that": true, "with": true, "from": true, "into": true,
	"when": true, "then": true, "than": true, "have": true, "will": true,
	"should": true, "using": true, "make": true, "made": true, "task": true,
}

func goalKey(goal string) string {
	for _, w := range strings.Fields(strings.ToLower(goal)) {
		w = strings.Trim(w, "`.,:;()[]{}'\"")
		if len(w) >= 4 && !stopwords[w] {
			return w
		}
	}
	return "goal"
}

func slug(s string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		case r == '-' || r == '_' || r == ' ':
			if !prevDash && b.Len() > 0 {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > 64 {
		out = out[:64]
	}
	return out
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
