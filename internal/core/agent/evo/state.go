package evo

import (
	"strings"
)

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
	// baseSystemPrompt is the agent's proven system prompt captured at enter.
	// The reconstructed optimal theory keeps it as the stable core and layers
	// distilled lessons on top, so fixation never discards what already worked.
	baseSystemPrompt string
	// lessons are the distilled principles extracted from successful turns.
	// Deduplicated and order-preserving; each successful iteration appends one.
	lessons []string
	// PendingPrompt is the reconstructed optimal-theory prompt. Each iteration
	// refines it via ReconstructTheory; on exit it is fixed as a revision.
	PendingPrompt string
	// CapturedSkills are emergent skill names observed during the session.
	CapturedSkills []string
}

// LessonDistiller reduces a successful turn (its prompt and last response) to
// a generalized, reusable principle — the unit of emergence. The default
// distiller keeps the trimmed prompt; an LLM-backed distiller can be injected
// via the ObservabilityExtension to produce abstracted principles (the
// hermes GEPA insight: read the trace, propose a targeted improvement). The
// State itself stays model-free so it can be unit-tested without a provider.
type LessonDistiller func(prompt, response string) string

// defaultDistiller keeps the trimmed successful prompt as the lesson. It is
// the zero-overhead baseline; callers wanting genuine emergence inject a
// model-backed distiller.
var defaultDistiller LessonDistiller = func(prompt, _ string) string {
	return strings.TrimSpace(prompt)
}

// distiller is the active lesson-distillation strategy. Overridable via
// SetDistiller; defaults to defaultDistiller.
var distiller LessonDistiller = defaultDistiller

// SetDistiller replaces the global lesson-distillation strategy. The /evo
// wiring calls this once at startup to install a model-backed distiller when
// a judge model is available, and leaves the default (trim) otherwise. This
// is a process-global strategy because the State is value-typed and copied
// (e.g. into the ConfirmExit revision), so a per-State field would not survive.
func SetDistiller(d LessonDistiller) {
	if d == nil {
		d = defaultDistiller
	}
	distiller = d
}

// IsActive reports whether an evo session is running (Active or ConfirmExit).
func (s *State) IsActive() bool {
	return s.Mode == ModeActive || s.Mode == ModeConfirmExit
}

// Enter starts an evo session for the named agent, capturing its proven system
// prompt as the stable core of the reconstructed theory. An empty basePrompt
// is allowed (the theory is then built purely from distilled lessons).
func (s *State) Enter(agentName, baseSystemPrompt string) {
	s.Mode = ModeActive
	s.AgentName = agentName
	s.Iterations = 0
	s.baseSystemPrompt = strings.TrimSpace(baseSystemPrompt)
	s.lessons = nil
	s.PendingPrompt = s.baseSystemPrompt
	s.CapturedSkills = nil
}

// RecordIteration folds one self-iteration's result into the pending state.
// The successful turn's prompt is distilled into a lesson (the actionable
// principle the turn demonstrates) and accumulated; the optimal-theory prompt
// is then reconstructed as base + lessons. newSkills extend the captured set
// (deduplicated).
func (s *State) RecordIteration(successfulPrompt, response string, newSkills []string) {
	s.Iterations++
	lesson := distiller(successfulPrompt, response)
	if lesson != "" {
		seen := false
		for _, existing := range s.lessons {
			if existing == lesson {
				seen = true
				break
			}
		}
		if !seen {
			s.lessons = append(s.lessons, lesson)
		}
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
	s.PendingPrompt = s.ReconstructTheory()
}

// ReconstructTheory synthesizes the固化 system prompt from the proven base
// and the distilled lessons. The base is preserved verbatim (it already
// worked); lessons are appended under a "Refined theory" section so the
// agent keeps what worked and gains the emergent refinements. This is the
// "maintain optimal theory" reconstruction — deterministic and LLM-free at
// this layer; a richer LLM distillation can replace distillLesson later.
func (s *State) ReconstructTheory() string {
	var b strings.Builder
	if s.baseSystemPrompt != "" {
		b.WriteString(s.baseSystemPrompt)
		b.WriteString("\n\n")
	}
	if len(s.lessons) == 0 {
		return strings.TrimSpace(b.String())
	}
	b.WriteString("## Refined theory (")
	b.WriteString(itoa(len(s.lessons)))
	b.WriteString(" lessons distilled across ")
	b.WriteString(itoa(s.Iterations))
	b.WriteString(" iterations)\n\n")
	for _, lesson := range s.lessons {
		b.WriteString("- ")
		b.WriteString(lesson)
		b.WriteByte('\n')
	}
	return strings.TrimSpace(b.String())
}

// itoa is a dependency-free int->string for the reconstruction header.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
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
		SystemPrompt: s.ReconstructTheory(),
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
