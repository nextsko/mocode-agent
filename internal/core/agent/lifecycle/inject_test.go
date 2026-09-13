package lifecycle

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nextsko/mocode-agent/internal/domain/session/message"
)

// Inject persists the guidance as a user message and buffers it for the
// running turn's next model step.
func TestInject_PersistsAndBuffers(t *testing.T) {
	large := &scriptedModel{script: []scriptedResponse{{until: make(chan struct{})}}}
	small := &scriptedModel{script: []scriptedResponse{{text: "Title"}}}
	agent, sessions, messages := newLifecycleTestAgent(t, large, small, nil, nil)

	ctx := t.Context()
	sess, err := sessions.Create(ctx, "inject")
	require.NoError(t, err)

	require.NoError(t, agent.Inject(ctx, sess.ID, "prefer option B"))

	// Persisted: a user message with the guidance text exists.
	found := false
	msgs, err := messages.List(ctx, sess.ID)
	require.NoError(t, err)
	for _, m := range msgs {
		if m.Role == message.User && strings.Contains(messageText(m), "prefer option B") {
			found = true
		}
	}
	require.True(t, found, "guidance must be persisted as a user message")

	// Buffered: the drain returns it exactly once.
	drained := agent.drainInjected(sess.ID)
	require.Equal(t, []string{"prefer option B"}, drained)
	require.Empty(t, agent.drainInjected(sess.ID), "drain must clear the buffer")
}

// Inject validation.
func TestInject_Validation(t *testing.T) {
	large := &scriptedModel{script: []scriptedResponse{{text: "ok"}}}
	small := &scriptedModel{script: []scriptedResponse{{text: "T"}}}
	agent, _, _ := newLifecycleTestAgent(t, large, small, nil, nil)

	require.ErrorIs(t, agent.Inject(t.Context(), "", "x"), ErrSessionMissing)
	require.ErrorIs(t, agent.Inject(t.Context(), "s1", ""), ErrEmptyPrompt)
}

// ForceRun on an idle session behaves like a plain Run.
func TestForceRun_IdleRunsDirectly(t *testing.T) {
	large := &scriptedModel{script: []scriptedResponse{{text: "did A"}}}
	small := &scriptedModel{script: []scriptedResponse{{text: "Title"}}}
	agent, sessions, _ := newLifecycleTestAgent(t, large, small, nil, nil)

	ctx := t.Context()
	sess, err := sessions.Create(ctx, "force-idle")
	require.NoError(t, err)

	_, err = agent.ForceRun(ctx, SessionAgentCall{SessionID: sess.ID, Prompt: "task A"})
	require.NoError(t, err)
	require.NotEmpty(t, large.callsSnapshot(), "forced call must run")
}

// ForceRun on a busy session interrupts the running turn and dispatch
// continues into the forced call instead of stopping the session (unlike a
// plain Cancel, which stops everything).
func TestForceRun_BusyPreemptsAndContinues(t *testing.T) {
	release := make(chan struct{})
	large := &scriptedModel{script: []scriptedResponse{
		{until: release},     // task A: blocks until force-kicked
		{text: "did FORCED"}, // the forced call
	}}
	small := &scriptedModel{script: []scriptedResponse{{text: "Title"}}}
	agent, sessions, _ := newLifecycleTestAgent(t, large, small, nil, nil)

	ctx := t.Context()
	sess, err := sessions.Create(ctx, "force-busy")
	require.NoError(t, err)

	done := make(chan struct{})
	go func() {
		defer close(done)
		res, err := agent.Run(ctx, SessionAgentCall{SessionID: sess.ID, Prompt: "task A"})
		// The interrupted dispatcher hands over to the forced call and the
		// original Run returns cancelled-but-nil-error (force kick path).
		_ = res
		_ = err
	}()

	require.Eventually(t, func() bool { return len(large.callsSnapshot()) >= 1 },
		5*time.Second, 10*time.Millisecond)

	_, err = agent.ForceRun(ctx, SessionAgentCall{SessionID: sess.ID, Prompt: "URGENT task"})
	require.NoError(t, err)

	// The forced turn must actually execute (dispatch continued after kick).
	require.Eventually(t, func() bool {
		calls := large.callsSnapshot()
		return len(calls) >= 2 && strings.Contains(promptText(calls[len(calls)-1].Prompt), "URGENT")
	}, 5*time.Second, 10*time.Millisecond, "forced call must run after preemption")

	close(release)
	<-done
	assert.False(t, agent.IsSessionBusy(sess.ID), "session must be idle again at the end")
}

// messageText extracts the flat text of a message for assertions.
func messageText(m message.Message) string {
	var b strings.Builder
	for _, part := range m.Parts {
		if tp, ok := part.(message.TextContent); ok {
			b.WriteString(tp.Text)
		}
	}
	return b.String()
}
