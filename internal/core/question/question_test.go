package question

import (
	"context"
	"testing"
	"time"

	"github.com/nextsko/mocode-agent/internal/util/pubsub"
	"github.com/stretchr/testify/require"
)

func yesNo(text string) Question {
	return Question{Type: TypeYesNo, Text: text, Description: "pick one"}
}

func TestServiceAskAnswer(t *testing.T) {
	svc := NewService()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	// Subscriber sees the published request.
	requests := svc.Subscribe(ctx)

	go func() {
		evt := <-requests
		yes := true
		evt.Payload.Questions[0].ID = "q1"
		require.True(t, svc.Answer(evt.Payload.ID, []Answer{{QuestionID: "q1", Yes: &yes}}))
	}()

	answers, err := svc.Ask(ctx, Request{Questions: []Question{yesNo("continue?")}})
	require.NoError(t, err)
	require.Len(t, answers, 1)
	require.NotNil(t, answers[0].Yes)
	require.True(t, *answers[0].Yes)
	require.False(t, svc.Answer("", nil), "no question stays pending after answering")
}

func TestServiceAskCancel(t *testing.T) {
	svc := NewService()

	go func() {
		time.Sleep(50 * time.Millisecond)
		require.True(t, svc.Cancel(""))
	}()

	_, err := svc.Ask(t.Context(), Request{Questions: []Question{yesNo("stuck?")}})
	require.ErrorIs(t, err, ErrCancelled)
	require.False(t, svc.Cancel(""), "second cancel finds nothing pending")
}

func TestServiceAskContextCancel(t *testing.T) {
	svc := NewService()
	ctx, cancel := context.WithCancel(t.Context())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	_, err := svc.Ask(ctx, Request{Questions: []Question{yesNo("abort?")}})
	require.ErrorIs(t, err, context.Canceled)
}

// TestServiceConcurrentBatchesAreIndependent is the regression test for the
// single-pending-batch bug: two sessions ask at the same time and each answer
// must reach its own batch.
func TestServiceConcurrentBatchesAreIndependent(t *testing.T) {
	svc := NewService()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	requests := svc.Subscribe(ctx)

	type asked struct {
		id string
	}
	seen := make(chan asked, 2)
	go func() {
		for i := 0; i < 2; i++ {
			evt := <-requests
			seen <- asked{id: evt.Payload.ID}
		}
	}()

	results := make(chan []Answer, 2)
	for i, text := range []string{"first?", "second?"} {
		go func(i int, text string) {
			answers, err := svc.Ask(ctx, Request{ID: []string{"batch-a", "batch-b"}[i], Questions: []Question{yesNo(text)}})
			require.NoError(t, err)
			results <- answers
		}(i, text)
	}

	ids := map[string]bool{}
	for i := 0; i < 2; i++ {
		select {
		case a := <-seen:
			ids[a.id] = true
		case <-time.After(2 * time.Second):
			t.Fatal("both batches were not published")
		}
	}
	require.Len(t, ids, 2, "the two batches must not overwrite each other")

	// Answer each batch by its own ID.
	require.True(t, svc.Answer("batch-a", []Answer{{QuestionID: "qa"}}))
	require.True(t, svc.Answer("batch-b", []Answer{{QuestionID: "qb"}}))

	answered := map[string]bool{}
	for i := 0; i < 2; i++ {
		select {
		case answers := <-results:
			require.Len(t, answers, 1)
			answered[answers[0].QuestionID] = true
		case <-time.After(2 * time.Second):
			t.Fatal("both batches were not answered")
		}
	}
	require.Equal(t, map[string]bool{"qa": true, "qb": true}, answered)
	require.False(t, svc.Answer("batch-a", nil), "a resolved batch is gone")
}

// TestServiceAnswerUnknownBatch checks that addressing a batch that is not
// pending is a no-op rather than a panic.
func TestServiceAnswerUnknownBatch(t *testing.T) {
	svc := NewService()
	require.False(t, svc.Answer("nope", nil))
	require.False(t, svc.Cancel("nope"))
	require.Empty(t, svc.Pending())
}

// TestServiceAnswerEmptyIDNamesOnlyBatch keeps the pre-batch-ID callers
// working: an empty ID resolves the single pending batch.
func TestServiceAnswerEmptyIDNamesOnlyBatch(t *testing.T) {
	svc := NewService()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	requests := svc.Subscribe(ctx)
	go func() {
		<-requests
		require.True(t, svc.Answer("", []Answer{{QuestionID: "q1"}}))
	}()

	answers, err := svc.Ask(ctx, Request{Questions: []Question{yesNo("ok?")}})
	require.NoError(t, err)
	require.Len(t, answers, 1)
	require.Equal(t, "q1", answers[0].QuestionID)
}

// TestServicePendingReportsOpenBatches checks the reconnection aid: a client
// that reports its session can learn which batches are still waiting.
func TestServicePendingReportsOpenBatches(t *testing.T) {
	svc := NewService()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	requests := svc.Subscribe(ctx)
	go func() {
		for range requests {
		}
	}()

	go func() {
		_, _ = svc.Ask(ctx, Request{ID: "batch-1", SessionID: "s1", Questions: []Question{yesNo("a?")}})
	}()
	go func() {
		_, _ = svc.Ask(ctx, Request{ID: "batch-2", SessionID: "s2", Questions: []Question{yesNo("b?")}})
	}()

	require.Eventually(t, func() bool {
		return len(svc.Pending()) == 2
	}, 2*time.Second, 10*time.Millisecond)

	sessions := map[string]bool{}
	for _, req := range svc.Pending() {
		sessions[req.SessionID] = true
	}
	require.Equal(t, map[string]bool{"s1": true, "s2": true}, sessions)
}

// TestServiceNotificationCarriesSession checks that a client can tell which
// session a resolved batch belonged to.
func TestServiceNotificationCarriesSession(t *testing.T) {
	svc := NewService()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	notifs := svc.SubscribeNotifications(ctx)

	yes := true
	requests := svc.Subscribe(ctx)
	go func() {
		<-requests
		require.True(t, svc.Answer("batch-1", []Answer{{QuestionID: "q1", Yes: &yes}}))
	}()
	_, _ = svc.Ask(ctx, Request{ID: "batch-1", SessionID: "session-7", Questions: []Question{{ID: "q1", Type: TypeYesNo, Text: "x", Description: "d"}}})

	select {
	case evt := <-notifs:
		require.Equal(t, "batch-1", evt.Payload.BatchID)
		require.Equal(t, "session-7", evt.Payload.SessionID)
	case <-time.After(2 * time.Second):
		t.Fatal("resolution notification was not published")
	}
}

// TestServiceTimeoutIsTheNonInteractiveSafetyNet checks that a batch nobody
// answers gives up instead of blocking forever.
func TestServiceTimeoutIsTheNonInteractiveSafetyNet(t *testing.T) {
	svc := NewServiceWithTimeout(50 * time.Millisecond)
	_, err := svc.Ask(t.Context(), Request{Questions: []Question{yesNo("nobody home?")}})
	require.Error(t, err)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Contains(t, err.Error(), "no answer received")
	require.Empty(t, svc.Pending(), "a timed-out batch is not pending anymore")
}

// TestServiceAskFillsConfirmDefaults checks the multi-question confirm copy.
func TestServiceAskFillsConfirmDefaults(t *testing.T) {
	svc := NewService()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	requests := svc.Subscribe(ctx)
	go func() {
		<-requests
		require.True(t, svc.Cancel(""))
	}()

	_, err := svc.Ask(ctx, Request{Questions: []Question{yesNo("a?"), yesNo("b?")}})
	require.ErrorIs(t, err, ErrCancelled)
}

func TestRequestValidate(t *testing.T) {
	require.Error(t, Request{}.Validate(), "empty request must fail")

	q := yesNo("ok?")
	q.Choices = nil
	require.NoError(t, Request{Questions: []Question{q}}.Validate())

	choice := Question{
		Type: TypeSingleChoice, Text: "pick", Description: "d",
		Choices: []Choice{{ID: "a", Label: "A"}, {ID: "b", Label: "B"}},
	}
	require.NoError(t, Request{Questions: []Question{choice}}.Validate())

	single := choice
	single.Choices = single.Choices[:1]
	require.Error(t, Request{Questions: []Question{single}}.Validate(), "single_choice needs >= 2 choices")

	many := Request{Questions: make([]Question, MaxQuestions+1)}
	for i := range many.Questions {
		many.Questions[i] = yesNo("q")
	}
	require.Error(t, many.Validate(), "batch over the limit must fail")
}

func TestNotificationBroadcast(t *testing.T) {
	svc := NewService()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	notifs := svc.SubscribeNotifications(ctx)

	yes := true
	requests := svc.Subscribe(ctx)
	go func() {
		<-requests
		require.True(t, svc.Answer("batch-1", []Answer{{QuestionID: "q1", Yes: &yes}}))
	}()
	_, _ = svc.Ask(ctx, Request{ID: "batch-1", Questions: []Question{{ID: "q1", Type: TypeYesNo, Text: "x", Description: "d"}}})

	select {
	case evt := <-notifs:
		require.Equal(t, "batch-1", evt.Payload.BatchID)
	case <-time.After(2 * time.Second):
		t.Fatal("resolution notification was not published")
	}
}

func TestQuestionNoteKey(t *testing.T) {
	require.Equal(t, "_question", QuestionNoteKey)
}

var _ pubsub.Subscriber[Request] = Service(nil)
