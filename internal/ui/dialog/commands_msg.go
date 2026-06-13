package dialog

import (
	"image"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"github.com/package-register/mocode/internal/ui/list"
)

func (c *Commands) HandleMsg(msg tea.Msg) Action {
	switch msg := msg.(type) {
	case dockerMCPAvailabilityCheckedMsg:
		c.dockerMCPAvailable = &msg.available
		c.dockerMCPCheckInFlight = false
		if c.selected == SystemCommands {
			c.setCommandItems(c.selected)
		}
		return nil
	case spinner.TickMsg:
		if c.loading {
			var cmd tea.Cmd
			c.spinner, cmd = c.spinner.Update(msg)
			return ActionCmd{Cmd: cmd}
		}
	case tea.MouseWheelMsg:
		switch msg.Button {
		case tea.MouseWheelUp:
			if c.list.IsSelectedFirst() {
				c.list.SelectLast()
			} else {
				c.list.SelectPrev()
			}
			c.list.ScrollToSelected()
		case tea.MouseWheelDown:
			if c.list.IsSelectedLast() {
				c.list.SelectFirst()
			} else {
				c.list.SelectNext()
			}
			c.list.ScrollToSelected()
		}
	case tea.MouseClickMsg:
		return c.handleMouseClick(msg)
	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, c.keyMap.Close):
			if c.isInSubmenu() {
				c.popSubmenu()
				return nil
			}
			return ActionClose{}
		case key.Matches(msg, c.keyMap.Left):
			if c.isInSubmenu() {
				c.popSubmenu()
			}
			return nil
		case key.Matches(msg, c.keyMap.Right):
			if selectedItem := c.list.SelectedItem(); selectedItem != nil {
				if item, ok := selectedItem.(*CommandItem); ok && item != nil && item.HasChildren() {
					c.pushSubmenu(item)
				}
			}
			return nil
		case key.Matches(msg, c.keyMap.Previous):
			c.list.Focus()
			if c.list.IsSelectedFirst() {
				c.list.SelectLast()
			} else {
				c.list.SelectPrev()
			}
			c.list.ScrollToSelected()
		case key.Matches(msg, c.keyMap.Next):
			c.list.Focus()
			if c.list.IsSelectedLast() {
				c.list.SelectFirst()
			} else {
				c.list.SelectNext()
			}
			c.list.ScrollToSelected()
		case key.Matches(msg, c.keyMap.Select):
			if selectedItem := c.list.SelectedItem(); selectedItem != nil {
				if item, ok := selectedItem.(*CommandItem); ok && item != nil {
					// If filtering at root level and selected a child item,
					// navigate into its parent submenu first.
					if c.filterActive && item.parent != nil && !c.isInSubmenu() {
						c.navStack = append(c.navStack, item.parent)
						c.pushSubmenu(item.parent)
						// Find and select the child in the submenu.
						for i, filterable := range c.list.FilteredItems() {
							if child, ok := filterable.(*CommandItem); ok && child == item {
								c.list.SetSelected(i)
								break
							}
						}
						c.list.ScrollToSelected()
						c.input.SetValue("")
						c.filterActive = false
						// If the selected child itself has children, let the user
						// navigate deeper; otherwise execute its action directly.
						if item.HasChildren() {
							return nil
						}
						return item.Action()
					}
					if item.HasChildren() {
						c.pushSubmenu(item)
						return nil
					}
					return item.Action()
				}
			}
		case key.Matches(msg, c.keyMap.Tab):
			if len(c.customCommands) > 0 || len(c.mcpPrompts) > 0 {
				c.selected = c.nextCommandType()
				c.setCommandItems(c.selected)
			}
		case key.Matches(msg, c.keyMap.ShiftTab):
			if len(c.customCommands) > 0 || len(c.mcpPrompts) > 0 {
				c.selected = c.previousCommandType()
				c.setCommandItems(c.selected)
			}
		default:
			var cmd tea.Cmd
			for _, item := range c.list.FilteredItems() {
				if item, ok := item.(*CommandItem); ok && item != nil {
					if msg.String() == item.Shortcut() {
						if item.HasChildren() {
							c.pushSubmenu(item)
							return nil
						}
						return item.Action()
					}
				}
			}
			c.input, cmd = c.input.Update(msg)
			value := c.input.Value()

			// Cross-level fuzzy search: when typing a filter at root level,
			// swap to the flattened allItems list so children are searchable.
			if !c.isInSubmenu() && c.allItems != nil {
				if value != "" && !c.filterActive {
					c.list.SetItems(c.allItems...)
					c.filterActive = true
				} else if value == "" && c.filterActive {
					// Restore root-level items when filter is cleared.
					items := c.defaultCommands()
					filterableItems := make([]list.FilterableItem, len(items))
					for i, item := range items {
						filterableItems[i] = item
					}
					c.list.SetItems(filterableItems...)
					c.filterActive = false
				}
			}

			c.list.SetFilter(value)
			c.list.ScrollToTop()
			c.list.SetSelected(0)
			return ActionCmd{cmd}
		}
	}
	return nil
}

func (c *Commands) handleMouseClick(msg tea.MouseClickMsg) Action {
	if !image.Pt(msg.X, msg.Y).In(c.lastArea) {
		return nil
	}
	if !image.Pt(msg.X, msg.Y).In(c.lastListArea) {
		return nil
	}
	idx, _ := c.list.ItemIndexAtPosition(msg.X-c.lastListArea.Min.X, msg.Y-c.lastListArea.Min.Y)
	if idx < 0 {
		return nil
	}
	c.list.SetSelected(idx)
	c.list.ScrollToSelected()
	if item := c.selectedCommandItem(); item != nil {
		if item.HasChildren() {
			c.pushSubmenu(item)
			return nil
		}
		return item.Action()
	}
	return nil
}

func (c *Commands) selectedCommandItem() *CommandItem {
	selected := c.list.SelectedItem()
	if selected == nil {
		return nil
	}
	item, ok := selected.(*CommandItem)
	if !ok || item == nil {
		return nil
	}
	return item
}
