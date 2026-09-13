package dialog

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/nextsko/mocode-agent/internal/core/question"
	"github.com/nextsko/mocode-agent/internal/ui/common"
	"github.com/nextsko/mocode-agent/internal/ui/styles"
)

// questionResponder extends InlineEditor with access to the last
// response. Used internally by QuestionForm to collect answers
// from child components.
type questionResponder interface {
	InlineEditor
	Response() question.Answer
	SetHover(x, y int)
	HandleMouseClick(x, y int) (done bool, handled bool)
}

// QuestionForm presents multiple questions as a tabbed form.
// Tab/shift+tab switches between questions; each question keeps
// its own internal keybindings. For multi-question batches, a
// Confirm tab is appended automatically.

type QuestionForm struct {
	Styles       *styles.Styles
	BatchID      string
	questions    []questionResponder // includes ConfirmComponent as last item for batches
	labels       []string            // includes "Confirm" for batches
	requestIDs   []string
	answers      []*question.Answer // nil until answered; only covers real questions
	activeIdx    int
	focused      bool
	hasConfirm   bool              // whether a confirm tab exists
	showTabs     bool              // whether to render tab chrome
	numQuestions int               // real question count (excludes confirm tab)
	confirmComp  *ConfirmComponent // nil when no confirm tab

	keyPrevTab key.Binding
	keyNextTab key.Binding
	keyClose   key.Binding

	// Compositor for tab hit detection. Built during Draw() from
	// tab layers positioned at their screen coordinates.
	compositor *lipgloss.Compositor

	// Hover position for highlighting interactive elements.
	hoverX, hoverY int

	// OnAnswer is called when the form is submitted. The UI sets
	// this to wire up workspace submission.
	OnAnswer func(responses []question.Answer)

	// OnCancel is called when the user presses escape to cancel
	// the entire question batch. The UI sets this to wire up
	// workspace cancellation.
	OnCancel func()
}

// NewQuestionForm creates a tabbed multi-question form from a
// batch request. Each question is wrapped in its existing
// component type (YesNo, SingleChoice, MultiChoice, FreeText).
// A Confirm tab is appended for multi-question batches.

func NewQuestionForm(sty *styles.Styles, batch question.Request) *QuestionForm {
	comps := make([]questionResponder, len(batch.Questions))
	labels := make([]string, len(batch.Questions))
	ids := make([]string, len(batch.Questions))
	for i, req := range batch.Questions {
		switch req.Type {
		case question.TypeYesNo:
			comps[i] = NewYesNo(sty, req)
		case question.TypeSingleChoice:
			comps[i] = NewSingleChoice(sty, req)
		case question.TypeMultiChoice:
			comps[i] = NewMultiChoice(sty, req)
		case question.TypeFreeText:
			comps[i] = NewFreeText(sty, req)
		}
		if req.Label != "" {
			labels[i] = req.Label
		} else {
			labels[i] = shortLabel(req.Text)
		}
		ids[i] = req.ID
	}

	numQuestions := len(comps)
	// Confirm tab only for multi-question batches.
	hasConfirm := numQuestions > 1
	answers := make([]*question.Answer, numQuestions)

	var confirmComp *ConfirmComponent
	allLabels := labels
	if hasConfirm {
		confirmTitle := batch.ConfirmTitle
		if confirmTitle == "" {
			confirmTitle = "Confirm"
		}
		confirmComp = NewConfirmComponent(
			sty,
			confirmTitle,
			batch.ConfirmDescription,
			labels,
			batch.Questions,
			answers,
		)
		allLabels = make([]string, len(labels)+1)
		copy(allLabels, labels)
		allLabels[len(labels)] = "Confirm"
	}
	showTabs := numQuestions > 1

	f := &QuestionForm{
		Styles:       sty,
		BatchID:      batch.ID,
		questions:    comps,
		labels:       allLabels,
		requestIDs:   ids,
		answers:      answers,
		hasConfirm:   hasConfirm,
		showTabs:     showTabs,
		numQuestions: numQuestions,
		confirmComp:  confirmComp,
		keyPrevTab: key.NewBinding(
			key.WithKeys("[", "ctrl+left"),
			key.WithHelp("[", "prev tab"),
		),
		keyNextTab: key.NewBinding(
			key.WithKeys("]", "ctrl+right"),
			key.WithHelp("]", "next tab"),
		),
		keyClose: CloseKey,
	}

	// Wire confirm callbacks.
	if confirmComp != nil {
		confirmComp.OnConfirm = f.submit
		confirmComp.OnReject = func() {
			if idx := f.firstUnanswered(); idx >= 0 {
				f.switchTab(idx)
			} else if numQuestions > 0 {
				f.switchTab(numQuestions - 1)
			}
		}
	}

	if len(comps) > 0 {
		comps[0].SetFocused(true)
	}
	return f
}

// shortLabel truncates a question to at most three words for use
// as a tab header.

func shortLabel(q string) string {
	q = strings.ReplaceAll(q, "\n", " ")
	words := strings.Fields(q)
	if len(words) > 3 {
		words = words[:3]
	}
	return strings.Join(words, " ")
}

// isConfirmTab reports whether the active tab is the confirm tab.

func (f *QuestionForm) isConfirmTab() bool {
	return f.hasConfirm && f.activeIdx == f.numQuestions
}

// isAnswered reports whether a question has a meaningful answer.

func (f *QuestionForm) isAnswered(idx int) bool {
	if idx >= len(f.answers) || f.answers[idx] == nil {
		return false
	}
	resp := f.answers[idx]
	return len(resp.SelectedIDs) > 0 || resp.FillInText != "" || resp.Yes != nil
}

// firstUnanswered returns the index of the first unanswered
// question, or -1 if all are answered.

func (f *QuestionForm) firstUnanswered() int {
	for i, ans := range f.answers {
		if ans == nil {
			return i
		}
		if len(ans.SelectedIDs) == 0 && ans.FillInText == "" && ans.Yes == nil {
			return i
		}
	}
	return -1
}

// HandleKey routes keys to the active tab. Returns true when the
// entire batch is submitted.

func (f *QuestionForm) HandleKey(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	// Tab navigation works on all tabs including confirm.
	switch {
	case key.Matches(msg, f.keyNextTab):
		f.switchTab(f.activeIdx + 1)
		return false, nil
	case key.Matches(msg, f.keyPrevTab):
		f.switchTab(f.activeIdx - 1)
		return false, nil
	}

	// Confirm tab delegates to ConfirmComponent.
	if f.isConfirmTab() {
		done, cmd := f.confirmComp.HandleKey(msg)
		if done {
			return true, cmd
		}
		return false, cmd
	}

	// Global keys for question tabs.
	if key.Matches(msg, f.keyClose) {
		f.cancel()
		return true, nil
	}

	// Route to active question.
	if f.activeIdx < f.numQuestions {
		done, cmd := f.questions[f.activeIdx].HandleKey(msg)
		if done {
			resp := f.questions[f.activeIdx].Response()
			f.answers[f.activeIdx] = &resp
			f.syncConfirmAnswers()
			if f.activeIdx < len(f.labels)-1 {
				f.switchTab(f.activeIdx + 1)
			} else if !f.hasConfirm {
				f.submit()
				return true, cmd
			}
			return false, cmd
		}
		return false, cmd
	}
	return false, nil
}

// HandleWheel scrolls the active choice list vertically, or delegates
// to the active question if it supports wheel scrolling.

func (f *QuestionForm) HandleWheel(deltaX, deltaY float64) {
	if f.isConfirmTab() {
		if deltaY < 0 && f.confirmComp.scrollOffset > 0 {
			f.confirmComp.scrollOffset--
		} else if deltaY > 0 {
			f.confirmComp.scrollOffset++
		}
		return
	}
	if f.activeIdx >= f.numQuestions {
		return
	}
	if we, ok := f.questions[f.activeIdx].(common.WheelScrollable); ok {
		we.HandleWheel(deltaX, deltaY)
	}
}

// switchTab moves focus to the given tab index, wrapping around.
// Snapshots the current question's response before leaving.

func (f *QuestionForm) switchTab(idx int) {
	totalTabs := len(f.labels)
	if totalTabs == 0 {
		return
	}
	// Snapshot and unfocus current.
	if !f.isConfirmTab() && f.activeIdx < f.numQuestions {
		resp := f.questions[f.activeIdx].Response()
		f.answers[f.activeIdx] = &resp
		f.questions[f.activeIdx].SetFocused(false)
	} else if f.isConfirmTab() {
		f.confirmComp.SetFocused(false)
	}
	// Wrap.
	if idx < 0 {
		idx = totalTabs - 1
	} else if idx >= totalTabs {
		idx = 0
	}
	f.activeIdx = idx
	// Focus new.
	if f.isConfirmTab() {
		f.syncConfirmAnswers()
		f.confirmComp.SetFocused(f.focused)
	} else if f.activeIdx < f.numQuestions {
		f.questions[f.activeIdx].SetFocused(f.focused)
	}
}

// syncConfirmAnswers pushes the latest answers to the confirm
// component so its summary stays current.

func (f *QuestionForm) syncConfirmAnswers() {
	if f.confirmComp != nil {
		f.confirmComp.UpdateAnswers(f.answers)
	}
}

// submit collects stored responses and calls OnAnswer.

func (f *QuestionForm) submit() {
	responses := make([]question.Answer, f.numQuestions)
	for i, ans := range f.answers {
		if ans != nil {
			responses[i] = *ans
		} else {
			responses[i] = question.Answer{
				QuestionID: f.requestIDs[i],
			}
		}
	}
	if f.OnAnswer != nil {
		f.OnAnswer(responses)
	}
}

// cancel calls OnCancel to signal that the user dismissed the
// question batch without answering.

func (f *QuestionForm) cancel() {
	if f.OnCancel != nil {
		f.OnCancel()
	}
}

// ShortHelp returns key bindings for the status bar.

func (f *QuestionForm) ShortHelp() []key.Binding {
	if f.isConfirmTab() {
		return f.confirmComp.ShortHelp()
	}
	bindings := []key.Binding{f.keyPrevTab, f.keyNextTab}
	if f.activeIdx < f.numQuestions {
		bindings = append(bindings, f.questions[f.activeIdx].ShortHelp()...)
	}
	return bindings
}

// Height returns the total height using the max tab height so
// switching tabs doesn't cause layout jumps.

func (f *QuestionForm) SetFocused(focused bool) {
	f.focused = focused
	if f.isConfirmTab() {
		f.confirmComp.SetFocused(focused)
	} else if f.activeIdx < f.numQuestions {
		f.questions[f.activeIdx].SetFocused(focused)
	}
}

// SetHover implements MouseClickableEditor. Stores the hover
// position and propagates it to the active component.

func (f *QuestionForm) SetHover(x, y int) {
	f.hoverX = x
	f.hoverY = y
	if f.isConfirmTab() && f.confirmComp != nil {
		f.confirmComp.SetHover(x, y)
	} else if f.activeIdx < len(f.questions) {
		f.questions[f.activeIdx].SetHover(x, y)
	}
}

// HandlePaste implements PasteableEditor. Forwards paste events
// to the active question component if it supports pasting.

func (f *QuestionForm) HandlePaste(msg tea.PasteMsg) tea.Cmd {
	if f.isConfirmTab() {
		return nil
	}
	if f.activeIdx < f.numQuestions {
		if p, ok := f.questions[f.activeIdx].(PasteableEditor); ok {
			return p.HandlePaste(msg)
		}
	}
	return nil
}

// HandleMouseClick implements MouseClickableEditor. It checks if
// the click landed on a tab and switches to it, or delegates to
// the active component for content-area clicks.

func (f *QuestionForm) HandleMouseClick(x, y int) (bool, bool) {
	// Check tabs first.
	if f.showTabs && f.compositor != nil {
		hit := f.compositor.Hit(x, y)
		if !hit.Empty() {
			var idx int
			if _, err := fmt.Sscanf(hit.ID(), "tab_%d", &idx); err == nil {
				if idx >= 0 && idx < len(f.labels) && idx != f.activeIdx {
					f.switchTab(idx)
				}
				return false, true
			}
		}
	}

	// Delegate to active component.
	if f.isConfirmTab() && f.confirmComp != nil {
		return f.confirmComp.HandleMouseClick(x, y)
	}
	if f.activeIdx < len(f.questions) {
		done, handled := f.questions[f.activeIdx].HandleMouseClick(x, y)
		if handled {
			resp := f.questions[f.activeIdx].Response()
			f.answers[f.activeIdx] = &resp
			f.syncConfirmAnswers()
			if done {
				if f.activeIdx < len(f.labels)-1 {
					f.switchTab(f.activeIdx + 1)
					return false, true
				} else if !f.hasConfirm {
					f.submit()
					return true, true
				}
			}
			return false, true
		}
	}
	return false, false
}
