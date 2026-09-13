package sandbox

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"charm.land/fantasy"

	"github.com/nextsko/mocode-agent/internal/core/agent/toolutil"
	"github.com/nextsko/mocode-agent/internal/core/permission"
)

const TsRunToolName = "ts_run"

//go:embed ts_run.md
var tsRunDescription []byte

type TsRunParams struct {
	Code       string `json:"code" description:"TypeScript/JavaScript source to execute"`
	TimeoutSec int    `json:"timeout_sec,omitempty" description:"Wall-clock timeout in seconds (default 60, max 300)"`
	WorkingDir string `json:"working_dir,omitempty" description:"Optional base directory; the sandbox still runs in an isolated temp dir, but this dir is readable via the env var SANDBOX_WORKDIR"`
}

// NewTsRunTool builds the TypeScript sandbox tool (bun preferred, node fallback).
func NewTsRunTool(permissions permission.Service, workingDir string) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		TsRunToolName,
		toolutil.FirstLineDescription(tsRunDescription),
		func(ctx context.Context, params TsRunParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
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
				ToolName:    TsRunToolName,
				Action:      "execute",
				Description: "Execute TypeScript in an isolated sandbox",
				Params:      params,
			})
			if err != nil || !approved {
				return toolutil.NewPermissionDeniedResponse(), nil
			}

			runtime := lookupRuntime("bun", "node")
			if runtime == "" {
				return fantasy.NewTextErrorResponse("no TypeScript runtime found: install bun (recommended) or node"), nil
			}

			dir, err := os.MkdirTemp("", "mocode-ts-*")
			if err != nil {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("create sandbox dir: %v", err)), nil
			}
			defer os.RemoveAll(dir)

			script := filepath.Join(dir, "main.ts")
			if err := os.WriteFile(script, []byte(params.Code), 0o600); err != nil {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("write script: %v", err)), nil
			}

			argv := []string{runtime, "run", script}
			env := os.Environ()
			if params.WorkingDir != "" {
				env = append(env, "SANDBOX_WORKDIR="+params.WorkingDir)
			}
			res, err := runInDir(ctx, dir, argv, env, timeSeconds(params.TimeoutSec))
			if err != nil {
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}
			return fantasy.NewTextResponse(formatResult("ts", res)), nil
		},
	)
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
