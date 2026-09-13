// Package slash implements TUI `/` slash commands and MCP prompt loading.
// Prefer CommandRegistry (command_registry.go) for new command definitions.
package slash

import (
	"context"
	"hash/fnv"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/nextsko/mocode-agent/internal/core/config"
	"github.com/nextsko/mocode-agent/internal/util/infra"
	"github.com/nextsko/mocode-agent/internal/core/tools/external/mcp"
)

var namedArgPattern = regexp.MustCompile(`\$([A-Z][A-Z0-9_]*)`)

const (
	userCommandPrefix    = "user:"
	projectCommandPrefix = "project:"
)

// Argument represents a command argument with its metadata.
type Argument struct {
	ID          string
	Title       string
	Description string
	Required    bool
}

// MCPPrompt represents a custom command loaded from an MCP server.
type MCPPrompt struct {
	ID          string
	Title       string
	Description string
	PromptID    string
	ClientID    string
	Arguments   []Argument
}

// CustomCommand represents a user-defined custom command loaded from markdown files.
type CustomCommand struct {
	ID        string
	Name      string
	Path      string // relative path to the .md source — preserved for display
	Content   string
	Arguments []Argument
}

type commandSource struct {
	path   string
	prefix string
}

// LoadCustomCommands loads custom commands from multiple sources including
// XDG config directory, home directory, and project directory.
func LoadCustomCommands(cfg *config.Config) ([]CustomCommand, error) {
	return loadAll(buildCommandSources(cfg))
}

// LoadMCPPrompts loads custom commands from available MCP servers.
func LoadMCPPrompts() ([]MCPPrompt, error) {
	var commands []MCPPrompt
	for mcpName, prompts := range mcp.Prompts() {
		for _, prompt := range prompts {
			key := mcpName + ":" + prompt.Name
			var args []Argument
			for _, arg := range prompt.Arguments {
				title := arg.Title
				if title == "" {
					title = arg.Name
				}
				args = append(args, Argument{
					ID:          arg.Name,
					Title:       title,
					Description: arg.Description,
					Required:    arg.Required,
				})
			}
			commands = append(commands, MCPPrompt{
				ID:          key,
				Title:       prompt.Title,
				Description: prompt.Description,
				PromptID:    prompt.Name,
				ClientID:    mcpName,
				Arguments:   args,
			})
		}
	}
	return commands, nil
}

func buildCommandSources(cfg *config.Config) []commandSource {
	return []commandSource{
		{
			path:   filepath.Join(infra.Config(), "Mocode", "commands"),
			prefix: userCommandPrefix,
		},
		{
			path:   filepath.Join(infra.Dir(), ".Mocode", "commands"),
			prefix: userCommandPrefix,
		},
		{
			path:   filepath.Join(infra.Config(), "mocode", "commands"),
			prefix: projectCommandPrefix,
		},
	}
}

func loadAll(sources []commandSource) ([]CustomCommand, error) {
	var commands []CustomCommand

	for _, source := range sources {
		if cmds, err := loadFromSource(source); err == nil {
			commands = append(commands, cmds...)
		}
	}

	return commands, nil
}

func loadFromSource(source commandSource) ([]CustomCommand, error) {
	if _, err := os.Stat(source.path); os.IsNotExist(err) {
		return nil, nil
	}

	var commands []CustomCommand

	err := filepath.WalkDir(source.path, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !isMarkdownFile(d.Name()) {
			return err
		}

		cmd, err := loadCommand(path, source.path, source.prefix)
		if err != nil {
			return nil // Skip invalid files
		}

		commands = append(commands, cmd)
		return nil
	})

	return commands, err
}

func loadCommand(path, baseDir, prefix string) (CustomCommand, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return CustomCommand{}, err
	}

	id := buildCommandID(path, baseDir, prefix)
	rel, _ := filepath.Rel(baseDir, path)

	return CustomCommand{
		ID:        id,
		Name:      id,
		Path:      rel,
		Content:   string(content),
		Arguments: extractArgNames(string(content)),
	}, nil
}

func extractArgNames(content string) []Argument {
	matches := namedArgPattern.FindAllStringSubmatch(content, -1)
	if len(matches) == 0 {
		return nil
	}

	seen := make(map[string]bool)
	var args []Argument

	for _, match := range matches {
		arg := match[1]
		if !seen[arg] {
			seen[arg] = true
			// for normal custom commands, all args are required
			args = append(args, Argument{ID: arg, Title: arg, Required: true})
		}
	}

	return args
}

func buildCommandID(path, baseDir, prefix string) string {
	// Use a stable hash of the relative path instead of joining directory
	// names with `:` — a folder whose name itself contains `:` (e.g.
	// `8;2;104;255;214m`) would otherwise split the ID mid-sequence and
	// collide with the desc row. The full relative path is stored in
	// the command for hash stability and to recover the display path.
	rel, err := filepath.Rel(baseDir, path)
	if err != nil {
		rel = path
	}
	return prefix + commandPathHash(rel)
}

// commandPathHash returns a short, stable hash of the command's relative
// path. The hash is the only piece used in the ID so that paths with
// separator-like characters (`/`, `:`, `;`, `\`) can never split it.
func commandPathHash(rel string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(rel))
	// Base36 of the FNV-1a 32-bit hash, 7 chars (≈ 36^7 ≈ 78B unique) is
	// plenty for per-source custom command counts.
	return encodeBase36(uint64(h.Sum32()), 7)
}

// encodeBase36 returns the value as a fixed-width base-36 string (uppercase).
// Width 0 means "as many digits as needed".
func encodeBase36(v uint64, width int) string {
	const digits = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	if v == 0 {
		if width <= 0 {
			return "0"
		}
		return strings.Repeat("0", width)
	}
	var buf []byte
	for v > 0 {
		buf = append([]byte{digits[v%36]}, buf...)
		v /= 36
	}
	for len(buf) < width {
		buf = append([]byte{'0'}, buf...)
	}
	return string(buf)
}

func isMarkdownFile(name string) bool {
	return strings.HasSuffix(strings.ToLower(name), ".md")
}

func GetMCPPrompt(cfg *config.ConfigStore, clientID, promptID string, args map[string]string) (string, error) {
	// Create a context with timeout since tea.Cmd doesn't support context passing.
	// The MCP client has its own timeout, but this provides an additional safeguard.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := mcp.GetPromptMessages(ctx, cfg, clientID, promptID, args)
	if err != nil {
		return "", err
	}
	return strings.Join(result, " "), nil
}
