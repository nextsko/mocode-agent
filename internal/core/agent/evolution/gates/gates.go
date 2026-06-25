// Package gates provides a quality-gate pipeline that validates evolution
// patch candidates before they are persisted and injected into agent system
// prompts.
//
// The design is adapted from trpc-agent-go's evolution gate chain
// (SpecGate -> SafetyGate -> EffectivenessGate -> HumanGate) but is
// self-contained: the gates package defines its own Candidate, Outcome, and
// Report types so it has zero dependency on the parent evolution package.
// This keeps the gate logic reusable and testable in isolation, and avoids
// the import cycle that would arise if gates imported evolution while the
// Producer (in evolution) imports gates.
//
// Gate chain order is fixed and intentional:
//
//	SpecGate          - deterministic schema/content hygiene (cheap, no I/O).
//	SafetyGate        - deterministic scan for secrets, dangerous shell, and
//	                    path-traversal patterns.
//	EffectivenessGate - outcome-based heuristic (was the producing session
//	                    good enough?).
//	HumanGate         - hold-for-review policy (optional).
//
// The first rejecting gate short-circuits the chain. All gates are optional
// and nil-safe: an unconfigured pipeline passes everything, preserving the
// legacy direct-publish behavior.
package gates

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

// Action classifies what a candidate would do if promoted.
type Action string

const (
	ActionCreate Action = "create"
	ActionUpdate Action = "update"
	ActionDelete Action = "delete"
)

// OutcomeStatus classifies the evaluator verdict for the producing session.
type OutcomeStatus string

const (
	OutcomeUnknown    OutcomeStatus = ""
	OutcomeSuccess    OutcomeStatus = "success"
	OutcomePartial    OutcomeStatus = "partial"
	OutcomeFail       OutcomeStatus = "fail"
	OutcomeAgentError OutcomeStatus = "agent_error"
)

// Outcome carries the producing session's verdict. It drives
// EffectivenessGate decisions. A nil outcome means "no evaluator signal".
type Outcome struct {
	Status OutcomeStatus `json:"status,omitempty"`
	// Score is a normalized metric on a 0..1 scale. nil means "no score".
	Score *float64 `json:"score,omitempty"`
	Notes string    `json:"notes,omitempty"`
}

// Candidate is a proposed evolution patch awaiting gate validation. The
// parent evolution package constructs a Candidate from its own Patch and
// patch-body content, then persists only on a passing verdict.
type Candidate struct {
	Action      Action
	Title       string
	Description string
	// Body is the patch file content that will be persisted and later
	// injected into the agent's system prompt. SafetyGate scans it.
	Body string
	// ExistingTitles are titles of patches already in the store, used by
	// SpecGate for duplicate detection on create actions.
	ExistingTitles []string
	// Outcome is the producing session's verdict (nil = no signal).
	Outcome *Outcome
}

// Verdict is the aggregate result of running a Candidate through the
// Pipeline.
type Verdict struct {
	Passed        bool
	RejectingGate string
	Reasons       []string
}

// Report is the structured output of a single gate.
type Report struct {
	Gate    string
	Passed  bool
	Reasons []string
}

// SpecGate performs deterministic schema/content hygiene on a candidate.
type SpecGate interface {
	Validate(ctx context.Context, c *Candidate) (*Report, error)
}

// SafetyGate scans candidate text for secrets, dangerous shell markers, and
// path-traversal patterns.
type SafetyGate interface {
	Scan(ctx context.Context, c *Candidate) (*Report, error)
}

// EffectivenessGate evaluates whether a candidate is likely to improve or at
// least not regress the agent, based on the producing session's Outcome.
type EffectivenessGate interface {
	Evaluate(ctx context.Context, c *Candidate) (*Report, error)
}

// HumanGate decides whether a candidate that passed all automatic gates
// should be held for human approval. It only decides "should we wait?".
type HumanGate interface {
	ShouldHold(ctx context.Context, c *Candidate) (bool, error)
}

// Pipeline runs a candidate through the fixed gate chain. Gates that are nil
// are skipped, so an empty Pipeline passes everything.
type Pipeline struct {
	Spec          SpecGate
	Safety        SafetyGate
	Effectiveness EffectivenessGate
	Human         HumanGate
}

// Run executes the gate chain in fixed order. The first rejecting gate
// short-circuits. A candidate that passes all automatic gates but is held by
// HumanGate returns Passed=false with RejectingGate="human".
func (p *Pipeline) Run(ctx context.Context, c *Candidate) (*Verdict, error) {
	if c == nil {
		return &Verdict{Passed: false, RejectingGate: "spec", Reasons: []string{"nil candidate"}}, nil
	}
	steps := []struct {
		name string
		fn   func(context.Context, *Candidate) (*Report, error)
	}{
		{"spec", p.specReport},
		{"safety", p.safetyReport},
		{"effectiveness", p.effectivenessReport},
	}
	for _, step := range steps {
		r, err := step.fn(ctx, c)
		if err != nil {
			return nil, fmt.Errorf("gate %s: %w", step.name, err)
		}
		if r != nil && !r.Passed {
			return &Verdict{Passed: false, RejectingGate: r.Gate, Reasons: r.Reasons}, nil
		}
	}
	if p.Human != nil {
		hold, err := p.Human.ShouldHold(ctx, c)
		if err != nil {
			return nil, fmt.Errorf("gate human: %w", err)
		}
		if hold {
			return &Verdict{Passed: false, RejectingGate: "human", Reasons: []string{"held for human approval"}}, nil
		}
	}
	return &Verdict{Passed: true}, nil
}

func (p *Pipeline) specReport(ctx context.Context, c *Candidate) (*Report, error) {
	if p.Spec == nil {
		return &Report{Gate: "spec", Passed: true}, nil
	}
	return p.Spec.Validate(ctx, c)
}

func (p *Pipeline) safetyReport(ctx context.Context, c *Candidate) (*Report, error) {
	if p.Safety == nil {
		return &Report{Gate: "safety", Passed: true}, nil
	}
	return p.Safety.Scan(ctx, c)
}

func (p *Pipeline) effectivenessReport(ctx context.Context, c *Candidate) (*Report, error) {
	if p.Effectiveness == nil {
		return &Report{Gate: "effectiveness", Passed: true}, nil
	}
	return p.Effectiveness.Evaluate(ctx, c)
}

// DefaultSpecGate enforces non-empty title/description and duplicate
// detection on create actions.
type DefaultSpecGate struct {
	MaxTitleLen int // 0 means default 200
}

// NewDefaultSpecGate returns a SpecGate with sane defaults.
func NewDefaultSpecGate() SpecGate { return &DefaultSpecGate{} }

// Validate implements SpecGate.
func (g *DefaultSpecGate) Validate(_ context.Context, c *Candidate) (*Report, error) {
	if c == nil {
		return &Report{Gate: "spec", Passed: false, Reasons: []string{"nil candidate"}}, nil
	}
	if c.Action == ActionDelete {
		return &Report{Gate: "spec", Passed: true}, nil
	}
	reasons := make([]string, 0, 3)
	maxLen := g.MaxTitleLen
	if maxLen <= 0 {
		maxLen = 200
	}
	if strings.TrimSpace(c.Title) == "" {
		reasons = append(reasons, "empty title")
	}
	if len(c.Title) > maxLen {
		reasons = append(reasons, fmt.Sprintf("title longer than %d chars", maxLen))
	}
	if strings.TrimSpace(c.Description) == "" {
		reasons = append(reasons, "missing description")
	}
	if c.Action == ActionCreate {
		cand := canonicalTitle(c.Title)
		for _, ex := range c.ExistingTitles {
			if canonicalTitle(ex) == cand {
				reasons = append(reasons, fmt.Sprintf("duplicate of existing patch %q; should be an update", ex))
				break
			}
		}
	}
	return &Report{Gate: "spec", Passed: len(reasons) == 0, Reasons: reasons}, nil
}

var alnumSqueeze = regexp.MustCompile(`[^a-z0-9]+`)

// canonicalTitle lowercases and squeezes non-alphanumeric runs to "-" for
// cheap duplicate detection.
func canonicalTitle(name string) string {
	return alnumSqueeze.ReplaceAllString(strings.ToLower(strings.TrimSpace(name)), "-")
}

// OutcomeBasedEffectivenessGate is the default EffectivenessGate.
type OutcomeBasedEffectivenessGate struct {
	MinScore     float64 // 0 means "no score threshold"
	RejectOnFail bool    // reject candidates from fail/agent_error sessions
}

// NewOutcomeBasedEffectivenessGate returns a gate with defaults.
func NewOutcomeBasedEffectivenessGate() EffectivenessGate {
	return &OutcomeBasedEffectivenessGate{MinScore: 0.8, RejectOnFail: true}
}

// Evaluate implements EffectivenessGate.
func (g *OutcomeBasedEffectivenessGate) Evaluate(_ context.Context, c *Candidate) (*Report, error) {
	if c == nil {
		return &Report{Gate: "effectiveness", Passed: false, Reasons: []string{"nil candidate"}}, nil
	}
	if c.Action == ActionDelete || c.Outcome == nil {
		return &Report{Gate: "effectiveness", Passed: true}, nil
	}
	reasons := make([]string, 0, 2)
	if g.RejectOnFail {
		switch c.Outcome.Status {
		case OutcomeFail, OutcomeAgentError:
			reasons = append(reasons, fmt.Sprintf("session outcome is %q; candidate held for manual review", c.Outcome.Status))
		}
	}
	if g.MinScore > 0 && c.Outcome.Score != nil && *c.Outcome.Score < g.MinScore {
		reasons = append(reasons, fmt.Sprintf("session score %.1f is below threshold %.1f", *c.Outcome.Score, g.MinScore))
	}
	return &Report{Gate: "effectiveness", Passed: len(reasons) == 0, Reasons: reasons}, nil
}

// AlwaysHoldGate holds every candidate for human review.
type AlwaysHoldGate struct{}

// NewAlwaysHoldGate returns a HumanGate that holds all candidates.
func NewAlwaysHoldGate() HumanGate { return &AlwaysHoldGate{} }

// ShouldHold implements HumanGate.
func (g *AlwaysHoldGate) ShouldHold(_ context.Context, _ *Candidate) (bool, error) {
	return true, nil
}

// CreateOnlyHoldGate holds new patches for human review but auto-passes updates.
type CreateOnlyHoldGate struct{}

// NewCreateOnlyHoldGate returns a HumanGate that only holds creates.
func NewCreateOnlyHoldGate() HumanGate { return &CreateOnlyHoldGate{} }

// ShouldHold implements HumanGate.
func (g *CreateOnlyHoldGate) ShouldHold(_ context.Context, c *Candidate) (bool, error) {
	return c != nil && c.Action == ActionCreate, nil
}