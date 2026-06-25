package kngs

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

//go:embed templates/*.md
var templatesFS embed.FS

// InitTemplates writes embedded knowledge base templates to the given
// directories. It creates the directories if they don't exist.
// Templates are only written if the target file doesn't already exist,
// preserving user modifications.
func InitTemplates(paths []string) ([]string, error) {
	var written []string

	entries, err := templatesFS.ReadDir("templates")
	if err != nil {
		return nil, fmt.Errorf("read embedded kngs templates: %w", err)
	}

	for _, base := range paths {
		if err := os.MkdirAll(base, 0o755); err != nil {
			return written, fmt.Errorf("create kngs dir %s: %w", base, err)
		}

		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			targetPath := filepath.Join(base, e.Name())

			// Skip if file already exists (preserve user edits).
			if _, err := os.Stat(targetPath); err == nil {
				continue
			}

			data, err := templatesFS.ReadFile("templates/" + e.Name())
			if err != nil {
				return written, fmt.Errorf("read embedded template %s: %w", e.Name(), err)
			}

			if err := os.WriteFile(targetPath, data, 0o644); err != nil {
				return written, fmt.Errorf("write kngs template %s: %w", targetPath, err)
			}

			written = append(written, targetPath)
		}
	}

	return written, nil
}

// TemplateNames returns the list of available template filenames.
func TemplateNames() []string {
	entries, err := templatesFS.ReadDir("templates")
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			names = append(names, e.Name())
		}
	}
	return names
}
