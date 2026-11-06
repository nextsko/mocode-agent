package model

import (
	"image"
	"slices"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/nextsko/mocode-agent/internal/domain/session"
	pillspkg "github.com/nextsko/mocode-agent/internal/ui/pills"
	"github.com/nextsko/mocode-agent/internal/ui/styles"
)

const (
	// pillHeightWithBorder is the height of a pill including its border.
	pillHeightWithBorder = 3
)

// pillSection represents which section of the pills panel is focused.
type pillSection int

const (
	pillSectionTodos pillSection = iota
	pillSectionQueue
)

type pillClickTargetKind int

const (
	pillClickNone pillClickTargetKind = iota
	pillClickTodoSummary
	pillClickTodoRow
	pillClickQueue
)

type pillClickTarget struct {
	kind      pillClickTargetKind
	todoIndex int
}

// togglePillsExpanded toggles the pills panel expansion state.
func (m *UI) togglePillsExpanded() tea.Cmd {
	if !m.hasSession() {
		return nil
	}
	hasPills := pillspkg.HasIncompleteTodos(m.session.Todos) || m.promptQueue > 0
	if !hasPills {
		return nil
	}
	m.pillsExpanded = !m.pillsExpanded
	if m.pillsExpanded {
		if pillspkg.HasIncompleteTodos(m.session.Todos) {
			m.focusedPillSection = pillSectionTodos
		} else {
			m.focusedPillSection = pillSectionQueue
		}
	}
	m.updateLayoutAndSize()

	// Make sure to follow scroll if follow is enabled when toggling pills.
	if m.chat.Follow() {
		m.chat.ScrollToBottom()
	}

	return nil
}

// switchPillSection changes focus between todo and queue sections.
func (m *UI) switchPillSection(dir int) tea.Cmd {
	if !m.pillsExpanded || !m.hasSession() {
		return nil
	}
	hasIncompleteTodos := pillspkg.HasIncompleteTodos(m.session.Todos)
	hasQueue := m.promptQueue > 0

	if dir < 0 && m.focusedPillSection == pillSectionQueue && hasIncompleteTodos {
		m.focusedPillSection = pillSectionTodos
		m.updateLayoutAndSize()
		return nil
	}
	if dir > 0 && m.focusedPillSection == pillSectionTodos && hasQueue {
		m.focusedPillSection = pillSectionQueue
		m.updateLayoutAndSize()
		return nil
	}
	return nil
}

// pillsAreaHeight calculates the total height needed for the pills area.
func (m *UI) pillsAreaHeight() int {
	if !m.hasSession() {
		return 0
	}
	hasIncomplete := pillspkg.HasIncompleteTodos(m.session.Todos)
	hasQueue := m.promptQueue > 0
	hasPills := hasIncomplete || hasQueue
	if !hasPills {
		return 0
	}

	pillsAreaHeight := pillHeightWithBorder
	if m.pillsExpanded {
		if m.focusedPillSection == pillSectionTodos && hasIncomplete {
			pillsAreaHeight += len(m.session.Todos)
		} else if m.focusedPillSection == pillSectionQueue && hasQueue {
			pillsAreaHeight += m.promptQueue
		}
	}
	return pillsAreaHeight
}

// renderPills renders the pills panel and stores it in m.pillsView.
func (m *UI) renderPills() {
	m.pillsView = ""
	if !m.hasSession() {
		return
	}

	width := m.layout.pills.Dx()
	if width <= 0 {
		return
	}

	paddingLeft := 3
	contentWidth := max(width-paddingLeft, 0)

	hasIncomplete := pillspkg.HasIncompleteTodos(m.session.Todos)
	hasQueue := m.promptQueue > 0

	if !hasIncomplete && !hasQueue {
		return
	}

	t := m.com.Styles
	todosFocused := m.pillsExpanded && m.focusedPillSection == pillSectionTodos
	queueFocused := m.pillsExpanded && m.focusedPillSection == pillSectionQueue

	inProgressIcon := t.Tool.TodoInProgressIcon.Render(styles.SpinnerIcon)
	if m.todoIsSpinning {
		inProgressIcon = m.todoSpinner.View()
	}

	var pills []string
	if hasIncomplete {
		pills = append(pills, pillspkg.TodoPill(m.session.Todos, inProgressIcon, todosFocused, m.pillsExpanded, t))
	}
	if hasQueue {
		pills = append(pills, pillspkg.QueuePill(m.promptQueue, queueFocused, m.pillsExpanded, t))
	}

	var expandedList string
	if m.pillsExpanded {
		if todosFocused && hasIncomplete {
			expandedList = pillspkg.TodoList(m.session.Todos, inProgressIcon, t, contentWidth)
			if state := m.todoContinuations[m.session.ID]; state != nil && state.Stalled {
				stalled := t.Pills.HelpText.Render("Todo stalled, manual intervention required")
				if expandedList != "" {
					expandedList = expandedList + "\n" + stalled
				} else {
					expandedList = stalled
				}
			}
		} else if queueFocused && hasQueue {
			if m.com.Workspace.AgentIsReady() {
				queueItems := m.com.Workspace.AgentQueuedPromptsList(m.session.ID)
				expandedList = pillspkg.QueueList(queueItems, t)
			}
		}
	}

	if len(pills) == 0 {
		return
	}

	pillsRow := lipgloss.JoinHorizontal(lipgloss.Top, pills...)

	helpDesc := "open"
	if m.pillsExpanded {
		helpDesc = "close"
	}
	helpKey := t.Pills.HelpKey.Render("ctrl+t")
	helpText := t.Pills.HelpText.Render(helpDesc)
	helpHint := lipgloss.JoinHorizontal(lipgloss.Center, helpKey, " ", helpText)
	pillsRow = lipgloss.JoinHorizontal(lipgloss.Center, pillsRow, " ", helpHint)

	pillsArea := pillsRow
	if expandedList != "" {
		pillsArea = lipgloss.JoinVertical(lipgloss.Left, pillsRow, expandedList)
	}

	m.pillsView = t.Pills.Area.MaxWidth(width).PaddingLeft(paddingLeft).Render(pillsArea)
}

func (m *UI) handlePillsMouseClick(msg tea.MouseClickMsg) tea.Cmd {
	if !m.hasSession() || !image.Pt(msg.X, msg.Y).In(m.layout.pills) {
		return nil
	}
	target := m.pillClickTargetAt(msg.X, msg.Y)
	if target.kind != pillClickTodoSummary && target.kind != pillClickTodoRow {
		m.lastPillClick = time.Time{}
		m.lastPillTarget = pillClickTarget{}
		return nil
	}

	now := time.Now()
	if now.Sub(m.lastPillClick) <= doubleClickThreshold && target == m.lastPillTarget {
		m.lastPillClick = time.Time{}
		m.lastPillTarget = pillClickTarget{}
		return m.openTodoDialog(target.todoIndex)
	}
	m.lastPillClick = now
	m.lastPillTarget = target
	return nil
}

func (m *UI) pillClickTargetAt(x, y int) pillClickTarget {
	if !m.hasSession() {
		return pillClickTarget{}
	}
	width := m.layout.pills.Dx()
	if width <= 0 {
		return pillClickTarget{}
	}

	paddingLeft := 3
	localX := x - m.layout.pills.Min.X - paddingLeft
	localY := y - m.layout.pills.Min.Y
	if localX < 0 || localY < 0 {
		return pillClickTarget{}
	}

	hasIncomplete := pillspkg.HasIncompleteTodos(m.session.Todos)
	hasQueue := m.promptQueue > 0
	if !hasIncomplete && !hasQueue {
		return pillClickTarget{}
	}

	t := m.com.Styles
	inProgressIcon := t.Tool.TodoInProgressIcon.Render(styles.SpinnerIcon)
	if m.todoIsSpinning {
		inProgressIcon = m.todoSpinner.View()
	}

	todosFocused := m.pillsExpanded && m.focusedPillSection == pillSectionTodos
	queueFocused := m.pillsExpanded && m.focusedPillSection == pillSectionQueue
	if localY < pillHeightWithBorder {
		offset := 0
		if hasIncomplete {
			w := lipgloss.Width(pillspkg.TodoPill(m.session.Todos, inProgressIcon, todosFocused, m.pillsExpanded, t))
			if localX >= offset && localX < offset+w {
				return pillClickTarget{kind: pillClickTodoSummary, todoIndex: -1}
			}
			offset += w
		}
		if hasQueue {
			w := lipgloss.Width(pillspkg.QueuePill(m.promptQueue, queueFocused, m.pillsExpanded, t))
			if localX >= offset && localX < offset+w {
				return pillClickTarget{kind: pillClickQueue}
			}
		}
		return pillClickTarget{}
	}

	if !m.pillsExpanded || !todosFocused {
		return pillClickTarget{}
	}
	row := localY - pillHeightWithBorder
	if idx, ok := m.todoIndexForExpandedRow(row); ok {
		return pillClickTarget{kind: pillClickTodoRow, todoIndex: idx}
	}
	return pillClickTarget{}
}

func (m *UI) todoIndexForExpandedRow(row int) (int, bool) {
	if row < 0 || row >= len(m.session.Todos) {
		return 0, false
	}
	order := make([]int, len(m.session.Todos))
	for i := range m.session.Todos {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int {
		return todoDisplayOrder(m.session.Todos[a].Status) - todoDisplayOrder(m.session.Todos[b].Status)
	})
	return order[row], true
}

func todoDisplayOrder(status session.TodoStatus) int {
	switch status {
	case session.TodoStatusCompleted:
		return 0
	case session.TodoStatusInProgress:
		return 1
	default:
		return 2
	}
}
