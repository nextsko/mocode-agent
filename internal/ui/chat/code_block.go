package chat

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// CodeBlock 表示一个代码块.
type CodeBlock struct {
	Language  string
	Content   string
	StartLine int
	EndLine   int
	Collapsed bool
	Copied    bool
	Index     int
}

// CodeBlockRenderer 渲染代码块.
type CodeBlockRenderer struct {
	blocks    []CodeBlock
	clipboard string
	width     int
	style     CodeBlockStyle
}

// CodeBlockStyle 代码块样式.
type CodeBlockStyle struct {
	HeaderStyle   lipgloss.Style
	LangStyle     lipgloss.Style
	CopyBtnStyle  lipgloss.Style
	CopiedStyle   lipgloss.Style
	ContentStyle  lipgloss.Style
	CollapseStyle lipgloss.Style
	BorderStyle   lipgloss.Style
}

// DefaultCodeBlockStyle 返回默认样式.
func DefaultCodeBlockStyle() CodeBlockStyle {
	return CodeBlockStyle{
		HeaderStyle: lipgloss.NewStyle().
			Padding(0, 1),
		LangStyle: lipgloss.NewStyle().
			Background(lipgloss.Color("62")).
			Foreground(lipgloss.Color("230")).
			Padding(0, 1),
		CopyBtnStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")).
			MarginLeft(1),
		CopiedStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("42")).
			MarginLeft(1),
		ContentStyle: lipgloss.NewStyle().
			Padding(0, 1),
		CollapseStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")).
			Italic(true),
		BorderStyle: lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color("240")),
	}
}

// NewCodeBlockRenderer 创建代码块渲染器.
func NewCodeBlockRenderer() *CodeBlockRenderer {
	return &CodeBlockRenderer{
		style: DefaultCodeBlockStyle(),
		width: 80,
	}
}

// SetWidth 设置渲染宽度.
func (r *CodeBlockRenderer) SetWidth(width int) {
	r.width = width
}

// AddBlock 添加代码块.
func (r *CodeBlockRenderer) AddBlock(block CodeBlock) {
	block.Index = len(r.blocks)
	r.blocks = append(r.blocks, block)
}

// Render 渲染代码块.
func (r *CodeBlockRenderer) Render(block CodeBlock) string {
	// 语言标签
	langLabel := ""
	if block.Language != "" {
		langLabel = r.style.LangStyle.Render(block.Language)
	}

	// 复制按钮
	copyBtn := r.style.CopyBtnStyle.Render("[copy]")
	if block.Copied {
		copyBtn = r.style.CopiedStyle.Render("[copied!]")
	}

	// 折叠/展开按钮
	collapseBtn := ""
	if r.canCollapse(block) {
		if block.Collapsed {
			collapseBtn = r.style.CollapseStyle.Render("▶ expand")
		} else {
			collapseBtn = r.style.CollapseStyle.Render("▼ collapse")
		}
	}

	// 头部
	headerParts := []string{}
	if langLabel != "" {
		headerParts = append(headerParts, langLabel)
	}
	headerParts = append(headerParts, copyBtn)
	if collapseBtn != "" {
		headerParts = append(headerParts, collapseBtn)
	}
	header := lipgloss.JoinHorizontal(lipgloss.Center, headerParts...)

	// 内容
	content := block.Content
	if block.Collapsed {
		content = r.collapseContent(content)
	}

	// 渲染内容
	renderedContent := r.style.ContentStyle.Render(content)

	// 组合
	return lipgloss.JoinVertical(lipgloss.Left, header, renderedContent)
}

// canCollapse 检查是否可以折叠.
func (r *CodeBlockRenderer) canCollapse(block CodeBlock) bool {
	lines := strings.Split(block.Content, "\n")
	return len(lines) > 5
}

// collapseContent 折叠内容.
func (r *CodeBlockRenderer) collapseContent(content string) string {
	lines := strings.Split(content, "\n")
	if len(lines) <= 5 {
		return content
	}

	// 显示前 3 行和后 2 行
	result := strings.Join(lines[:3], "\n")
	result += "\n" + r.style.CollapseStyle.Render(fmt.Sprintf("... (%d lines hidden) ...", len(lines)-5))
	result += "\n" + strings.Join(lines[len(lines)-2:], "\n")

	return result
}

// HandleClick 处理点击事件.
func (r *CodeBlockRenderer) HandleClick(blockIdx int, x, y int) tea.Cmd {
	if blockIdx < 0 || blockIdx >= len(r.blocks) {
		return nil
	}

	// 检测是否点击了复制按钮
	if r.isCopyButtonClick(blockIdx, x, y) {
		return r.copyToClipboard(blockIdx)
	}

	// 检测是否点击了折叠/展开
	if r.isCollapseClick(blockIdx, x, y) {
		return r.toggleCollapse(blockIdx)
	}

	return nil
}

// isCopyButtonClick 检测是否点击了复制按钮.
func (r *CodeBlockRenderer) isCopyButtonClick(blockIdx int, x, y int) bool {
	// 简化实现：假设复制按钮在头部右侧
	// 实际实现需要根据布局计算位置
	return false
}

// isCollapseClick 检测是否点击了折叠/展开按钮.
func (r *CodeBlockRenderer) isCollapseClick(blockIdx int, x, y int) bool {
	// 简化实现：假设折叠按钮在头部右侧
	// 实际实现需要根据布局计算位置
	return false
}

// copyToClipboard 复制到剪贴板.
func (r *CodeBlockRenderer) copyToClipboard(blockIdx int) tea.Cmd {
	if blockIdx < 0 || blockIdx >= len(r.blocks) {
		return nil
	}

	r.clipboard = r.blocks[blockIdx].Content
	r.blocks[blockIdx].Copied = true

	content := r.blocks[blockIdx].Content
	return func() tea.Msg {
		return CodeBlockCopiedMsg{
			BlockIndex: blockIdx,
			Content:    content,
		}
	}
}

// toggleCollapse 切换折叠/展开.
func (r *CodeBlockRenderer) toggleCollapse(blockIdx int) tea.Cmd {
	if blockIdx < 0 || blockIdx >= len(r.blocks) {
		return nil
	}

	block := &r.blocks[blockIdx]
	block.Collapsed = !block.Collapsed

	return func() tea.Msg {
		return CodeBlockToggledMsg{
			BlockIndex: blockIdx,
			Collapsed:  block.Collapsed,
		}
	}
}

// GetBlocks 获取所有代码块.
func (r *CodeBlockRenderer) GetBlocks() []CodeBlock {
	return r.blocks
}

// GetBlock 获取指定代码块.
func (r *CodeBlockRenderer) GetBlock(index int) *CodeBlock {
	if index < 0 || index >= len(r.blocks) {
		return nil
	}
	return &r.blocks[index]
}

// ClearBlocks 清空代码块.
func (r *CodeBlockRenderer) ClearBlocks() {
	r.blocks = nil
}

// GetClipboard 获取剪贴板内容.
func (r *CodeBlockRenderer) GetClipboard() string {
	return r.clipboard
}

// CodeBlockCopiedMsg 代码块复制消息.
type CodeBlockCopiedMsg struct {
	BlockIndex int
	Content    string
}

// CodeBlockToggledMsg 代码块折叠/展开消息.
type CodeBlockToggledMsg struct {
	BlockIndex int
	Collapsed  bool
}

// DetectLanguage 检测代码语言.
func DetectLanguage(content string) string {
	// 简单的语言检测
	content = strings.TrimSpace(content)

	// 检查 shebang
	if strings.HasPrefix(content, "#!/") {
		if strings.Contains(content, "python") {
			return "python"
		}
		if strings.Contains(content, "bash") || strings.Contains(content, "sh") {
			return "bash"
		}
		if strings.Contains(content, "node") {
			return "javascript"
		}
	}

	// 检查常见模式
	if strings.Contains(content, "func ") && strings.Contains(content, "{") {
		return "go"
	}
	if strings.Contains(content, "def ") && strings.Contains(content, ":") {
		return "python"
	}
	if strings.Contains(content, "function ") && strings.Contains(content, "{") {
		return "javascript"
	}
	if strings.Contains(content, "class ") && strings.Contains(content, "{") {
		return "java"
	}
	if strings.Contains(content, "SELECT ") || strings.Contains(content, "FROM ") {
		return "sql"
	}
	if strings.HasPrefix(content, "<") && strings.HasSuffix(content, ">") {
		return "html"
	}
	if strings.Contains(content, "{") && strings.Contains(content, "}") && strings.Contains(content, ":") {
		return "json"
	}

	return ""
}

// FormatCodeBlock 格式化代码块.
func FormatCodeBlock(content string, language string) string {
	if language == "" {
		language = DetectLanguage(content)
	}

	return fmt.Sprintf("```%s\n%s\n```", language, content)
}
