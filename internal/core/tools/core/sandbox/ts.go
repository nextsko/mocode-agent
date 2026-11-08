package sandbox

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nextsko/mocode-agent/internal/core/agent/toolutil"
	"github.com/nextsko/mocode-agent/internal/core/permission"
	"github.com/nextsko/mocode-agent/internal/core/tools/contract"
)

const TsRunToolName = "ts_run"

//go:embed ts_run.md
var tsRunDescription []byte

type TsRunParams struct {
	Code       string `json:"code" description:"TypeScript/JavaScript source to execute"`
	TimeoutSec int    `json:"timeout_sec,omitempty" description:"Wall-clock timeout in seconds (default 60, max 300)"`
	WorkingDir string `json:"working_dir,omitempty" description:"Optional base directory; the sandbox still runs in an isolated temp dir, but this dir is readable via the env var SANDBOX_WORKDIR"`
}

// NewTsRunPortTool is the framework-agnostic form of the TypeScript sandbox
// tool (bun preferred, node fallback).
func NewTsRunPortTool(permissions permission.Service, workingDir string) contract.Tool {
	return contract.NewFunc(
		TsRunToolName,
		toolutil.FirstLineDescription(tsRunDescription),
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"code": map[string]any{
					"type":        "string",
					"description": "TypeScript/JavaScript source to execute",
				},
				"timeout_sec": map[string]any{
					"type":        "integer",
					"description": "Wall-clock timeout in seconds (default 60, max 300)",
				},
				"working_dir": map[string]any{
					"type":        "string",
					"description": "Optional base directory; the sandbox still runs in an isolated temp dir, but this dir is readable via the env var SANDBOX_WORKDIR",
				},
			},
			"required": []string{"code"},
		},
		func(ctx context.Context, _ contract.ToolContext, args json.RawMessage) (contract.ToolResult, error) {
			var params TsRunParams
			if len(args) > 0 {
				if err := json.Unmarshal(args, &params); err != nil {
					return contract.ToolResult{}, fmt.Errorf("invalid parameters: %w", err)
				}
			}
			callID, _ := toolutil.ToolCallIDFromCtx(ctx)
			return runTsRun(ctx, permissions, callID, params)
		},
	)
}

// runTsRun materializes the snippet in a throwaway dir and executes it.
func runTsRun(ctx context.Context, permissions permission.Service, callID string, params TsRunParams) (contract.ToolResult, error) {
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
		ToolName:    TsRunToolName,
		Action:      "execute",
		Description: "Execute TypeScript in an isolated sandbox",
		Params:      params,
	})
	if err != nil || !approved {
		return permissionDeniedResult(), nil
	}

	runtime := lookupRuntime("bun", "node")
	if runtime == "" {
		return contract.Error("no TypeScript runtime found: install bun (recommended) or node"), nil
	}

	dir, err := os.MkdirTemp("", "mocode-ts-*")
	if err != nil {
		return contract.Error(fmt.Sprintf("create sandbox dir: %v", err)), nil
	}
	defer func() { _ = os.RemoveAll(dir) }()

	script := filepath.Join(dir, "main.ts")
	if err := os.WriteFile(script, []byte(params.Code), 0o600); err != nil {
		return contract.Error(fmt.Sprintf("write script: %v", err)), nil
	}

	argv := []string{runtime, "run", script}
	env := os.Environ()
	if params.WorkingDir != "" {
		env = append(env, "SANDBOX_WORKDIR="+params.WorkingDir)
	}
	res, err := runInDir(ctx, dir, argv, env, timeSeconds(params.TimeoutSec))
	if err != nil {
		return contract.Error(err.Error()), nil
	}
	return contract.Text(formatResult("ts", res)), nil
}

func timeSeconds(sec int) time.Duration {
	if sec <= 0 {
		return 0
	}
	return time.Duration(sec) * time.Second
}

func formatResult(lang string, res result) string {
	label := fmt.Sprintf("[%s sandbox] runtime ok · exit %d · %s", lang, res.ExitCode, res.Duration)
	out := strings.TrimSpace(res.Output)
	if out == "" {
		return label + "\n(no output)"
	}
	return label + "\n" + out
}
