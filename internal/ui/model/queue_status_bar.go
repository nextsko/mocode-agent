package model

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/nextsko/mocode-agent/internal/ui/components"
)

// QueueStatusBar 队列状态栏
// 将 QueueIndicator 集成到状态栏中
type QueueStatusBar struct {
	indicator *components.QueueIndicator
	visible   bool
	width     int
	style     QueueStatusBarStyle
}

// QueueStatusBarStyle 队列状态栏样式
type QueueStatusBarStyle struct {
	ContainerStyle lipgloss.Style
	TextStyle      lipgloss.Style
	IconStyle      lipgloss.Style
}

// DefaultQueueStatusBarStyle 返回默认样式
func DefaultQueueStatusBarStyle() QueueStatusBarStyle {
	return QueueStatusBarStyle{
		ContainerStyle: lipgloss.NewStyle().
			Padding(0, 1),
		TextStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("252")),
		IconStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("214")),
	}
}

// NewQueueStatusBar 创建队列状态栏
func NewQueueStatusBar() *QueueStatusBar {
	return &QueueStatusBar{
		indicator: components.NewQueueIndicator(),
		visible:   true,
		width:     40,
		style:     DefaultQueueStatusBarStyle(),
	}
}

// SetWidth 设置宽度
func (q *QueueStatusBar) SetWidth(width int) {
	q.width = width
	q.indicator.SetWidth(width)
}

// SetVisible 设置可见性
func (q *QueueStatusBar) SetVisible(visible bool) {
	q.visible = visible
	q.indicator.SetVisible(visible)
}

// Update 更新队列状态
func (q *QueueStatusBar) Update(status components.QueueStatus) {
	q.indicator.Update(status)
}

// UpdateFromMsg 处理消息更新
func (q *QueueStatusBar) UpdateFromMsg(msg tea.Msg) tea.Cmd {
	return q.indicator.UpdateFromMsg(msg)
}

// View 渲染队列状态栏
func (q *QueueStatusBar) View() string {
	if !q.visible {
		return ""
	}

	view := q.indicator.View()
	if view == "" {
		return ""
	}

	// 添加样式
	return q.style.ContainerStyle.Render(view)
}

// HasMessages 检查是否有待处理消息
func (q *QueueStatusBar) HasMessages() bool {
	return q.indicator.HasMessages()
}

// StartAnimation 启动动画
func (q *QueueStatusBar) StartAnimation() tea.Cmd {
	return q.indicator.StartAnimation()
}

// StatusBarWithQueue 带队列状态的状态栏
type StatusBarWithQueue struct {
	status    *Status
	queueBar  *QueueStatusBar
	width     int
	showQueue bool
}

// NewStatusBarWithQueue 创建带队列状态的状态栏
func NewStatusBarWithQueue(status *Status) *StatusBarWithQueue {
	return &StatusBarWithQueue{
		status:    status,
		queueBar:  NewQueueStatusBar(),
		showQueue: true,
	}
}

// SetWidth 设置宽度
func (s *StatusBarWithQueue) SetWidth(width int) {
	s.width = width
	s.queueBar.SetWidth(width)
}

// SetShowQueue 设置是否显示队列
func (s *StatusBarWithQueue) SetShowQueue(show bool) {
	s.showQueue = show
	s.queueBar.SetVisible(show)
}

// UpdateQueueStatus 更新队列状态
func (s *StatusBarWithQueue) UpdateQueueStatus(status components.QueueStatus) {
	s.queueBar.Update(status)
}

// Update 处理消息
func (s *StatusBarWithQueue) Update(msg tea.Msg) tea.Cmd {
	return s.queueBar.UpdateFromMsg(msg)
}

// View 渲染状态栏
func (s *StatusBarWithQueue) View() string {
	var parts []string

	// 原始状态栏内容
	if s.status.line != "" {
		parts = append(parts, s.status.line)
	}

	// 队列状态
	if s.showQueue {
		queueView := s.queueBar.View()
		if queueView != "" {
			parts = append(parts, queueView)
		}
	}

	if len(parts) == 0 {
		return ""
	}

	return lipgloss.JoinHorizontal(lipgloss.Center, parts...)
}

// HasQueueMessages 检查是否有队列消息
func (s *StatusBarWithQueue) HasQueueMessages() bool {
	return s.queueBar.HasMessages()
}

// GetQueueView 获取队列视图
func (s *StatusBarWithQueue) GetQueueView() string {
	return s.queueBar.View()
}

// QueueStatusComponent 队列状态组件
// 可以独立使用的队列状态显示组件
type QueueStatusComponent struct {
	indicator *components.QueueIndicator
	width     int
	height    int
	focused   bool
}

// NewQueueStatusComponent 创建队列状态组件
func NewQueueStatusComponent() *QueueStatusComponent {
	return &QueueStatusComponent{
		indicator: components.NewQueueIndicator(),
		width:     40,
		height:    1,
	}
}

// SetSize 设置大小
func (c *QueueStatusComponent) SetSize(width, height int) {
	c.width = width
	c.height = height
	c.indicator.SetWidth(width)
}

// SetFocused 设置焦点状态
func (c *QueueStatusComponent) SetFocused(focused bool) {
	c.focused = focused
}

// Update 更新队列状态
func (c *QueueStatusComponent) Update(status components.QueueStatus) {
	c.indicator.Update(status)
}

// UpdateFromMsg 处理消息更新
func (c *QueueStatusComponent) UpdateFromMsg(msg tea.Msg) tea.Cmd {
	return c.indicator.UpdateFromMsg(msg)
}

// View 渲染队列状态组件
func (c *QueueStatusComponent) View() string {
	view := c.indicator.View()
	if view == "" {
		return ""
	}

	// 添加焦点样式
	if c.focused {
		focusStyle := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("62"))
		return focusStyle.Render(view)
	}

	return view
}

// HasMessages 检查是否有待处理消息
func (c *QueueStatusComponent) HasMessages() bool {
	return c.indicator.HasMessages()
}

// StartAnimation 启动动画
func (c *QueueStatusComponent) StartAnimation() tea.Cmd {
	return c.indicator.StartAnimation()
}

// FormatQueueStatus 格式化队列状态
func FormatQueueStatus(status components.QueueStatus) string {
	if status.SteerDepth == 0 && status.FollowupDepth == 0 {
		return ""
	}

	var parts []string
	if status.SteerDepth > 0 {
		parts = append(parts, fmt.Sprintf("⚡ %d/%d", status.SteerDepth, status.SteerCapacity))
	}
	if status.FollowupDepth > 0 {
		parts = append(parts, fmt.Sprintf("📨 %d/%d", status.FollowupDepth, status.FollowupCapacity))
	}

	return strings.Join(parts, " │ ")
}
