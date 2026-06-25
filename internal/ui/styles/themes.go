package styles

import "github.com/charmbracelet/x/exp/charmtone"

// ThemeForProvider returns the Styles associated with the given provider
// ID. Unknown or empty provider IDs yield the default Fromsko Pantera
// theme.
func ThemeForProvider(providerID string) Styles {
	switch providerID {
	case "hyper":
		return HypermocodeObsidiana()
	case "evo":
		return EvoCrimson()
	default:
		return FromskoPantera()
	}
}

// FromskoPantera returns the Fromsko dark theme. It's the default style
// for the UI, using green/yellow primary tones.
func FromskoPantera() Styles {
	return quickStyle(quickStyleOpts{
		primary:   charmtone.Bok,
		secondary: charmtone.Citron,
		accent:    charmtone.Guac,
		keyword:   charmtone.Blush,

		fgBase:       charmtone.Ash,
		fgMoreSubtle: charmtone.Squid,
		fgSubtle:     charmtone.Smoke,
		fgMostSubtle: charmtone.Oyster,

		onPrimary: charmtone.Pepper,

		bgBase:         charmtone.Pepper,
		bgLeastVisible: charmtone.BBQ,
		bgLessVisible:  charmtone.Charcoal,
		bgMostVisible:  charmtone.Iron,

		separator: charmtone.Charcoal,

		destructive:       charmtone.Coral,
		error:             charmtone.Sriracha,
		warningSubtle:     charmtone.Zest,
		warning:           charmtone.Mustard,
		busy:              charmtone.Citron,
		info:              charmtone.Malibu,
		infoMoreSubtle:    charmtone.Sardine,
		infoMostSubtle:    charmtone.Damson,
		success:           charmtone.Julep,
		successMoreSubtle: charmtone.Bok,
		successMostSubtle: charmtone.Guac,
	})
}

// EvoCrimson returns the self-evolution (/evo) theme. It uses a deep purple
// primary (Plum) with red accents (Cherry/Sriracha) so the evolution mode is
// visually distinct from the default blue-green theme while staying readable
// on the same dark background. Backgrounds stay neutral (Pepper/Charcoal) to
// keep contrast stable; only the brand + status colors shift.
func EvoCrimson() Styles {
	return quickStyle(quickStyleOpts{
		primary:   charmtone.Plum,
		secondary: charmtone.Violet,
		accent:    charmtone.Orchid,
		keyword:   charmtone.Cherry,

		fgBase:       charmtone.Ash,
		fgMoreSubtle: charmtone.Squid,
		fgSubtle:     charmtone.Smoke,
		fgMostSubtle: charmtone.Oyster,

		onPrimary: charmtone.Butter,

		bgBase:         charmtone.Pepper,
		bgLeastVisible: charmtone.BBQ,
		bgLessVisible:  charmtone.Charcoal,
		bgMostVisible:  charmtone.Iron,

		separator: charmtone.Charcoal,

		destructive:       charmtone.Coral,
		error:             charmtone.Sriracha,
		warningSubtle:     charmtone.Zest,
		warning:           charmtone.Paprika,
		busy:              charmtone.Grape,
		info:              charmtone.Violet,
		infoMoreSubtle:    charmtone.Mauve,
		infoMostSubtle:    charmtone.Damson,
		success:           charmtone.Julep,
		successMoreSubtle: charmtone.Bok,
		successMostSubtle: charmtone.Guac,
	})
}

// CharmtonePantera returns the Charmtone dark theme. It's the default style
// for the UI.
func CharmtonePantera() Styles {
	return quickStyle(quickStyleOpts{
		primary:   charmtone.Charple,
		secondary: charmtone.Dolly,
		accent:    charmtone.Bok,
		keyword:   charmtone.Blush,

		fgBase:       charmtone.Ash,
		fgMoreSubtle: charmtone.Squid,
		fgSubtle:     charmtone.Smoke,
		fgMostSubtle: charmtone.Oyster,

		onPrimary: charmtone.Butter,

		bgBase:         charmtone.Pepper,
		bgLeastVisible: charmtone.BBQ,
		bgLessVisible:  charmtone.Charcoal,
		bgMostVisible:  charmtone.Iron,

		separator: charmtone.Charcoal,

		destructive:       charmtone.Coral,
		error:             charmtone.Sriracha,
		warningSubtle:     charmtone.Zest,
		warning:           charmtone.Mustard,
		busy:              charmtone.Citron,
		info:              charmtone.Malibu,
		infoMoreSubtle:    charmtone.Sardine,
		infoMostSubtle:    charmtone.Damson,
		success:           charmtone.Julep,
		successMoreSubtle: charmtone.Bok,
		successMostSubtle: charmtone.Guac,
	})
}

// HypermocodeObsidiana returns the Hypermocode dark theme.
func HypermocodeObsidiana() Styles {
	return quickStyle(quickStyleOpts{
		primary:   charmtone.Charple,
		secondary: charmtone.Dolly,
		accent:    charmtone.Bok,

		fgBase:       charmtone.Ash,
		fgMoreSubtle: charmtone.Squid,
		fgSubtle:     charmtone.Smoke,
		fgMostSubtle: charmtone.Oyster,

		onPrimary: charmtone.Butter,

		bgBase:         charmtone.Pepper,
		bgLeastVisible: charmtone.BBQ,
		bgLessVisible:  charmtone.Charcoal,
		bgMostVisible:  charmtone.Iron,

		separator: charmtone.Charcoal,

		destructive:       charmtone.Coral,
		error:             charmtone.Sriracha,
		warningSubtle:     charmtone.Zest,
		warning:           charmtone.Mustard,
		busy:              charmtone.Citron,
		info:              charmtone.Malibu,
		infoMoreSubtle:    charmtone.Sardine,
		infoMostSubtle:    charmtone.Damson,
		success:           charmtone.Julep,
		successMoreSubtle: charmtone.Bok,
		successMostSubtle: charmtone.Guac,
	})
}
