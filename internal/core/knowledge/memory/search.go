package memory

import (
	"strings"
	"unicode"
)

// searchEntries scores and ranks memory entries based on keyword matching.
func searchEntries(entries []*Entry, query string, minScore float64, maxResults int) []*Entry {
	query = strings.TrimSpace(query)
	if query == "" {
		return []*Entry{}
	}

	candidates := make([]scoredEntry, 0, len(entries))
	for _, entry := range entries {
		score := scoreEntry(entry, query)
		if score >= minScore && score > 0 {
			candidates = append(candidates, scoredEntry{entry: entry, score: score})
		}
	}

	// Sort by score (descending), then by updated time (descending)
	sortScoredEntries(candidates)

	// Apply limit
	if maxResults > 0 && len(candidates) > maxResults {
		candidates = candidates[:maxResults]
	}

	// Convert to result format
	results := make([]*Entry, len(candidates))
	for i, c := range candidates {
		cloned := *c.entry
		cloned.Score = c.score
		results[i] = &cloned
	}

	return results
}

// scoreEntry calculates a relevance score for an entry based on query tokens.
func scoreEntry(entry *Entry, query string) float64 {
	if entry == nil || entry.Memory == nil {
		return 0
	}

	tokens := buildSearchTokens(query)
	if len(tokens) == 0 {
		// Fallback to substring match
		const fallbackScore = 0.5
		ql := strings.ToLower(query)
		contentLower := strings.ToLower(entry.Memory.Memory)
		if strings.Contains(contentLower, ql) {
			return fallbackScore
		}
		for _, topic := range entry.Memory.Topics {
			if strings.Contains(strings.ToLower(topic), ql) {
				return fallbackScore
			}
		}
		return 0
	}

	contentLower := strings.ToLower(entry.Memory.Memory)
	matched := 0
	for _, tk := range tokens {
		if tk == "" {
			continue
		}
		hit := false
		if strings.Contains(contentLower, tk) {
			hit = true
		} else {
			for _, topic := range entry.Memory.Topics {
				if strings.Contains(strings.ToLower(topic), tk) {
					hit = true
					break
				}
			}
		}
		if hit {
			matched++
		}
	}

	return float64(matched) / float64(len(tokens))
}

// buildSearchTokens builds tokens for searching.
func buildSearchTokens(query string) []string {
	const minTokenLen = 2

	q := strings.TrimSpace(strings.ToLower(query))
	if q == "" {
		return nil
	}

	// Replace non-alphanumeric characters with spaces
	b := make([]rune, 0, len(q))
	for _, r := range q {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b = append(b, r)
		} else {
			b = append(b, ' ')
		}
	}

	parts := strings.Fields(string(b))
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if len(p) < minTokenLen {
			continue
		}
		if isStopword(p) {
			continue
		}
		out = append(out, p)
	}

	return dedupStrings(out)
}

// scoredEntry is a helper type for scoring and sorting.
type scoredEntry struct {
	entry *Entry
	score float64
}

// isStopword checks if a word is a stopword.
func isStopword(s string) bool {
	switch s {
	case "a", "an", "the", "and", "or", "of", "in", "on", "to",
		"for", "with", "is", "are", "am", "be":
		return true
	default:
		return false
	}
}

// dedupStrings returns a deduplicated copy of the input slice.
func dedupStrings(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

// sortScoredEntries sorts entries by score and updated time.
func sortScoredEntries(entries []scoredEntry) {
	for i := 0; i < len(entries)-1; i++ {
		for j := i + 1; j < len(entries); j++ {
			shouldSwap := false
			if entries[i].score != entries[j].score {
				shouldSwap = entries[i].score < entries[j].score
			} else if !entries[i].entry.UpdatedAt.Equal(entries[j].entry.UpdatedAt) {
				shouldSwap = entries[i].entry.UpdatedAt.Before(entries[j].entry.UpdatedAt)
			} else if !entries[i].entry.CreatedAt.Equal(entries[j].entry.CreatedAt) {
				shouldSwap = entries[i].entry.CreatedAt.Before(entries[j].entry.CreatedAt)
			} else {
				shouldSwap = entries[i].entry.ID > entries[j].entry.ID
			}

			if shouldSwap {
				entries[i], entries[j] = entries[j], entries[i]
			}
		}
	}
}

// normalizeEntry normalizes a memory entry.
func normalizeEntry(entry *Entry) {
	if entry == nil || entry.Memory == nil {
		return
	}
	entry.Memory.Participants = normalizeParticipants(entry.Memory.Participants)
	entry.Memory.Location = strings.TrimSpace(entry.Memory.Location)
	if entry.Memory.Kind == "" {
		entry.Memory.Kind = KindFact
	}
}
