package coordinator

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestInteractiveToolsEnabled covers the rule that gates the question tool:
// only the main agent of an interactive transport may ask the user something.
// A sub-agent never can, and a non-interactive transport never can.
func TestInteractiveToolsEnabled(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		interactive bool
		isSubAgent  bool
		want        bool
	}{
		{"interactive main agent", true, false, true},
		{"interactive sub-agent", true, true, false},
		{"non-interactive main agent", false, false, false},
		{"non-interactive sub-agent", false, true, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := &Coordinator{interactive: tt.interactive}
			require.Equal(t, tt.want, c.interactiveToolsEnabled(tt.isSubAgent))
		})
	}
}
