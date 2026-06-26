package evo

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ModeAgentID is the config agent ID prefix for a materialized evo agent.
// Using "evo-" (not "evo:") keeps it filesystem-safe as a .md filename.
func ModeAgentID(name string) string {
	return "evo-" + sanitizeName(name)
}

// MaterializeAgent writes a config-loadable .md file for a fixed agent into the
// given agents directory. This is the last mile of the closed loop: the config
// loader (LoadAgentsFromDir) scans that directory, so a materialized agent
// appears in the mode picker and becomes runnable via SwitchAgent — usable
// "like a mode", exactly as the /evo vision describes.
//
// The file mirrors the built-in mode .md format: YAML frontmatter (id, name,
// description, tools) followed by the固化 system prompt as the body. The
// agent's captured skills become its allowed tool list; when none were
// captured the tools list is omitted (config then grants all tools).
//
// The source of truth stays the FixationStore (agent.json + revisions);
// materialization is a projection derived from the active revision. Re-running
// /evo and exiting overwrites the .md with the latest active revision.
func MaterializeAgent(entry AgentEntry, agentsDir string) error {
	id := ModeAgentID(entry.Manifest.Name)
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		return fmt.Errorf("evo: create agents dir: %w", err)
	}
	path := filepath.Join(agentsDir, id+".md")
	body, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("evo: write agent file: %w", err)
	}
	defer body.Close()

	fmt.Fprintln(body, "---")
	fmt.Fprintf(body, "id: %q\n", id)
	name := strings.TrimSpace(entry.Manifest.Title)
	if name == "" {
		name = entry.Manifest.Name
	}
	fmt.Fprintf(body, "name: %q\n", name)
	fmt.Fprintf(body, "description: %q\n", entry.Summary())
	// Only restrict tools when the agent demonstrably used a subset; an empty
	// list means "all tools" in the config loader, which is the safe default.
	if skills := nonEmpty(entry.Revision.Skills); len(skills) > 0 {
		fmt.Fprintln(body, "tools:")
		for _, t := range skills {
			fmt.Fprintf(body, "  - %q\n", t)
		}
	}
	fmt.Fprintln(body, "---")
	fmt.Fprintln(body)
	fmt.Fprintln(body, strings.TrimSpace(entry.Revision.SystemPrompt))
	return nil
}

// nonEmpty drops blank skill entries so the tools list stays clean.
func nonEmpty(in []string) []string {
	var out []string
	for _, s := range in {
		if strings.TrimSpace(s) != "" {
			out = append(out, s)
		}
	}
	return out
}