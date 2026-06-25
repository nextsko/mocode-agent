// router.go implements the butler's intelligent routing layer.
// It routes natural language messages to the correct session based on context.
package wechat

import (
	"strings"
)

// ButlerRouter routes user messages to the appropriate session.
type ButlerRouter struct {
	sessions *SessionManager
}

// NewButlerRouter creates a new router.
func NewButlerRouter(sessions *SessionManager) *ButlerRouter {
	return &ButlerRouter{sessions: sessions}
}

// RouteResult describes where a message should be routed.
type RouteResult struct {
	// Action is one of: "send_to_session", "create_session", "aggregate_status", "direct_reply"
	Action    string
	SessionID string // target session (for send_to_session)
	WorkDir   string // for create_session
	Reply     string // direct reply (bypass agent)
}

// Route determines where a user message should be sent.
func (r *ButlerRouter) Route(userID, text, activeSession string) RouteResult {
	text = strings.TrimSpace(text)

	// Short messages (< 30 chars) go to active session if one exists.
	if len(text) < 30 && activeSession != "" {
		if _, ok := r.sessions.Get(activeSession); ok {
			return RouteResult{Action: "send_to_session", SessionID: activeSession}
		}
	}

	// Keywords that suggest session switching.
	switchKeywords := map[string]string{
		"neuron": "neuron",
		"mocode": "mocode",
		"school": "school",
		"论文":     "school",
		"毕业":     "school",
	}

	lowerText := strings.ToLower(text)
	for keyword, sessionHint := range switchKeywords {
		if strings.Contains(lowerText, keyword) {
			// Find matching session.
			for _, sess := range r.sessions.List() {
				if strings.Contains(strings.ToLower(sess.Name), sessionHint) ||
					strings.Contains(strings.ToLower(sess.WorkDir), sessionHint) {
					return RouteResult{Action: "send_to_session", SessionID: sess.ID}
				}
			}
		}
	}

	// Status/summary requests.
	if strings.Contains(lowerText, "状态") || strings.Contains(lowerText, "status") ||
		strings.Contains(lowerText, "进展") || strings.Contains(lowerText, "汇总") {
		return RouteResult{Action: "aggregate_status"}
	}

	// Default: send to active session if exists, otherwise create new.
	if activeSession != "" {
		if _, ok := r.sessions.Get(activeSession); ok {
			return RouteResult{Action: "send_to_session", SessionID: activeSession}
		}
	}

	return RouteResult{Action: "direct_reply", Reply: "没有活跃会话，请先使用 /list 查看或创建新会话。"}
}
