package chat

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/package-register/mocode/internal/core/hooks"
	"github.com/package-register/mocode/internal/ui/styles"
)

func toolOutputHookIndicator(sty *styles.Styles, metadata string, width int) string {
	if metadata == "" {
		return ""
	}
	var meta struct {
		Hook *hooks.HookMetadata `json:"hook"`
	}
	if err := json.Unmarshal([]byte(metadata), &meta); err != nil || meta.Hook == nil {
		return ""
	}
	h := meta.Hook
	if len(h.Hooks) == 0 {
		return ""
	}

	// Sanitize names (replace newlines with ¶) and compute max widths
	// for the name, matcher, and detail columns so they align. The name
	// column is capped at maxHookNameWidth characters.
	const maxHookNameWidth = 30
	sanitizedNames := make([]string, len(h.Hooks))
	details := make([]string, len(h.Hooks))
	maxNameWidth := 0
	maxMatcherWidth := 0
	maxDetailWidth := 0
	for i, hi := range h.Hooks {
		sanitizedNames[i] = strings.ReplaceAll(hi.Name, "\n", "¶")
		w := lipgloss.Width(sty.Tool.HookName.Render(sanitizedNames[i]))
		if w > maxNameWidth {
			maxNameWidth = w
		}
		if hi.Matcher != "" {
			mw := lipgloss.Width(sty.Tool.HookMatcher.Render(hi.Matcher))
			if mw > maxMatcherWidth {
				maxMatcherWidth = mw
			}
		}
		details[i] = hookDetail(sty, hi)
		if dw := lipgloss.Width(details[i]); dw > maxDetailWidth {
			maxDetailWidth = dw
		}
	}

	if maxNameWidth > maxHookNameWidth {
		maxNameWidth = maxHookNameWidth
	}

	// Cap the name column so the widest line still fits in width. The
	// per-line layout is:
	//   "Hook " + name(padded) + [" " + matcher(padded)] + " → " + detail
	if width > 0 {
		fixed := lipgloss.Width(sty.Tool.HookLabel.Render("Hook")) + 1
		if maxMatcherWidth > 0 {
			fixed += 1 + maxMatcherWidth
		}
		fixed += 1 + lipgloss.Width(sty.Tool.HookArrow.Render(styles.ArrowRightIcon)) + 1
		fixed += maxDetailWidth
		if budget := width - fixed; budget < maxNameWidth {
			maxNameWidth = max(1, budget)
		}
	}

	var lines []string
	for i, hi := range h.Hooks {
		name := truncateHookName(sanitizedNames[i], maxNameWidth)
		lines = append(lines, renderHookLine(sty, hi, name, details[i], maxNameWidth, maxMatcherWidth))
	}
	return strings.Join(lines, "\n")
}

func truncateHookName(name string, maxWidth int) string {
	if ansi.StringWidth(name) <= maxWidth {
		return name
	}
	if isLikelyPath(name) {
		// ansi.TruncateLeft removes n graphemes from the start; pick n
		// so the result plus the "…" prefix fits in maxWidth.
		n := ansi.StringWidth(name) - maxWidth + 1
		return ansi.TruncateLeft(name, n, "…")
	}
	return ansi.Truncate(name, maxWidth, "…")
}

func isLikelyPath(s string) bool {
	if s == "" || strings.ContainsAny(s, " \t\n¶'\"|&;<>$`*?(){}[]\\") {
		return false
	}
	if filepath.IsAbs(s) {
		return true
	}
	return strings.Contains(s, "/")
}

func renderHookLine(sty *styles.Styles, hi hooks.HookInfo, rawName, detail string, maxNameWidth, maxMatcherWidth int) string {
	name := sty.Tool.HookName.Render(rawName)
	namePad := strings.Repeat(" ", max(0, maxNameWidth-lipgloss.Width(name)))

	var matcherPart string
	if maxMatcherWidth > 0 {
		if hi.Matcher != "" {
			matcher := sty.Tool.HookMatcher.Render(hi.Matcher)
			matcherPad := strings.Repeat(" ", maxMatcherWidth-lipgloss.Width(matcher))
			matcherPart = " " + matcher + matcherPad
		} else {
			matcherPart = " " + strings.Repeat(" ", maxMatcherWidth)
		}
	}

	labelStyle := sty.Tool.HookLabel
	arrowStyle := sty.Tool.HookArrow
	if hi.Decision == "deny" {
		labelStyle = sty.Tool.HookDeniedLabel
		arrowStyle = sty.Tool.HookDeniedLabel
	}

	return fmt.Sprintf(
		"%s %s%s%s %s %s",
		labelStyle.Render("Hook"),
		name,
		namePad,
		matcherPart,
		arrowStyle.Render(styles.ArrowRightIcon),
		detail,
	)
}

func hookDetail(sty *styles.Styles, hi hooks.HookInfo) string {
	const (
		okMessage      = "OK"
		denialMessage  = "Denied"
		rewroteMessage = "Rewrote Output"
	)
	switch hi.Decision {
	case "deny":
		if hi.Reason != "" {
			return sty.Tool.HookDenied.Render(denialMessage) + " " + sty.Tool.HookDeniedReason.Render(hi.Reason)
		}
		return sty.Tool.HookDenied.Render(denialMessage)
	case "allow":
		result := sty.Tool.HookOK.Render(okMessage)
		if hi.InputRewrite {
			result += " " + sty.Tool.HookRewrote.Render(rewroteMessage)
		}
		return result
	default:
		result := sty.Tool.HookOK.Render(okMessage)
		if hi.InputRewrite {
			result += " " + sty.Tool.HookRewrote.Render(rewroteMessage)
		}
		return result
	}
}
