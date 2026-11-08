package uidissect

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// RootDir is where dissections and replicated components are stored, relative
// to the project working directory.
const RootDir = ".mocode/ui"

// Dir returns the artifact directory for a project.
func Dir(workingDir string) string { return filepath.Join(workingDir, RootDir) }

// Saved lists the files produced by Save.
type Saved struct {
	JSONPath string
	Markdown string
}

var slugRE = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = slugRE.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		return "page"
	}
	if len(s) > 48 {
		s = s[:48]
	}
	return s
}

// Save writes the map as JSON + Markdown and appends it to the index.
func Save(m Map, workingDir string) (Saved, error) {
	dir := Dir(workingDir)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return Saved{}, fmt.Errorf("create ui dir: %w", err)
	}
	stamp := time.Now().UTC().Format("20060102T150405")
	slug := slugify(firstNonEmpty(hostOf(m.URL), m.Title))
	base := filepath.Join(dir, stamp+"-"+slug)

	raw, err := RenderJSON(m)
	if err != nil {
		return Saved{}, fmt.Errorf("marshal map: %w", err)
	}
	jsonPath := base + ".json"
	if err := os.WriteFile(jsonPath, raw, 0o600); err != nil {
		return Saved{}, fmt.Errorf("write map json: %w", err)
	}
	mdPath := base + ".md"
	if err := os.WriteFile(mdPath, []byte(RenderMarkdown(m)), 0o600); err != nil {
		return Saved{}, fmt.Errorf("write map markdown: %w", err)
	}

	index := filepath.Join(dir, "index.md")
	entry := fmt.Sprintf("- %s · %s · %d nodes · %d findings · [%s](%s) · [%s](%s)\n",
		stamp, firstNonEmpty(m.Title, m.URL), m.Stats.Nodes, m.Stats.Findings,
		filepath.Base(mdPath), filepath.Base(mdPath), filepath.Base(jsonPath), filepath.Base(jsonPath))
	f, err := os.OpenFile(index, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return Saved{}, fmt.Errorf("open index: %w", err)
	}
	defer f.Close()
	if _, err := f.WriteString(entry); err != nil {
		return Saved{}, fmt.Errorf("append index: %w", err)
	}
	return Saved{JSONPath: jsonPath, Markdown: mdPath}, nil
}

// SaveComponent persists a replicated component and returns its file path.
func SaveComponent(m Map, comp Component, html, css, workingDir string) (string, error) {
	dir := filepath.Join(Dir(workingDir), "components")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", fmt.Errorf("create components dir: %w", err)
	}
	name := time.Now().UTC().Format("20060102T150405") + "-" + slugify(comp.Kind+"-"+comp.ID)
	path := filepath.Join(dir, name+".html")
	doc := fmt.Sprintf("<!-- source: %s · component %s (%s) -->\n<style>\n%s\n</style>\n%s\n", m.URL, comp.ID, comp.Kind, css, html)
	if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
		return "", fmt.Errorf("write component: %w", err)
	}
	return path, nil
}

// List returns artifact entries (newest last), or nil when none exist.
func List(workingDir string) ([]string, error) {
	dir := Dir(workingDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read ui dir: %w", err)
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") || e.Name() == "index.md" {
			continue
		}
		out = append(out, filepath.Join(dir, e.Name()))
	}
	sort.Strings(out)
	return out, nil
}

func hostOf(raw string) string {
	raw = strings.TrimSpace(raw)
	i := strings.Index(raw, "://")
	if i < 0 {
		return raw
	}
	rest := raw[i+3:]
	if j := strings.IndexAny(rest, "/?#"); j >= 0 {
		rest = rest[:j]
	}
	return rest
}
