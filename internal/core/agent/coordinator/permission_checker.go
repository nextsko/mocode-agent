package coordinator

import (
	"context"

	"github.com/nextsko/mocode-agent/internal/core/agent/toolutil"
	"github.com/nextsko/mocode-agent/internal/core/permission"
)

// permissionCheckerAdapter bridges permission.Service to the minimal
// Allow(name) bool contract used by tools.ToolContext. It carries the request
// context so Request can prompt the user exactly like a legacy tool would.
//
// Deny-by-default: a nil service or an errored request denies, never allows.
type permissionCheckerAdapter struct {
	ctx       context.Context
	svc       permission.Service
	sessionID string
}

func (p permissionCheckerAdapter) Allow(name string) bool {
	if p.svc == nil {
		return false
	}
	if p.svc.SkipRequests() {
		return true
	}
	callID, _ := toolutil.ToolCallIDFromCtx(p.ctx)
	ok, err := p.svc.Request(p.ctx, permission.CreatePermissionRequest{
		SessionID:   p.sessionID,
		ToolCallID:  callID,
		ToolName:    name,
		Description: "Allow tool: " + name,
		Action:      "execute",
	})
	if err != nil {
		return false
	}
	return ok
}
