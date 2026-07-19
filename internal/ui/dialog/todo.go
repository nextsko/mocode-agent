package dialog

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"github.com/nextsko/mocode-agent/internal/domain/session"
	"github.com/nextsko/mocode-agent/internal/ui/common"
	"github.com/nextsko/mocode-agent/internal/ui/styles"
)

const TodoID = "todo"

type todoMode int

const (
	todoModeBrowse todoMode = iota
	todoModeEdit
)

type todoEditField int

const (
	todoEditContent todoEditField = iota
	todoEditActiveForm
)

type Todo struct {
	com      *common.Common
	help     help.Model
	session  string
	todos    []session.Todo
	selected int
	scroll   int
	mode     todoMode
	isNew    bool
	field    todoEditField
	content  textarea.Model
	active   textarea.Model

	keyMap struct {
		Close       key.Binding
		Next        key.Binding
		Previous    key.Binding
		Top         key.Binding
		Bottom      key.Binding
		Add         key.Binding
		Edit        key.Binding
		Delete      key.Binding
		Save        key.Binding
		ToggleField key.Binding
		Newline     key.Binding
		StatusPend  key.Binding
		StatusProg  key.Binding
		StatusDone  key.Binding
	}
}

var _ Dialog = (*Todo)(nil)

func NewTodo(com *common.Common, sess session.Session, selected int) (*Todo, error) {
	d := &Todo{
		com:     com,
		session: sess.ID,
		todos:   append([]session.Todo(nil), sess.Todos...),
	}
	if selected >= 0 && selected < len(d.todos) {
		d.selected = selected
	}

	h := help.New()
	h.Styles = com.Styles.DialogHelpStyles()
	d.help = h

	d.content = textarea.New()
	d.content.SetStyles(com.Styles.Editor.Textarea)
	d.content.ShowLineNumbers = false
	d.content.CharLimit = -1
	d.content.Prompt = ""

	d.active = textarea.New()
	d.active.SetStyles(com.Styles.Editor.Textarea)
	d.active.ShowLineNumbers = false
	d.active.CharLimit = -1
	d.active.Prompt = ""

	d.keyMap.Close = CloseKey
	d.keyMap.Next = key.NewBinding(key.WithKeys("down", "ctrl+n"), key.WithHelp("↓", "next"))
	d.keyMap.Previous = key.NewBinding(key.WithKeys("up", "ctrl+p"), key.WithHelp("↑", "prev"))
	d.keyMap.Top = key.NewBinding(key.WithKeys("g"), key.WithHelp("g", "top"))
	d.keyMap.Bottom = key.NewBinding(key.WithKeys("G"), key.WithHelp("G", "bottom"))
	d.keyMap.Add = key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "add"))
	d.keyMap.Edit = key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "edit"))
	d.keyMap.Delete = key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "delete"))
	d.keyMap.Save = key.NewBinding(key.WithKeys("enter", "ctrl+s"), key.WithHelp("enter", "save"))
	d.keyMap.ToggleField = key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "field"))
	d.keyMap.Newline = key.NewBinding(key.WithKeys("ctrl+j"), key.WithHelp("ctrl+j", "newline"))
	d.keyMap.StatusPend = key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "pending"))
	d.keyMap.StatusProg = key.NewBinding(key.WithKeys("i"), key.WithHelp("i", "in progress"))
	d.keyMap.StatusDone = key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "completed"))
	return d, nil
}

func (d *Todo) ID() string { return TodoID }

func (d *Todo) HandleMsg(msg tea.Msg) Action {
	keyMsg, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}

	if d.mode == todoModeEdit {
		switch {
		case key.Matches(keyMsg, d.keyMap.Close):
			d.mode = todoModeBrowse
			d.content.Blur()
			d.active.Blur()
			return nil
		case key.Matches(keyMsg, d.keyMap.Save):
			return d.saveEdit()
		case key.Matches(keyMsg, d.keyMap.ToggleField):
			d.toggleEditField()
			return nil
		case key.Matches(keyMsg, d.keyMap.Newline):
			if d.field == todoEditActiveForm {
				d.active.InsertRune('\n')
			} else {
				d.content.InsertRune('\n')
			}
			return nil
		default:
			if d.field == todoEditContent {
				updated, cmd := d.content.Update(msg)
				d.content = updated
				return ActionCmd{Cmd: cmd}
			}
			updated, cmd := d.active.Update(msg)
			d.active = updated
			return ActionCmd{Cmd: cmd}
		}
	}

	switch {
	case key.Matches(keyMsg, d.keyMap.Close):
		return ActionClose{}
	case key.Matches(keyMsg, d.keyMap.Previous):
		if d.selected > 0 {
			d.selected--
		}
	case key.Matches(keyMsg, d.keyMap.Next):
		if d.selected < len(d.todos)-1 {
			d.selected++
		}
	case key.Matches(keyMsg, d.keyMap.Top):
		d.selected = 0
		d.scroll = 0
	case key.Matches(keyMsg, d.keyMap.Bottom):
		if len(d.todos) > 0 {
			d.selected = len(d.todos) - 1
		}
	case key.Matches(keyMsg, d.keyMap.Add):
		d.startAdd()
	case key.Matches(keyMsg, d.keyMap.Edit):
		d.startEdit()
	case key.Matches(keyMsg, d.keyMap.Delete):
		return d.deleteSelected()
	case key.Matches(keyMsg, d.keyMap.StatusPend):
		return d.updateStatus(session.TodoStatusPending)
	case key.Matches(keyMsg, d.keyMap.StatusProg):
		return d.updateStatus(session.TodoStatusInProgress)
	case key.Matches(keyMsg, d.keyMap.StatusDone):
		return d.updateStatus(session.TodoStatusCompleted)
	}
	return nil
}

func (d *Todo) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	t := d.com.Styles
	maxW := area.Dx() - t.Dialog.View.GetHorizontalFrameSize() - 4
	width := todoMax(72, todoMin(126, maxW))
	maxH := area.Dy() - t.Dialog.View.GetVerticalFrameSize() - 6
	height := todoMax(12, todoMin(30, maxH))

	rc := NewRenderContext(t, width)
	rc.Title = "Todo"
	if len(d.todos) > 0 {
		rc.TitleInfo = t.Dialog.ListItem.InfoFocused.Render(fmt.Sprintf(" %d/%d", d.selected+1, len(d.todos)))
	}

	if d.mode == todoModeEdit {
		d.content.SetWidth(todoMax(24, width-t.Dialog.View.GetHorizontalFrameSize()-6))
		d.content.SetHeight(5)
		d.active.SetWidth(todoMax(24, width-t.Dialog.View.GetHorizontalFrameSize()-6))
		d.active.SetHeight(3)
		rc.AddPart(t.Dialog.PrimaryText.Render("Editing selected todo. Enter saves, Tab switches field, Ctrl+J inserts newline, Esc cancels."))
		rc.AddPart(t.Dialog.TitleText.Render("Content"))
		rc.AddPart(d.content.View())
		rc.AddPart(t.Dialog.TitleText.Render("Active Form"))
		rc.AddPart(d.active.View())
		rc.Help = d.help.View(d)
		cur := d.cursor()
		DrawCenterCursor(scr, area, rc.Render(), cur)
		return cur
	}

	bodyH := height - 4
	leftW := todoMax(20, todoMin(34, width/3))
	rightW := todoMax(28, width-leftW-8)
	left := d.renderTodoDirectory(t, leftW, bodyH)
	right := d.renderTodoDetail(t, rightW, bodyH)
	rc.AddPart(lipgloss.JoinHorizontal(lipgloss.Top, left, "  ", right))
	rc.Help = d.help.View(d)
	DrawCenterCursor(scr, area, rc.Render(), nil)
	return nil
}

func (d *Todo) ShortHelp() []key.Binding {
	if d.mode == todoModeEdit {
		return []key.Binding{d.keyMap.Save, d.keyMap.ToggleField, d.keyMap.Newline, d.keyMap.Close}
	}
	upDown := key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑/↓", "select"))
	return []key.Binding{upDown, d.keyMap.Add, d.keyMap.Edit, d.keyMap.Delete, d.keyMap.Close}
}

func (d *Todo) FullHelp() [][]key.Binding {
	if d.mode == todoModeEdit {
		return [][]key.Binding{{d.keyMap.Save, d.keyMap.ToggleField, d.keyMap.Newline, d.keyMap.Close}}
	}
	return [][]key.Binding{
		{d.keyMap.Previous, d.keyMap.Next, d.keyMap.Top, d.keyMap.Bottom},
		{d.keyMap.Add, d.keyMap.Edit, d.keyMap.Delete},
		{d.keyMap.StatusPend, d.keyMap.StatusProg, d.keyMap.StatusDone, d.keyMap.Close},
	}
}

func (d *Todo) renderTodoDirectory(t *styles.Styles, width, height int) string {
	if len(d.todos) == 0 {
		return t.Dialog.PrimaryText.Width(width).Render("No todos")
	}
	d.ensureVisible(height - 2)
	lines := []string{t.Dialog.TitleText.Render("Todos")}
	for i := 0; i < height-2 && d.scroll+i < len(d.todos); i++ {
		idx := d.scroll + i
		todo := d.todos[idx]
		row := ansi.Truncate(fmt.Sprintf("%s %s", d.statusGlyph(todo.Status), todoOneLine(d.todoLabel(&todo))), width-2, "…")
		if idx == d.selected {
			lines = append(lines, t.Dialog.SelectedItem.Width(width).Render(row))
		} else {
			lines = append(lines, t.Dialog.NormalItem.Width(width).Render(row))
		}
	}
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func (d *Todo) renderTodoDetail(t *styles.Styles, width, height int) string {
	todo := d.selectedTodo()
	if todo == nil {
		return t.Dialog.PrimaryText.Width(width).Render("No todo selected")
	}
	lines := []string{
		t.Dialog.TitleText.Render("Details"),
		fmt.Sprintf("Status: %s", todo.Status),
		"",
		t.Dialog.TitleText.Render("Content"),
	}
	lines = append(lines, todoClampLines(todo.Content, width, todoMax(3, height/3))...)
	lines = append(lines, "", t.Dialog.TitleText.Render("Active Form"))
	active := todo.ActiveForm
	if strings.TrimSpace(active) == "" {
		active = "(empty)"
	}
	lines = append(lines, todoClampLines(active, width, todoMax(2, height/4))...)
	return t.Dialog.PrimaryText.Width(width).Render(strings.Join(lines, "\n"))
}

func (d *Todo) selectedTodo() *session.Todo {
	if d.selected < 0 || d.selected >= len(d.todos) {
		return nil
	}
	return &d.todos[d.selected]
}

func (d *Todo) ensureVisible(visible int) {
	if visible <= 0 {
		return
	}
	if d.selected < d.scroll {
		d.scroll = d.selected
	}
	if d.selected >= d.scroll+visible {
		d.scroll = d.selected - visible + 1
	}
	if d.scroll < 0 {
		d.scroll = 0
	}
}

func (d *Todo) startAdd() {
	d.mode = todoModeEdit
	d.isNew = true
	d.field = todoEditContent
	d.content.SetValue("")
	d.active.SetValue("")
	d.content.Focus()
	d.active.Blur()
}

func (d *Todo) startEdit() {
	todo := d.selectedTodo()
	if todo == nil {
		d.startAdd()
		return
	}
	d.mode = todoModeEdit
	d.isNew = false
	d.field = todoEditContent
	d.content.SetValue(todo.Content)
	d.content.MoveToEnd()
	d.active.SetValue(todo.ActiveForm)
	d.active.MoveToEnd()
	d.content.Focus()
	d.active.Blur()
}

func (d *Todo) saveEdit() Action {
	content := strings.TrimSpace(d.content.Value())
	if content == "" {
		return nil
	}
	active := strings.TrimSpace(d.active.Value())
	isAdd := d.isNew || d.selected < 0 || d.selected >= len(d.todos)
	if isAdd {
		d.todos = append(d.todos, session.Todo{
			Content:    content,
			ActiveForm: active,
			Status:     session.TodoStatusPending,
		})
		d.selected = len(d.todos) - 1
	} else {
		d.todos[d.selected].Content = content
		d.todos[d.selected].ActiveForm = active
	}
	d.mode = todoModeBrowse
	d.isNew = false
	d.content.Blur()
	d.active.Blur()
	return ActionSetSessionTodos{Todos: append([]session.Todo(nil), d.todos...)}
}

func (d *Todo) deleteSelected() Action {
	if d.selected < 0 || d.selected >= len(d.todos) {
		return nil
	}
	d.todos = append(d.todos[:d.selected], d.todos[d.selected+1:]...)
	if d.selected >= len(d.todos) && d.selected > 0 {
		d.selected--
	}
	return ActionSetSessionTodos{Todos: append([]session.Todo(nil), d.todos...)}
}

func (d *Todo) updateStatus(status session.TodoStatus) Action {
	todo := d.selectedTodo()
	if todo == nil {
		return nil
	}
	todo.Status = status
	if status != session.TodoStatusInProgress {
		todo.ActiveForm = strings.TrimSpace(todo.ActiveForm)
	}
	return ActionSetSessionTodos{Todos: append([]session.Todo(nil), d.todos...)}
}

func (d *Todo) toggleEditField() {
	if d.field == todoEditContent {
		d.field = todoEditActiveForm
		d.content.Blur()
		d.active.Focus()
		return
	}
	d.field = todoEditContent
	d.active.Blur()
	d.content.Focus()
}

func (d *Todo) cursor() *tea.Cursor {
	if d.field == todoEditActiveForm {
		cur := d.active.Cursor()
		if cur != nil {
			cur.Y += 8 + d.content.Height()
		}
		return InputCursor(d.com.Styles, cur)
	}
	return InputCursor(d.com.Styles, d.content.Cursor())
}

func (d *Todo) todoLabel(todo *session.Todo) string {
	if todo.Status == session.TodoStatusInProgress && strings.TrimSpace(todo.ActiveForm) != "" {
		return todo.ActiveForm
	}
	return todo.Content
}

func (d *Todo) statusGlyph(status session.TodoStatus) string {
	switch status {
	case session.TodoStatusCompleted:
		return "✓"
	case session.TodoStatusInProgress:
		return "→"
	default:
		return "•"
	}
}

func todoClampLines(text string, width, maxLines int) []string {
	if strings.TrimSpace(text) == "" || maxLines <= 0 {
		return nil
	}
	var out []string
	for _, line := range strings.Split(text, "\n") {
		out = append(out, ansi.Truncate(line, width, "…"))
		if len(out) >= maxLines {
			break
		}
	}
	if len(strings.Split(text, "\n")) > maxLines {
		out = append(out, "…")
	}
	return out
}

func todoOneLine(text string) string {
	text = strings.ReplaceAll(strings.TrimSpace(text), "\n", " ")
	text = strings.Join(strings.Fields(text), " ")
	return text
}

func todoMin(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func todoMax(a, b int) int {
	if a > b {
		return a
	}
	return b
}
