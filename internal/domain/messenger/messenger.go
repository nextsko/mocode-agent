// Package messenger defines the core-side port for delivering media to an
// external messaging account (e.g. WeChat). It is a layer-1 domain port: the
// tool plugins in core/agent/tools/plugins import it instead of reaching up
// into the integration layer, and the integration provides an adapter at
// wiring time. This removes the upward core -> integration dependency edge.
package messenger

import "context"

// Messenger resolves the send surface for the currently active account.
type Messenger interface {
	// ActiveSender returns the active, logged-in sender, or nil if no account
	// is available. Tools call this to resolve a destination before sending.
	ActiveSender() Sender
}

// Sender is the send surface of one active messaging account.
type Sender interface {
	IsLoggedIn() bool
	SendImage(ctx context.Context, userID string, data []byte) error
}

// NoopMessenger is a Messenger with no active account. It is the zero value
// used when no integration is wired in (e.g. tests, non-wechat builds), so the
// wechat tools degrade to a clear "no active account" error instead of
// panicking on a nil interface.
type NoopMessenger struct{}

func (NoopMessenger) ActiveSender() Sender { return nil }
