package wechat

import (
	"context"

	"github.com/package-register/mocode/internal/domain/messenger"
)

// messengerAccountManager adapts *AccountManager to the domain messenger port.
// It is the integration-side implementation injected into the core tool layer
// so core never imports this package directly.
type messengerAccountManager struct{ m *AccountManager }

// AsMessenger returns a domain.Messenger backed by this account manager.
// Returns a no-op-friendly value when no manager exists.
func AsMessenger(m *AccountManager) messengerAccountManager {
	return messengerAccountManager{m: m}
}

func (a messengerAccountManager) ActiveSender() messenger.Sender {
	if a.m == nil {
		return nil
	}
	if ch := a.m.GetActive(); ch != nil {
		return messengerSender{ch: ch}
	}
	return nil
}

// messengerSender adapts *Channel to the messenger.Sender port.
type messengerSender struct{ ch *Channel }

func (s messengerSender) IsLoggedIn() bool { return s.ch.IsLoggedIn() }

func (s messengerSender) SendImage(ctx context.Context, userID string, data []byte) error {
	return s.ch.SendImage(ctx, userID, data)
}
