package memory

import (
	"context"
	"testing"
	"time"

	"charm.land/fantasy"
	"github.com/stretchr/testify/require"
)

type stubMemoryService struct{}

func (stubMemoryService) AddMemory(context.Context, string, string, string, []string, Kind, *time.Time, []string, string) error {
	return nil
}
func (stubMemoryService) UpdateMemory(context.Context, string, string, string, string, []string, Kind, *time.Time, []string, string) error {
	return nil
}
func (stubMemoryService) DeleteMemory(context.Context, string, string, string) error { return nil }
func (stubMemoryService) ClearMemories(context.Context, string, string) error        { return nil }
func (stubMemoryService) ReadMemories(context.Context, string, string, int) ([]*Entry, error) {
	return nil, nil
}
func (stubMemoryService) SearchMemories(context.Context, string, string, string, int) ([]*Entry, error) {
	return nil, nil
}
func (stubMemoryService) Tools() []fantasy.AgentTool { return nil }
func (stubMemoryService) Close() error               { return nil }

func TestToolsForReturnsTools(t *testing.T) {
	t.Parallel()

	tools := ToolsFor(stubMemoryService{}, DefaultToolOptions())
	require.Len(t, tools, 5)
}
