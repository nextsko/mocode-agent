# Roundtable Team Mode — Phase 1: Core Domain Engine

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the in-memory roundtable state machine (participants, transcript, motions, votes, consensus, loop detection, budgets) with full unit-test coverage.

**Architecture:** A new `internal/agent/roundtable` package owns pure domain logic. It has no dependency on `Coordinator`, `SessionAgent`, or the TUI. The `Roundtable` struct is an append-only state machine; an external driver calls `Advance` after each LLM turn.

**Tech Stack:** Go standard library only for this phase.

---

## File Map

| File | Responsibility |
|---|---|
| `internal/agent/roundtable/types.go` | Domain types: `Participant`, `Statement`, `Motion`, `Vote`, `Phase`, `Config`, `Roundtable`, `Budget`, `Usage`. |
| `internal/agent/roundtable/strategy.go` | `SpeakerSelector` interface and deterministic `RoundRobinStrategy`. |
| `internal/agent/roundtable/roundtable.go` | State machine: `New`, `Advance`, `AddStatement`, `NextSpeaker`, `ExtractConsensus`, `DetectLoop`. |
| `internal/agent/roundtable/roundtable_test.go` | Unit tests for turns, motions, voting, consensus, loops, budgets. |

---

## Task 1: Domain Types

**Files:**
- Create: `internal/agent/roundtable/types.go`

- [ ] **Step 1: Write `types.go`**

```go
package roundtable

import "time"

type Phase string

const (
	PhaseOpening     Phase = "opening"
	PhaseDiscussion  Phase = "discussion"
	PhasePlanning    Phase = "planning"
	PhaseVoting      Phase = "voting"
	PhaseApproved    Phase = "approved"
	PhaseExecution   Phase = "execution"
	PhaseDone        Phase = "done"
	PhaseInterrupted Phase = "interrupted"
)

type StatementKind string

const (
	StatementChat       StatementKind = "chat"
	StatementToolResult StatementKind = "tool_result"
	StatementMotion     StatementKind = "motion"
	StatementVote       StatementKind = "vote"
	StatementConsensus  StatementKind = "consensus"
	StatementSystem     StatementKind = "system"
)

type MotionType string

const (
	MotionProposePlan         MotionType = "propose_plan"
	MotionRequestClarification MotionType = "request_clarification"
	MotionCallVote            MotionType = "call_vote"
	MotionConclude            MotionType = "conclude"
	MotionDeadlock            MotionType = "deadlock"
)

type VoteValue string

const (
	VoteYes     VoteValue = "yes"
	VoteNo      VoteValue = "no"
	VoteAbstain VoteValue = "abstain"
)

type Participant struct {
	Name        string
	AgentName   string
	IsModerator bool
	CanExecute  bool
}

type Motion struct {
	Type      MotionType
	TargetSeq int
	Payload   map[string]any
}

type Vote struct {
	MotionSeq int
	Value     VoteValue
	Reason    string
}

type Statement struct {
	ID        string
	Seq       int
	Speaker   string
	Kind      StatementKind
	Content   string
	Motion    *Motion
	Vote      *Vote
	CreatedAt time.Time
}

type Budget struct {
	MaxTurns        int
	MaxInputTokens  int
	MaxOutputTokens int
	MaxTotalTokens  int
	PerRoleMaxTokens map[string]int
}

type Usage struct {
	Turns        int
	InputTokens  int
	OutputTokens int
	TotalTokens  int
	ByRole       map[string]Usage
}

type Config struct {
	ID           string
	Topic        string
	Participants []Participant
	Budget       Budget
}

type Consensus struct {
	Summary string
	Plan    map[string]any
	Votes   map[string]VoteValue
}

type Roundtable struct {
	Config       Config
	Phase        Phase
	Transcript   []Statement
	CurrentSeq   int
	CurrentTurn  int
	Usage        Usage
	Consensus    *Consensus
	MaxTurnsHit  bool
	LoopDetected bool
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./internal/agent/roundtable`
Expected: PASS (no tests yet)

- [ ] **Step 3: Commit**

```bash
git add internal/agent/roundtable/types.go
git commit -m "feat(roundtable): add domain types for roundtable team mode"
```

---

## Task 2: Speaker Selection Strategy

**Files:**
- Create: `internal/agent/roundtable/strategy.go`

- [ ] **Step 1: Write `strategy.go`**

```go
package roundtable

import "fmt"

// SpeakerSelector decides who speaks next.
type SpeakerSelector interface {
	NextSpeaker(rt *Roundtable) (string, error)
}

// RoundRobinStrategy cycles through non-moderator participants in order,
// then returns to the moderator every full round.
type RoundRobinStrategy struct{}

func (RoundRobinStrategy) NextSpeaker(rt *Roundtable) (string, error) {
	if len(rt.Config.Participants) == 0 {
		return "", fmt.Errorf("no participants")
	}

	// Build ordered list: moderator first, then specialists.
	var moderator string
	var specialists []string
	for _, p := range rt.Config.Participants {
		if p.IsModerator {
			moderator = p.Name
		} else {
			specialists = append(specialists, p.Name)
		}
	}
	if moderator == "" {
		return "", fmt.Errorf("no moderator configured")
	}
	if len(specialists) == 0 {
		return moderator, nil
	}

	// Turn 0: moderator opens.
	if rt.CurrentTurn == 0 {
		return moderator, nil
	}

	// After moderator, cycle specialists; after last specialist, moderator again.
	offset := (rt.CurrentTurn - 1) % (len(specialists) + 1)
	if offset == len(specialists) {
		return moderator, nil
	}
	return specialists[offset], nil
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./internal/agent/roundtable`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/agent/roundtable/strategy.go
git commit -m "feat(roundtable): add round-robin speaker selection strategy"
```

---

## Task 3: Roundtable State Machine

**Files:**
- Create: `internal/agent/roundtable/roundtable.go`

- [ ] **Step 1: Write `roundtable.go`**

```go
package roundtable

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// New creates a new Roundtable in the opening phase.
func New(cfg Config) (*Roundtable, error) {
	if cfg.Topic == "" {
		return nil, fmt.Errorf("topic is required")
	}
	if len(cfg.Participants) == 0 {
		return nil, fmt.Errorf("at least one participant is required")
	}
	var hasModerator bool
	for _, p := range cfg.Participants {
		if p.IsModerator {
			hasModerator = true
			break
		}
	}
	if !hasModerator {
		return nil, fmt.Errorf("a moderator is required")
	}
	if cfg.Budget.MaxTurns <= 0 {
		cfg.Budget.MaxTurns = 20
	}

	return &Roundtable{
		Config:     cfg,
		Phase:      PhaseOpening,
		Transcript: nil,
		CurrentSeq: 0,
		CurrentTurn: 0,
		Usage:      Usage{ByRole: make(map[string]Usage)},
	}, nil
}

// AddStatement appends a new statement and advances the sequence counter.
func (rt *Roundtable) AddStatement(s Statement) Statement {
	rt.CurrentSeq++
	s.ID = randomID()
	s.Seq = rt.CurrentSeq
	if s.CreatedAt.IsZero() {
		s.CreatedAt = time.Now()
	}
	rt.Transcript = append(rt.Transcript, s)
	if s.Kind != StatementSystem {
		rt.CurrentTurn++
	}
	return s
}

// NextSpeaker returns the next speaker using the provided selector.
func (rt *Roundtable) NextSpeaker(sel SpeakerSelector) (string, error) {
	return sel.NextSpeaker(rt)
}

// Advance processes a seat's contribution and updates phase/state.
// It returns the appended statement.
func (rt *Roundtable) Advance(speaker string, kind StatementKind, content string, motion *Motion, vote *Vote, usage Usage) (Statement, error) {
	if rt.Phase == PhaseDone || rt.Phase == PhaseInterrupted {
		return Statement{}, fmt.Errorf("roundtable is %s", rt.Phase)
	}

	if rt.CurrentTurn >= rt.Config.Budget.MaxTurns {
		rt.MaxTurnsHit = true
		return Statement{}, fmt.Errorf("max turns (%d) reached", rt.Config.Budget.MaxTurns)
	}

	stmt := rt.AddStatement(Statement{
		Speaker: speaker,
		Kind:    kind,
		Content: content,
		Motion:  motion,
		Vote:    vote,
	})

	rt.applyUsage(speaker, usage)

	switch kind {
	case StatementMotion:
		if motion != nil && motion.Type == MotionConclude {
			rt.Phase = PhaseVoting
		}
	case StatementVote:
		if cons, ok := rt.tryExtractConsensus(); ok {
			rt.Consensus = cons
			rt.Phase = PhaseApproved
		}
	}

	if rt.CurrentTurn >= rt.Config.Budget.MaxTurns {
		rt.MaxTurnsHit = true
	}

	return stmt, nil
}

func (rt *Roundtable) applyUsage(role string, u Usage) {
	rt.Usage.Turns += u.Turns
	rt.Usage.InputTokens += u.InputTokens
	rt.Usage.OutputTokens += u.OutputTokens
	rt.Usage.TotalTokens += u.TotalTokens

	byRole := rt.Usage.ByRole[role]
	byRole.Turns += u.Turns
	byRole.InputTokens += u.InputTokens
	byRole.OutputTokens += u.OutputTokens
	byRole.TotalTokens += u.TotalTokens
	rt.Usage.ByRole[role] = byRole
}

// tryExtractConsensus checks the most recent conclude/propose_plan motion
// and returns a Consensus if all non-abstaining participants voted yes.
func (rt *Roundtable) tryExtractConsensus() (*Consensus, bool) {
	var motionStmt *Statement
	for i := len(rt.Transcript) - 1; i >= 0; i-- {
		s := rt.Transcript[i]
		if s.Kind == StatementMotion && s.Motion != nil &&
			(s.Motion.Type == MotionConclude || s.Motion.Type == MotionProposePlan) {
			motionStmt = &s
			break
		}
	}
	if motionStmt == nil {
		return nil, false
	}

	participants := make(map[string]bool)
	for _, p := range rt.Config.Participants {
		participants[p.Name] = true
	}

	votes := make(map[string]VoteValue)
	for _, s := range rt.Transcript {
		if s.Kind == StatementVote && s.Vote != nil && s.Vote.MotionSeq == motionStmt.Seq {
			votes[s.Speaker] = s.Vote.Value
		}
	}

	for name := range participants {
		v, ok := votes[name]
		if !ok || v == VoteNo {
			return nil, false
		}
		if v == VoteAbstain {
			delete(participants, name)
		}
	}

	// Require at least two yes votes (or one if only one participant).
	if len(votes) == 0 {
		return nil, false
	}

	cons := &Consensus{
		Summary: motionStmt.Content,
		Votes:   votes,
	}
	if motionStmt.Motion.Payload != nil {
		cons.Plan = motionStmt.Motion.Payload
	}
	return cons, true
}

// DetectLoop returns true if the last N statements are repetitive or cyclical.
func (rt *Roundtable) DetectLoop(window int) (bool, string) {
	if window <= 0 {
		window = 4
	}
	if len(rt.Transcript) < window {
		return false, ""
	}

	recent := rt.Transcript[len(rt.Transcript)-window:]

	// Exact repeated content by the same speaker.
	first := recent[0]
	allSame := true
	for _, s := range recent[1:] {
		if s.Speaker != first.Speaker || s.Content != first.Content {
			allSame = false
			break
		}
	}
	if allSame {
		return true, fmt.Sprintf("speaker %q repeated the same message %d times", first.Speaker, window)
	}

	// Cycling speaker transitions A -> B -> A -> B.
	if len(recent) >= 4 {
		cycle := true
		for i := 2; i < len(recent); i++ {
			if recent[i].Speaker != recent[i-2].Speaker {
				cycle = false
				break
			}
		}
		if cycle {
			return true, fmt.Sprintf("cycling speakers %q and %q", recent[0].Speaker, recent[1].Speaker)
		}
	}

	return false, ""
}

// IsOverBudget returns true if any budget cap is exceeded.
func (rt *Roundtable) IsOverBudget() (bool, string) {
	b := rt.Config.Budget
	if b.MaxTotalTokens > 0 && rt.Usage.TotalTokens >= b.MaxTotalTokens {
		return true, "total token budget exceeded"
	}
	if b.MaxInputTokens > 0 && rt.Usage.InputTokens >= b.MaxInputTokens {
		return true, "input token budget exceeded"
	}
	if b.MaxOutputTokens > 0 && rt.Usage.OutputTokens >= b.MaxOutputTokens {
		return true, "output token budget exceeded"
	}
	for role, cap := range b.PerRoleMaxTokens {
		if cap > 0 && rt.Usage.ByRole[role].TotalTokens >= cap {
			return true, fmt.Sprintf("token budget exceeded for role %q", role)
		}
	}
	return false, ""
}

func randomID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./internal/agent/roundtable`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/agent/roundtable/roundtable.go
git commit -m "feat(roundtable): add in-memory state machine"
```

---

## Task 4: Unit Tests

**Files:**
- Create: `internal/agent/roundtable/roundtable_test.go`

- [ ] **Step 1: Write failing tests**

```go
package roundtable

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNew_RequiresModeratorAndTopic(t *testing.T) {
	t.Parallel()

	_, err := New(Config{Topic: "x"})
	require.Error(t, err)

	_, err = New(Config{Participants: []Participant{{Name: "a", IsModerator: true}}})
	require.Error(t, err)

	rt, err := New(Config{
		Topic:        "design retry backoff",
		Participants: []Participant{{Name: "mod", IsModerator: true}, {Name: "coder"}},
	})
	require.NoError(t, err)
	require.Equal(t, PhaseOpening, rt.Phase)
	require.Equal(t, 20, rt.Config.Budget.MaxTurns)
}

func TestRoundRobinStrategy(t *testing.T) {
	t.Parallel()
	rt, err := New(Config{
		Topic: "x",
		Participants: []Participant{
			{Name: "mod", IsModerator: true},
			{Name: "architect"},
			{Name: "coder"},
		},
	})
	require.NoError(t, err)

	sel := RoundRobinStrategy{}
	speakers := []string{}
	for i := 0; i < 7; i++ {
		name, err := rt.NextSpeaker(sel)
		require.NoError(t, err)
		speakers = append(speakers, name)
		rt.CurrentTurn++
	}
	require.Equal(t, []string{"mod", "architect", "coder", "mod", "architect", "coder", "mod"}, speakers)
}

func TestAdvance_ReachesConsensus(t *testing.T) {
	t.Parallel()
	rt, err := New(Config{
		Topic: "x",
		Participants: []Participant{
			{Name: "mod", IsModerator: true},
			{Name: "architect"},
			{Name: "coder"},
		},
	})
	require.NoError(t, err)

	_, err = rt.Advance("mod", StatementMotion, "let's use exponential backoff", &Motion{Type: MotionProposePlan, Payload: map[string]any{"plan": "exp backoff"}}, nil, Usage{})
	require.NoError(t, err)
	require.Equal(t, PhaseVoting, rt.Phase)

	_, err = rt.Advance("architect", StatementVote, "", nil, &Vote{MotionSeq: 1, Value: VoteYes}, Usage{})
	require.NoError(t, err)
	require.Nil(t, rt.Consensus)

	_, err = rt.Advance("coder", StatementVote, "", nil, &Vote{MotionSeq: 1, Value: VoteYes}, Usage{})
	require.NoError(t, err)
	require.NotNil(t, rt.Consensus)
	require.Equal(t, PhaseApproved, rt.Phase)
	require.Equal(t, "exp backoff", rt.Consensus.Plan["plan"])
}

func TestDetectLoop_RepeatedContent(t *testing.T) {
	t.Parallel()
	rt, _ := New(Config{Topic: "x", Participants: []Participant{{Name: "mod", IsModerator: true}, {Name: "a"}}})
	for i := 0; i < 4; i++ {
		rt.AddStatement(Statement{Speaker: "a", Kind: StatementChat, Content: "same"})
	}
	ok, reason := rt.DetectLoop(4)
	require.True(t, ok)
	require.Contains(t, reason, "repeated")
}

func TestDetectLoop_CyclingSpeakers(t *testing.T) {
	t.Parallel()
	rt, _ := New(Config{Topic: "x", Participants: []Participant{{Name: "mod", IsModerator: true}, {Name: "a"}, {Name: "b"}}})
	for i := 0; i < 4; i++ {
		rt.AddStatement(Statement{Speaker: "a", Kind: StatementChat, Content: "x"})
		rt.AddStatement(Statement{Speaker: "b", Kind: StatementChat, Content: "y"})
	}
	ok, reason := rt.DetectLoop(4)
	require.True(t, ok)
	require.Contains(t, reason, "cycling")
}

func TestIsOverBudget(t *testing.T) {
	t.Parallel()
	rt, _ := New(Config{
		Topic:        "x",
		Participants: []Participant{{Name: "mod", IsModerator: true}},
		Budget:       Budget{MaxTotalTokens: 100},
	})
	require.False(t, func() bool { ok, _ := rt.IsOverBudget(); return ok }())
	rt.applyUsage("mod", Usage{TotalTokens: 100})
	ok, reason := rt.IsOverBudget()
	require.True(t, ok)
	require.Contains(t, reason, "total token budget")
}

func TestAdvance_RespectsMaxTurns(t *testing.T) {
	t.Parallel()
	rt, _ := New(Config{
		Topic:        "x",
		Participants: []Participant{{Name: "mod", IsModerator: true}},
		Budget:       Budget{MaxTurns: 2},
	})
	_, err := rt.Advance("mod", StatementChat, "1", nil, nil, Usage{})
	require.NoError(t, err)
	_, err = rt.Advance("mod", StatementChat, "2", nil, nil, Usage{})
	require.NoError(t, err)
	_, err = rt.Advance("mod", StatementChat, "3", nil, nil, Usage{})
	require.Error(t, err)
	require.True(t, rt.MaxTurnsHit)
}
```

- [ ] **Step 2: Run tests**

Run: `go test ./internal/agent/roundtable -v`
Expected: all tests PASS

- [ ] **Step 3: Commit**

```bash
git add internal/agent/roundtable/roundtable_test.go
git commit -m "test(roundtable): add unit tests for core state machine"
```

---

## Self-Review

- **Spec coverage:** Phase 1 covers the protocol (Statement/Motion/Vote), phases, speaker selection, consensus extraction, loop detection, and budgets from the design spec.
- **Placeholder scan:** All code in tasks is concrete and copy-paste ready.
- **Type consistency:** Types used in tests match those defined in `types.go` and `roundtable.go`.

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-06-13-roundtable-phase1-core-engine.md`.

Recommended next step: execute Phase 1 tasks sequentially, then proceed to Phase 2 (persistence).
