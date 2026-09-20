package duckduckgo

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/nextsko/mocode-agent/pkg/search"
)

// cooldownDuration is how long the provider stays unavailable after a
// bot-detection challenge. The upstream TS has no backoff; this window
// is the Go subplan's addition so the chain stops hammering a blocked
// endpoint.
const cooldownDuration = 5 * time.Minute

// challengeMessage is the user-facing explanation, ported verbatim from
// the upstream scraper.
const challengeMessage = "DuckDuckGo blocked the request with a " +
	"bot-detection challenge. DuckDuckGo throttles automated HTML " +
	"searches from datacenter/shared-egress IPs; configure a " +
	"credentialed provider such as Brave, Tavily, Exa, or Kagi for " +
	"reliable web search."

// packageCooldown is the process-wide backoff window shared by every
// DDGProvider instance, so multiple instances cannot hammer past a
// challenge one after another.
var packageCooldown = &cooldownState{}

// cooldownState tracks one backoff window with an injectable clock for
// tests.
type cooldownState struct {
	mu    sync.Mutex
	until time.Time
	now   func() time.Time
}

// newCooldown creates an isolated backoff window ticking with now; a nil
// now falls back to time.Now.
func newCooldown(now func() time.Time) *cooldownState {
	return &cooldownState{now: now}
}

// clock returns the current time, defaulting to time.Now.
func (c *cooldownState) clock() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}

// trigger starts or restarts the backoff window lasting d from now.
func (c *cooldownState) trigger(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.until = c.clock().Add(d)
}

// active reports whether the backoff window still holds.
func (c *cooldownState) active() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.clock().Before(c.until)
}

// remaining reports how long the backoff window still holds, zero once
// expired.
func (c *cooldownState) remaining() time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	remaining := c.until.Sub(c.clock())
	if remaining < 0 {
		return 0
	}
	return remaining
}

// isAnomalyResponse reports whether the page DuckDuckGo returned is the
// bot-challenge modal instead of real results. DuckDuckGo mixes status
// codes (200 vs 202) on these pages, so the body markers are the
// reliable signal.
func isAnomalyResponse(html string) bool {
	return strings.Contains(html, "anomaly-modal") ||
		strings.Contains(html, "anomaly.js")
}

// challengeError builds the classified error raised when a
// bot-detection challenge page is detected. It carries 429 semantics so
// the chain records the failure and falls through.
func challengeError() *search.SearchProviderError {
	return &search.SearchProviderError{
		Provider:   ProviderID,
		Code:       search.ErrorCodeRateLimited,
		Message:    challengeMessage,
		HTTPStatus: 429,
	}
}

// cooldownError builds the fast-fail error raised while the backoff
// window after a challenge still holds.
func cooldownError(remaining time.Duration) *search.SearchProviderError {
	return &search.SearchProviderError{
		Provider: ProviderID,
		Code:     search.ErrorCodeRateLimited,
		Message: fmt.Sprintf("cooldown active after bot-detection "+
			"challenge, retry in %s", remaining.Round(time.Second)),
		HTTPStatus: 429,
	}
}
