package connector

import (
	"context"
	"testing"

	"charm.land/fantasy"

	"github.com/stretchr/testify/assert"
)

// fakeConnector exercises the port contract the way ssh/gitea will.
type fakeConnector struct {
	name   string
	closed bool
}

func (f *fakeConnector) Name() string { return f.name }
func (f *fakeConnector) Tools(_ context.Context) []fantasy.AgentTool {
	return nil
}
func (f *fakeConnector) Close(_ context.Context) error {
	f.closed = true
	return nil
}

// The port must be implementable with zero framework coupling beyond the
// tool type itself — this is the contract ssh/ and gitea/ will migrate onto.
func TestConnectorPortContract(t *testing.T) {
	var c Connector = &fakeConnector{name: "fake"}
	var closer Closer = c.(*fakeConnector)

	assert.Equal(t, "fake", c.Name())
	assert.Nil(t, c.Tools(t.Context()))

	assert.NoError(t, closer.Close(t.Context()))
	assert.True(t, closer.(*fakeConnector).closed, "Close must be callable via the Closer interface")
}

// Compile-time proof the interface split is intentional: a connector that
// owns nothing implements only Connector, and the registry can type-switch
// for Startable/Closer without requiring them.
func TestConnectorOptionality(t *testing.T) {
	minimal := &fakeConnector{name: "minimal"}
	// Not Startable — fine.
	_, isStartable := any(minimal).(Startable)
	assert.False(t, isStartable, "Startable must be optional")
	// Is Closer — fine.
	_, isCloser := any(minimal).(Closer)
	assert.True(t, isCloser)
}
