package dialog

import (
	"github.com/nextsko/mocode-agent/internal/ui/list"
)

func (c *Commands) isInSubmenu() bool {
	return len(c.navStack) > 0
}

func (c *Commands) pushSubmenu(parent *CommandItem) {
	if !parent.HasChildren() {
		return
	}
	c.navStack = append(c.navStack, parent)
	items := make([]list.FilterableItem, len(parent.Children))
	for i, child := range parent.Children {
		items[i] = child
	}
	c.list.SetItems(items...)
	c.list.SetFilter("")
	c.list.SetSelected(0)
	c.list.ScrollToTop()
	c.input.SetValue("")
	c.filterActive = false
}

func (c *Commands) popSubmenu() {
	if len(c.navStack) == 0 {
		return
	}
	c.navStack = c.navStack[:len(c.navStack)-1]
	c.filterActive = false
	if len(c.navStack) == 0 {
		c.setCommandItems(c.selected)
	} else {
		// Restore parent's children directly without modifying navStack again.
		parent := c.navStack[len(c.navStack)-1]
		items := make([]list.FilterableItem, len(parent.Children))
		for i, child := range parent.Children {
			items[i] = child
		}
		c.list.SetItems(items...)
		c.list.SetFilter("")
		c.list.SetSelected(0)
		c.list.ScrollToTop()
		c.input.SetValue("")
	}
}
