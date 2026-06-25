package dialog

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/sahilm/fuzzy"

	"github.com/package-register/mocode/internal/core/config"
	"github.com/package-register/mocode/internal/ui/styles"
)

// ModeItem wraps an agent mode for the modes selection list.
type ModeItem struct {
	modeID  string
	agent   config.Agent
	t       *styles.Styles
	m       fuzzy.Match
	cache   map[int]string
	focused bool
	active  bool
}

var _ ListItem = (*ModeItem)(nil)

// NewModeItem creates a new ModeItem. modeID is the agent ID from the config map key.
func NewModeItem(t *styles.Styles, modeID string, agent config.Agent, active bool) *ModeItem {
	return &ModeItem{
		modeID: modeID,
		agent:  agent,
		t:      t,
		cache:  make(map[int]string),
		active: active,
	}
}

// Filter implements ListItem.
func (m *ModeItem) Filter() string {
	name := m.agent.Name
	if name == "" {
		name = m.modeID
	}
	return name + " " + m.agent.Description
}

// ID implements ListItem.
func (m *ModeItem) ID() string {
	return m.modeID
}

// SetFocused implements ListItem.
func (m *ModeItem) SetFocused(focused bool) {
	if m.focused != focused {
		m.cache = nil
	}
	m.focused = focused
}

// SetMatch implements ListItem.
func (m *ModeItem) SetMatch(fm fuzzy.Match) {
	m.cache = nil
	m.m = fm
}

// ModeID returns the agent mode ID.
func (m *ModeItem) ModeID() string {
	return m.modeID
}

func (m *ModeItem) Agent() config.Agent {
	return m.agent
}

func (m *ModeItem) Title() string {
	title := m.agent.Name
	if title == "" {
		title = m.modeID
	}
	return title
}

func (m *ModeItem) Active() bool {
	return m.active
}

// Render implements ListItem.
func (m *ModeItem) Render(width int) string {
	if m.cache == nil {
		m.cache = make(map[int]string)
	}
	if cached, ok := m.cache[width]; ok {
		return cached
	}

	style := m.t.Dialog.NormalItem
	if m.focused {
		style = m.t.Dialog.SelectedItem
	}

	lineWidth := max(0, width-style.GetHorizontalFrameSize())
	marker := " "
	if m.active {
		marker = "●"
	}
	badge := ""
	if m.active {
		badge = "current"
	}
	title := m.Title()
	// Agent colour badge - use the modeID to get the agent style.
	renderedTitle := styles.AgentBadgeStyleFor(m.modeID).Render(title)
	if !m.focused {
		renderedTitle = styles.AgentAccentStyleFor(m.modeID).Render(title)
	}
	titleWidth := lipgloss.Width(renderedTitle)
	badgeWidth := lipgloss.Width(badge)
	if badgeWidth > 0 {
		badgeWidth += 2
	}
	gap := strings.Repeat(" ", max(0, lineWidth-titleWidth-badgeWidth-1-lipgloss.Width(marker)))

	var row string
	if m.focused {
		if badge != "" {
			badge = " " + badge + " "
		}
		row = marker + " " + renderedTitle + gap + badge
	} else {
		renderedMarker := m.t.Dialog.ListItem.InfoBlurred.Render(marker)
		if m.active {
			renderedMarker = m.t.Dialog.TitleText.Render(marker)
		}
		renderedBadge := ""
		if badge != "" {
			renderedBadge = m.t.Dialog.TitleAccent.Render(" " + badge + " ")
		}
		row = renderedMarker + " " + renderedTitle + gap + renderedBadge
	}

	rendered := style.Render(row)
	m.cache[width] = rendered
	return rendered
}
