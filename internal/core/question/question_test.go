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
		require.True(t, svc.Answer([]Answer{{QuestionID: "q1", Yes: &yes}}))
	}()

	answers, err := svc.Ask(ctx, Request{Questions: []Question{yesNo("continue?")}})
	require.NoError(t, err)
	require.Len(t, answers, 1)
	require.NotNil(t, answers[0].Yes)
	require.True(t, *answers[0].Yes)
	require.False(t, svc.Answer(nil), "no question stays pending after answering")
}

func TestServiceAskCancel(t *testing.T) {
	svc := NewService()

	go func() {
		time.Sleep(50 * time.Millisecond)
		require.True(t, svc.Cancel())
	}()

	_, err := svc.Ask(t.Context(), Request{Questions: []Question{yesNo("stuck?")}})
	require.ErrorIs(t, err, ErrCancelled)
	require.False(t, svc.Cancel(), "second cancel finds nothing pending")
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

func TestRequestValidate(t *testing.T) {
	require.Error(t, Request{}.Validate(), "empty request must fail")

	q := yesNo("ok?")
	q.Choices = nil
	require.NoError(t, Request{Questions: []Question{q}}.Validate())

	choice := Question{Type: TypeSingleChoice, Text: "pick", Description: "d",
		Choices: []Choice{{ID: "a", Label: "A"}, {ID: "b", Label: "B"}}}
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
	go func() {
		svc.Subscribe(ctx) // drain-free: the request stays unread
		require.True(t, svc.Answer([]Answer{{QuestionID: "q1", Yes: &yes}}))
	}()
	svc.Ask(ctx, Request{ID: "batch-1", Questions: []Question{{ID: "q1", Type: TypeYesNo, Text: "x", Description: "d"}}})

	select {
	case evt := <-notifs:
		require.Equal(t, "batch-1", evt.Payload.BatchID)
	case <-time.After(2 * time.Second):
		t.Fatal("resolution notification was not published")
	}
}

var _ pubsub.Subscriber[Request] = (Service)(nil)
