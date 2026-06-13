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
		Config:      cfg,
		Phase:       PhaseOpening,
		Transcript:  nil,
		CurrentSeq:  0,
		CurrentTurn: 0,
		Usage:       Usage{ByRole: make(map[string]RoleUsage)},
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
		if motion != nil && (motion.Type == MotionConclude || motion.Type == MotionProposePlan) {
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
// and returns a Consensus when all non-abstaining participants voted yes.
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
	// The motion author is considered an implicit yes.
	votes[motionStmt.Speaker] = VoteYes
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

// DetectLoop returns true if the last window of statements is repetitive or cyclical.
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
