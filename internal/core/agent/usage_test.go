package agent

import (
	"testing"

	"charm.land/fantasy"

	"github.com/package-register/mocode/internal/domain/session"
)

func TestUpdateSessionUsageIncludesAllInputTokenTypes(t *testing.T) {
	a := &sessionAgent{}
	sess := &session.Session{}
	usage := fantasy.Usage{
		InputTokens:         100,
		OutputTokens:        50,
		CacheReadTokens:     25,
		CacheCreationTokens: 10,
	}

	a.updateSessionUsage(Model{}, sess, usage, nil)

	if sess.PromptTokens != 135 {
		t.Fatalf("expected prompt tokens to include regular, cache read, and cache creation tokens, got %d", sess.PromptTokens)
	}
	if sess.CompletionTokens != 50 {
		t.Fatalf("expected completion tokens 50, got %d", sess.CompletionTokens)
	}
	if sess.CacheReadTokens != 25 {
		t.Fatalf("expected cache read tokens 25, got %d", sess.CacheReadTokens)
	}
	if sess.CacheCreationTokens != 10 {
		t.Fatalf("expected cache creation tokens 10, got %d", sess.CacheCreationTokens)
	}
}
