package styles

import "charm.land/lipgloss/v2"

// The per-agent color registry (a distinct hue per agent, rebuilt from the
// agent order) was removed together with the sidebar Agents section. All
// agent badges now share one fixed badge style and one accent style.
var (
	// AgentBadgeStyle renders agent name badges (white on blue, padded).
	AgentBadgeStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#ffffff")).
			Background(lipgloss.Color("#1D63ED")).
			Padding(0, 1)

	// AgentAccentStyle renders agent names as foreground-only text (green).
	AgentAccentStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#98C379"))
)
