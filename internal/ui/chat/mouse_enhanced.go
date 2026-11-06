package chat

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Position 表示位置.
type Position struct {
	X int
	Y int
}

// URLInfo 表示检测到的 URL.
type URLInfo struct {
	URL   string
	Start int
	End   int
	Line  int
	Col   int
}

// Selection 表示文本选择.
type Selection struct {
	Start     Position
	End       Position
	Content   string
	StartTime time.Time
}

// EnhancedMouseHandler 增强的鼠标处理器.
type EnhancedMouseHandler struct {
	// URL 悬停
	hoveredURL *URLInfo
	urlStyle   lipgloss.Style

	// 文本选择
	selection     *Selection
	clickCount    int
	lastClickTime time.Time
	lastClickX    int
	lastClickY    int

	// 拖拽
	isDragging bool
	dragStart  Position
	dragEnd    Position

	// 状态
	enabled bool
}

// NewEnhancedMouseHandler 创建增强的鼠标处理器.
func NewEnhancedMouseHandler() *EnhancedMouseHandler {
	return &EnhancedMouseHandler{
		enabled: true,
		urlStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("39")).
			Underline(true),
	}
}

// SetEnabled 设置是否启用.
func (h *EnhancedMouseHandler) SetEnabled(enabled bool) {
	h.enabled = enabled
}

// HandleMouseClick 处理鼠标点击.
func (h *EnhancedMouseHandler) HandleMouseClick(btn ansi.MouseButton, x, y int) tea.Cmd {
	if !h.enabled {
		return nil
	}

	now := time.Now()

	// 检测双击/三击
	if now.Sub(h.lastClickTime) < 500*time.Millisecond &&
		abs(x-h.lastClickX) < 2 && abs(y-h.lastClickY) < 2 {
		h.clickCount++
	} else {
		h.clickCount = 1
	}

	h.lastClickTime = now
	h.lastClickX = x
	h.lastClickY = y

	switch h.clickCount {
	case 1:
		// 单击: 展开/折叠
		return h.handleSingleClick(x, y)
	case 2:
		// 双击: 选择单词
		return h.handleDoubleClick(x, y)
	case 3:
		// 三击: 选择行
		h.clickCount = 0
		return h.handleTripleClick(x, y)
	}

	return nil
}

// HandleMouseMotion 处理鼠标移动.
func (h *EnhancedMouseHandler) HandleMouseMotion(x, y int) {
	if !h.enabled {
		return
	}

	// 检测 URL 悬停
	if h.hoveredURL != nil {
		// 检查是否还在 URL 上
		if !h.isInURL(x, y) {
			h.hoveredURL = nil
		}
	}
}

// HandleMouseDrag 处理鼠标拖拽.
func (h *EnhancedMouseHandler) HandleMouseDrag(x, y int) {
	if !h.enabled {
		return
	}

	if !h.isDragging {
		h.isDragging = true
		h.dragStart = Position{X: x, Y: y}
	}
	h.dragEnd = Position{X: x, Y: y}
}

// HandleMouseUp 处理鼠标释放.
func (h *EnhancedMouseHandler) HandleMouseUp(x, y int) {
	if !h.enabled {
		return
	}

	if h.isDragging {
		h.isDragging = false
		h.updateSelection()
	}
}

// handleSingleClick 处理单击.
func (h *EnhancedMouseHandler) handleSingleClick(x, y int) tea.Cmd {
	// 检查是否点击了 URL
	if h.hoveredURL != nil {
		return h.openURL(h.hoveredURL.URL)
	}

	// 检查是否点击了可展开/折叠的元素
	return h.toggleExpand(x, y)
}

// handleDoubleClick 处理双击.
func (h *EnhancedMouseHandler) handleDoubleClick(x, y int) tea.Cmd {
	// 选择单词
	word := h.getWordAt(x, y)
	if word != "" {
		h.selection = &Selection{
			Start:     Position{X: x - len(word)/2, Y: y},
			End:       Position{X: x + len(word)/2, Y: y},
			Content:   word,
			StartTime: time.Now(),
		}
		return h.copyToClipboard(word)
	}
	return nil
}

// handleTripleClick 处理三击.
func (h *EnhancedMouseHandler) handleTripleClick(x, y int) tea.Cmd {
	// 选择行
	line := h.getLineAt(y)
	if line != "" {
		h.selection = &Selection{
			Start:     Position{X: 0, Y: y},
			End:       Position{X: len(line), Y: y},
			Content:   line,
			StartTime: time.Now(),
		}
		return h.copyToClipboard(line)
	}
	return nil
}

// updateSelection 更新选择区域.
func (h *EnhancedMouseHandler) updateSelection() {
	if h.dragStart.Y == h.dragEnd.Y {
		// 单行选择
		startX := min(h.dragStart.X, h.dragEnd.X)
		endX := max(h.dragStart.X, h.dragEnd.X)
		content := h.getTextBetween(h.dragStart.Y, startX, endX)
		h.selection = &Selection{
			Start:     Position{X: startX, Y: h.dragStart.Y},
			End:       Position{X: endX, Y: h.dragEnd.Y},
			Content:   content,
			StartTime: time.Now(),
		}
	} else {
		// 多行选择
		content := h.getTextBetweenLines(h.dragStart.Y, h.dragEnd.Y)
		h.selection = &Selection{
			Start:     h.dragStart,
			End:       h.dragEnd,
			Content:   content,
			StartTime: time.Now(),
		}
	}
}

// isInURL 检查位置是否在 URL 上.
func (h *EnhancedMouseHandler) isInURL(x, y int) bool {
	if h.hoveredURL == nil {
		return false
	}
	return y == h.hoveredURL.Line && x >= h.hoveredURL.Col && x <= h.hoveredURL.Col+len(h.hoveredURL.URL)
}

// getWordAt 获取指定位置的单词.
func (h *EnhancedMouseHandler) getWordAt(x, y int) string {
	// TODO: 实现从内容中获取单词
	return ""
}

// getLineAt 获取指定行的内容.
func (h *EnhancedMouseHandler) getLineAt(y int) string {
	// TODO: 实现从内容中获取行
	return ""
}

// getTextBetween 获取两个位置之间的文本.
func (h *EnhancedMouseHandler) getTextBetween(y, startX, endX int) string {
	// TODO: 实现从内容中获取文本
	return ""
}

// getTextBetweenLines 获取两行之间的文本.
func (h *EnhancedMouseHandler) getTextBetweenLines(startY, endY int) string {
	// TODO: 实现从内容中获取多行文本
	return ""
}

// openURL 打开 URL.
func (h *EnhancedMouseHandler) openURL(url string) tea.Cmd {
	return func() tea.Msg {
		// TODO: 实现打开 URL
		return nil
	}
}

// toggleExpand 切换展开/折叠.
func (h *EnhancedMouseHandler) toggleExpand(x, y int) tea.Cmd {
	return func() tea.Msg {
		// TODO: 实现展开/折叠
		return nil
	}
}

// copyToClipboard 复制到剪贴板.
func (h *EnhancedMouseHandler) copyToClipboard(content string) tea.Cmd {
	return func() tea.Msg {
		// TODO: 实现复制到剪贴板
		return nil
	}
}

// GetHoveredURL 获取悬停的 URL.
func (h *EnhancedMouseHandler) GetHoveredURL() *URLInfo {
	return h.hoveredURL
}

// SetHoveredURL 设置悬停的 URL.
func (h *EnhancedMouseHandler) SetHoveredURL(url *URLInfo) {
	h.hoveredURL = url
}

// GetSelection 获取当前选择.
func (h *EnhancedMouseHandler) GetSelection() *Selection {
	return h.selection
}

// ClearSelection 清除选择.
func (h *EnhancedMouseHandler) ClearSelection() {
	h.selection = nil
}

// IsDragging 是否正在拖拽.
func (h *EnhancedMouseHandler) IsDragging() bool {
	return h.isDragging
}

// GetDragStart 获取拖拽起点.
func (h *EnhancedMouseHandler) GetDragStart() Position {
	return h.dragStart
}

// GetDragEnd 获取拖拽终点.
func (h *EnhancedMouseHandler) GetDragEnd() Position {
	return h.dragEnd
}

// RenderSelection 渲染选择区域.
func (h *EnhancedMouseHandler) RenderSelection(content string, width int) string {
	if h.selection == nil {
		return content
	}

	// 高亮选择的文本
	selectedStyle := lipgloss.NewStyle().
		Background(lipgloss.Color("57")).
		Foreground(lipgloss.Color("230"))

	lines := strings.Split(content, "\n")
	var result []string

	for i, line := range lines {
		if i >= h.selection.Start.Y && i <= h.selection.End.Y {
			if i == h.selection.Start.Y && i == h.selection.End.Y {
				// 单行选择
				start := h.selection.Start.X
				end := h.selection.End.X
				if start > len(line) {
					start = len(line)
				}
				if end > len(line) {
					end = len(line)
				}
				selected := line[start:end]
				result = append(result, line[:start]+selectedStyle.Render(selected)+line[end:])
			} else if i == h.selection.Start.Y {
				// 多行选择开始
				start := h.selection.Start.X
				if start > len(line) {
					start = len(line)
				}
				result = append(result, line[:start]+selectedStyle.Render(line[start:]))
			} else if i == h.selection.End.Y {
				// 多行选择结束
				end := h.selection.End.X
				if end > len(line) {
					end = len(line)
				}
				result = append(result, selectedStyle.Render(line[:end])+line[end:])
			} else {
				// 多行选择中间
				result = append(result, selectedStyle.Render(line))
			}
		} else {
			result = append(result, line)
		}
	}

	return strings.Join(result, "\n")
}

// abs 返回绝对值.
func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// min 返回两个数中的较小值.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// max 返回两个数中的较大值.
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
