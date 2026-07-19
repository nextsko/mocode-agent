package chat

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2/tree"

	"github.com/package-register/mocode/internal/core/agent"
	"github.com/package-register/mocode/tools"
)

func getDigits(n int) int {
	if n == 0 {
		return 1
	}
	if n < 0 {
		n = -n
	}
	digits := 0
	for n > 0 {
		n /= 10
		digits++
	}
	return digits
}

func formatSize(bytes int) string {
	const (
		kb = 1024
		mb = kb * 1024
	)
	switch {
	case bytes >= mb:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(mb))
	case bytes >= kb:
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(kb))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

func formatTimeout(timeout int) string {
	if timeout == 0 {
		return ""
	}
	return fmt.Sprintf("%ds", timeout)
}

func formatNonZero(value int) string {
	if value == 0 {
		return ""
	}
	return fmt.Sprintf("%d", value)
}

func roundedEnumerator(lPadding, width int) tree.Enumerator {
	if width == 0 {
		width = 2
	}
	if lPadding == 0 {
		lPadding = 1
	}
	return func(children tree.Children, index int) string {
		line := strings.Repeat("─", width)
		padding := strings.Repeat(" ", lPadding)
		if children.Length()-1 == index {
			return padding + "╰" + line
		}
		return padding + "├" + line
	}
}

func prettifyToolName(name string) string {
	switch name {
	case agent.AgentToolName:
		return "Agent"
	case tools.BashToolName:
		return "Bash"
	case tools.JobOutputToolName:
		return "Job: Output"
	case tools.JobKillToolName:
		return "Job: Kill"
	case tools.DownloadToolName:
		return "Download"
	case tools.EditToolName:
		return "Edit"
	case tools.ReadFilesToolName:
		return "Read Files"
	case tools.MultiEditToolName:
		return "Multi-Edit"
	case tools.FetchToolName:
		return "Fetch"
	case tools.AgenticFetchToolName:
		return "Agentic Fetch"
	case tools.WebFetchToolName:
		return "Fetch"
	case tools.WebSearchToolName:
		return "Search"
	case tools.GlobToolName:
		return "Glob"
	case tools.GrepToolName:
		return "Grep"
	case tools.LSToolName:
		return "List"
	case tools.SourcegraphToolName:
		return "Sourcegraph"
	case tools.TodosToolName:
		return "To-Do"
	case tools.ViewToolName:
		return "View"
	case tools.WriteToolName:
		return "Write"
	default:
		return humanizedToolName(name)
	}
}
