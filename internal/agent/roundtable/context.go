package roundtable

import (
	"fmt"
	"strings"
)

// TurnPrompt contains the assembled prompt for one seat's turn.
type TurnPrompt struct {
	SystemPrompt string
	Context      string
	Instructions string
}

// DefaultReadOnlyTools lists tools that may be used during the discussion phase.
var DefaultReadOnlyTools = map[string]bool{
	"view":            true,
	"read_files":      true,
	"grep":            true,
	"glob":            true,
	"fetch":           true,
	"lsp_diagnostics": true,
	"lsp_references":  true,
	"mocode_info":     true,
	"mocode_logs":     true,
	"think":           true,
}

// ContextBuilder assembles a bounded prompt for a participant.
type ContextBuilder struct {
	// RecentTurns is the maximum number of transcript entries to include.
	RecentTurns int
}

// NewContextBuilder creates a builder with sensible defaults.
func NewContextBuilder() *ContextBuilder {
	return &ContextBuilder{RecentTurns: 8}
}

// BuildTurnPrompt creates a prompt for the given speaker.
func (b *ContextBuilder) BuildTurnPrompt(rt *Roundtable, speaker string) (TurnPrompt, error) {
	participant, err := rt.findParticipant(speaker)
	if err != nil {
		return TurnPrompt{}, err
	}

	system := fmt.Sprintf("You are %s at a Mocode roundtable meeting. %s", speaker, roleInstructions(participant))

	var parts []string
	parts = append(parts, fmt.Sprintf("Topic: %s", rt.Config.Topic))
	parts = append(parts, fmt.Sprintf("Current phase: %s", rt.Phase))

	if active := b.activeMotion(rt); active != nil {
		parts = append(parts, fmt.Sprintf("Active motion (#%d): %s", active.Seq, active.Content))
	}

	recent := b.recentTranscript(rt)
	if len(recent) > 0 {
		parts = append(parts, "", "Recent discussion:")
		for _, s := range recent {
			parts = append(parts, fmt.Sprintf("  [%s] %s: %s", s.Kind, s.Speaker, strings.ReplaceAll(s.Content, "\n", " ")))
		}
	}

	context := strings.Join(parts, "\n")

	instructions := `Speak as your role. Be concise. If you want to propose a plan or end the meeting, use a formal motion. During discussion you may only use read-only tools; do not write files or execute commands.`

	return TurnPrompt{
		SystemPrompt: system,
		Context:      context,
		Instructions: instructions,
	}, nil
}

func (b *ContextBuilder) activeMotion(rt *Roundtable) *Statement {
	for i := len(rt.Transcript) - 1; i >= 0; i-- {
		s := rt.Transcript[i]
		if s.Kind == StatementMotion && s.Motion != nil &&
			(s.Motion.Type == MotionConclude || s.Motion.Type == MotionProposePlan) {
			return &s
		}
	}
	return nil
}

func (b *ContextBuilder) recentTranscript(rt *Roundtable) []Statement {
	if len(rt.Transcript) <= b.RecentTurns {
		return rt.Transcript
	}
	return rt.Transcript[len(rt.Transcript)-b.RecentTurns:]
}

func (rt *Roundtable) findParticipant(name string) (Participant, error) {
	for _, p := range rt.Config.Participants {
		if p.Name == name {
			return p, nil
		}
	}
	return Participant{}, fmt.Errorf("participant %q not found", name)
}

func roleInstructions(p Participant) string {
	if p.IsModerator {
		return "You are the moderator. Facilitate the discussion, call votes, and synthesize decisions."
	}
	return fmt.Sprintf("You are a specialist. Focus on your area of expertise and help reach a high-quality decision.")
}
