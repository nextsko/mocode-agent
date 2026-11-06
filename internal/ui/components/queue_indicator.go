package components

import (
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// QueueStatus 表示队列的当前状态.
type QueueStatus struct {
	SteerDepth       int
	SteerCapacity    int
	FollowupDepth    int
	FollowupCapacity int
}

// QueueTickMsg 队列动画 tick 消息.
type QueueTickMsg struct{}

// QueueIndicator 显示消息队列状态.
type QueueIndicator struct {
	steerDepth       int
	steerCapacity    int
	followupDepth    int
	followupCapacity int
	animFrame        int
	visible          bool
	width            int
}

// NewQueueIndicator 创建队列指示器.
func NewQueueIndicator() *QueueIndicator {
	return &QueueIndicator{
		visible: true,
		width:   40,
	}
}

// SetWidth 设置指示器宽度.
func (q *QueueIndicator) SetWidth(width int) {
	q.width = width
}

// SetVisible 设置可见性.
func (q *QueueIndicator) SetVisible(visible bool) {
	q.visible = visible
}

// Update 更新队列状态.
func (q *QueueIndicator) Update(status QueueStatus) {
	q.steerDepth = status.SteerDepth
	q.steerCapacity = status.SteerCapacity
	q.followupDepth = status.FollowupDepth
	q.followupCapacity = status.FollowupCapacity
}

// UpdateFromMsg 处理消息更新.
func (q *QueueIndicator) UpdateFromMsg(msg tea.Msg) tea.Cmd {
	switch msg.(type) {
	case QueueTickMsg:
		q.animFrame++
		if q.HasMessages() {
			return q.Tick()
		}
		return nil
	}
	return nil
}

// HasMessages 检查是否有待处理消息.
func (q *QueueIndicator) HasMessages() bool {
	return q.steerDepth > 0 || q.followupDepth > 0
}

// View 渲染队列指示器.
func (q *QueueIndicator) View() string {
	if !q.visible || !q.HasMessages() {
		return ""
	}

	var parts []string

	// 紧急队列
	if q.steerDepth > 0 {
		icon := "⚡"
		if q.animFrame%2 == 0 && q.steerDepth > 0 {
			icon = "🔄"
		}
		steerStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("214")).
			Bold(true)
		parts = append(parts, steerStyle.Render(fmt.Sprintf("%s %d/%d", icon, q.steerDepth, q.steerCapacity)))
	}

	// 普通队列
	if q.followupDepth > 0 {
		followupStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("245"))
		parts = append(parts, followupStyle.Render(fmt.Sprintf("📨 %d/%d", q.followupDepth, q.followupCapacity)))
	}

	result := lipgloss.JoinHorizontal(lipgloss.Center, parts...)

	// 添加边框
	borderStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("240")).
		Padding(0, 1)

	return borderStyle.Render(result)
}

// Tick 启动动画 tick.
func (q *QueueIndicator) Tick() tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg {
		return QueueTickMsg{}
	})
}

// StartAnimation 启动动画（如果有消息）.
func (q *QueueIndicator) StartAnimation() tea.Cmd {
	if q.HasMessages() {
		return q.Tick()
	}
	return nil
}

// Style 返回样式信息.
type QueueIndicatorStyle struct {
	SteerStyle    lipgloss.Style
	FollowupStyle lipgloss.Style
	BorderStyle   lipgloss.Style
	Separator     string
}

// DefaultStyle 返回默认样式.
func DefaultQueueIndicatorStyle() QueueIndicatorStyle {
	return QueueIndicatorStyle{
		SteerStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("214")).
			Bold(true),
		FollowupStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("245")),
		BorderStyle: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("240")).
			Padding(0, 1),
		Separator: lipgloss.NewStyle().
			Foreground(lipgloss.Color("240")).
			Render(" │ "),
	}
}
