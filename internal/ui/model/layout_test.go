package model

import (
	"strconv"
	"strings"
	"testing"

	"charm.land/bubbles/v2/textarea"

	"github.com/nextsko/mocode-agent/internal/domain/session/message"
	"github.com/nextsko/mocode-agent/internal/ui/chat"
	"github.com/nextsko/mocode-agent/internal/ui/common"
	"github.com/nextsko/mocode-agent/internal/ui/components"
	"github.com/nextsko/mocode-agent/internal/ui/styles"
)

// testMessageItem is a minimal chat item used to populate the chat list
// without pulling in full message rendering machinery.
type testMessageItem struct {
	id   string
	text string
}

func (m testMessageItem) ID() string           { return m.id }
func (m testMessageItem) Render(int) string    { return m.text }
func (m testMessageItem) RawRender(int) string { return m.text }

var _ chat.MessageItem = testMessageItem{}

// newTestUI builds a focused uiChat model with dynamic textarea sizing enabled.
// It intentionally keeps dependencies minimal so layout behavior can be tested
// in isolation.
func newTestUI() *UI {
	com := common.DefaultCommon(nil)

	ta := textarea.New()
	ta.SetStyles(com.Styles.Editor.Textarea)
	ta.ShowLineNumbers = false
	ta.CharLimit = -1
	ta.SetVirtualCursor(false)
	ta.DynamicHeight = true
	ta.MinHeight = TextareaMinHeight
	ta.MaxHeight = TextareaMaxHeight
	ta.Focus()

	u := &UI{
		com:      com,
		status:   NewStatus(com),
		chat:     NewChat(com),
		textarea: ta,
		state:    uiChat,
		focus:    uiFocusEditor,
		width:    140,
		height:   45,
	}

	return u
}

func TestUpdateLayoutAndSize_EditorGrowthShrinksChat(t *testing.T) {
	t.Parallel()

	// Baseline layout at min textarea height.
	u := newTestUI()
	u.updateLayoutAndSize()

	initialEditorHeight := u.layout.editor.Dy()
	initialChatHeight := u.layout.main.Dy()

	// Increase textarea content enough to trigger growth, then run the
	// same resize hook used in the real update path.
	prevHeight := u.textarea.Height()
	u.textarea.SetValue(strings.Repeat("line\n", 8))
	u.textarea.MoveToEnd()
	_ = u.handleTextareaHeightChange(prevHeight)

	if got := u.layout.editor.Dy(); got <= initialEditorHeight {
		t.Fatalf("expected editor to grow: got %d, want > %d", got, initialEditorHeight)
	}

	if got := u.layout.main.Dy(); got >= initialChatHeight {
		t.Fatalf("expected chat to shrink: got %d, want < %d", got, initialChatHeight)
	}
}

func TestEditorReservesSubagentSummaryHeight(t *testing.T) {
	t.Parallel()

	// Baseline layout: no sub-agent has run, so the summary box is absent.
	u := newTestUI()
	u.updateLayoutAndSize()
	baseEditor := u.layout.editor.Dy()
	baseMain := u.layout.main.Dy()

	// A single running Agent tool call makes the summary box appear.
	sty := styles.ThemeForProvider("")
	tc := message.ToolCall{ID: "tc1", Name: "agent", Input: `{"prompt":"x"}`}
	u.chat.SetMessages(chat.NewAgentToolMessageItem(&sty, tc, nil, false))
	u.updateLayoutAndSize()

	if got, want := u.layout.editor.Dy(), baseEditor+components.SummaryHeight; got != want {
		t.Fatalf("editor height = %d, want %d (base %d + summary %d)", got, want, baseEditor, components.SummaryHeight)
	}
	if got, want := u.layout.main.Dy(), baseMain-components.SummaryHeight; got != want {
		t.Fatalf("chat height = %d, want %d (summary box must steal rows from the chat)", got, want)
	}
}

func TestEditorHidesSummaryWhenSubagentsFinish(t *testing.T) {
	t.Parallel()

	u := newTestUI()
	u.updateLayoutAndSize()
	baseEditor := u.layout.editor.Dy()
	baseMain := u.layout.main.Dy()

	// A finished Agent tool call yields a done-only roster, which must not
	// reserve any editor rows (the box auto-hides once nothing is running).
	sty := styles.ThemeForProvider("")
	tc := message.ToolCall{ID: "tc1", Name: "agent", Input: `{"prompt":"x"}`, Finished: true}
	res := &message.ToolResult{ToolCallID: "tc1", Name: "agent", Content: "done"}
	u.chat.SetMessages(chat.NewAgentToolMessageItem(&sty, tc, res, false))
	u.updateLayoutAndSize()

	if got := u.layout.editor.Dy(); got != baseEditor {
		t.Fatalf("editor height = %d, want %d (finished sub-agents must hide the summary box)", got, baseEditor)
	}
	if got := u.layout.main.Dy(); got != baseMain {
		t.Fatalf("chat height = %d, want %d (hidden box must give rows back)", got, baseMain)
	}
}

func TestHandleTextareaHeightChange_FollowModeStaysAtBottom(t *testing.T) {
	t.Parallel()

	// Use enough messages to make the chat scrollable so AtBottom/Follow
	// assertions are meaningful.
	u := newTestUI()

	msgs := make([]chat.MessageItem, 0, 60)
	for i := range 60 {
		msgs = append(msgs, testMessageItem{
			id:   "m-" + strconv.Itoa(i),
			text: "message " + strconv.Itoa(i),
		})
	}
	u.chat.SetMessages(msgs...)
	u.updateLayoutAndSize()

	// Enter follow mode and verify we're anchored at the bottom first.
	u.chat.ScrollToBottom()
	if !u.chat.AtBottom() {
		t.Fatal("expected chat to start at bottom")
	}

	// Grow the editor; follow mode should keep the chat pinned to the end
	// even as the chat viewport shrinks.
	prevHeight := u.textarea.Height()
	u.textarea.SetValue(strings.Repeat("line\n", 10))
	u.textarea.MoveToEnd()
	_ = u.handleTextareaHeightChange(prevHeight)

	if !u.chat.Follow() {
		t.Fatal("expected follow mode to remain enabled")
	}
	if !u.chat.AtBottom() {
		t.Fatal("expected chat to remain at bottom after editor resize in follow mode")
	}
}
