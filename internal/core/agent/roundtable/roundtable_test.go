package roundtable

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNew_RequiresModeratorAndTopic(t *testing.T) {
	t.Parallel()

	_, err := New(Config{Topic: "x"})
	require.Error(t, err)

	_, err = New(Config{Participants: []Participant{{Name: "a", IsModerator: true}}})
	require.Error(t, err)

	rt, err := New(Config{
		Topic:        "design retry backoff",
		Participants: []Participant{{Name: "mod", IsModerator: true}, {Name: "coder"}},
	})
	require.NoError(t, err)
	require.Equal(t, PhaseOpening, rt.Phase)
	require.Equal(t, 20, rt.Config.Budget.MaxTurns)
}

func TestRoundRobinStrategy(t *testing.T) {
	t.Parallel()
	rt, err := New(Config{
		Topic: "x",
		Participants: []Participant{
			{Name: "mod", IsModerator: true},
			{Name: "architect"},
			{Name: "coder"},
		},
	})
	require.NoError(t, err)

	sel := RoundRobinStrategy{}
	speakers := []string{}
	for i := 0; i < 7; i++ {
		name, err := rt.NextSpeaker(sel)
		require.NoError(t, err)
		speakers = append(speakers, name)
		rt.CurrentTurn++
	}
	require.Equal(t, []string{"mod", "architect", "coder", "mod", "architect", "coder", "mod"}, speakers)
}

func TestAdvance_ReachesConsensus(t *testing.T) {
	t.Parallel()
	rt, err := New(Config{
		Topic: "x",
		Participants: []Participant{
			{Name: "mod", IsModerator: true},
			{Name: "architect"},
			{Name: "coder"},
		},
	})
	require.NoError(t, err)

	_, err = rt.Advance("mod", StatementMotion, "let's use exponential backoff", &Motion{Type: MotionProposePlan, Payload: map[string]any{"plan": "exp backoff"}}, nil, Usage{})
	require.NoError(t, err)
	require.Equal(t, PhaseVoting, rt.Phase)

	_, err = rt.Advance("architect", StatementVote, "", nil, &Vote{MotionSeq: 1, Value: VoteYes}, Usage{})
	require.NoError(t, err)
	require.Nil(t, rt.Consensus)

	_, err = rt.Advance("coder", StatementVote, "", nil, &Vote{MotionSeq: 1, Value: VoteYes}, Usage{})
	require.NoError(t, err)
	require.NotNil(t, rt.Consensus)
	require.Equal(t, PhaseApproved, rt.Phase)
	require.Equal(t, "exp backoff", rt.Consensus.Plan["plan"])
}

func TestDetectLoop_RepeatedContent(t *testing.T) {
	t.Parallel()
	rt, _ := New(Config{Topic: "x", Participants: []Participant{{Name: "mod", IsModerator: true}, {Name: "a"}}})
	for i := 0; i < 4; i++ {
		rt.AddStatement(Statement{Speaker: "a", Kind: StatementChat, Content: "same"})
	}
	ok, reason := rt.DetectLoop(4)
	require.True(t, ok)
	require.Contains(t, reason, "repeated")
}

func TestDetectLoop_CyclingSpeakers(t *testing.T) {
	t.Parallel()
	rt, _ := New(Config{Topic: "x", Participants: []Participant{{Name: "mod", IsModerator: true}, {Name: "a"}, {Name: "b"}}})
	for i := 0; i < 4; i++ {
		rt.AddStatement(Statement{Speaker: "a", Kind: StatementChat, Content: "same"})
		rt.AddStatement(Statement{Speaker: "b", Kind: StatementChat, Content: "same"})
	}
	ok, reason := rt.DetectLoop(4)
	require.True(t, ok)
	require.Contains(t, reason, "cycling")
}

func TestIsOverBudget(t *testing.T) {
	t.Parallel()
	rt, _ := New(Config{
		Topic:        "x",
		Participants: []Participant{{Name: "mod", IsModerator: true}},
		Budget:       Budget{MaxTotalTokens: 100},
	})
	require.False(t, func() bool { ok, _ := rt.IsOverBudget(); return ok }())
	rt.applyUsage("mod", Usage{TotalTokens: 100})
	ok, reason := rt.IsOverBudget()
	require.True(t, ok)
	require.Contains(t, reason, "total token budget")
}

func TestAdvance_RespectsMaxTurns(t *testing.T) {
	t.Parallel()
	rt, _ := New(Config{
		Topic:        "x",
		Participants: []Participant{{Name: "mod", IsModerator: true}},
		Budget:       Budget{MaxTurns: 2},
	})
	_, err := rt.Advance("mod", StatementChat, "1", nil, nil, Usage{})
	require.NoError(t, err)
	_, err = rt.Advance("mod", StatementChat, "2", nil, nil, Usage{})
	require.NoError(t, err)
	_, err = rt.Advance("mod", StatementChat, "3", nil, nil, Usage{})
	require.Error(t, err)
	require.True(t, rt.MaxTurnsHit)
}
