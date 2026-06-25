package model

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/package-register/mocode/internal/domain/session/message"
	"github.com/package-register/mocode/internal/domain/session/sessionexport"
)

func TestExportExtension(t *testing.T) {
	t.Parallel()

	require.Equal(t, "md", sessionexport.Extension("markdown"))
	require.Equal(t, "md", sessionexport.Extension("obsidian-md"))
	require.Equal(t, "html", sessionexport.Extension("html"))
	require.Empty(t, sessionexport.Extension("pdf"))
}

func TestRenderSessionMarkdown(t *testing.T) {
	t.Parallel()

	content := sessionexport.RenderMarkdown("session/one", []message.Message{
		{
			Role: message.User,
			Parts: []message.ContentPart{
				message.TextContent{Text: "hello"},
			},
		},
		{
			Role: message.Assistant,
			Parts: []message.ContentPart{
				message.TextContent{Text: "world"},
				message.ToolCall{Name: "read_file", Input: `{"path":"a.go"}`},
			},
		},
	}, time.Unix(0, 0))

	require.Contains(t, content, "type: skcode-session-export")
	require.Contains(t, content, "session_id: \"session/one\"")
	require.Contains(t, content, "## User")
	require.Contains(t, content, "hello")
	require.Contains(t, content, "## Assistant")
	require.Contains(t, content, "### Tool Call: read_file")
}

func TestRenderSessionHTML(t *testing.T) {
	t.Parallel()

	content := sessionexport.RenderHTML("s", []message.Message{{
		Role: message.User,
		Parts: []message.ContentPart{
			message.TextContent{Text: "<script>alert(1)</script>"},
		},
	}}, time.Unix(0, 0))

	require.True(t, strings.HasPrefix(content, "<!doctype html>"))
	require.Contains(t, content, "&lt;script&gt;alert(1)&lt;/script&gt;")
	require.NotContains(t, content, "<script>alert(1)</script>")
}
