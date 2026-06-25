package panel

import (
	"strings"
	"sync"

	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"

	"github.com/package-register/mocode/internal/ui/styles"
)

// View manages a panel tree for displaying parallel agent execution.
type View struct {
	mu      sync.RWMutex
	root    *Panel
	active  string // ID of the active/focused panel
	visible bool

	// Styling
	sty *styles.Styles
}

// NewView creates a panel view.
func NewView(sty *styles.Styles) *View {
	return &View{
		sty:     sty,
		visible: false,
	}
}

// IsVisible returns whether the panel view is currently shown.
func (v *View) IsVisible() bool {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.visible
}

// Show makes the panel view visible.
func (v *View) Show() {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.visible = true
}

// Hide hides the panel view.
func (v *View) Hide() {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.visible = false
}

// Toggle toggles visibility.
func (v *View) Toggle() {
	if v.IsVisible() {
		v.Hide()
	} else {
		v.Show()
	}
}

// GetRoot returns the root panel.
func (v *View) GetRoot() *Panel {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.root
}

// SetRoot sets the root panel tree.
func (v *View) SetRoot(root *Panel) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.root = root
	if v.active == "" && root != nil {
		v.active = root.ID
	}
}

// SetActive sets the active panel by ID.
func (v *View) SetActive(id string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	// Deactivate previous
	if v.root != nil {
		if prev := v.root.Find(v.active); prev != nil {
			prev.Active = false
		}
	}
	// Activate new
	if v.root != nil {
		if next := v.root.Find(id); next != nil {
			next.Active = true
		}
	}
	v.active = id
}

// ActiveID returns the active panel ID.
func (v *View) ActiveID() string {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.active
}

// UpdateContent updates a specific panel's content.
func (v *View) UpdateContent(id, content string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.root == nil {
		return
	}
	if p := v.root.Find(id); p != nil {
		p.SetContent(content)
	}
}

// BuildAgentPanels creates a panel tree from agent task data.
func (v *View) BuildAgentPanels(agentID string, subAgents []AgentPanelData) {
	panels := make([]*Panel, 0, len(subAgents)+1)

	// Main agent panel
	mainPanel := New(agentID+"-main", "Main")
	mainPanel.Active = true
	panels = append(panels, mainPanel)

	// Sub-agent panels
	for _, sa := range subAgents {
		subPanel := New(sa.ID, sa.Title)
		subPanel.SetContent(sa.Content)
		panels = append(panels, subPanel)
	}

	var root *Panel
	if len(panels) == 1 {
		root = panels[0]
	} else {
		root = NewSplit(agentID+"-split", Vertical, panels...)
	}

	v.SetRoot(root)
	v.SetActive(agentID + "-main")
	v.Show()
}

// Draw renders the panel view into the screen.
func (v *View) Draw(scr uv.Screen, area uv.Rectangle) {
	v.mu.RLock()
	defer v.mu.RUnlock()

	if v.root == nil || !v.visible {
		return
	}

	v.root.RenderAt(scr, area)
}

// RenderMain returns a string for inline rendering (used in chat list).
func (v *View) RenderMain(width int) string {
	v.mu.RLock()
	defer v.mu.RUnlock()

	if v.root == nil || !v.visible {
		return ""
	}

	var b strings.Builder
	b.WriteString("\n")
	b.WriteString(lipgloss.NewStyle().
		Foreground(lipgloss.Color("252")).
		Background(lipgloss.Color("236")).
		Padding(0, 1).
		Render(" ▌ AGENT PANELS "))
	b.WriteString("\n")

	// List agents
	for _, child := range v.root.Children {
		marker := "  "
		if child.Active {
			marker = "▸ "
		}
		title := child.Title
		if title == "" {
			title = child.ID
		}
		b.WriteString(marker + title)
		if child.IsLeaf() {
			content := child.GetContent()
			if content != "" {
				// Show first line of content
				firstLine := strings.Split(content, "\n")[0]
				if len(firstLine) > width-20 {
					firstLine = firstLine[:width-20] + "…"
				}
				b.WriteString(": " + firstLine)
			}
		}
		b.WriteString("\n")
	}
	b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render(strings.Repeat("─", min(width, 60))))
	return b.String()
}

// AgentPanelData holds data for building a sub-agent panel.
type AgentPanelData struct {
	ID      string
	Title   string
	Content string
}
