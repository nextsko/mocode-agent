package coordinator

import (
	"github.com/nextsko/mocode-agent/internal/core/agent/toolutil"
	"github.com/nextsko/mocode-agent/internal/domain/session/sessionlog"
)

func (s sessionLogSink) LogToolCall(event, data string, m toolutil.SessionLogMeta) {
	s.l.LogToolCall(event, data, toSessionLogMeta(m))
}

func (s sessionLogSink) LogThink(event, data string, m toolutil.SessionLogMeta) {
	s.l.LogThink(event, data, toSessionLogMeta(m))
}

func (s sessionLogSink) LogBug(event, data string, m toolutil.SessionLogMeta) {
	s.l.LogBug(event, data, toSessionLogMeta(m))
}

func (s sessionLogSink) LogInfo(event, data string, m toolutil.SessionLogMeta) {
	s.l.LogInfo(event, data, toSessionLogMeta(m))
}

func toSessionLogMeta(m toolutil.SessionLogMeta) sessionlog.Meta {
	return sessionlog.Meta{
		ToolName:   m.ToolName,
		ToolCallID: m.ToolCallID,
		AgentID:    m.AgentID,
		DurationMs: m.DurationMs,
		ErrorType:  m.ErrorType,
	}
}
