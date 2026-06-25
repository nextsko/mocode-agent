package evo

// Mode is the lifecycle state of a /evo session.
type Mode string

const (
	// ModeInactive means no evo session is running; the default theme and
	// agent are in effect.
	ModeInactive Mode = "inactive"
	// ModeActive means a self-evolution session is running: the evo theme is
	// shown, the agent iterates on itself, and observability extensions run.
	ModeActive Mode = "active"
	// ModeConfirmExit means the user requested to leave the evo session and
	// must confirm a second time (second-confirmation exit) so an accidental
	// key does not discard an in-progress fixation.
	ModeConfirmExit Mode = "confirm_exit"
)

// State holds the runtime state of a /evo session. It is owned by the TUI
// model; the core package keeps it mode-agnostic so it can be unit-tested
// without a Bubble Tea program.
type State struct {
	// Mode is the current lifecycle phase.
	Mode Mode
	// AgentName is the agent being tamed in this session. Empty until the
	// user names it (or on first fixation).
	AgentName string
	// Iterations counts how many self-iteration rounds have run.
	Iterations int
	// PendingPrompt is the optimal-theory prompt reconstructed so far. Each
	// iteration may refine it; on exit it is fixed as a revision.
	PendingPrompt string
	// CapturedSkills are emergent skill names observed during the session.
	CapturedSkills []string
}

// IsActive reports whether an evo session is running (Active or ConfirmExit).
func (s *State) IsActive() bool {
	return s.Mode == ModeActive || s.Mode == ModeConfirmExit
}

// Enter starts an evo session for the named agent, moving to ModeActive.
func (s *State) Enter(agentName string) {
	s.Mode = ModeActive
	s.AgentName = agentName
	s.Iterations = 0
	s.PendingPrompt = ""
	s.CapturedSkills = nil
}

// RecordIteration folds one self-iteration's result into the pending state:
// the refined optimal-theory prompt and any newly-emergent skills.
func (s *State) RecordIteration(refinedPrompt string, newSkills []string) {
	s.Iterations++
	if refinedPrompt != "" {
		s.PendingPrompt = refinedPrompt
	}
	for _, skill := range newSkills {
		if skill == "" {
			continue
		}
		seen := false
		for _, existing := range s.CapturedSkills {
			if existing == skill {
				seen = true
				break
			}
		}
		if !seen {
			s.CapturedSkills = append(s.CapturedSkills, skill)
		}
	}
}

// RequestExit moves an active session into the second-confirmation phase.
// It returns false (not handled) if no session is active.
func (s *State) RequestExit() bool {
	if s.Mode != ModeActive {
		return false
	}
	s.Mode = ModeConfirmExit
	return true
}

// ConfirmExit completes the exit, returning the snapshot to fix and resetting
// to inactive. The caller fixes the returned revision if it is non-zero.
func (s *State) ConfirmExit() Revision {
	rev := Revision{
		AgentName:    s.AgentName,
		SystemPrompt: s.PendingPrompt,
		Skills:       append([]string(nil), s.CapturedSkills...),
		Reason:       "evo session exit",
	}
	*s = State{Mode: ModeInactive}
	return rev
}

// CancelExit aborts the second-confirmation and returns to ModeActive.
func (s *State) CancelExit() {
	if s.Mode == ModeConfirmExit {
		s.Mode = ModeActive
	}
}
