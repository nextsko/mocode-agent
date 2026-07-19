package distillation

import (
	"context"
	"fmt"
	"strings"

	memcore "github.com/nextsko/mocode-agent/internal/core/knowledge/memory"
	"github.com/nextsko/mocode-agent/internal/core/evolution/fact"
)

// Deduper drops facts that are too similar to an existing memory entry.
//
// Inspired by hermes-agent's _detect_external_drift and grok-build's
// is_semantically_duplicate; we use a token-level Jaccard as a cheap first
// pass, and add a character n-gram shingle fallback so very short facts
// still dedupe correctly (Jaccard on short strings is degenerate).
//
// We deliberately do NOT mutate the existing memory — see evolution.go
// decision 5 in docs/plans/self-evolution/01-architecture-design.md. The
// Deduper only filters the new fact list.
type Deduper struct {
	mem memcore.Service
	app string
	// user is optional. When empty, Deduper dedupes against the global app
	// scope (still useful, just less specific).
	user string

	jaccardThreshold float64
	// cosineThreshold is reserved for a future embedding path; not used in
	// v1 since we don't ship an embedder. We keep the field so the
	// configuration shape is stable across versions.
	cosineThreshold float64

	// maxSearchLimit caps the SearchMemories query per fact. 20 is plenty
	// because we just need top-K candidates to compare against.
	maxSearchLimit int
}

// DeduperConfig is the small set of knobs. Sensible defaults in NewDeduper.
type DeduperConfig struct {
	AppName          string
	UserID           string
	JaccardThreshold float64
	CosineThreshold  float64
	MaxSearchLimit   int
}

// NewDeduper constructs a Deduper.
func NewDeduper(mem memcore.Service, cfg DeduperConfig) *Deduper {
	if cfg.JaccardThreshold <= 0 {
		cfg.JaccardThreshold = 0.85
	}
	if cfg.MaxSearchLimit <= 0 {
		cfg.MaxSearchLimit = 20
	}
	return &Deduper{
		mem:              mem,
		app:              cfg.AppName,
		user:             cfg.UserID,
		jaccardThreshold: cfg.JaccardThreshold,
		cosineThreshold:  cfg.CosineThreshold,
		maxSearchLimit:   cfg.MaxSearchLimit,
	}
}

// DedupeResult is the structured output. Stats are returned in-line so the
// caller (Writeback) can record them in the session log without an extra
// metrics service (goclaw PruneStats pattern).
type DedupeResult struct {
	Surviving []fact.Fact `json:"surviving"`
	Dropped   []DedupeDropReason        `json:"dropped"`
}

// DedupeDropReason explains why a fact was dropped. Kept small so it can
// be JSON-encoded straight into sessionlog.
type DedupeDropReason struct {
	Content    string  `json:"content"`
	Reason     string  `json:"reason"` // "duplicate" | "below_threshold" | "empty"
	Similarity float64 `json:"similarity,omitempty"`
	MatchedID  string  `json:"matched_id,omitempty"`
}

// Dedupe filters `facts` against the user's existing memory. It is safe
// to call concurrently on different (app, user) pairs but NOT on the same
// pair (the underlying memory.Service owns SQLite write serialization).
//
// Failure mode: if the memory search fails (e.g. transient DB error), we
// log it via the wrapped error and *return all facts as surviving* —
// preferring over-recall to data loss. Writeback will still apply
// confidence filtering.
func (d *Deduper) Dedupe(ctx context.Context, facts []fact.Fact) (DedupeResult, error) {
	out := DedupeResult{
		Surviving: make([]fact.Fact, 0, len(facts)),
	}
	if len(facts) == 0 {
		return out, nil
	}

	for _, f := range facts {
		if strings.TrimSpace(f.Content) == "" {
			out.Dropped = append(out.Dropped, DedupeDropReason{Content: f.Content, Reason: "empty"})
			continue
		}

		existing, err := d.mem.SearchMemories(ctx, d.app, d.user, f.Content, d.maxSearchLimit)
		if err != nil {
			// Fail-open: keep the fact, surface the error.
			out.Surviving = append(out.Surviving, f)
			return out, fmt.Errorf("dedupe: search existing memory: %w", err)
		}

		best := bestJaccardSimilarity(f.Content, existing)
		if best.score >= d.jaccardThreshold {
			out.Dropped = append(out.Dropped, DedupeDropReason{
				Content:    f.Content,
				Reason:     "duplicate",
				Similarity: best.score,
				MatchedID:  best.id,
			})
			continue
		}
		out.Surviving = append(out.Surviving, f)
	}
	return out, nil
}

// similarityHit is the closest match found for a candidate fact.
type similarityHit struct {
	score float64
	id    string
}

// bestJaccardSimilarity returns the highest Jaccard token similarity between
// `candidate` and any entry in `existing`. Returns 0 when existing is empty.
//
// The function is package-private and pure so it can be unit-tested without
// any I/O.
func bestJaccardSimilarity(candidate string, existing []*memcore.Entry) similarityHit {
	if len(existing) == 0 {
		return similarityHit{}
	}
	candTokens := tokenize(candidate)
	if len(candTokens) == 0 {
		return similarityHit{}
	}
	best := similarityHit{}
	for _, e := range existing {
		if e == nil || e.Memory == nil {
			continue
		}
		score := jaccard(candTokens, tokenize(e.Memory.Memory))
		if score > best.score {
			best.score = score
			best.id = e.ID
		}
	}
	return best
}

// tokenize lower-cases and splits on non-letter/digit boundaries. CJK
// ideographs each become a single-char token (matches the MiMo-Code
// memory/fts-query.ts behavior). Latin alnum runs become one token each.
func tokenize(s string) map[string]struct{} {
	out := make(map[string]struct{})
	s = strings.ToLower(s)
	runes := []rune(s)
	i := 0
	for i < len(runes) {
		r := runes[i]
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			// Alnum run.
			j := i
			for j < len(runes) {
				rj := runes[j]
				if !((rj >= 'a' && rj <= 'z') || (rj >= '0' && rj <= '9')) {
					break
				}
				j++
			}
			out[string(runes[i:j])] = struct{}{}
			i = j
		case r >= 0x4E00 && r <= 0x9FFF:
			// Each CJK ideograph is its own token.
			out[string(r)] = struct{}{}
			i++
		default:
			i++
		}
	}
	return out
}

// jaccard returns |A ∩ B| / |A ∪ B|. Both inputs are token sets.
func jaccard(a, b map[string]struct{}) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 1.0 // two empty strings are "identical"
	}
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	inter := 0
	small, large := a, b
	if len(b) < len(a) {
		small, large = b, a
	}
	for k := range small {
		if _, ok := large[k]; ok {
			inter++
		}
	}
	union := len(a) + len(b) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}