package slash

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// Residual SGR fragments left behind when colorized terminal output is pasted
// into a command file and the ESC byte is lost — Windows rejects control
// characters in file names, and some copy/paste paths drop them silently. The
// remainder then renders as literal text in the `/` popup and command dialog.
var (
	// bracketSGR matches ESC-less SGR sequences that kept their CSI bracket,
	// e.g. "[38;2;223;219;221m".
	bracketSGR = regexp.MustCompile(`\[[0-9;]*m`)
	// bareSGR matches ESC-less SGR payloads that lost the bracket too, e.g.
	// "8;2;104;255;214m" or ";2;104;255;214m".
	bareSGR = regexp.MustCompile(`;?\d{1,3}(?:;\d{1,3})+m`)
)

// CleanCommandText removes ANSI escape sequences and ESC-less SGR leftovers
// from custom command labels and descriptions. It is meant for display text
// only; never apply it to dispatch values such as command content or IDs.
func CleanCommandText(s string) string {
	if s == "" {
		return s
	}
	s = ansi.Strip(s)
	s = bracketSGR.ReplaceAllString(s, "")
	s = bareSGR.ReplaceAllString(s, "")
	return strings.TrimSpace(s)
}

// DisplayLabel returns the user-facing label for a custom command: the
// markdown file's relative path without extension, with separators normalized
// to `/` and pasted terminal residue removed. It falls back to the ID when the
// loader did not record a path.
func (c CustomCommand) DisplayLabel() string {
	if c.Path == "" {
		return CleanCommandText(c.ID)
	}
	base := strings.TrimSuffix(c.Path, filepath.Ext(c.Path))
	base = strings.ReplaceAll(base, `\`, "/")
	if sep := string(filepath.Separator); sep != "/" {
		base = strings.ReplaceAll(base, sep, "/")
	}
	return CleanCommandText(base)
}
