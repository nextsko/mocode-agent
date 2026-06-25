package roundtable

import "fmt"

// SpeakerSelector decides which participant speaks next.
type SpeakerSelector interface {
	NextSpeaker(rt *Roundtable) (string, error)
}

// RoundRobinStrategy cycles through non-moderator participants in order,
// returning to the moderator at the start of each new round.
type RoundRobinStrategy struct{}

// NextSpeaker implements the SpeakerSelector interface.
func (RoundRobinStrategy) NextSpeaker(rt *Roundtable) (string, error) {
	if len(rt.Config.Participants) == 0 {
		return "", fmt.Errorf("no participants")
	}

	var moderator string
	var specialists []string
	for _, p := range rt.Config.Participants {
		if p.IsModerator {
			moderator = p.Name
			continue
		}
		specialists = append(specialists, p.Name)
	}
	if moderator == "" {
		return "", fmt.Errorf("no moderator configured")
	}
	if len(specialists) == 0 {
		return moderator, nil
	}

	// Turn 0 is always the moderator's opening statement.
	if rt.CurrentTurn == 0 {
		return moderator, nil
	}

	// After the moderator, cycle specialists; after the last specialist,
	// return to the moderator.
	offset := (rt.CurrentTurn - 1) % (len(specialists) + 1)
	if offset == len(specialists) {
		return moderator, nil
	}
	return specialists[offset], nil
}
