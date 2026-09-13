package sandbox

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"charm.land/fantasy"

	"github.com/nextsko/mocode-agent/internal/core/agent/toolutil"
	"github.com/nextsko/mocode-agent/internal/core/permission"
)

const PyRunToolName = "py_run"

//go:embed py_run.md
var pyRunDescription []byte

type PyRunParams struct {
	Code       string   `json:"code" description:"Python source to execute"`
	Deps       []string `json:"deps,omitempty" description:"Ephemeral dependencies resolved by uv (--with), e.g. [\"numpy\", \"pandas>=2\"]. Ignored when uv is unavailable."`
	TimeoutSec int      `json:"timeout_sec,omitempty" description:"Wall-clock timeout in seconds (default 60, max 300)"`
	WorkingDir string   `json:"working_dir,omitempty" description:"Optional base directory exposed to the script via the env var SANDBOX_WORKDIR"`
}

// NewPyRunTool builds the Python sandbox tool (uv preferred for ephemeral
// dependency environments, plain python fallback).
func NewPyRunTool(permissions permission.Service, workingDir string) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		PyRunToolName,
		toolutil.FirstLineDescription(pyRunDescription),
		func(ctx context.Context, params PyRunParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if strings.TrimSpace(params.Code) == "" {
				return fantasy.NewTextErrorResponse("missing code"), nil
			}
			sessionID := toolutil.GetSessionFromContext(ctx)
			if sessionID == "" {
				return fantasy.NewTextErrorResponse("session ID is required"), nil
			}
			approved, err := permissions.Request(ctx, permission.CreatePermissionRequest{
				SessionID:   sessionID,
				Path:        params.WorkingDir,
				ToolCallID:  call.ID,
				ToolName:    PyRunToolName,
				Action:      "execute",
				Description: "Execute Python in an isolated sandbox",
				Params:      params,
			})
			if err != nil || !approved {
				return toolutil.NewPermissionDeniedResponse(), nil
			}

			uv := lookupRuntime("uv")
			python := lookupRuntime("python", "python3")
			if uv == "" && python == "" {
				return fantasy.NewTextErrorResponse("no Python runtime found: install uv (recommended) or python"), nil
			}

			dir, err := os.MkdirTemp("", "mocode-py-*")
			if err != nil {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("create sandbox dir: %v", err)), nil
			}
			defer os.RemoveAll(dir)

			script := filepath.Join(dir, "main.py")
			if err := os.WriteFile(script, []byte(params.Code), 0o600); err != nil {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("write script: %v", err)), nil
			}

			var argv []string
			label := "python"
			if uv != "" {
				argv = []string{uv, "run", "--no-progress"}
				for _, d := range params.Deps {
					argv = append(argv, "--with", d)
				}
				argv = append(argv, script)
				label = "uv python"
			} else {
				if len(params.Deps) > 0 {
					return fantasy.NewTextErrorResponse("deps require uv (https://docs.astral.sh/uv/); it is not installed"), nil
				}
				argv = []string{python, script}
			}

			env := os.Environ()
			if params.WorkingDir != "" {
				env = append(env, "SANDBOX_WORKDIR="+params.WorkingDir)
			}
			res, err := runInDir(ctx, dir, argv, env, timeSeconds(params.TimeoutSec))
			if err != nil {
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}
			return fantasy.NewTextResponse(formatResult(label, res)), nil
		},
	)
}
