package agent

import (
	"time"

	"charm.land/fantasy"
)

// truncateForLog caps a string to a log-friendly length so session logs stay
// readable and bounded. Used by the OnToolCall/OnToolResult observability hooks.
func truncateForLog(s string) string {
	const max = 500
	if len(s) <= max {
		return s
	}
	return s[:max] + "...[truncated]"
}

func (a *sessionAgent) eventPromptSent(sessionID string) {
	// Metrics collection removed
}

func (a *sessionAgent) eventPromptResponded(sessionID string, duration time.Duration) {
	// Metrics collection removed
}

func (a *sessionAgent) eventTokensUsed(sessionID string, model Model, usage fantasy.Usage, cost float64) {
	// Metrics collection removed
}

func (a *sessionAgent) eventCommon(sessionID string, model Model) []any {
	m := model.ModelCfg

	return []any{
		"session id", sessionID,
		"provider", m.Provider,
		"model", m.Model,
		"reasoning effort", m.ReasoningEffort,
		"thinking mode", m.Think,
		"yolo mode", a.isYolo,
	}
}
