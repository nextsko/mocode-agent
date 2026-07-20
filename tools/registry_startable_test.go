package tools

import (
	"context"
	"testing"

	"charm.land/fantasy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── helpers ──────────────────────────────────────────────────────────────────

// plainPlugin has no Start/Stop — non-startable.
type plainPlugin struct{}

func (plainPlugin) Descriptors() []ToolDescriptor                           { return nil }
func (plainPlugin) Build(_ context.Context, _ ToolDeps) []fantasy.AgentTool { return nil }

// startablePlugin counts Start/Stop calls and implements capability.Startable.
type startablePlugin struct {
	starts int
	stops  int
}

func (s *startablePlugin) Descriptors() []ToolDescriptor                           { return nil }
func (s *startablePlugin) Build(_ context.Context, _ ToolDeps) []fantasy.AgentTool { return nil }
func (s *startablePlugin) Start(_ context.Context) error                           { s.starts++; return nil }
func (s *startablePlugin) Stop(_ context.Context) error                            { s.stops++; return nil }

// ─── StartPlugin ──────────────────────────────────────────────────────────────

func TestStartPlugin_NonStartable_IsNoOp(t *testing.T) {
	t.Parallel()
	r := NewRegistryWithPlugins()
	r.AddPlugin(plainPlugin{})
	err := r.StartAll(context.Background())
	require.NoError(t, err)
}

func TestStartPlugin_Startable_CallsStart(t *testing.T) {
	t.Parallel()
	p := &startablePlugin{}
	r := NewRegistryWithPlugins(p)
	err := r.StartAll(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, p.starts)
}

func TestStartAll_CallsAllStartablePlugins(t *testing.T) {
	t.Parallel()
	p1 := &startablePlugin{}
	p2 := &startablePlugin{}
	r := NewRegistryWithPlugins(p1, plainPlugin{}, p2)
	require.NoError(t, r.StartAll(context.Background()))
	assert.Equal(t, 1, p1.starts)
	assert.Equal(t, 1, p2.starts)
}

func TestStopAll_CallsAllStartablePlugins(t *testing.T) {
	t.Parallel()
	p := &startablePlugin{}
	r := NewRegistryWithPlugins(p)
	require.NoError(t, r.StartAll(context.Background()))
	require.NoError(t, r.StopAll(context.Background()))
	assert.Equal(t, 1, p.stops)
}

func TestStopAll_AfterStart_ResetsForRestart(t *testing.T) {
	t.Parallel()
	p := &startablePlugin{}
	r := NewRegistryWithPlugins(p)
	require.NoError(t, r.StartAll(context.Background()))
	require.NoError(t, r.StopAll(context.Background()))
	require.NoError(t, r.StartAll(context.Background()))
	assert.Equal(t, 2, p.starts)
	assert.Equal(t, 1, p.stops)
}
