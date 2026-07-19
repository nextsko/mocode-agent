package styles

import (
	"image/color"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nextsko/mocode-agent/internal/domain/theme"
)

// TestSpinnerThemer_ImplementsPort is a compile-time check that SpinnerThemer
// satisfies the domain.theme.SpinnerThemer interface. If the method signature
// drifts, this fails the build here rather than at the core/app call site.
func TestSpinnerThemer_ImplementsPort(t *testing.T) {
	var _ theme.SpinnerThemer = SpinnerThemer{}
}

// TestSpinnerThemer_ReturnsColorsForProvider verifies the adapter maps the
// styles theme system to the domain port for a known provider. The default
// (empty) provider returns a real theme with non-nil colors.
func TestSpinnerThemer_ReturnsColorsForProvider(t *testing.T) {
	themer := SpinnerThemer{}
	colors := themer.SpinnerColorsForProvider("")

	// The default theme must populate all three fields. We don't assert
	// specific RGBA values (they're theme-dependent), only that the adapter
	// doesn't leave nil holes that would cause a nil-pointer deref in the
	// spinner's color handling.
	require.NotNil(t, colors.LabelColor)
	require.NotNil(t, colors.GradFrom)
	require.NotNil(t, colors.GradTo)
}

// TestSpinnerThemer_ConsistencyWithThemeForProvider confirms the adapter is a
// faithful bridge: SpinnerColorsForProvider returns exactly what
// ThemeForProvider exposes. If someone changes the field mapping, this breaks.
func TestSpinnerThemer_ConsistencyWithThemeForProvider(t *testing.T) {
	provider := "anthropic"
	s := ThemeForProvider(provider)
	colors := SpinnerThemer{}.SpinnerColorsForProvider(provider)

	require.Equal(t, s.WorkingLabelColor, colors.LabelColor)
	require.Equal(t, s.WorkingGradFromColor, colors.GradFrom)
	require.Equal(t, s.WorkingGradToColor, colors.GradTo)
}

// TestNoopThemer_ReturnsBlack confirms the fallback themer used when no UI is
// wired returns concrete (black) colors, not nil, so the spinner never panics.
func TestNoopThemer_ReturnsBlack(t *testing.T) {
	colors := theme.NoopThemer{}.SpinnerColorsForProvider("anything")
	require.Equal(t, color.Black, colors.LabelColor)
	require.Equal(t, color.Black, colors.GradFrom)
	require.Equal(t, color.Black, colors.GradTo)
}
