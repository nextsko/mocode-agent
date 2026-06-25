package wechat

import (
	"log/slog"
	"strings"
)

const sessionKeyPrefix = "wx:"

// SessionKey returns the canonical map key for a WeChat user's session binding.
func SessionKey(userID string) string {
	return sessionKeyPrefix + userID
}

// ClearBindingsForSessionID removes all user bindings that point at sessionID.
func (c *Channel) ClearBindingsForSessionID(sessionID string) {
	if sessionID == "" {
		return
	}
	c.sessions.Range(func(key, value any) bool {
		sid, ok := value.(string)
		if !ok || sid != sessionID {
			return true
		}
		c.sessions.Delete(key)
		return true
	})
	if err := c.saveSessions(); err != nil {
		slog.Warn("Failed to save WeChat session bindings after clear", "error", err)
	}
}

// stripSessionKeyPrefix returns the raw WeChat user ID from a binding key when present.
func stripSessionKeyPrefix(key string) string {
	return strings.TrimPrefix(key, sessionKeyPrefix)
}
