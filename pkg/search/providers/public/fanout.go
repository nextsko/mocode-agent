package public

import (
	"context"
	"net/http"
	"time"

	"github.com/nextsko/mocode-agent/pkg/search"
	"github.com/nextsko/mocode-agent/pkg/search/providers/public/engines"
)

// engineFailure records one engine's failure for the aggregate error.
type engineFailure struct {
	engineID string
	err      error
}

// fanoutOutcome is the fan-out result: per-engine source lists indexed
// by roster order (nil for engines that did not deliver) plus the
// failures observed before the race resolved.
type fanoutOutcome struct {
	perEngine [][]search.SearchSource
	failures  []engineFailure
}

// engineOutcome is one settled engine's delivery on the internal
// channel.
type engineOutcome struct {
	index   int
	sources []search.SearchSource
	err     error
}

// fanoutEngines races every engine concurrently under the upstream
// aggregate semantics. Phase one waits until all engines settle or the
// soft deadline fires; when the soft deadline fires with no success in
// hand and engines still running, the aggregate keeps waiting — up to
// the hard deadline — for the first success, so a slow field degrades
// to fewer engines rather than an empty answer. Once the race
// resolves, stragglers are aborted: still-running engines see their
// context cancelled while already-delivered results survive. The
// deliveries land on a buffered channel, so aborted goroutines never
// block on send.
func fanoutEngines(
	ctx context.Context,
	client *http.Client,
	roster []engines.SubEngine,
	queryString string,
	limit int,
	softDeadline, hardDeadline time.Duration,
) fanoutOutcome {
	hardCtx, hardCancel := context.WithTimeout(ctx, hardDeadline)
	defer hardCancel()
	runCtx, stragglerCancel := context.WithCancel(hardCtx)
	defer stragglerCancel()

	outcomes := make(chan engineOutcome, len(roster))
	for i, engine := range roster {
		go func() {
			sources, err := engine.Fetch(runCtx, client, queryString, limit)
			outcomes <- engineOutcome{index: i, sources: sources, err: err}
		}()
	}

	outcome := fanoutOutcome{perEngine: make([][]search.SearchSource, len(roster))}
	settled, successCount := 0, 0
	record := func(delivery engineOutcome) {
		settled++
		if delivery.err != nil {
			outcome.perEngine[delivery.index] = nil
			outcome.failures = append(outcome.failures, engineFailure{
				engineID: roster[delivery.index].ID(),
				err:      delivery.err,
			})
			return
		}
		outcome.perEngine[delivery.index] = delivery.sources
		successCount++
	}

	// Phase one: race every engine settling against the soft deadline.
	// runCtx covers caller cancellation and the hard cap.
	softTimer := time.NewTimer(softDeadline)
	defer softTimer.Stop()
phaseOne:
	for settled < len(roster) {
		select {
		case delivery := <-outcomes:
			record(delivery)
		case <-softTimer.C:
			break phaseOne
		case <-runCtx.Done():
			break phaseOne
		}
	}

	// Phase two: the soft deadline fired with no success in hand while
	// some engines are still unsettled — keep waiting for the first
	// success, for everyone to settle, or for the hard deadline.
	if successCount == 0 && settled < len(roster) {
	phaseTwo:
		for successCount == 0 && settled < len(roster) {
			select {
			case delivery := <-outcomes:
				record(delivery)
			case <-runCtx.Done():
				break phaseTwo
			}
		}
	}

	// Abort the stragglers: whatever already delivered survives, the
	// rest never blocks the return.
	stragglerCancel()
	return outcome
}
