package shell

import (
	"bytes"
	"cmp"
	"context"
	_ "embed"
	"fmt"
	"html/template"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/nextsko/mocode-agent/internal/core/agent/toolutil"
	"github.com/nextsko/mocode-agent/internal/util/errcoll"

	"charm.land/fantasy"

	"github.com/nextsko/mocode-agent/internal/core/config"
	"github.com/nextsko/mocode-agent/internal/core/permission"
	"github.com/nextsko/mocode-agent/internal/core/shellruntime/shell"
	"github.com/nextsko/mocode-agent/internal/util/fsext"
)

type BashParams struct {
	Description         string `json:"description" description:"A brief description of what the command does, try to keep it under 30 characters or so"`
	Command             string `json:"command" description:"The command to execute"`
	WorkingDir          string `json:"working_dir,omitempty" description:"The working directory to execute the command in (defaults to current directory)"`
	RunInBackground     bool   `json:"run_in_background,omitempty" description:"Set to true (boolean) to run this command in the background. You will be notified on completion via job status updates; use job_output to read output on demand."`
	AutoBackgroundAfter int    `json:"auto_background_after,omitempty" description:"Seconds to wait before automatically moving the command to a background job (default: 30, max: 30)"`
	TTY                 bool   `json:"tty,omitempty" description:"When true, run the command in a terminal-like PTY on supported platforms. Useful for live terminal output and more reliable cancellation of long-running jobs."`
}

type BashPermissionsParams struct {
	Description         string `json:"description"`
	Command             string `json:"command"`
	WorkingDir          string `json:"working_dir"`
	RunInBackground     bool   `json:"run_in_background"`
	AutoBackgroundAfter int    `json:"auto_background_after"`
	TTY                 bool   `json:"tty"`
}

type BashResponseMetadata struct {
	StartTime        int64  `json:"start_time"`
	EndTime          int64  `json:"end_time"`
	Output           string `json:"output"`
	Description      string `json:"description"`
	WorkingDirectory string `json:"working_directory"`
	Background       bool   `json:"background,omitempty"`
	ShellID          string `json:"shell_id,omitempty"`
	TTY              bool   `json:"tty,omitempty"`
}

const (
	BashToolName = "bash"

	// DefaultAutoBackgroundAfter is how long a synchronous command runs
	// before it is moved to the background (was 60s; aligned to Codex's
	// 30s yield ceiling so the agent turn never blocks longer than that).
	DefaultAutoBackgroundAfter = 30
	// MaxAutoBackgroundAfter caps the model-requested wait window.
	MaxAutoBackgroundAfter = 30
	MaxOutputLength        = 30000
	BashNoOutput           = "no output"
)

//go:embed bash.tpl
var bashDescriptionTmpl []byte

var bashDescriptionTpl = template.Must(
	template.New("bashDescription").
		Parse(string(bashDescriptionTmpl)),
)

type bashDescriptionData struct {
	BannedCommands  string
	MaxOutputLength int
	Attribution     config.Attribution
	ModelName       string
}

// bannedCommands is a minimal, catastrophic-only deny list. Routine commands
// (network tools, package managers, sudo, ...) are NOT hard-blocked anymore —
// they simply go through the normal permission flow. Only commands that can
// instantly destroy the system or make it unbootable are blocked outright.
var bannedCommands = []string{
	// Disk / partition destruction
	"fdisk",
	"mkfs",
	"parted",

	// Power state — killing the machine mid-session
	"halt",
	"poweroff",
	"reboot",
	"shutdown",
}

// criticalRootDirs are top-level system directories whose recursive deletion
// makes the system unbootable. User-writable locations (/home, /tmp, /opt,
// /srv, ...) are intentionally NOT protected — deleting those is the user's
// own call.
var criticalRootDirs = map[string]struct{}{
	"/":      {},
	"/bin":   {},
	"/boot":  {},
	"/dev":   {},
	"/etc":   {},
	"/lib":   {},
	"/lib32": {},
	"/lib64": {},
	"/proc":  {},
	"/root":  {},
	"/run":   {},
	"/sbin":  {},
	"/sys":   {},
	"/usr":   {},
	"/var":   {},
}

// catastrophicCommandBlocker hard-blocks commands that instantly destroy the
// system: `rm -rf /` (and friends), fork bombs, mkfs.* helper binaries and
// `dd` writing over raw disks. Everything else is left to the permission
// system.
func catastrophicCommandBlocker() shell.BlockFunc {
	return func(args []string) bool {
		if len(args) == 0 {
			return false
		}
		lower := strings.ToLower(strings.Join(args, " "))

		// Fork bombs, mkfs.* binaries and raw-disk dd.
		if strings.HasPrefix(lower, ":(){") ||
			strings.HasPrefix(lower, "mkfs.") ||
			(args[0] == "dd" && strings.Contains(lower, "of=/dev/sd")) ||
			(args[0] == "dd" && strings.Contains(lower, "of=/dev/nvme")) ||
			(args[0] == "dd" && strings.Contains(lower, "of=/dev/vd")) ||
			(args[0] == "dd" && strings.Contains(lower, "of=/dev/hd")) {
			return true
		}

		// rm -rf targeting the filesystem root or critical system dirs.
		if args[0] != "rm" {
			return false
		}
		recursive, force := false, false
		targets := make([]string, 0, len(args))
		for _, arg := range args[1:] {
			switch {
			case arg == "--recursive":
				recursive = true
			case arg == "--force":
				force = true
			case strings.HasPrefix(arg, "-") && len(arg) > 1:
				for _, c := range strings.TrimPrefix(arg, "-") {
					if c == 'r' {
						recursive = true
					}
					if c == 'f' {
						force = true
					}
				}
			default:
				targets = append(targets, arg)
			}
		}
		if !recursive || !force {
			return false
		}
		for _, t := range targets {
			cleaned := strings.TrimRight(t, "/*")
			if cleaned == "" {
				cleaned = "/"
			}
			if _, bad := criticalRootDirs[cleaned]; bad {
				return true
			}
		}
		return false
	}
}

func bashDescription(attribution *config.Attribution, modelName string) string {
	bannedCommandsStr := strings.Join(bannedCommands, ", ")
	var out bytes.Buffer
	if err := bashDescriptionTpl.Execute(&out, bashDescriptionData{
		BannedCommands:  bannedCommandsStr,
		MaxOutputLength: MaxOutputLength,
		Attribution:     *attribution,
		ModelName:       modelName,
	}); err != nil {
		// this should never happen.
		panic("failed to execute bash description template: " + err.Error())
	}
	return out.String()
}

func blockFuncs() []shell.BlockFunc {
	return []shell.BlockFunc{
		shell.CommandsBlocker(bannedCommands),
		catastrophicCommandBlocker(),
	}
}

func classifyCommandError(stderr string) errcoll.ErrorCategory {
	lower := strings.ToLower(stderr)
	switch {
	case strings.Contains(lower, "command not found"),
		strings.Contains(lower, "is not recognized"),
		strings.Contains(lower, "cannot find"),
		strings.Contains(lower, "the term") && strings.Contains(lower, "not recognized"):
		return errcoll.CategoryCrossPlatform
	case strings.Contains(lower, "permission denied") || strings.Contains(lower, "access is denied"):
		return errcoll.CategoryPermission
	default:
		return errcoll.CategoryToolExecution
	}
}

// shellMissingCommandREs extract the offending command name from the various
// "not found" diagnostics emitted by mvdan/sh, POSIX shells and Windows cmd.
// The first capture group is always the command name.
var shellMissingCommandREs = []*regexp.Regexp{
	regexp.MustCompile(`([A-Za-z0-9_.-]+)"?\s*:\s*executable file not found`),
	regexp.MustCompile(`([A-Za-z0-9_.-]+):\s*command not found`),
	regexp.MustCompile(`(?i)'([^']+)' is not recognized`),
}

// shellCommandRouting maps a missing shell utility to the dedicated tool the
// model should use for that job instead. Only utilities that have a
// first-class tool are listed, so a genuinely absent dependency (e.g. `tea`)
// produces no hint.
var shellCommandRouting = map[string]string{
	"grep":  "use the `grep` tool (regex search that honors ignore files)",
	"egrep": "use the `grep` tool",
	"fgrep": "use the `grep` tool",
	"rg":    "use the `grep` tool",
	"head":  "use the `view` tool with `limit` to preview files (bash output is auto-truncated)",
	"tail":  "use the `view` tool with `offset`; finished background jobs already report the tail",
	"cat":   "use the `view` or `read_files` tool",
	"find":  "use the `glob` tool",
	"ls":    "use the `ls` tool",
	"sed":   "use the `edit` tool for file changes, or `ts_run`/`py_run` for text transforms",
	"awk":   "use `ts_run`/`py_run` for text processing",
	"wc":    "use `ts_run`/`py_run`, or the `grep` tool for counts",
	"sort":  "use `ts_run`/`py_run`",
	"uniq":  "use `ts_run`/`py_run`",
	"cut":   "use `ts_run`/`py_run`",
	"tr":    "use `ts_run`/`py_run`",
}

// missingShellCommand returns the lower-cased command name reported as missing
// in stderr, or "" when none of the known "not found" diagnostics match.
func missingShellCommand(stderr string) string {
	for _, re := range shellMissingCommandREs {
		if m := re.FindStringSubmatch(stderr); m != nil {
			return strings.ToLower(m[1])
		}
	}
	return ""
}

// commandRoutingHint returns an actionable hint when a command failed because a
// common Unix utility is unavailable in this cross-platform shell, pointing the
// model at the dedicated tool for that job. It returns "" when stderr carries
// no *routable* missing command, so unrelated failures stay untouched.
func commandRoutingHint(stderr string) string {
	cmd := missingShellCommand(stderr)
	if cmd == "" {
		return ""
	}
	advice, ok := shellCommandRouting[cmd]
	if !ok {
		return ""
	}
	return fmt.Sprintf(
		"hint: this shell has no `%s` (no system binary and no built-in) — %s. "+
			"Prefer dedicated tools over Unix text utilities in a pipeline.",
		cmd, advice,
	)
}

func NewBashTool(permissions permission.Service, workingDir string, attribution *config.Attribution, modelName string) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		BashToolName,
		bashDescription(attribution, modelName),
		func(ctx context.Context, params BashParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			record := func(category errcoll.ErrorCategory, err error, msg string) {
				if c := errcoll.FromContext(ctx); c != nil {
					c.Record(errcoll.ErrorRecord{
						SessionID: toolutil.GetSessionFromContext(ctx),
						ToolName:  BashToolName,
						Command:   params.Command,
						Error:     msg,
						Category:  category,
					})
				}
			}

			if params.Command == "" {
				return fantasy.NewTextErrorResponse("missing command"), nil
			}

			// Determine working directory
			execWorkingDir := cmp.Or(params.WorkingDir, workingDir)

			isSafeReadOnly := false
			cmdLower := strings.ToLower(params.Command)

			for _, safe := range safeCommands {
				if strings.HasPrefix(cmdLower, safe) {
					if len(cmdLower) == len(safe) || cmdLower[len(safe)] == ' ' || cmdLower[len(safe)] == '-' {
						isSafeReadOnly = true
						break
					}
				}
			}

			sessionID := toolutil.GetSessionFromContext(ctx)
			if sessionID == "" {
				msg := "session ID is required for executing shell command"
				record(errcoll.CategoryToolExecution, nil, msg)
				return fantasy.NewTextErrorResponse(msg), nil
			}
			if !isSafeReadOnly {
				p, err := permissions.Request(
					ctx,
					permission.CreatePermissionRequest{
						SessionID:   sessionID,
						Path:        execWorkingDir,
						ToolCallID:  call.ID,
						ToolName:    BashToolName,
						Action:      "execute",
						Description: fmt.Sprintf("Execute command: %s", params.Command),
						Params:      BashPermissionsParams(params),
					},
				)
				if err != nil {
					msg := "Permission request failed: " + err.Error()
					record(errcoll.CategoryPermission, err, msg)
					return fantasy.NewTextErrorResponse(msg), nil
				}
				if !p {
					record(errcoll.CategoryPermission, nil, "permission denied")
					return toolutil.NewPermissionDeniedResponse(), nil
				}
			}

			// If explicitly requested as background, start immediately with detached context
			if params.RunInBackground {
				if shell.BackgroundTasksDisabled() {
					return fantasy.NewTextErrorResponse("background tasks are disabled by MO_CODE_DISABLE_BACKGROUND_TASKS; run the command synchronously instead"), nil
				}
				startTime := time.Now()
				bgManager := shell.GetBackgroundShellManager()
				bgManager.Cleanup()
				// Use background context so it continues after tool returns
				bgShell, err := bgManager.Start(
					context.Background(),
					execWorkingDir,
					blockFuncs(),
					params.Command,
					params.Description,
					shell.BackgroundShellOptions{TTY: params.TTY, SessionID: sessionID},
				)
				if err != nil {
					msg := fmt.Sprintf("error starting background shell: %v", err)
					record(errcoll.CategoryToolExecution, err, msg)
					return fantasy.NewTextErrorResponse(msg), nil
				}

				// Explicit yield window (Codex model) instead of a blind
				// sleep: wait up to 2s for fast failures (blocked command,
				// syntax error) before handing the job back as background.
				const startupCheckWindow = 2 * time.Second
				if bgShell.WaitFor(startupCheckWindow) {
					// Command failed or completed very quickly
					_ = bgManager.Remove(bgShell.ID)

					stdout, stderr, _, execErr := bgShell.GetOutput()
					interrupted := shell.IsInterrupt(execErr)
					exitCode := shell.ExitCode(execErr)
					if exitCode == 0 && !interrupted && execErr != nil {
						record(classifyCommandError(stderr), execErr, execErr.Error())
					}

					stdout = formatOutput(stdout, stderr, execErr)

					metadata := BashResponseMetadata{
						StartTime:        startTime.UnixMilli(),
						EndTime:          time.Now().UnixMilli(),
						Output:           stdout,
						Description:      params.Description,
						Background:       params.RunInBackground,
						WorkingDirectory: bgShell.WorkingDir,
						TTY:              bgShell.TTY,
					}
					if stdout == "" {
						return fantasy.WithResponseMetadata(fantasy.NewTextResponse(BashNoOutput), metadata), nil
					}
					stdout += fmt.Sprintf("\n\n<cwd>%s</cwd>", normalizeWorkingDir(bgShell.WorkingDir))
					return fantasy.WithResponseMetadata(fantasy.NewTextResponse(stdout), metadata), nil
				}

				// Still running after fast-failure check - return as background job
				metadata := BashResponseMetadata{
					StartTime:        startTime.UnixMilli(),
					EndTime:          time.Now().UnixMilli(),
					Description:      params.Description,
					WorkingDirectory: bgShell.WorkingDir,
					Background:       true,
					ShellID:          bgShell.ID,
					TTY:              bgShell.TTY,
				}
				response := fmt.Sprintf("Background shell started with ID: %s\n\nYou will be notified automatically when it finishes (exit code + tail output). Use job_output only to fetch more output on demand, job_input to answer prompts, or job_kill to terminate.", bgShell.ID)
				return fantasy.WithResponseMetadata(fantasy.NewTextResponse(response), metadata), nil
			}

			// Start synchronous execution with auto-background support
			startTime := time.Now()

			// Start with detached context so it can survive if moved to background
			bgManager := shell.GetBackgroundShellManager()
			bgManager.Cleanup()
			bgShell, err := bgManager.Start(
				context.Background(),
				execWorkingDir,
				blockFuncs(),
				params.Command,
				params.Description,
				shell.BackgroundShellOptions{TTY: params.TTY, SessionID: sessionID},
			)
			if err != nil {
				msg := fmt.Sprintf("error starting shell: %v", err)
				record(errcoll.CategoryToolExecution, err, msg)
				return fantasy.NewTextErrorResponse(msg), nil
			}

			// Wait for either completion, auto-background threshold, or context
			// cancellation — event-driven on the job's done channel (the old
			// 100ms ticker poll added latency and wakeups for nothing).
			autoBackgroundAfter := params.AutoBackgroundAfter
			if autoBackgroundAfter <= 0 {
				autoBackgroundAfter = DefaultAutoBackgroundAfter
			}
			autoBackgroundAfter = min(autoBackgroundAfter, MaxAutoBackgroundAfter)
			timeout := time.After(time.Duration(autoBackgroundAfter) * time.Second)
			// Ctrl+B promote: hand the wait back as a background job on user
			// demand instead of waiting out the window (Claude Code Ctrl+B).
			promote := shell.PromoteSignal()

			var stdout, stderr string
			var done bool
			var execErr error

		waitLoop:
			for {
				select {
				case <-bgShell.Done():
					break waitLoop
				case <-timeout:
					if shell.BackgroundTasksDisabled() {
						// Kill switch: never move to background — keep the
						// synchronous wait window open until completion.
						timeout = time.After(time.Duration(autoBackgroundAfter) * time.Second)
						continue
					}
					break waitLoop
				case <-promote:
					// User promoted: keep the job registered as background
					// and return its ID immediately.
					metadata := BashResponseMetadata{
						StartTime:        startTime.UnixMilli(),
						EndTime:          time.Now().UnixMilli(),
						Description:      params.Description,
						WorkingDirectory: bgShell.WorkingDir,
						Background:       true,
						ShellID:          bgShell.ID,
						TTY:              bgShell.TTY,
					}
					response := fmt.Sprintf("Command moved to background by user (Ctrl+B).\n\nBackground shell ID: %s\n\nYou will be notified automatically when it finishes.", bgShell.ID)
					return fantasy.WithResponseMetadata(fantasy.NewTextResponse(response), metadata), nil
				case <-ctx.Done():
					// Incoming context was cancelled before we moved to background
					// Kill the shell and return error
					_ = bgManager.Kill(bgShell.ID)
					return fantasy.ToolResponse{}, ctx.Err()
				}
			}
			stdout, stderr, done, execErr = bgShell.GetOutput()

			if done {
				// Command completed within threshold - return synchronously
				// Remove from background manager since we're returning directly
				// Don't call Kill() as it cancels the context and corrupts the exit code
				_ = bgManager.Remove(bgShell.ID)

				interrupted := shell.IsInterrupt(execErr)
				exitCode := shell.ExitCode(execErr)
				if exitCode == 0 && !interrupted && execErr != nil {
					record(classifyCommandError(stderr), execErr, execErr.Error())
				}

				stdout = formatOutput(stdout, stderr, execErr)

				metadata := BashResponseMetadata{
					StartTime:        startTime.UnixMilli(),
					EndTime:          time.Now().UnixMilli(),
					Output:           stdout,
					Description:      params.Description,
					Background:       params.RunInBackground,
					WorkingDirectory: bgShell.WorkingDir,
					TTY:              bgShell.TTY,
				}
				if stdout == "" {
					return fantasy.WithResponseMetadata(fantasy.NewTextResponse(BashNoOutput), metadata), nil
				}
				stdout += fmt.Sprintf("\n\n<cwd>%s</cwd>", normalizeWorkingDir(bgShell.WorkingDir))
				return fantasy.WithResponseMetadata(fantasy.NewTextResponse(stdout), metadata), nil
			}

			// Still running - keep as background job
			metadata := BashResponseMetadata{
				StartTime:        startTime.UnixMilli(),
				EndTime:          time.Now().UnixMilli(),
				Description:      params.Description,
				WorkingDirectory: bgShell.WorkingDir,
				Background:       true,
				ShellID:          bgShell.ID,
				TTY:              bgShell.TTY,
			}
			response := fmt.Sprintf("Command is taking longer than expected and has been moved to background.\n\nBackground shell ID: %s\n\nUse job_output to view output, job_input to answer prompts, or job_kill to terminate.", bgShell.ID)
			return fantasy.WithResponseMetadata(fantasy.NewTextResponse(response), metadata), nil
		},
	)
}

// formatOutput formats the output of a completed command with error handling
func formatOutput(stdout, stderr string, execErr error) string {
	interrupted := shell.IsInterrupt(execErr)
	exitCode := shell.ExitCode(execErr)

	// Detect a missing-command failure on the *original* stderr so the routing
	// hint never loses the diagnostic to truncation.
	routingHint := ""
	if execErr != nil {
		routingHint = commandRoutingHint(stderr)
	}

	stdout = truncateOutput(stdout)
	stderr = truncateOutput(stderr)

	errorMessage := stderr
	if errorMessage == "" && execErr != nil {
		errorMessage = execErr.Error()
	}

	if interrupted {
		if errorMessage != "" {
			errorMessage += "\n"
		}
		errorMessage += "Command was aborted before completion"
	} else if exitCode != 0 {
		if errorMessage != "" {
			errorMessage += "\n"
		}
		errorMessage += fmt.Sprintf("Exit code %d", exitCode)
	}

	if routingHint != "" {
		if errorMessage != "" {
			errorMessage += "\n\n"
		}
		errorMessage += routingHint
	}

	hasBothOutputs := stdout != "" && stderr != ""

	if hasBothOutputs {
		stdout += "\n"
	}

	if errorMessage != "" {
		stdout += "\n" + errorMessage
	}

	return stdout
}

func truncateOutput(content string) string {
	if len(content) <= MaxOutputLength {
		return content
	}

	halfLength := MaxOutputLength / 2
	start := content[:halfLength]
	end := content[len(content)-halfLength:]

	truncatedLinesCount := countLines(content[halfLength : len(content)-halfLength])
	return fmt.Sprintf("%s\n\n... [%d lines truncated] ...\n\n%s", start, truncatedLinesCount, end)
}

func countLines(s string) int {
	if s == "" {
		return 0
	}
	return len(strings.Split(s, "\n"))
}

func normalizeWorkingDir(path string) string {
	if runtime.GOOS == "windows" {
		path = strings.ReplaceAll(path, fsext.WindowsWorkingDirDrive(), "")
	}
	return filepath.ToSlash(path)
}
