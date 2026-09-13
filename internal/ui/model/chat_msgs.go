package model

import (
	tea "charm.land/bubbletea/v2"
	"github.com/nextsko/mocode-agent/internal/ui/chat"
	"github.com/nextsko/mocode-agent/internal/ui/list"
	"github.com/nextsko/mocode-agent/internal/util/anim"
)

func (m *Chat) SetMessages(msgs ...chat.MessageItem) {
	m.idInxMap = make(map[string]int)
	m.pausedAnimations = make(map[string]struct{})

	items := make([]list.Item, len(msgs))
	for i, msg := range msgs {
		m.idInxMap[msg.ID()] = i
		// Register nested tool IDs for tools that contain nested tools.
		if container, ok := msg.(chat.NestedToolContainer); ok {
			for _, nested := range container.NestedTools() {
				m.idInxMap[nested.ID()] = i
			}
		}
		items[i] = msg
	}
	m.list.SetItems(items...)
	m.ScrollToBottom()
}

// AppendMessages appends a new message item to the chat list.
// If in follow mode or currently at the bottom, it will auto-scroll to show the new messages.

func (m *Chat) AppendMessages(msgs ...chat.MessageItem) {
	// Check if we should auto-scroll before appending
	shouldAutoScroll := m.follow || m.AtBottom()

	items := make([]list.Item, len(msgs))
	indexOffset := m.list.Len()
	for i, msg := range msgs {
		m.idInxMap[msg.ID()] = indexOffset + i
		// Register nested tool IDs for tools that contain nested tools.
		if container, ok := msg.(chat.NestedToolContainer); ok {
			for _, nested := range container.NestedTools() {
				m.idInxMap[nested.ID()] = indexOffset + i
			}
		}
		items[i] = msg
	}
	m.list.AppendItems(items...)

	// Auto-scroll to bottom if in follow mode or was at bottom
	if shouldAutoScroll {
		m.ScrollToBottom()
	}
}

// UpdateNestedToolIDs updates the ID map for nested tools within a container.
// Call this after modifying nested tools to ensure animations work correctly.

func (m *Chat) UpdateNestedToolIDs(containerID string) {
	idx, ok := m.idInxMap[containerID]
	if !ok {
		return
	}

	item, ok := m.list.ItemAt(idx).(chat.MessageItem)
	if !ok {
		return
	}

	container, ok := item.(chat.NestedToolContainer)
	if !ok {
		return
	}

	// Register all nested tool IDs to point to the container's index.
	for _, nested := range container.NestedTools() {
		m.idInxMap[nested.ID()] = idx
	}
}

// Animate animates items in the chat list. Only propagates animation messages
// to visible items to save CPU. When items are not visible, their animation ID
// is tracked so it can be restarted when they become visible again.

func (m *Chat) Animate(msg anim.StepMsg) tea.Cmd {
	idx, ok := m.idInxMap[msg.ID]
	if !ok {
		return nil
	}

	animatable, ok := m.list.ItemAt(idx).(chat.Animatable)
	if !ok {
		return nil
	}

	// Check if item is currently visible.
	startIdx, endIdx := m.list.VisibleItemIndices()
	isVisible := idx >= startIdx && idx <= endIdx

	if !isVisible {
		// Item not visible - pause animation by not propagating.
		// Track it so we can restart when it becomes visible.
		m.pausedAnimations[msg.ID] = struct{}{}
		return nil
	}

	// Item is visible - remove from paused set and animate.
	delete(m.pausedAnimations, msg.ID)
	return animatable.Animate(msg)
}

// RestartPausedVisibleAnimations restarts animations for items that were paused
// due to being scrolled out of view but are now visible again.

func (m *Chat) RestartPausedVisibleAnimations() tea.Cmd {
	if len(m.pausedAnimations) == 0 {
		return nil
	}

	startIdx, endIdx := m.list.VisibleItemIndices()
	var cmds []tea.Cmd

	for id := range m.pausedAnimations {
		idx, ok := m.idInxMap[id]
		if !ok {
			// Item no longer exists.
			delete(m.pausedAnimations, id)
			continue
		}

		if idx >= startIdx && idx <= endIdx {
			// Item is now visible - restart its animation.
			if animatable, ok := m.list.ItemAt(idx).(chat.Animatable); ok {
				if cmd := animatable.StartAnimation(); cmd != nil {
					cmds = append(cmds, cmd)
				}
			}
			delete(m.pausedAnimations, id)
		}
	}

	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}

// Focus sets the focus state of the chat component.

func (m *Chat) ClearMessages() {
	m.idInxMap = make(map[string]int)
	m.pausedAnimations = make(map[string]struct{})
	m.list.SetItems()
	m.ClearMouse()
}

// RemoveMessage removes a message from the chat list by its ID.

func (m *Chat) RemoveMessage(id string) {
	idx, ok := m.idInxMap[id]
	if !ok {
		return
	}

	// Remove from list
	m.list.RemoveItem(idx)

	// Remove from index map
	delete(m.idInxMap, id)

	// Rebuild index map for all items after the removed one
	for i := idx; i < m.list.Len(); i++ {
		if item, ok := m.list.ItemAt(i).(chat.MessageItem); ok {
			m.idInxMap[item.ID()] = i
		}
	}

	// Clean up any paused animations for this message
	delete(m.pausedAnimations, id)
}

// MessageItem returns the message item with the given ID, or nil if not found.

func (m *Chat) MessageItem(id string) chat.MessageItem {
	idx, ok := m.idInxMap[id]
	if !ok {
		return nil
	}
	item, ok := m.list.ItemAt(idx).(chat.MessageItem)
	if !ok {
		return nil
	}
	return item
}

// CountRunningAgentTools returns how many Agent tool calls are still in
// flight (parallel sub-agents included) — the prime-agent style roster count
// surfaced in the status line.

func (m *Chat) CountRunningAgentTools() int {
	n := 0
	for i := 0; i < m.list.Len(); i++ {
		item, ok := m.list.ItemAt(i).(chat.MessageItem)
		if !ok {
			continue
		}
		if at, ok := item.(*chat.AgentToolMessageItem); ok {
			switch at.Status() {
			case chat.ToolStatusRunning, chat.ToolStatusAwaitingPermission:
				n++
			}
		}
	}
	return n
}

// ToggleExpandedSelectedItem expands the selected message item if it is expandable.
