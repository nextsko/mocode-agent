package dialog

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
)

// QuestionFormID is the identifier for the question form dialog.
const QuestionFormID = "question-form"

// QuestionDialog adapts the inline QuestionForm component (crush-compatible)
// to mocode's dialog stack: keys, paste and draw are forwarded; the form's
// own OnAnswer/OnCancel callbacks close it via the UI layer.
type QuestionDialog struct {
	Form *QuestionForm
}

var _ Dialog = (*QuestionDialog)(nil)

// NewQuestionDialog wraps a QuestionForm for the dialog overlay.
func NewQuestionDialog(form *QuestionForm) *QuestionDialog {
	return &QuestionDialog{Form: form}
}

func (*QuestionDialog) ID() string { return QuestionFormID }

func (q *QuestionDialog) HandleMsg(msg tea.Msg) Action {
	switch m := msg.(type) {
	case tea.KeyPressMsg:
		q.Form.HandleKey(m)
	case tea.PasteMsg:
		q.Form.HandlePaste(m)
	case tea.MouseClickMsg:
		// The form owns its own hit compositor coordinates; clicks are
		// handled during Draw bookkeeping, nothing to do here.
	}
	return nil
}

func (q *QuestionDialog) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	return q.Form.Draw(scr, area)
}

// ShortHelp forwards the form's key hints.
func (q *QuestionDialog) ShortHelp() []key.Binding {
	return q.Form.ShortHelp()
}
