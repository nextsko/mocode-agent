package model

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"github.com/package-register/mocode/internal/core/config"
	"github.com/package-register/mocode/internal/domain/session"
	"github.com/package-register/mocode/internal/ui/common"
	"github.com/package-register/mocode/internal/ui/styles"
	"github.com/package-register/mocode/internal/util/fsext"
)

const (
	leftPadding  = 1
	rightPadding = 1
)

type headerStats struct {
	startedAt           time.Time
	frame               int
	cacheReadTokens     int64
	cacheCreationTokens int64
	hasCache            bool
}

type header struct {
	// cached logo and compact logo
	logo        string
	compactLogo string
	shimmer     []string

	com     *common.Common
	width   int
	compact bool
}

// newHeader creates a new header model.
func newHeader(com *common.Common) *header {
	h := &header{
		com: com,
	}
	h.refresh()
	return h
}

// refresh rebuilds cached logo strings using the current styles. Call
// after the theme changes.
func (h *header) refresh() {
	t := h.com.Styles
	isHyper := h.com.IsHyper()
	charm := config.GetAppName(h.com.Config())
	if !isHyper {
		charm = " " + charm
	}
	name := "oMo code"
	if isHyper {
		name = "HYPERMOCODE"
	}
	h.compactLogo = t.Header.Charm.Render(charm) + " " +
		styles.ApplyBoldForegroundGrad(t.Header.LogoGradCanvas, name, t.Header.LogoGradFromColor, t.Header.LogoGradToColor) + " "
	// Force drawHeader to re-render the wide logo on the next frame.
	h.width = 0
	h.logo = ""
	h.shimmer = nil
}

// drawHeader draws the header for the given session.
func (h *header) drawHeader(
	scr uv.Screen,
	area uv.Rectangle,
	session *session.Session,
	compact bool,
	detailsOpen bool,
	width int,
	hyperCredits *int,
	stats headerStats,
) {
	t := h.com.Styles
	h.width = width
	h.compact = compact

	lspErrorCount := 0
	for _, info := range h.com.Workspace.LSPGetStates() {
		lspErrorCount += info.DiagnosticCount
	}
	view := renderHeaderHUD(
		h.com,
		session,
		lspErrorCount,
		detailsOpen,
		max(0, width-leftPadding-rightPadding),
		hyperCredits,
		stats,
		h.shimmerHeaderText(config.GetAppName(h.com.Config()), stats.frame),
	)

	uv.NewStyledString(t.Header.Wrapper.Padding(0, rightPadding, 0, leftPadding).Render(view)).Draw(scr, area)
}

func renderHeaderHUD(
	com *common.Common,
	session *session.Session,
	lspErrorCount int,
	detailsOpen bool,
	availWidth int,
	hyperCredits *int,
	stats headerStats,
	logo string,
) string {
	t := com.Styles

	now := time.Now()
	left := strings.Join([]string{
		t.Header.Percentage.Render(now.Format("15:04")),
		logo,
	}, " ")

	var parts []string
	cwd := fsext.DirTrim(fsext.PrettyPath(com.Workspace.WorkingDir()), 4)
	parts = append(parts, t.Header.WorkingDir.Render(cwd))
	if lspErrorCount > 0 {
		parts = append(parts, t.LSP.ErrorDiagnostic.Render(fmt.Sprintf("%s%d", styles.LSPErrorIcon, lspErrorCount)))
	}

	activeMode := activeAgentMode(com)
	agentCfg, ok := com.Config().Agents[activeMode]
	if !ok {
		agentCfg = com.Config().Agents[config.AgentCoder]
	}
	model := com.Config().GetModelByType(agentCfg.Model)
	if model != nil && model.ContextWindow > 0 && session != nil {
		used := session.CompletionTokens + session.PromptTokens
		percentage := (float64(used) / float64(model.ContextWindow)) * 100
		parts = append(parts,
			renderContextBattery(t, percentage)+" "+t.Header.Percentage.Render(fmt.Sprintf("%s/%s", formatHeaderTokens(used), formatHeaderTokens(model.ContextWindow))),
		)
	}

	cache := "cache --"
	if stats.hasCache {
		cache = fmt.Sprintf("cache %s/%s", formatHeaderTokens(stats.cacheReadTokens), formatHeaderTokens(stats.cacheCreationTokens))
	}
	parts = append(parts, t.Header.KeystrokeTip.Render(cache))

	if com.IsHyper() && hyperCredits != nil {
		hc := t.Header.Hypercredit.Render(styles.HypercreditIcon) + " " + t.Header.Percentage.Render(common.FormatCredits(*hyperCredits))
		parts = append(parts, hc)
	}

	const keystroke = "ctrl+d"
	if detailsOpen {
		parts = append(parts, t.Header.Keystroke.Render(keystroke)+t.Header.KeystrokeTip.Render(" close"))
	} else {
		parts = append(parts, t.Header.Keystroke.Render(keystroke)+t.Header.KeystrokeTip.Render(" open "))
	}

	details := strings.Join(parts, t.Header.Separator.Render(" │ "))
	right := t.Header.Percentage.Render("⏱ " + formatHeaderDuration(now.Sub(stats.startedAt)))
	if stats.startedAt.IsZero() {
		right = t.Header.Percentage.Render("⏱ 0s")
	}

	return fitHeaderSegments(left, details, right, availWidth)
}

func (h *header) shimmerHeaderText(text string, frame int) string {
	runes := []rune(text)
	total := len(runes) + 4
	if total <= 0 {
		return ""
	}
	if len(h.shimmer) != total {
		h.shimmer = make([]string, total)
		for i := range h.shimmer {
			h.shimmer[i] = shimmerHeaderText(h.com.Styles, text, i)
		}
	}
	return h.shimmer[frame%len(h.shimmer)]
}

func shimmerHeaderText(t *styles.Styles, text string, frame int) string {
	runes := []rune(text)
	if len(runes) == 0 {
		return ""
	}
	active := frame % (len(runes) + 4)
	var b strings.Builder
	for i, r := range runes {
		dist := i - active
		if dist < 0 {
			dist = -dist
		}
		switch {
		case dist == 0:
			b.WriteString(t.Header.Diagonals.Bold(true).Render(string(r)))
		case dist == 1:
			b.WriteString(t.Header.Charm.Render(string(r)))
		default:
			b.WriteString(t.Header.Keystroke.Render(string(r)))
		}
	}
	return b.String()
}

func fitHeaderSegments(left, details, right string, availWidth int) string {
	if availWidth <= 0 {
		return ""
	}
	rightWidth := lipgloss.Width(right)
	if availWidth <= rightWidth {
		return ansi.Truncate(right, availWidth, "")
	}

	leftMax := min(lipgloss.Width(left), max(0, availWidth-rightWidth-1))
	left = ansi.Truncate(left, leftMax, "…")
	leftWidth := lipgloss.Width(left)

	detailMax := max(0, availWidth-leftWidth-rightWidth-2)
	if detailMax <= 0 || details == "" {
		gap := strings.Repeat(" ", max(0, availWidth-leftWidth-rightWidth))
		return left + gap + right
	}

	details = ansi.Truncate(details, detailMax, "…")
	prefix := left + " " + details
	gapWidth := availWidth - lipgloss.Width(prefix) - rightWidth
	if gapWidth < 0 {
		prefix = ansi.Truncate(prefix, max(0, availWidth-rightWidth), "…")
		gapWidth = availWidth - lipgloss.Width(prefix) - rightWidth
	}
	return prefix + strings.Repeat(" ", max(0, gapWidth)) + right
}

func renderContextBattery(t *styles.Styles, percentage float64) string {
	const cells = 10
	filled := int((percentage / 100) * cells)
	if filled < 0 {
		filled = 0
	}
	if filled > cells {
		filled = cells
	}
	return t.Header.KeystrokeTip.Render("ctx ") +
		t.Header.Diagonals.Render(strings.Repeat("█", filled)) +
		t.Header.Separator.Render(strings.Repeat("░", cells-filled))
}

func formatHeaderTokens(tokens int64) string {
	switch {
	case tokens >= 1_000_000:
		return strings.TrimSuffix(fmt.Sprintf("%.1f", float64(tokens)/1_000_000), ".0") + "M"
	case tokens >= 1_000:
		return strings.TrimSuffix(fmt.Sprintf("%.1f", float64(tokens)/1_000), ".0") + "K"
	default:
		return fmt.Sprintf("%d", tokens)
	}
}

func formatHeaderDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	totalSeconds := int64(d.Seconds())
	switch {
	case totalSeconds < 60:
		return fmt.Sprintf("%ds", totalSeconds)
	case totalSeconds < 3600:
		return fmt.Sprintf("%dm", totalSeconds/60)
	case totalSeconds < 86400:
		return fmt.Sprintf("%dh %dm", totalSeconds/3600, (totalSeconds%3600)/60)
	default:
		return fmt.Sprintf("%dd %dh", totalSeconds/86400, (totalSeconds%86400)/3600)
	}
}
