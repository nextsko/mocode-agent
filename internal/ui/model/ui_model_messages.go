package model

import (
	"github.com/nextsko/mocode-agent/internal/core/tools/external/mcp"
	"github.com/nextsko/mocode-agent/internal/domain/session/message"
	"github.com/nextsko/mocode-agent/internal/ui/slash"
)

type openEditorMsg struct {
	Text string
}

type (
	// cancelTimerExpiredMsg is sent when the cancel timer expires.
	cancelTimerExpiredMsg struct{}
	// userCommandsLoadedMsg is sent when user commands are loaded.
	userCommandsLoadedMsg struct {
		Commands []slash.CustomCommand
	}
	// breathTickMsg is sent periodically while the agent is busy to animate
	// the editor prompt with a pulsing breathing effect.
	breathTickMsg struct{}
	// mcpPromptsLoadedMsg is sent when mcp prompts are loaded.
	mcpPromptsLoadedMsg struct {
		Prompts []slash.MCPPrompt
	}
	// mcpStateChangedMsg is sent when there is a change in MCP client states.
	mcpStateChangedMsg struct {
		states map[string]mcp.ClientInfo
	}
	// sendMessageMsg is sent to send a message.
	// currently only used for mcp prompts.
	sendMessageMsg struct {
		Content     string
		Attachments []message.Attachment
	}

	// closeDialogMsg is sent to close the current dialog.
	closeDialogMsg struct{}

	// copyChatHighlightMsg is sent to copy the current chat highlight to clipboard.
	copyChatHighlightMsg struct{}

	// sessionFilesUpdatesMsg is sent when the files for this session have been updated.
	sessionFilesUpdatesMsg struct {
		sessionFiles []SessionFile
	}
	hudTickMsg struct{}

	backgroundJobOutputMsg struct {
		ShellID string
		Output  string
		Done    bool
		Err     error
	}
)
