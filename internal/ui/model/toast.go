package model

import (
	"image"
	"time"

	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"github.com/package-register/mocode/internal/ui/common"
	"github.com/package-register/mocode/internal/ui/util"
)

// DefaultToastTTL is the default time-to-live for toast messages.
const DefaultToastTTL = 5 * time.Second

// ToastMaxWidth is the maximum width of the toast notification.
const ToastMaxWidth = 60

// Toast is a floating notification that appears at the bottom-right corner.
// It replaces the old full-width status bar overlay with a more elegant design.
type Toast struct {
	com     *common.Common
	msg     util.InfoMsg
	timer   *time.Timer
	visible bool

	// cached rendered strings for the current message
	indicator string
	text      string
}

// NewToast creates a new Toast notification component.
func NewToast(com *common.Common) *Toast {
	return &Toast{com: com}
}

// Show displays a toast notification with the given message.
func (t *Toast) Show(msg util.InfoMsg) {
	if msg.IsEmpty() {
		return
	}

	t.msg = msg
	t.visible = true
	t.cacheRender()

	// Stop any existing timer
	if t.timer != nil {
		t.timer.Stop()
	}

	// Auto-hide after TTL
	ttl := msg.TTL
	if ttl <= 0 {
		ttl = DefaultToastTTL
	}
	t.timer = time.AfterFunc(ttl, func() {
		t.Hide()
	})
}

// Hide immediately hides the toast.
func (t *Toast) Hide() {
	t.visible = false
	t.msg = util.InfoMsg{}
	t.indicator = ""
	t.text = ""
	if t.timer != nil {
		t.timer.Stop()
		t.timer = nil
	}
}

// IsVisible returns whether the toast is currently shown.
func (t *Toast) IsVisible() bool {
	return t.visible
}

// cacheRender pre-computes the indicator and text styles.
func (t *Toast) cacheRender() {
	if t.msg.IsEmpty() {
		return
	}

	st := t.com.Styles

	switch t.msg.Type {
	case util.InfoTypeError:
		t.indicator = st.Toast.ErrorIndicator.Render()
		t.text = st.Toast.ErrorMessage.Render(t.msg.Msg)
	case util.InfoTypeWarn:
		t.indicator = st.Toast.WarnIndicator.Render()
		t.text = st.Toast.WarnMessage.Render(t.msg.Msg)
	case util.InfoTypeUpdate:
		t.indicator = st.Toast.UpdateIndicator.Render()
		t.text = st.Toast.UpdateMessage.Render(t.msg.Msg)
	case util.InfoTypeSuccess:
		t.indicator = st.Toast.SuccessIndicator.Render()
		t.text = st.Toast.SuccessMessage.Render(t.msg.Msg)
	case util.InfoTypeInfo:
		t.indicator = st.Toast.InfoIndicator.Render()
		t.text = st.Toast.InfoMessage.Render(t.msg.Msg)
	}
}

// Draw draws the toast notification at the bottom-right of the screen.
func (t *Toast) Draw(scr uv.Screen, area uv.Rectangle) {
	if !t.visible || t.msg.IsEmpty() {
		return
	}

	// Build the toast content
	content := lipgloss.JoinHorizontal(lipgloss.Left, t.indicator, t.text)

	// Measure the rendered content
	contentWidth := lipgloss.Width(content)
	contentHeight := lipgloss.Height(content)
	_ = contentHeight // no longer used directly, replaced by renderedHeight below

	// Clamp width
	maxW := min(ToastMaxWidth, area.Dx()/2)
	if contentWidth > maxW {
		// Truncate the text part
		indWidth := lipgloss.Width(t.indicator)
		avail := maxW - indWidth - 2 // 2 for padding
		truncated := ansi.Truncate(t.msg.Msg, avail, "…")

		// Re-render with truncated text
		st := t.com.Styles
		switch t.msg.Type {
		case util.InfoTypeError:
			t.text = st.Toast.ErrorMessage.Render(truncated)
		case util.InfoTypeWarn:
			t.text = st.Toast.WarnMessage.Render(truncated)
		case util.InfoTypeUpdate:
			t.text = st.Toast.UpdateMessage.Render(truncated)
		case util.InfoTypeSuccess:
			t.text = st.Toast.SuccessMessage.Render(truncated)
		case util.InfoTypeInfo:
			t.text = st.Toast.InfoMessage.Render(truncated)
		}
		content = lipgloss.JoinHorizontal(lipgloss.Left, t.indicator, t.text)
		contentWidth = lipgloss.Width(content)
	}

	// Wrap content in a container with padding and rounded border.
	st := t.com.Styles
	var container lipgloss.Style
	switch t.msg.Type {
	case util.InfoTypeError:
		container = st.Toast.ErrorMessage.Border(lipgloss.RoundedBorder()).BorderForeground(st.Toast.ErrorMessage.GetForeground())
	case util.InfoTypeWarn:
		container = st.Toast.WarnMessage.Border(lipgloss.RoundedBorder()).BorderForeground(st.Toast.WarnMessage.GetForeground())
	case util.InfoTypeUpdate:
		container = st.Toast.UpdateMessage.Border(lipgloss.RoundedBorder()).BorderForeground(st.Toast.UpdateMessage.GetForeground())
	case util.InfoTypeSuccess:
		container = st.Toast.SuccessMessage.Border(lipgloss.RoundedBorder()).BorderForeground(st.Toast.SuccessMessage.GetForeground())
	case util.InfoTypeInfo:
		container = st.Toast.InfoMessage.Border(lipgloss.RoundedBorder()).BorderForeground(st.Toast.InfoMessage.GetForeground())
	default:
		container = st.Toast.InfoMessage.Border(lipgloss.RoundedBorder()).BorderForeground(st.Toast.InfoMessage.GetForeground())
	}

	toastStyle := container.Width(contentWidth + 4).Padding(0, 1)
	rendered := toastStyle.Render(content)
	renderedWidth := lipgloss.Width(rendered)
	renderedHeight := lipgloss.Height(rendered)

	// Calculate position: bottom-right, offset from status bar
	padX := 2 // right margin
	padY := 1 // above the status bar
	toastX := area.Max.X - renderedWidth - padX
	toastY := area.Max.Y - renderedHeight - padY

	toastRect := image.Rect(toastX, toastY, toastX+renderedWidth, toastY+renderedHeight)

	// Clamp to area bounds
	if toastRect.Min.X < area.Min.X {
		toastRect.Min.X = area.Min.X
		toastRect.Max.X = toastRect.Min.X + renderedWidth
	}
	if toastRect.Min.Y < area.Min.Y {
		toastRect.Min.Y = area.Min.Y
		toastRect.Max.Y = toastRect.Min.Y + renderedHeight
	}

	// Draw toast
	uv.NewStyledString(rendered).Draw(scr, toastRect)
}
