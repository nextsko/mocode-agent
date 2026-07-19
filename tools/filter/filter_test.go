package filter_test

import (
	"context"
	"encoding/json"
	"testing"

	"charm.land/fantasy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/package-register/mocode/tools/filter"
)

// ─── helpers ──────────────────────────────────────────────────────────────────

// stubTool creates a minimal fantasy.AgentTool whose Info().Name returns name.
func stubTool(name string) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		name,
		"stub "+name,
		func(_ context.Context, _ struct{}, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
			return fantasy.NewTextResponse("ok"), nil
		},
	)
}

func toolNames(tools []fantasy.AgentTool) []string {
	names := make([]string, len(tools))
	for i, t := range tools {
		names[i] = t.Info().Name
	}
	return names
}

var _ = json.Marshal // keep import

// ─── Apply ────────────────────────────────────────────────────────────────────

func TestApply_NoFilters_ReturnsAll(t *testing.T) {
	t.Parallel()
	tools := []fantasy.AgentTool{stubTool("a"), stubTool("b"), stubTool("c")}
	got := filter.Apply(context.Background(), tools)
	require.Equal(t, tools, got)
}

func TestApply_EmptyInput(t *testing.T) {
	t.Parallel()
	got := filter.Apply(context.Background(), nil, filter.IncludeNames("a"))
	assert.Empty(t, got)
}

// ─── IncludeNames ─────────────────────────────────────────────────────────────

func TestIncludeNames_KeepsMatchingTools(t *testing.T) {
	t.Parallel()
	tools := []fantasy.AgentTool{stubTool("bash"), stubTool("view"), stubTool("fetch")}
	got := filter.Apply(context.Background(), tools, filter.IncludeNames("bash", "view"))
	assert.Equal(t, []string{"bash", "view"}, toolNames(got))
}

func TestIncludeNames_NoneMatch_ReturnsEmpty(t *testing.T) {
	t.Parallel()
	tools := []fantasy.AgentTool{stubTool("bash"), stubTool("view")}
	got := filter.Apply(context.Background(), tools, filter.IncludeNames("fetch"))
	assert.Empty(t, got)
}

func TestIncludeNames_AllMatch(t *testing.T) {
	t.Parallel()
	tools := []fantasy.AgentTool{stubTool("a"), stubTool("b")}
	got := filter.Apply(context.Background(), tools, filter.IncludeNames("a", "b"))
	assert.Equal(t, []string{"a", "b"}, toolNames(got))
}

// ─── ExcludeNames ─────────────────────────────────────────────────────────────

func TestExcludeNames_BlocksMatchingTools(t *testing.T) {
	t.Parallel()
	tools := []fantasy.AgentTool{stubTool("bash"), stubTool("view"), stubTool("fetch")}
	got := filter.Apply(context.Background(), tools, filter.ExcludeNames("fetch"))
	assert.Equal(t, []string{"bash", "view"}, toolNames(got))
}

func TestExcludeNames_NoneBlocked_ReturnsAll(t *testing.T) {
	t.Parallel()
	tools := []fantasy.AgentTool{stubTool("bash"), stubTool("view")}
	got := filter.Apply(context.Background(), tools, filter.ExcludeNames("fetch"))
	assert.Equal(t, []string{"bash", "view"}, toolNames(got))
}

func TestExcludeNames_AllBlocked_ReturnsEmpty(t *testing.T) {
	t.Parallel()
	tools := []fantasy.AgentTool{stubTool("a"), stubTool("b")}
	got := filter.Apply(context.Background(), tools, filter.ExcludeNames("a", "b"))
	assert.Empty(t, got)
}

// ─── Chain ────────────────────────────────────────────────────────────────────

func TestChain_AndSemantics_BothMustPass(t *testing.T) {
	t.Parallel()
	tools := []fantasy.AgentTool{stubTool("bash"), stubTool("view"), stubTool("fetch")}
	// include only bash and view, then exclude view → only bash should remain
	chained := filter.Chain(
		filter.IncludeNames("bash", "view"),
		filter.ExcludeNames("view"),
	)
	got := filter.Apply(context.Background(), tools, chained)
	assert.Equal(t, []string{"bash"}, toolNames(got))
}

func TestChain_Empty_PassesAll(t *testing.T) {
	t.Parallel()
	tools := []fantasy.AgentTool{stubTool("a"), stubTool("b")}
	chained := filter.Chain()
	got := filter.Apply(context.Background(), tools, chained)
	assert.Equal(t, []string{"a", "b"}, toolNames(got))
}

// ─── Context propagation ──────────────────────────────────────────────────────

func TestFilterFunc_ReceivesContext(t *testing.T) {
	t.Parallel()
	type ctxKey string
	const key ctxKey = "role"

	tools := []fantasy.AgentTool{stubTool("bash"), stubTool("fetch")}

	// Dynamic filter: only allow fetch when role==network
	dynamicFilter := filter.FilterFunc(func(ctx context.Context, tool fantasy.AgentTool) bool {
		role, _ := ctx.Value(key).(string)
		if tool.Info().Name == "fetch" {
			return role == "network"
		}
		return true
	})

	ctxDefault := context.Background()
	ctxNetwork := context.WithValue(context.Background(), key, "network")

	gotDefault := filter.Apply(ctxDefault, tools, dynamicFilter)
	gotNetwork := filter.Apply(ctxNetwork, tools, dynamicFilter)

	assert.Equal(t, []string{"bash"}, toolNames(gotDefault))
	assert.Equal(t, []string{"bash", "fetch"}, toolNames(gotNetwork))
}

// ─── Idempotency ──────────────────────────────────────────────────────────────

func TestIncludeNames_Idempotent(t *testing.T) {
	t.Parallel()
	tools := []fantasy.AgentTool{stubTool("bash"), stubTool("view")}
	f := filter.IncludeNames("bash", "view")
	ctx := context.Background()
	first := filter.Apply(ctx, tools, f)
	second := filter.Apply(ctx, tools, f)
	assert.Equal(t, toolNames(first), toolNames(second))
}
