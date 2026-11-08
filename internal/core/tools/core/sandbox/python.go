package sandbox

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nextsko/mocode-agent/internal/core/agent/toolutil"
	"github.com/nextsko/mocode-agent/internal/core/permission"
	"github.com/nextsko/mocode-agent/internal/core/tools/contract"
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

// NewPyRunPortTool is the framework-agnostic form of the Python sandbox tool
// (uv preferred for ephemeral dependency environments, plain python fallback).
func NewPyRunPortTool(permissions permission.Service, workingDir string) contract.Tool {
	return contract.NewFunc(
		PyRunToolName,
		toolutil.FirstLineDescription(pyRunDescription),
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"code": map[string]any{
					"type":        "string",
					"description": "Python source to execute",
				},
				"deps": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "Ephemeral dependencies resolved by uv (--with), e.g. [\"numpy\", \"pandas>=2\"]. Ignored when uv is unavailable.",
				},
				"timeout_sec": map[string]any{
					"type":        "integer",
					"description": "Wall-clock timeout in seconds (default 60, max 300)",
				},
				"working_dir": map[string]any{
					"type":        "string",
					"description": "Optional base directory exposed to the script via the env var SANDBOX_WORKDIR",
				},
			},
			"required": []string{"code"},
		},
		func(ctx context.Context, _ contract.ToolContext, args json.RawMessage) (contract.ToolResult, error) {
			var params PyRunParams
			if len(args) > 0 {
				if err := json.Unmarshal(args, &params); err != nil {
					return contract.ToolResult{}, fmt.Errorf("invalid parameters: %w", err)
				}
			}
			callID, _ := toolutil.ToolCallIDFromCtx(ctx)
			return runPyRun(ctx, permissions, callID, params)
		},
	)
}

// runPyRun materializes the snippet in a throwaway dir and executes it.
func runPyRun(ctx context.Context, permissions permission.Service, callID string, params PyRunParams) (contract.ToolResult, error) {
	if strings.TrimSpace(params.Code) == "" {
		return contract.Error("missing code"), nil
	}
	sessionID := toolutil.GetSessionFromContext(ctx)
	if sessionID == "" {
		return contract.Error("session ID is required"), nil
	}
	approved, err := permissions.Request(ctx, permission.CreatePermissionRequest{
		SessionID:   sessionID,
		Path:        params.WorkingDir,
		ToolCallID:  callID,
		ToolName:    PyRunToolName,
		Action:      "execute",
		Description: "Execute Python in an isolated sandbox",
		Params:      params,
	})
	if err != nil || !approved {
		return permissionDeniedResult(), nil
	}

	uv := lookupRuntime("uv")
	python := lookupRuntime("python", "python3")
	if uv == "" && python == "" {
		return contract.Error("no Python runtime found: install uv (recommended) or python"), nil
	}

	dir, err := os.MkdirTemp("", "mocode-py-*")
	if err != nil {
		return contract.Error(fmt.Sprintf("create sandbox dir: %v", err)), nil
	}
	defer func() { _ = os.RemoveAll(dir) }()

	script := filepath.Join(dir, "main.py")
	if err := os.WriteFile(script, []byte(params.Code), 0o600); err != nil {
		return contract.Error(fmt.Sprintf("write script: %v", err)), nil
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
			return contract.Error("deps require uv (https://docs.astral.sh/uv/); it is not installed"), nil
		}
		argv = []string{python, script}
	}

	env := os.Environ()
	if params.WorkingDir != "" {
		env = append(env, "SANDBOX_WORKDIR="+params.WorkingDir)
	}
	res, err := runInDir(ctx, dir, argv, env, timeSeconds(params.TimeoutSec))
	if err != nil {
		return contract.Error(err.Error()), nil
	}
	return contract.Text(formatResult(label, res)), nil
}
