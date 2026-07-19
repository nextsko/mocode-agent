package model

import (
	"context"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/nextsko/mocode-agent/internal/domain/session/sessionexport"
	"github.com/nextsko/mocode-agent/internal/ui/dialog"
	"github.com/nextsko/mocode-agent/internal/ui/util"
)

func (m *UI) exportSession(action dialog.ActionExportSession) tea.Cmd {
	return func() tea.Msg {
		sessionID := action.SessionID
		if sessionID == "" {
			return util.NewErrorMsg(fmt.Errorf("no active session to export"))
		}

		messages, err := m.com.Workspace.ListMessages(context.Background(), sessionID)
		if err != nil {
			return util.NewErrorMsg(err)
		}
		if action.Scope == "recent10" && len(messages) > 10 {
			messages = messages[len(messages)-10:]
		}

		result, err := sessionexport.Export(messages, sessionexport.Options{
			SessionID:  sessionID,
			Format:     action.Format,
			Scope:      action.Scope,
			WorkingDir: m.com.Workspace.WorkingDir(),
			Now:        time.Now(),
		})
		if err != nil {
			return util.NewErrorMsg(err)
		}

		return util.NewInfoMsg("Session exported to " + result.Path)
	}
}
