package commands

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type errorCommandProvider struct {
	info ProviderInfo
	err  error
}

func (p errorCommandProvider) ProviderInfo() ProviderInfo {
	return p.info
}

func (p errorCommandProvider) Commands(CommandContext) ([]CommandDescriptor, error) {
	return nil, p.err
}

func TestCommandRegistryCommandsStableOrder(t *testing.T) {
	registry := NewCommandRegistry(
		StaticCommandProvider{
			Info: ProviderInfo{ID: "a", Name: "A", Kind: ProviderKindBuiltin},
			Items: []CommandDescriptor{
				{ID: "one", Title: "One"},
			},
		},
		StaticCommandProvider{
			Info: ProviderInfo{ID: "b", Name: "B", Kind: ProviderKindMCP},
			Items: []CommandDescriptor{
				{ID: "two", Title: "Two"},
			},
		},
	)

	commands, diagnostics := registry.Commands(CommandContext{})

	require.Empty(t, diagnostics)
	require.Len(t, commands, 2)
	require.Equal(t, "one", commands[0].ID)
	require.Equal(t, "a", commands[0].Provider.ID)
	require.Equal(t, "two", commands[1].ID)
	require.Equal(t, "b", commands[1].Provider.ID)
}

func TestCommandRegistryCollectsDiagnostics(t *testing.T) {
	registry := NewCommandRegistry(
		errorCommandProvider{
			info: ProviderInfo{ID: "bad", Name: "Bad", Kind: ProviderKindCustomCommand},
			err:  errors.New("boom"),
		},
		StaticCommandProvider{
			Info: ProviderInfo{ID: "ok", Name: "OK", Kind: ProviderKindBuiltin},
			Items: []CommandDescriptor{
				{ID: "valid", Title: "Valid"},
			},
		},
	)

	commands, diagnostics := registry.Commands(CommandContext{})

	require.Len(t, commands, 1)
	require.Equal(t, "valid", commands[0].ID)
	require.Len(t, diagnostics, 1)
	require.Equal(t, "bad", diagnostics[0].ProviderID)
	require.EqualError(t, diagnostics[0].Err, "boom")
}
