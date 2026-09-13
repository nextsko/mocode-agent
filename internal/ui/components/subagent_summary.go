package components

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/nextsko/mocode-agent/internal/ui/styles"
)

// SubagentCounts summarizes the state of every Agent tool call in the chat.
type SubagentCounts struct {
	Running int
	Done    int
}

// Total reports how many sub-agent invocations are tracked.
func (c SubagentCounts) Total() int { return c.Running + c.Done }

// RenderSubagentSummary draws the prime-agent style sub-agent summary box:
//
//	╭─ subagents ──────────────────────╮
//	│ ● 2 running   ○ 5 done   ctrl+g │
//	╰──────────────────────────────────╯
//
// It renders nothing when no sub-agent has ever run, so the editor area
// stays clean for regular chats.
func RenderSubagentSummary(sty *styles.Styles, counts SubagentCounts, width int) string {
	if counts.Total() == 0 {
		return ""
	}
	inner := max(0, width-2)

	label := " subagents "
	runPart := ""
	if counts.Running > 0 {
		runPart = fmt.Sprintf("● %d running", counts.Running)
	}
	donePart := ""
	if counts.Done > 0 {
		donePart = fmt.Sprintf("○ %d done", counts.Done)
	}
	hint := "ctrl+g"
	sep := "   "

	body := strings.Join(nonEmpty(runPart, donePart), sep)
	bodyWidth := ansi.StringWidth(body) + ansi.StringWidth(sep) + ansi.StringWidth(hint)
	pad := max(1, inner-bodyWidth-1)
	line := " " + body + strings.Repeat(" ", pad) + hint + " "

	top := "╭─" + label + strings.Repeat("─", max(0, inner-ansi.StringWidth(label)-1)) + "╮"
	mid := "│" + line + strings.Repeat(" ", max(0, inner-ansi.StringWidth(line))) + "│"
	bot := "╰" + strings.Repeat("─", inner) + "╯"

	styledTop := sty.Tool.StateWaiting.Render(top)
	styledMid := sty.Tool.StateWaiting.Render(mid)
	styledBot := sty.Tool.StateWaiting.Render(bot)
	return strings.Join([]string{styledTop, styledMid, styledBot}, "\n")
}

func nonEmpty(parts ...string) []string {
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
