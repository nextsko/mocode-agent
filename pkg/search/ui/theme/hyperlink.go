package theme

// OSC 8 hyperlink escape fragments: the opener carries the URL, the ST
// (ESC \) terminates each OSC sequence, and the closer re-opens with an
// empty URL to end the link.
const (
	osc8Open  = "\x1b]8;;"
	osc8Close = "\x1b\\"
)

// Hyperlink wraps text in an OSC 8 hyperlink pointing at url. Terminals
// that support OSC 8 (kitty, iTerm2, WezTerm, Windows Terminal) render a
// clickable link; others display just the text.
func Hyperlink(url, text string) string {
	return osc8Open + url + osc8Close + text + osc8Open + osc8Close
}
