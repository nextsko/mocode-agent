package model

import (
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nextsko/mocode-agent/internal/ui/chat"
	"github.com/nextsko/mocode-agent/internal/ui/list"
)

func (m *Chat) AtBottom() bool {
	return m.list.AtBottom()
}

// Follow returns whether the chat view is in follow mode (auto-scroll to
// bottom on new messages).

func (m *Chat) Follow() bool {
	return m.follow
}

// ScrollToBottom scrolls the chat view to the bottom.

func (m *Chat) ScrollToBottom() {
	m.list.ScrollToBottom()
	m.follow = true // Enable follow mode when user scrolls to bottom
}

// ScrollToTop scrolls the chat view to the top.

func (m *Chat) ScrollToTop() {
	m.list.ScrollToTop()
	m.follow = false // Disable follow mode when user scrolls up
}

// ScrollBy scrolls the chat view by the given number of line deltas.

func (m *Chat) ScrollBy(lines int) {
	m.list.ScrollBy(lines)
	m.follow = lines > 0 && m.AtBottom() // Disable follow mode if user scrolls up
}

// ScrollToSelected scrolls the chat view to the selected item.

func (m *Chat) ScrollToSelected() {
	m.list.ScrollToSelected()
	m.follow = m.AtBottom() // Disable follow mode if user scrolls up
}

// ScrollToIndex scrolls the chat view to the item at the given index.

func (m *Chat) ScrollToIndex(index int) {
	m.list.ScrollToIndex(index)
	m.follow = m.AtBottom() // Disable follow mode if user scrolls up
}

// ScrollToTopAndAnimate scrolls the chat view to the top and returns a command to restart
// any paused animations that are now visible.

func (m *Chat) ScrollToTopAndAnimate() tea.Cmd {
	m.ScrollToTop()
	return m.RestartPausedVisibleAnimations()
}

// ScrollToBottomAndAnimate scrolls the chat view to the bottom and returns a command to
// restart any paused animations that are now visible.

func (m *Chat) ScrollToBottomAndAnimate() tea.Cmd {
	m.ScrollToBottom()
	return m.RestartPausedVisibleAnimations()
}

// ScrollByAndAnimate scrolls the chat view by the given number of line deltas and returns
// a command to restart any paused animations that are now visible.

func (m *Chat) ScrollByAndAnimate(lines int) tea.Cmd {
	m.ScrollBy(lines)
	return m.RestartPausedVisibleAnimations()
}

// ScrollToSelectedAndAnimate scrolls the chat view to the selected item and returns a
// command to restart any paused animations that are now visible.

func (m *Chat) ScrollToSelectedAndAnimate() tea.Cmd {
	m.ScrollToSelected()
	return m.RestartPausedVisibleAnimations()
}

// SelectedItemInView returns whether the selected item is currently in view.

func (m *Chat) SelectedItemInView() bool {
	return m.list.SelectedItemInView()
}

func (m *Chat) isSelectable(index int) bool {
	item := m.list.ItemAt(index)
	if item == nil {
		return false
	}
	_, ok := item.(list.Focusable)
	return ok
}

// SetSelected sets the selected message index in the chat list.

func (m *Chat) SetSelected(index int) {
	m.list.SetSelected(index)
	if index < 0 || index >= m.list.Len() {
		return
	}
	for {
		if m.isSelectable(m.list.Selected()) {
			return
		}
		if m.list.SelectNext() {
			continue
		}
		// If we're at the end and the last item isn't selectable, walk backwards
		// to find the nearest selectable item.
		for {
			if !m.list.SelectPrev() {
				return
			}
			if m.isSelectable(m.list.Selected()) {
				return
			}
		}
	}
}

// SelectPrev selects the previous message in the chat list.

func (m *Chat) SelectPrev() {
	for {
		if !m.list.SelectPrev() {
			return
		}
		if m.isSelectable(m.list.Selected()) {
			return
		}
	}
}

// SelectNext selects the next message in the chat list.

func (m *Chat) SelectNext() {
	for {
		if !m.list.SelectNext() {
			return
		}
		if m.isSelectable(m.list.Selected()) {
			return
		}
	}
}

// SelectFirst selects the first message in the chat list.

func (m *Chat) SelectFirst() {
	if !m.list.SelectFirst() {
		return
	}
	if m.isSelectable(m.list.Selected()) {
		return
	}
	for {
		if !m.list.SelectNext() {
			return
		}
		if m.isSelectable(m.list.Selected()) {
			return
		}
	}
}

// SelectLast selects the last message in the chat list.

func (m *Chat) SelectLast() {
	if !m.list.SelectLast() {
		return
	}
	if m.isSelectable(m.list.Selected()) {
		return
	}
	for {
		if !m.list.SelectPrev() {
			return
		}
		if m.isSelectable(m.list.Selected()) {
			return
		}
	}
}

// SelectFirstInView selects the first message currently in view.

func (m *Chat) SelectFirstInView() {
	startIdx, endIdx := m.list.VisibleItemIndices()
	for i := startIdx; i <= endIdx; i++ {
		if m.isSelectable(i) {
			m.list.SetSelected(i)
			return
		}
	}
}

// SelectLastInView selects the last message currently in view.

func (m *Chat) SelectLastInView() {
	startIdx, endIdx := m.list.VisibleItemIndices()
	for i := endIdx; i >= startIdx; i-- {
		if m.isSelectable(i) {
			m.list.SetSelected(i)
			return
		}
	}
}

// ClearMessages removes all messages from the chat list.

func (m *Chat) HandleDelayedClick(msg DelayedClickMsg) bool {
	// Ignore if this click was superseded by a newer click (double/triple).
	if msg.ClickID != m.pendingClickID {
		return false
	}

	// Don't expand if user dragged to select text.
	if m.HasHighlight() {
		return false
	}

	// Execute the click action (e.g., expansion).
	selectedItem := m.list.SelectedItem()
	if clickable, ok := selectedItem.(list.MouseClickable); ok {
		handled := clickable.HandleMouseClick(ansi.MouseButton1, msg.X, msg.Y)
		// Toggle expansion if applicable.
		if expandable, ok := selectedItem.(chat.Expandable); ok {
			if !expandable.ToggleExpanded() {
				m.ScrollToIndex(m.list.Selected())
			}
		}
		if m.AtBottom() {
			m.ScrollToBottom()
		}
		return handled
	}

	return false
}

// HandleMouseUp handles mouse up events for the chat component.
