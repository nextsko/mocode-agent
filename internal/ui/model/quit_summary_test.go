package model

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/package-register/mocode/internal/domain/session"
	"github.com/package-register/mocode/internal/domain/session/message"
	"github.com/package-register/mocode/internal/ui/common"
	"github.com/package-register/mocode/internal/ui/styles"
)

func TestQuitMessageStats(t *testing.T) {
	t.Parallel()

	t.Run("empty messages", func(t *testing.T) {
		t.Parallel()
		users, assistants, tools := quitMessageStats(nil)
		require.Equal(t, 0, users)
		require.Equal(t, 0, assistants)
		require.Equal(t, 0, tools)
	})

	t.Run("counts by role", func(t *testing.T) {
		t.Parallel()
		msgs := []message.Message{
			{Role: message.User},
			{Role: message.User},
			{Role: message.Assistant},
			{Role: message.Assistant},
			{Role: message.Assistant},
			{Role: message.Tool},
		}
		users, assistants, tools := quitMessageStats(msgs)
		require.Equal(t, 2, users)
		require.Equal(t, 3, assistants)
		require.Equal(t, 1, tools)
	})
}

func TestQuitFileStats(t *testing.T) {
	t.Parallel()

	t.Run("empty files", func(t *testing.T) {
		t.Parallel()
		changed, additions, deletions := quitFileStats(nil)
		require.Equal(t, 0, changed)
		require.Equal(t, 0, additions)
		require.Equal(t, 0, deletions)
	})

	t.Run("sums correctly", func(t *testing.T) {
		t.Parallel()
		files := []SessionFile{
			{Additions: 10, Deletions: 3},
			{Additions: 5, Deletions: 7},
			{Additions: 0, Deletions: 0},
		}
		changed, additions, deletions := quitFileStats(files)
		require.Equal(t, 3, changed)
		require.Equal(t, 15, additions)
		require.Equal(t, 10, deletions)
	})
}

func TestQuitSessionPath(t *testing.T) {
	t.Parallel()

	p := quitSessionPath(session.Session{ID: "abc-123"})
	// The path now uses the global data directory with the session's hash ID
	require.Contains(t, p, "sessions")
	require.Contains(t, p, "history")
	require.Contains(t, p, session.HashID("abc-123"))
}

func TestRenderQuitSummary(t *testing.T) {
	t.Parallel()

	summary := quitSummary{
		Status:       "completed",
		Duration:     15 * time.Minute,
		Agent:        "coder",
		Model:        "anthropic / claude",
		CWD:          "/tmp/work",
		UserMessages: 3,
		AIMessages:   5,
		ToolMessages: 7,
		PromptTokens: 1500,
		OutputTokens: 300,
		FilesChanged: 2,
		Additions:    20,
		Deletions:    10,
		SessionID:    "session-abc-123",
		AppName:      "TestCode",
	}

	out := renderQuitSummary(summary, styles.QuitSummary{})

	// Core data
	require.Contains(t, out, "session summary")
	require.Contains(t, out, "completed")
	require.Contains(t, out, "15m0s")
	require.Contains(t, out, "coder")
	require.Contains(t, out, "anthropic / claude")
	require.Contains(t, out, "/tmp/work")
	require.Contains(t, out, "user 3 / assistant 5 / tool 7")
	require.Contains(t, out, "prompt 1500 / completion 300 / total 1800")
	require.Contains(t, out, "2 changed / +20 -10")
	require.Contains(t, out, "session-abc-123")
	require.Contains(t, out, "--session")

	// New visual elements
	require.Contains(t, out, "── Session ──")
	require.Contains(t, out, "── Statistics ──")
	require.Contains(t, out, "── Info ──")
}

func TestQuitSummaryMethod(t *testing.T) {
	t.Parallel()

	t.Run("returns empty when no pending summary", func(t *testing.T) {
		t.Parallel()
		ui := &UI{}
		require.Equal(t, "", ui.QuitSummary())
	})

	t.Run("returns rendered summary when set", func(t *testing.T) {
		t.Parallel()
		com := common.DefaultCommon(nil)
		ui := &UI{
			com: com,
			pendingQuitSummary: &quitSummary{
				Status:    "interrupted",
				Duration:  2 * time.Second,
				Agent:     "task",
				Model:     "openai / gpt-4",
				CWD:       "/tmp/test",
				SessionID: "sid",
				AppName:   "TestApp",
			},
		}
		require.NotNil(t, ui.pendingQuitSummary)
		out := ui.QuitSummary()
		require.NotEmpty(t, out)
		require.Contains(t, out, "session summary")
		require.Contains(t, out, "interrupted")
		require.Contains(t, out, "task")
	})
}

func TestRenderQuitSummaryMinimal(t *testing.T) {
	t.Parallel()

	summary := quitSummary{
		Status:       "interrupted",
		Duration:     3 * time.Second,
		Agent:        "task",
		Model:        "openai / gpt-4",
		CWD:          "/tmp",
		AppName:      "TestApp",
		UserMessages: 1,
		AIMessages:   0,
		ToolMessages: 0,
		PromptTokens: 100,
		OutputTokens: 50,
		FilesChanged: 0,
		Additions:    0,
		Deletions:    0,
		SessionID:    "sid",
	}

	out := renderQuitSummary(summary, styles.QuitSummary{})

	require.Contains(t, out, "session summary")
	require.Contains(t, out, "interrupted")
	require.Contains(t, out, "--session")
}
