package completions

import (
	"strings"
	"testing"

	"github.com/sahilm/fuzzy"
	"github.com/stretchr/testify/require"
)

func TestSplitMatchIndexes(t *testing.T) {
	// Filter text: "/model switch model" — cmdLen = len("/model") = 6.
	m := fuzzy.Match{MatchedIndexes: []int{1, 3, 7, 9}} // hits in cmd, sep, desc
	cmd, desc := splitMatchIndexes(m, 6)
	require.Equal(t, map[int]bool{1: true, 3: true}, cmd)
	require.Equal(t, map[int]bool{0: true, 2: true}, desc, "sep (6) skipped, desc indexes rebased")
}

func TestHighlightRunes(t *testing.T) {
	out := highlightRunes("abc", map[int]bool{1: true}, nil, nil, false)
	require.Contains(t, out, "\x1b[1mb\x1b[22m", "hit rune bolded")
	require.True(t, strings.HasPrefix(out, "a"))
	require.True(t, strings.HasSuffix(out, "c"))

	// No hits → untouched.
	require.Equal(t, "abc", highlightRunes("abc", nil, nil, nil, false))
}
