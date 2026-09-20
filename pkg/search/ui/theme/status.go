package theme

import "strings"

// State is the lifecycle state of a tool call block.
type State string

// Block lifecycle states.
const (
	StatePending State = "pending"
	StateRunning State = "running"
	StateSuccess State = "success"
	StateWarning State = "warning"
	StateError   State = "error"
)

// Status glyphs, one per State.
const (
	IconPending = "◐"
	IconRunning = "◑"
	IconSuccess = "✓"
	IconWarning = "⚠"
	IconError   = "✗"
)

// StatusLineMaxWidth bounds the assembled status line as a safety net;
// callers should pre-truncate wide fragments.
const StatusLineMaxWidth = 110

// StatusIcon returns the unstyled glyph for state.
func StatusIcon(state State) string {
	switch state {
	case StatePending:
		return IconPending
	case StateRunning:
		return IconRunning
	case StateSuccess:
		return IconSuccess
	case StateWarning:
		return IconWarning
	case StateError:
		return IconError
	default:
		return ""
	}
}

// StatusColor maps state to its semantic foreground token: pending renders
// muted, running accent, and the terminal states their named colors.
func StatusColor(state State) ThemeColor {
	switch state {
	case StatePending:
		return ColorMuted
	case StateRunning:
		return ColorAccent
	case StateSuccess:
		return ColorSuccess
	case StateWarning:
		return ColorWarning
	case StateError:
		return ColorError
	default:
		return ColorMuted
	}
}

// BorderColor maps state to its semantic border token. Success
// intentionally uses the muted border so finished blocks recede; pending
// and running use accent to stay visually active.
func BorderColor(state State) ThemeColor {
	switch state {
	case StatePending, StateRunning:
		return ColorAccent
	case StateWarning:
		return ColorWarning
	case StateError:
		return ColorError
	default:
		return ColorBorderMuted
	}
}

// flattenHeader collapses CR/LF runs in caller-supplied header fragments
// to single spaces so a stray newline cannot expand the status line into
// multiple rows.
func flattenHeader(text string) string {
	if !strings.ContainsAny(text, "\r\n") {
		return text
	}
	return strings.NewReplacer("\r\n", " ", "\r", " ", "\n", " ").Replace(text)
}

// RenderStatusLine is the single-entry header renderer: it assembles the
// state icon, an accent-colored title, an optional muted description after
// a colon, and an optional dim meta suffix into one line, flattening
// newlines and truncating to StatusLineMaxWidth.
func RenderStatusLine(t Theme, state State, title, desc, meta string) string {
	var b strings.Builder
	if icon := t.StatusIcon(state); icon != "" {
		b.WriteString(icon)
		b.WriteByte(' ')
	}
	b.WriteString(t.FG(ColorAccent, flattenHeader(title)))
	if d := flattenHeader(desc); d != "" {
		b.WriteString(": ")
		b.WriteString(t.FG(ColorMuted, d))
	}
	if m := flattenHeader(meta); m != "" {
		b.WriteByte(' ')
		b.WriteString(t.FG(ColorDim, m))
	}
	return TruncateToWidth(b.String(), StatusLineMaxWidth, DefaultEllipsis)
}
