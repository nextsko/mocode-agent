// Package theme defines the core-side port for resolving presentation colors
// used by the non-interactive spinner. It is a layer-1 domain port: core/app
// imports it instead of reaching up into the ui/styles package, and the UI
// layer provides an adapter at wiring time. This removes the upward
// core -> ui dependency edge.
package theme

import (
	"image/color"
)

// SpinnerColors are the colors the spinner needs to render its label and the
// working-state gradient. All are image/color.Color (stdlib), so this port has
// no upward dependency.
type SpinnerColors struct {
	LabelColor color.Color
	GradFrom   color.Color
	GradTo     color.Color
}

// SpinnerThemer resolves spinner colors for a given model provider. core/app
// calls this with the active provider ID; the UI layer adapts its theme system
// to satisfy it.
type SpinnerThemer interface {
	SpinnerColorsForProvider(providerID string) SpinnerColors
}

// NoopThemer returns zero-value colors (black). It is the default used when no
// UI theme is wired in (e.g. non-TUI builds without styles), so the spinner
// still renders without core importing ui/styles.
type NoopThemer struct{}

// SpinnerColorsForProvider implements SpinnerThemer.
func (NoopThemer) SpinnerColorsForProvider(_ string) SpinnerColors {
	return SpinnerColors{
		LabelColor: color.Black,
		GradFrom:   color.Black,
		GradTo:     color.Black,
	}
}
