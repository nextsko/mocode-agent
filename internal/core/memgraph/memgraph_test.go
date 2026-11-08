package memgraph

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpsertMergeSupersedeAndNeighbors(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	s, err := Open(dir)
	require.NoError(t, err)

	u, err := s.UpsertNode(Node{Kind: KindUser, Name: "owner", Props: map[string]any{"tone": "direct"}, Confidence: 0.7})
	require.NoError(t, err)
	p1, err := s.UpsertNode(Node{Kind: KindPreference, Name: "dark mode", Confidence: 0.6})
	require.NoError(t, err)
	p2, err := s.UpsertNode(Node{Kind: KindPreference, Name: "light mode", Confidence: 0.6})
	require.NoError(t, err)

	_, err = s.UpsertEdge(Edge{From: u.ID, To: p1.ID, Kind: EdgePrefers, Confidence: 0.8})
	require.NoError(t, err)
	_, err = s.UpsertEdge(Edge{From: u.ID, To: p2.ID, Kind: EdgePrefers, Confidence: 0.7})
	require.NoError(t, err)

	nodes, edges := s.Neighbors(u.ID, 1)
	assert.Len(t, edges, 2)
	assert.Len(t, nodes, 2)

	require.NoError(t, s.Supersede(p2.ID, p1.ID))
	old, ok := s.Get(p2.ID)
	require.True(t, ok)
	assert.Equal(t, p1.ID, old.SupersededBy)
	assert.NotNil(t, old.ValidTo)

	conflicts := s.Conflicts()
	require.Len(t, conflicts, 1)
	assert.Equal(t, "owner", conflicts[0].Subject)
}

func TestExplainTracesEvidence(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	s, err := Open(dir)
	require.NoError(t, err)

	pref, err := s.UpsertNode(Node{Kind: KindPreference, Name: "tabs over spaces"})
	require.NoError(t, err)
	ev, err := s.UpsertNode(Node{
		Kind: KindEvent, Name: "user said tabs",
		Sources: []Source{{Kind: "message", Ref: "m42", Quote: "I prefer tabs", At: time.Now().UTC()}},
	})
	require.NoError(t, err)
	_, err = s.UpsertEdge(Edge{From: pref.ID, To: ev.ID, Kind: EdgeEvidencedBy, Confidence: 0.9})
	require.NoError(t, err)

	chains := s.Explain(pref.ID)
	require.NotEmpty(t, chains)
	assert.Contains(t, chains[0].Path, "user said tabs")
	assert.NotEmpty(t, chains[0].Sources)
	assert.Contains(t, RenderExplain(pref.ID, chains), "为什么我相信")
}

func TestPersonaCardAndExport(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	s, err := Open(dir)
	require.NoError(t, err)

	persona, err := s.UpsertNode(Node{Kind: KindPersona, Name: "pragmatic engineer", Props: map[string]any{"traits": []any{"直接", "先验证再下结论"}}})
	require.NoError(t, err)
	user, err := s.UpsertNode(Node{Kind: KindUser, Name: "nextsko"})
	require.NoError(t, err)
	pref, err := s.UpsertNode(Node{Kind: KindPreference, Name: "结构化归档"})
	require.NoError(t, err)
	goal, err := s.UpsertNode(Node{Kind: KindGoal, Name: "发布可用的 mocode"})
	require.NoError(t, err)
	_, err = s.UpsertEdge(Edge{From: user.ID, To: pref.ID, Kind: EdgePrefers})
	require.NoError(t, err)
	_, err = s.UpsertEdge(Edge{From: user.ID, To: goal.ID, Kind: EdgeKnows})
	require.NoError(t, err)
	_, err = s.UpsertEdge(Edge{From: persona.ID, To: user.ID, Kind: EdgeRelatesTo})
	require.NoError(t, err)

	card := s.BuildCard()
	assert.Equal(t, "pragmatic engineer", card.Persona)
	assert.Equal(t, "nextsko", card.User)
	assert.Contains(t, card.Prefs, "结构化归档")
	assert.Contains(t, card.Goals, "发布可用的 mocode")
	rendered := RenderCard(card)
	assert.Contains(t, rendered, "AI 人格")
	assert.Contains(t, rendered, "用户画像")
	assert.NotEmpty(t, PromptCard(card, 200))

	exportDir, err := s.ExportBundle(dir)
	require.NoError(t, err)
	for _, name := range []string{"graph.json", "graph.mmd", "graph.html"} {
		_, statErr := os.Stat(filepath.Join(exportDir, name))
		require.NoError(t, statErr, name)
	}
	assert.Contains(t, RenderMermaid(s.Graph()), "graph LR")
}

func TestForgetAndDecay(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	s, err := Open(dir)
	require.NoError(t, err)
	n, err := s.UpsertNode(Node{Kind: KindFact, Name: "temp"})
	require.NoError(t, err)
	removed, err := s.Forget(n.ID)
	require.NoError(t, err)
	assert.Equal(t, 1, removed)

	old, err := s.UpsertNode(Node{Kind: KindFact, Name: "stale", Confidence: 0.9})
	require.NoError(t, err)
	// Age it artificially so decay has work to do.
	g := s.Graph()
	aged := g.Nodes[old.ID]
	aged.UpdatedAt = time.Now().Add(-30 * 24 * time.Hour)
	s.g.Nodes[old.ID] = aged
	changed, err := s.Decay(7*24*time.Hour, 0.1)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, changed, 1)
}

func TestExplainDoesNotCrossLinkDuplicateNames(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	s, err := Open(dir)
	require.NoError(t, err)

	// Two different beliefs share the same display name; each must resolve to
	// its own evidence instead of whichever node the map iterates first.
	a, err := s.UpsertNode(Node{ID: "pref-a", Kind: KindPreference, Name: "theme"})
	require.NoError(t, err)
	b, err := s.UpsertNode(Node{ID: "pref-b", Kind: KindPreference, Name: "theme"})
	require.NoError(t, err)
	ea, err := s.UpsertNode(Node{Kind: KindEvent, Name: "said dark", Sources: []Source{{Kind: "message", Ref: "m1", Quote: "dark please", At: time.Now().UTC()}}})
	require.NoError(t, err)
	eb, err := s.UpsertNode(Node{Kind: KindEvent, Name: "said light", Sources: []Source{{Kind: "message", Ref: "m2", Quote: "light please", At: time.Now().UTC()}}})
	require.NoError(t, err)
	_, err = s.UpsertEdge(Edge{From: a.ID, To: ea.ID, Kind: EdgeEvidencedBy})
	require.NoError(t, err)
	_, err = s.UpsertEdge(Edge{From: b.ID, To: eb.ID, Kind: EdgeEvidencedBy})
	require.NoError(t, err)

	ca := s.Explain(a.ID)
	cb := s.Explain(b.ID)
	require.NotEmpty(t, ca)
	require.NotEmpty(t, cb)
	assert.Equal(t, []string{"theme", "said dark"}, ca[0].Path)
	assert.Equal(t, []string{"theme", "said light"}, cb[0].Path)
	assert.Equal(t, "dark please", ca[0].Sources[0].Quote)
	assert.Equal(t, "light please", cb[0].Sources[0].Quote)
}
