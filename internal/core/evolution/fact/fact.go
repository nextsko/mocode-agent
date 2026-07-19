// Package fact defines the ExtractedFact value type shared by all
// evolution stages (reviewer, dreamer, writeback, dedupe).
//
// It is split out into its own package to break an import cycle between
// the top-level evolution package (which exposes the System handle) and
// the distillation subpackage (which performs the actual LLM extraction).
package fact

import (
	"time"

	memdom "github.com/nextsko/mocode-agent/internal/domain/memory"
)

// Kind is the high-level category of an extracted fact. Mapped to the
// existing memdom.Kind values.
type Kind string

const (
	// KindUserPref captures a persistent user preference / habit.
	KindUserPref Kind = "user_pref"
	// KindProjectRule captures a project-specific rule / convention.
	KindProjectRule Kind = "project_rule"
	// KindVerifiedFact captures a domain fact the user confirmed.
	KindVerifiedFact Kind = "verified_fact"
	// KindEpisode captures a one-off event worth remembering.
	KindEpisode Kind = "episode"
)

// ToMemoryKind maps a Kind to the existing memdom.Kind constant. Unknown
// kinds fall back to KindFact (safe default; episodes should be
// explicitly tagged).
func (k Kind) ToMemoryKind() memdom.Kind {
	switch k {
	case KindEpisode:
		return memdom.KindEpisode
	default:
		return memdom.KindFact
	}
}

// ParseKind accepts the LLM-emitted kind string and returns the matching
// Kind constant. Returns "" when the input is unrecognized.
func ParseKind(s string) Kind {
	switch s {
	case "user_pref":
		return KindUserPref
	case "project_rule":
		return KindProjectRule
	case "verified_fact":
		return KindVerifiedFact
	case "episode":
		return KindEpisode
	default:
		return ""
	}
}

// Fact is the structured output of an LLM extraction pass. Confidence
// is in [0, 1] and the Writeback layer filters out anything below
// MinConfidence.
type Fact struct {
	Content     string    `json:"content"`
	Topics      []string  `json:"topics,omitempty"`
	Kind        Kind      `json:"kind"`
	Confidence  float64   `json:"confidence"`
	SourceTurns []string  `json:"source_turns,omitempty"`
	At          time.Time `json:"at"`
}