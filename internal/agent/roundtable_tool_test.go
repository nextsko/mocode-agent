package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/package-register/mocode/internal/agent/notify"
	rtpkg "github.com/package-register/mocode/internal/agent/roundtable"
	"github.com/package-register/mocode/internal/pubsub"
)

type mockSeatRunner struct {
	responses map[string][]string
	calls     []string
	created   []string
}

func (m *mockSeatRunner) CreateSeatSession(_ context.Context, name string) (string, error) {
	m.created = append(m.created, name)
	return "seat-" + name, nil
}

func (m *mockSeatRunner) RunSeat(_ context.Context, _ string, participant rtpkg.Participant, _ rtpkg.Phase, _ rtpkg.TurnPrompt) (seatResult, error) {
	m.calls = append(m.calls, participant.Name)
	q := m.responses[participant.Name]
	if len(q) == 0 {
		return seatResult{Content: "No comment from " + participant.Name, Usage: rtpkg.Usage{Turns: 1}}, nil
	}
	content := q[0]
	m.responses[participant.Name] = q[1:]
	return seatResult{Content: content, Usage: rtpkg.Usage{Turns: 1}}, nil
}

type recordingPublisher struct {
	events []notify.Notification
}

func (r *recordingPublisher) Publish(_ pubsub.EventType, n notify.Notification) {
	r.events = append(r.events, n)
}

func newTestRoundtable(t *testing.T, topic string, participants []rtpkg.Participant, maxTurns int) *rtpkg.Roundtable {
	t.Helper()
	rt, err := rtpkg.New(rtpkg.Config{
		Topic:        topic,
		Participants: participants,
		Budget:       rtpkg.Budget{MaxTurns: maxTurns},
	})
	require.NoError(t, err)
	return rt
}

func TestRoundtableRunner_HaltsAtMaxTurns(t *testing.T) {
	t.Parallel()

	rt := newTestRoundtable(t, "test topic", []rtpkg.Participant{
		{Name: "Moderator", IsModerator: true},
		{Name: "Researcher"},
	}, 4)

	store := rtpkg.NewStore(t.TempDir())
	pub := &recordingPublisher{}
	seat := &mockSeatRunner{responses: map[string][]string{}}
	seat.responses["Moderator"] = []string{"Opening statement", "Closing statement"}
	seat.responses["Researcher"] = []string{"Research note 1", "Research note 2"}

	runner := &roundtableRunner{
		rt:                  rt,
		parentSessionID:     "parent",
		roundtableSessionID: "rt-session",
		store:               store,
		notifier:            pub,
		seatRunner:          seat,
		ctxBuilder:          rtpkg.NewContextBuilder(),
		seatSessions:        make(map[string]string),
	}

	err := runner.Run(context.Background())
	require.NoError(t, err)

	if rt.Phase != rtpkg.PhaseDone {
		t.Logf("unexpected phase %q, transcript:\n%+v", rt.Phase, rt.Transcript)
	}
	assert.Equal(t, rtpkg.PhaseDone, rt.Phase)
	assert.True(t, rt.MaxTurnsHit)
	assert.GreaterOrEqual(t, len(seat.calls), 4)
	assert.Len(t, pub.events, 4)

	// Verify persistence.
	loaded, err := store.Load(context.Background(), rt.Config.ID)
	require.NoError(t, err)
	assert.Equal(t, rt.CurrentTurn, loaded.CurrentTurn)
}

func TestRoundtableRunner_ExecutionAfterConsensus(t *testing.T) {
	t.Parallel()

	rt := newTestRoundtable(t, "plan topic", []rtpkg.Participant{
		{Name: "Moderator", IsModerator: true},
		{Name: "Researcher"},
		{Name: "Executor", CanExecute: true},
	}, 20)

	store := rtpkg.NewStore(t.TempDir())
	pub := &recordingPublisher{}
	seat := &mockSeatRunner{responses: map[string][]string{}}
	seat.responses["Moderator"] = []string{"Let's plan.", "VOTE: yes"}
	seat.responses["Researcher"] = []string{"MOTION: conclude We should adopt the plan"}
	seat.responses["Executor"] = []string{"VOTE: yes", "Executed."}

	runner := &roundtableRunner{
		rt:                  rt,
		parentSessionID:     "parent",
		roundtableSessionID: "rt-session",
		store:               store,
		notifier:            pub,
		seatRunner:          seat,
		ctxBuilder:          rtpkg.NewContextBuilder(),
		seatSessions:        make(map[string]string),
	}

	err := runner.Run(context.Background())
	require.NoError(t, err)

	assert.Equal(t, rtpkg.PhaseDone, rt.Phase)
	assert.NotNil(t, rt.Consensus)
	assert.Contains(t, rt.Consensus.Summary, "adopt the plan")
	assert.Len(t, seat.created, 3)

	turnEvents := 0
	finishEvents := 0
	for _, e := range pub.events {
		switch e.Type {
		case notify.TypeRoundtableTurn:
			turnEvents++
			assert.Equal(t, "rt-session", e.SessionID)
			assert.Equal(t, RoundtableToolName, e.ToolName)
		case notify.TypeRoundtableFinished:
			finishEvents++
		}
	}
	assert.GreaterOrEqual(t, turnEvents, 1)
	assert.Equal(t, 0, finishEvents, "runner should not publish finished; the coordinator tool does")
}

func TestRoundtableRunner_ContextCancellation(t *testing.T) {
	t.Parallel()

	rt := newTestRoundtable(t, "cancel topic", []rtpkg.Participant{
		{Name: "Moderator", IsModerator: true},
	}, 20)

	seat := &mockSeatRunner{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	runner := &roundtableRunner{
		rt:                  rt,
		parentSessionID:     "parent",
		roundtableSessionID: "rt-session",
		store:               rtpkg.NewStore(t.TempDir()),
		seatRunner:          seat,
		ctxBuilder:          rtpkg.NewContextBuilder(),
		seatSessions:        make(map[string]string),
	}

	err := runner.Run(ctx)
	require.NoError(t, err)
	assert.Equal(t, rtpkg.PhaseInterrupted, rt.Phase)
}

func TestRoundtableRunner_LoopDetection(t *testing.T) {
	t.Parallel()

	rt := newTestRoundtable(t, "loop topic", []rtpkg.Participant{
		{Name: "Moderator", IsModerator: true},
		{Name: "Researcher"},
	}, 20)

	seat := &mockSeatRunner{responses: map[string][]string{}}
	seat.responses["Moderator"] = []string{"same", "same"}
	seat.responses["Researcher"] = []string{"same", "same"}

	runner := &roundtableRunner{
		rt:                  rt,
		parentSessionID:     "parent",
		roundtableSessionID: "rt-session",
		store:               rtpkg.NewStore(t.TempDir()),
		seatRunner:          seat,
		ctxBuilder:          rtpkg.NewContextBuilder(),
		seatSessions:        make(map[string]string),
	}

	err := runner.Run(context.Background())
	require.NoError(t, err)
	assert.True(t, rt.LoopDetected)
	assert.Equal(t, rtpkg.PhaseInterrupted, rt.Phase)
}

func TestRoundtableRunner_SeatErrorContinues(t *testing.T) {
	t.Parallel()

	rt := newTestRoundtable(t, "error topic", []rtpkg.Participant{
		{Name: "Moderator", IsModerator: true},
		{Name: "Researcher"},
	}, 4)

	failingSeat := &failingSeatRunner{failName: "Researcher"}

	runner := &roundtableRunner{
		rt:                  rt,
		parentSessionID:     "parent",
		roundtableSessionID: "rt-session",
		store:               rtpkg.NewStore(t.TempDir()),
		seatRunner:          failingSeat,
		ctxBuilder:          rtpkg.NewContextBuilder(),
		seatSessions:        map[string]string{"Moderator": "seat-Moderator", "Researcher": "seat-Researcher"},
	}

	err := runner.Run(context.Background())
	require.NoError(t, err)
	assert.Equal(t, rtpkg.PhaseDone, rt.Phase)
	assert.Len(t, rt.Transcript, 4)

	systemCount := 0
	for _, s := range rt.Transcript {
		if s.Kind == rtpkg.StatementSystem {
			systemCount++
		}
	}
	assert.GreaterOrEqual(t, systemCount, 1)
}

func TestRoundtableRunner_Resume(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := rtpkg.NewStore(dir)

	rt := newTestRoundtable(t, "resume topic", []rtpkg.Participant{
		{Name: "Moderator", IsModerator: true},
		{Name: "Researcher"},
	}, 6)
	rt.AddStatement(rtpkg.Statement{Speaker: "Moderator", Kind: rtpkg.StatementChat, Content: "hello"})
	require.NoError(t, store.Save(context.Background(), rt))

	loaded, err := store.Load(context.Background(), rt.Config.ID)
	require.NoError(t, err)

	seat := &mockSeatRunner{responses: map[string][]string{}}
	seat.responses["Moderator"] = []string{"Continuing...", "Wrapping up."}
	seat.responses["Researcher"] = []string{"Agreed.", "Agreed again."}

	runner := &roundtableRunner{
		rt:                  loaded,
		parentSessionID:     "parent",
		roundtableSessionID: "rt-session",
		store:               store,
		seatRunner:          seat,
		ctxBuilder:          rtpkg.NewContextBuilder(),
		seatSessions:        make(map[string]string),
	}

	err = runner.Run(context.Background())
	require.NoError(t, err)
	assert.Equal(t, rtpkg.PhaseDone, loaded.Phase)
	assert.True(t, loaded.CurrentTurn >= 3)
}

func TestConfigFromRoundtableParams_Defaults(t *testing.T) {
	t.Parallel()
	cfg, err := configFromRoundtableParams(RoundtableParams{Topic: "hello"})
	require.NoError(t, err)
	assert.Equal(t, "hello", cfg.Topic)
	assert.Len(t, cfg.Participants, 4)
	assert.True(t, cfg.Participants[0].IsModerator)
	assert.True(t, cfg.Participants[3].CanExecute)
	assert.Equal(t, 20, cfg.Budget.MaxTurns)
}

func TestConfigFromRoundtableParams_CustomParticipants(t *testing.T) {
	t.Parallel()
	cfg, err := configFromRoundtableParams(RoundtableParams{
		Topic: "custom",
		Participants: []RoundtableParticipantInput{
			{Name: "Lead", AgentID: "coder", Role: "moderator"},
			{Name: "Worker", AgentID: "task"},
		},
		MaxTurns: 10,
	})
	require.NoError(t, err)
	assert.Len(t, cfg.Participants, 2)
	assert.True(t, cfg.Participants[0].IsModerator)
	assert.Equal(t, 10, cfg.Budget.MaxTurns)
}

func TestParseSeatStatement(t *testing.T) {
	t.Parallel()
	rt := newTestRoundtable(t, "parse", []rtpkg.Participant{
		{Name: "Moderator", IsModerator: true},
	}, 10)

	t.Run("chat", func(t *testing.T) {
		kind, content, motion, vote, err := parseSeatStatement(rt, "Hello everyone")
		require.NoError(t, err)
		assert.Equal(t, rtpkg.StatementChat, kind)
		assert.Equal(t, "Hello everyone", content)
		assert.Nil(t, motion)
		assert.Nil(t, vote)
	})

	t.Run("motion", func(t *testing.T) {
		kind, content, motion, vote, err := parseSeatStatement(rt, "MOTION: conclude We should finish | {\"foo\":\"bar\"}")
		require.NoError(t, err)
		assert.Equal(t, rtpkg.StatementMotion, kind)
		assert.Equal(t, "We should finish", content)
		require.NotNil(t, motion)
		assert.Equal(t, rtpkg.MotionConclude, motion.Type)
		assert.Equal(t, "bar", motion.Payload["foo"])
		assert.Nil(t, vote)
	})

	t.Run("motion without vote fails", func(t *testing.T) {
		rt2 := newTestRoundtable(t, "parse2", []rtpkg.Participant{
			{Name: "Moderator", IsModerator: true},
		}, 10)
		_, _, _, _, err := parseSeatStatement(rt2, "VOTE: yes")
		require.Error(t, err)
	})

	t.Run("motion after explanatory text", func(t *testing.T) {
		raw := "Let me wrap up.\n\nMOTION: conclude We are done here."
		kind, content, motion, _, err := parseSeatStatement(rt, raw)
		require.NoError(t, err)
		assert.Equal(t, rtpkg.StatementMotion, kind)
		assert.Equal(t, "We are done here.", content)
		require.NotNil(t, motion)
		assert.Equal(t, rtpkg.MotionConclude, motion.Type)
	})

	t.Run("motion wrapped in markdown bold", func(t *testing.T) {
		raw := "**MOTION: conclude End the meeting.**"
		kind, content, motion, _, err := parseSeatStatement(rt, raw)
		require.NoError(t, err)
		assert.Equal(t, rtpkg.StatementMotion, kind)
		assert.Equal(t, "End the meeting.", content)
		require.NotNil(t, motion)
		assert.Equal(t, rtpkg.MotionConclude, motion.Type)
	})

	t.Run("vote after explanatory text", func(t *testing.T) {
		// Add a motion to the transcript so the vote has a target.
		rt4 := newTestRoundtable(t, "parse4", []rtpkg.Participant{
			{Name: "Moderator", IsModerator: true},
		}, 10)
		rt4.AddStatement(rtpkg.Statement{
			Speaker: "Moderator",
			Kind:    rtpkg.StatementMotion,
			Motion:  &rtpkg.Motion{Type: rtpkg.MotionConclude},
			Content: "conclude test",
			Seq:     1,
		})
		raw := "I agree with the plan.\n\nVOTE: yes Great idea."
		kind, _, _, vote, err := parseSeatStatement(rt4, raw)
		require.NoError(t, err)
		assert.Equal(t, rtpkg.StatementVote, kind)
		require.NotNil(t, vote)
		assert.Equal(t, rtpkg.VoteYes, vote.Value)
		assert.Equal(t, "Great idea.", vote.Reason)
	})

	t.Run("vote embedded in markdown bold", func(t *testing.T) {
		rt3 := newTestRoundtable(t, "parse3", []rtpkg.Participant{
			{Name: "Moderator", IsModerator: true},
			{Name: "Researcher"},
		}, 10)
		// First create a motion.
		rt3.AddStatement(rtpkg.Statement{
			Speaker: "Moderator",
			Kind:    rtpkg.StatementMotion,
			Motion:  &rtpkg.Motion{Type: rtpkg.MotionConclude},
			Content: "test conclusion",
			Seq:     1,
		})
		raw := "同意提案。\n\n**VOTE: yes**"
		kind, _, _, vote, err := parseSeatStatement(rt3, raw)
		require.NoError(t, err)
		assert.Equal(t, rtpkg.StatementVote, kind)
		require.NotNil(t, vote)
		assert.Equal(t, rtpkg.VoteYes, vote.Value)
	})

	t.Run("chat with no markers stays chat", func(t *testing.T) {
		raw := "I think we should discuss the architecture.\nLet me elaborate."
		kind, _, _, _, err := parseSeatStatement(rt, raw)
		require.NoError(t, err)
		assert.Equal(t, rtpkg.StatementChat, kind)
	})
}

func TestRoundtableResultAsText(t *testing.T) {
	t.Parallel()
	rt := newTestRoundtable(t, "result", []rtpkg.Participant{
		{Name: "Moderator", IsModerator: true},
	}, 10)
	rt.AddStatement(rtpkg.Statement{Speaker: "Moderator", Kind: rtpkg.StatementChat, Content: "hello"})
	rt.Consensus = &rtpkg.Consensus{Summary: "We agreed."}

	text := roundtableResultAsText(rt)
	assert.Contains(t, text, "result")
	assert.Contains(t, text, "We agreed.")
	assert.Contains(t, text, "Moderator")
}

type failingSeatRunner struct {
	failName string
}

func (f *failingSeatRunner) CreateSeatSession(_ context.Context, name string) (string, error) {
	return "seat-" + name, nil
}

func (f *failingSeatRunner) RunSeat(_ context.Context, _ string, participant rtpkg.Participant, _ rtpkg.Phase, _ rtpkg.TurnPrompt) (seatResult, error) {
	if participant.Name == f.failName {
		return seatResult{}, errors.New("simulated seat failure")
	}
	return seatResult{Content: "ok", Usage: rtpkg.Usage{Turns: 1}}, nil
}
