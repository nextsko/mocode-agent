---
name: mocode-hooks
description: >-
  Use when the user asks about Mocode hooks, PreToolUse interception,
  tool call guarding, hook scripts, hook configuration, or wants to set up
  security guardrails that intercept tool calls before or after execution.
  Covers the hooks section of mocode.json including matcher patterns,
  shell command hooks, timeout settings, and the allow/deny/halt decision model.
---

# Mocode Hooks

Hooks are user-configured shell commands that fire on specific events during a session. They can **allow**, **deny**, or **rewrite** tool calls before execution, enabling security guardrails, audit logging, and custom policies.

---

## Configuration

Hooks are defined in `mocode.json` under the `hooks` key:

```json
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "^(edit|write|multiedit)$",
        "command": ".mocode/hooks/protect-files.sh",
        "timeout": 10
      }
    ]
  }
}
```

### Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `matcher` | string | No | Regex pattern to match tool names. If omitted, matches all tools |
| `command` | string | Yes | Shell command to execute |
| `timeout` | int | No | Timeout in seconds (default: 60) |

---

## Events

| Event | When it fires |
|-------|--------------|
| `PreToolUse` | Before a tool call executes |
| `pre_tool_use` | Alias (auto-normalized to `PreToolUse`) |

---

## Hook Execution

1. When a tool call is made, Mocode finds all hooks whose `matcher` regex matches the tool name
2. Matching hooks run in parallel as shell commands
3. Each hook receives context via **environment variables** and **stdin**
4. The hook's exit code and stdout determine the decision

### Environment Variables

| Variable | Description |
|----------|-------------|
| `MOCODE_HOOK_EVENT` | Event name (e.g. `PreToolUse`) |
| `MOCODE_TOOL_NAME` | Name of the tool being called |
| `MOCODE_SESSION_ID` | Current session ID |
| `MOCODE_CWD` | Current working directory |
| `MOCODE_PROJECT_DIR` | Project root directory |
| `MOCODE_TOOL_INPUT` | JSON string of the tool's input parameters |

### Stdin Payload

The hook also receives a JSON payload on stdin with the same information:

```json
{
  "event": "PreToolUse",
  "tool_name": "bash",
  "session_id": "abc-123",
  "cwd": "/home/user/project",
  "project_dir": "/home/user/project",
  "tool_input": { "command": "rm -rf /" }
}
```

---

## Decision Model

### Exit Codes

| Exit Code | Meaning |
|-----------|---------|
| `0` | **Allow** — tool call proceeds |
| `2` | **Deny** — tool call is blocked |
| `49` | **Halt** — stop all further hook evaluation |
| Other | Non-blocking, ignored |

### JSON Output

Hooks can also return structured JSON on stdout for finer control:

```json
{
  "decision": "allow",
  "reason": "File is not protected",
  "context": ["Verified path is outside .git/"],
  "updated_input": { "command": "ls -la" },
  "halt": false
}
```

| Field | Type | Description |
|-------|------|-------------|
| `decision` | string | `allow` or `deny` |
| `reason` | string | Human-readable explanation |
| `context` | string[] | Additional context injected into the agent |
| `updated_input` | object | Rewritten tool input (allows modification) |
| `halt` | bool | If true, stops further hook processing |

### Aggregation

When multiple hooks match:

- **Any deny** → final result is deny (reasons are concatenated)
- **All allow** → final result is allow
- **Halt** is sticky — once set, no further hooks run
- `updated_input` patches are shallow-merged in config order (later hooks override)

---

## Claude Code Compatibility

Hooks also accept the Claude Code JSON format:

```json
{
  "hookSpecificOutput": {
    "decision": "allow",
    "reason": "OK"
  }
}
```

---

## Examples

### Protect Sensitive Files

```json
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "^(edit|write|multiedit)$",
        "command": "bash -c 'input=$(cat); file=$(echo \"$input\" | jq -r .tool_input.file_path // \"\"); case \"$file\" in *secret*|*.env*) echo \\\"{\\\\\\\"decision\\\\\\\":\\\\\\\"deny\\\\\\\",\\\\\\\"reason\\\\\\\":\\\\\\\"Protected file\\\\\\\"}\\\"; exit 2;; esac'"
      }
    ]
  }
}
```

### Audit All Bash Commands

```json
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "^bash$",
        "command": "bash -c 'echo \"$(date) $MOCODE_TOOL_INPUT\" >> .mocode/audit.log'"
      }
    ]
  }
}
```

### Block Dangerous Operations

```json
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "^bash$",
        "command": ".mocode/hooks/safety-check.sh",
        "timeout": 5
      }
    ]
  }
}
```

---

## Best Practices

1. **Keep hooks fast** — they block the tool pipeline. Set appropriate timeouts
2. **Use specific matchers** — avoid `.*` when you only need to guard certain tools
3. **Test hooks independently** — run them with sample stdin before deploying
4. **Prefer deny over halt** — halt prevents other hooks from running
5. **Log for audit** — append to a log file for security review
