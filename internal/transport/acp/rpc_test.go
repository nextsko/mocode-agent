package acp

import (
	"context"
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPeerRequestResponse wires two peers over net.Pipe.
func TestPeerRequestResponse(t *testing.T) {
	t.Parallel()
	c1, c2 := net.Pipe()
	agent := NewPeer(c1, c1)
	client := NewPeer(c2, c2)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	agent.OnRequest(func(_ context.Context, method string, _ json.RawMessage) (any, *Error) {
		if method != "ping" {
			return nil, &Error{Code: CodeMethodNotFound, Message: "no"}
		}
		return map[string]string{"pong": "yes"}, nil
	})
	go func() { _ = agent.Serve(ctx) }()
	go func() { _ = client.Serve(ctx) }()

	raw, err := client.Call(ctx, "ping", map[string]string{})
	require.NoError(t, err)
	assert.JSONEq(t, `{"pong":"yes"}`, string(raw))
	_ = c1.Close()
	_ = c2.Close()
}

func TestPeerNotification(t *testing.T) {
	t.Parallel()
	c1, c2 := net.Pipe()
	agent := NewPeer(c1, c1)
	client := NewPeer(c2, c2)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	got := make(chan string, 1)
	client.OnNotification(func(method string, _ json.RawMessage) { got <- method })
	go func() { _ = agent.Serve(ctx) }()
	go func() { _ = client.Serve(ctx) }()

	require.NoError(t, agent.Notify("session/update", map[string]string{"x": "1"}))
	select {
	case m := <-got:
		assert.Equal(t, "session/update", m)
	case <-ctx.Done():
		t.Fatal("notification not delivered")
	}
	_ = c1.Close()
	_ = c2.Close()
}
