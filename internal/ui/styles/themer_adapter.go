package styles

import (
	"github.com/package-register/mocode/internal/domain/theme"
)

// SpinnerThemer adapts the styles theme system to the domain.theme port so
// core/app can resolve spinner colors without importing this UI package. The
// dependency arrow points down: core -> domain.theme (interface), ui/styles
// -> domain.theme (adapter).
type SpinnerThemer struct{}

// SpinnerColorsForProvider implements theme.SpinnerThemer.
func (SpinnerThemer) SpinnerColorsForProvider(providerID string) theme.SpinnerColors {
	s := ThemeForProvider(providerID)
	return theme.SpinnerColors{
		LabelColor: s.WorkingLabelColor,
		GradFrom:   s.WorkingGradFromColor,
		GradTo:     s.WorkingGradToColor,
	}
}
