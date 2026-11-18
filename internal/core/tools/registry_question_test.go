package tools

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nextsko/mocode-agent/internal/core/question"
	agentquestion "github.com/nextsko/mocode-agent/internal/core/tools/core/question"
)

// TestQuestionPlugin_RequiresInteractive is the regression test for the
// hang: in a transport where nobody can answer (mocode run, MCP serve, ACP,
// the WeChat gateway, every sub-agent) the question tool must not be
// registered at all, or the model blocks forever on an unanswered form.
func TestQuestionPlugin_RequiresInteractive(t *testing.T) {
	t.Parallel()

	svc := question.NewService()
	plugin := questionPlugin{}

	require.Empty(t, plugin.Build(context.Background(), ToolDeps{Questions: svc}),
		"a transport without a question service must not get the question tool")
	require.Empty(t, plugin.Build(context.Background(), ToolDeps{Questions: svc, Interactive: false}),
		"a non-interactive transport must not get the question tool")

	got := plugin.Build(context.Background(), ToolDeps{Questions: svc, Interactive: true})
	require.Len(t, got, 1)
	require.Equal(t, agentquestion.QuestionToolName, got[0].Info().Name)
}
