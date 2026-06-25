---
name: mocode-config
description: >-
  Use when the user asks about Mocode's configuration, settings, providers,
  models, MCP servers, LSP setup, environment variables, tools, permissions,
  hooks, agents, modes, or any mocode.json related questions. Also use when
  the user wants to change how Mocode behaves — set up a new provider/model,
  add an MCP server, configure LSP for a language, enable/disable tools or
  skills, tweak TUI appearance, configure network proxy, recording/records,
  attribution/trailer style, or manage agent modes. Covers everything in
  mocode.json from $schema to options.
---

# Mocode Configuration

Mocode uses JSON configuration files with the following priority (highest to lowest):

| Priority | File | Scope |
|----------|------|-------|
| 1 | `.mocode.json` (dotfile, hidden) | Project-local |
| 2 | `mocode.json` (visible) | Project-local |
| 3 | `%LOCALAPPDATA%/Mocode/mocode.json` (Windows) | Global user |
| 3 | `$XDG_CONFIG_HOME/mocode/mocode.json` or `~/.config/mocode/mocode.json` (Linux/macOS) | Global user |

---

## Table of Contents

- [Basic Structure](#basic-structure)
- [Model Selection (`models`)](#model-selection-models)
- [Providers (`providers`)](#providers-providers)
- [LSP Configuration (`lsp`)](#lsp-configuration-lsp)
- [MCP Servers (`mcp`)](#mcp-servers-mcp)
- [Options (`options`)](#options-options)
- [Permissions (`permissions`)](#permissions-permissions)
- [Hooks (`hooks`)](#hooks-hooks)
- [Tools (`tools`)](#tools-tools)
- [Agents / Modes](#agents--modes)
- [All Available Tools](#all-available-tools)
- [System Capabilities](#system-capabilities)
- [Environment Variables](#environment-variables)

---

## Basic Structure

```json
{
  "$schema": "./mocode-schema.json",
  "models": {},
  "providers": {},
  "mcp": {},
  "lsp": {},
  "hooks": {},
  "options": {},
  "permissions": {},
  "tools": {}
}
```

The `$schema` property enables IDE autocomplete and validation; it's optional but recommended.

---

## Model Selection (`models`)

Two model slots — `large` (primary coding) and `small` (lightweight tasks like summarization).

```json
{
  "models": {
    "large": {
      "model": "claude-sonnet-4-20250514",
      "provider": "anthropic",
      "max_tokens": 16384,
      "temperature": 0.7,
      "reasoning_effort": "high"
    },
    "small": {
      "model": "gpt-4o-mini",
      "provider": "openai"
    }
  }
}
```

**All fields:**

| Field | Type | Description |
|-------|------|-------------|
| `model` | string | **Required.** Model ID as used by the provider API |
| `provider` | string | **Required.** Must match a key in `providers` |
| `reasoning_effort` | string | For OpenAI models: `low`, `medium`, `high` |
| `think` | bool | Enable thinking mode for Anthropic reasoning models |
| `max_tokens` | int | Maximum response tokens |
| `temperature` | float | Sampling temperature (0-1) |
| `top_p` | float | Nucleus sampling (0-1) |
| `top_k` | int | Top-k sampling |
| `frequency_penalty` | float | Reduce repetition |
| `presence_penalty` | float | Increase topic diversity |
| `provider_options` | object | Provider-specific overrides |

---

## Providers (`providers`)

```json
{
  "providers": {
    "my-openai": {
      "type": "openai",
      "base_url": "https://api.openai.com/v1",
      "api_key": "$OPENAI_API_KEY",
      "models": [
        {
          "id": "gpt-4o",
          "name": "GPT-4o",
          "context_window": 128000
        }
      ]
    },
    "deepseek": {
      "type": "openai-compat",
      "base_url": "https://api.deepseek.com/v1",
      "api_key": "$DEEPSEEK_API_KEY",
      "models": [
        { "id": "deepseek-chat", "name": "DeepSeek V3", "context_window": 64000 }
      ]
    }
  }
}
```

**All fields:**

| Field | Type | Description |
|-------|------|-------------|
| `type` | string | `openai`, `openai-compat`, `anthropic`, `gemini`, `azure`, `vertexai`. Default: `openai` |
| `base_url` | string | Provider API endpoint URL |
| `api_key` | string | Supports `$ENV_VAR` syntax for environment variables |
| `name` | string | Human-readable display name |
| `disable` | bool | Mark as disabled without removing |
| `models` | array | List of available model definitions |
| `system_prompt_prefix` | string | Custom prefix prepended to system prompts |
| `extra_headers` | object | Additional HTTP headers sent with every request |
| `extra_body` | object | Extra JSON fields in request body (openai-compat only) |
| `provider_options` | object | Additional provider-specific options |
| `oauth` | object | OAuth2 token for providers that support it |

**Model definition within a provider:**

```json
{
  "id": "gpt-4o",
  "name": "GPT-4o",
  "context_window": 128000,
  "cost_per_1m_in": 2.5,
  "cost_per_1m_out": 10,
  "cost_per_1m_in_cached": 1.25,
  "cost_per_1m_out_cached": 5,
  "default_max_tokens": 4096,
  "can_reason": true,
  "reasoning_levels": ["low", "medium", "high"],
  "default_reasoning_effort": "medium",
  "supports_images": true
}
```

**Built-in default providers** (automatically available unless `disable_default_providers` is set):
- Anthropic (Claude models)
- OpenAI (GPT models)
- Gemini (Google models)
- These are auto-configured with known model lists and pricing

---

## LSP Configuration (`lsp`)

Language Servers provide IDE-like code intelligence (diagnostics, completions, references, go-to-definition).

```json
{
  "lsp": {
    "gopls": {
      "command": "gopls",
      "options": {
        "gofumpt": true,
        "staticcheck": true,
        "semanticTokens": true,
        "directoryFilters": ["-.git", "-node_modules"],
        "codelenses": {
          "gc_details": true,
          "generate": true,
          "run_govulncheck": true,
          "test": true,
          "tidy": true,
          "upgrade_dependency": true
        },
        "hints": {
          "assignVariableTypes": true,
          "compositeLiteralFields": true,
          "compositeLiteralTypes": true,
          "constantValues": true,
          "functionTypeParameters": true,
          "parameterNames": true,
          "rangeVariableTypes": true
        },
        "analyses": {
          "nilness": true,
          "unusedparams": true,
          "unusedvariable": true,
          "unusedwrite": true,
          "useany": true
        }
      }
    },
    "typescript": {
      "command": "typescript-language-server",
      "args": ["--stdio"],
      "filetypes": ["typescript", "typescriptreact"]
    }
  }
}
```

**All fields:**

| Field | Type | Description |
|-------|------|-------------|
| `command` | string | **Required.** LSP server executable |
| `args` | array | CLI arguments passed to the server |
| `env` | object | Environment variables for the server process |
| `disabled` | bool | Disable without removing |
| `filetypes` | array | File extensions to associate (e.g. `["go", "gomod"]`) |
| `root_markers` | array | Root markers like `["go.mod", ".git"]` |
| `init_options` | object | Initialization options passed to the server |
| `options` | object | Server-specific settings (sent via `workspace/didChangeConfiguration`) |
| `timeout` | int | Server timeout in seconds |

**Auto-LSP**: When `options.auto_lsp` is enabled (default), Mocode auto-detects LSP servers based on root markers (e.g. `go.mod` → gopls, `package.json` → typescript-language-server, `Cargo.toml` → rust-analyzer).

**LSP tools available to the agent:**
- `diagnostics` — query diagnostics for a file
- `references` — find references to a symbol
- `lsp_restart` — restart an LSP server

---

## MCP Servers (`mcp`)

Model Context Protocol servers extend Mocode with external tools and resources.

```json
{
  "mcp": {
    "filesystem": {
      "type": "stdio",
      "command": "node",
      "args": ["/path/to/mcp-server.js"]
    },
    "remote-api": {
      "type": "http",
      "url": "https://api.example.com/mcp",
      "headers": {
        "Authorization": "Bearer $TOKEN"
      },
      "disabled_tools": ["delete-all"]
    },
    "docker": {
      "type": "stdio",
      "command": "docker",
      "args": ["run", "-i", "--rm", "mcp/docker"]
    }
  }
}
```

**All fields:**

| Field | Type | Description |
|-------|------|-------------|
| `type` | string | **Required.** `stdio` (local process), `sse` (Server-Sent Events), or `http` |
| `command` | string | Command to execute (for `stdio` type) |
| `args` | array | Arguments for the command |
| `url` | string | URL endpoint (for `http`/`sse` types) |
| `env` | object | Environment variables for the server process |
| `headers` | object | HTTP headers (for `http`/`sse` types) |
| `disabled` | bool | Disable without removing |
| `disabled_tools` | array | Specific MCP tools to block (e.g. `["delete-all"]`) |
| `timeout` | int | Connection timeout in seconds |

**MCP tools available to the agent:**
- `list_mcp_resources` — list available resources from an MCP server
- `read_mcp_resource` — read a specific MCP resource

---

## Options (`options`)

All behavioral knobs in one place.

```json
{
  "options": {
    "active_mode": "skplan",
    "disabled_skills": ["mocode-config"],
    "disabled_tools": ["sourcegraph"],
    "skills_paths": ["./agents/skills"],
    "auto_lsp": true,
    "progress": true,
    "debug": false,
    "debug_lsp": false,
    "disable_auto_summarize": false,
    "disable_notifications": false,
    "disable_provider_auto_update": false,
    "disable_default_providers": false,
    "data_directory": "~/.mocode-data",
    "initialize_as": "coder",
    "agents_dir": ".mocode/agents",
    "tui": {
      "compact_mode": false,
      "diff_mode": "unified",
      "transparent": false,
      "completions": {
        "max_depth": 3,
        "max_items": 50
      }
    },
    "attribution": {
      "trailer_style": "assisted-by",
      "generated_with": true
    },
    "network": {
      "enabled": true,
      "proxy_url": "http://proxy:8080",
      "no_proxy": "localhost,127.0.0.1"
    },
    "records": {
      "enabled": false,
      "path": "./records",
      "record_types": ["bash", "edit"],
      "max_file_size_mb": 100
    },
    "context_paths": ["./CONTEXT.md"]
  }
}
```

### All Options Reference

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| **Skills & Tools** | | | |
| `skills_paths` | string[] | `[]` | Extra paths to search for SKILL.md files. Default paths (`.agents/skills/`, `.mocode/skills/`, `.claude/skills/`, `.cursor/skills/`) are always loaded |
| `disabled_skills` | string[] | `[]` | Disable builtin or local skills by name |
| `disabled_tools` | string[] | `[]` | Disable built-in tools by name |
| **LSP** | | | |
| `auto_lsp` | bool | `true` | Auto-detect and start LSP servers |
| `debug_lsp` | bool | `false` | Enable LSP debug logging |
| **TUI Appearance** | | | |
| `tui.compact_mode` | bool | `false` | Compact UI layout |
| `tui.diff_mode` | string | `"unified"` | Diff display mode |
| `tui.transparent` | bool | `false` | Transparent background support |
| `tui.completions.max_depth` | int | `3` | Max tab-completion depth |
| `tui.completions.max_items` | int | `50` | Max tab-completion items |
| **Attribution** | | | |
| `attribution.trailer_style` | string | `"assisted-by"` | Footer style: `assisted-by`, `generated-by`, or `none` |
| `attribution.generated_with` | bool | `true` | Show "Generated with Mocode" tag |
| **Network** | | | |
| `network.enabled` | bool | `true` | Enable network access for tools |
| `network.proxy_url` | string | `""` | HTTP proxy URL |
| `network.no_proxy` | string | `""` | Comma-separated bypass list |
| **Recording** | | | |
| `records.enabled` | bool | `false` | Enable session recording |
| `records.path` | string | `""` | Recording output directory |
| `records.record_types` | string[] | `[]` | Tool types to record (e.g. `["bash", "edit"]`) |
| `records.max_file_size_mb` | int | `100` | Max recording file size |
| **Behavior** | | | |
| `active_mode` | string | `""` | Active agent mode name |
| `initialize_as` | string | `""` | Agent type: `coder`, `task`, `git`, `rust`, `plan` |
| `agents_dir` | string | `""` | Directory for agent/mode definitions |
| `data_directory` | string | `""` | Override data storage location |
| `debug` | bool | `false` | Enable debug mode |
| `progress` | bool | `true` | Show progress indicators |
| `disable_auto_summarize` | bool | `false` | Disable automatic session summarization |
| `disable_notifications` | bool | `false` | Disable desktop notifications |
| `disable_provider_auto_update` | bool | `false` | Disable automatic provider model list updates |
| `disable_default_providers` | bool | `false` | Disable all built-in providers (Anthropic, OpenAI, Gemini) |
| `context_paths` | string[] | `[]` | Paths to context files prepended to system prompt |

---

## Permissions (`permissions`)

```json
{
  "permissions": {
    "allowed_tools": ["view", "ls", "grep", "glob"]
  }
}
```

| Field | Type | Description |
|-------|------|-------------|
| `allowed_tools` | string[] | Tools that can execute without prompting (bypasses the permission dialog) |

When `allowed_tools` is empty or absent, **every** tool call triggers a permission prompt. Tools listed here are auto-approved.

**Per-agent permissions** (in mode/agent definitions):
```json
{
  "allowed_tools": ["view", "ls", "grep", "glob"],
  "disabled_tools": ["bash"],
  "allowed_mcp": {
    "filesystem": ["read", "write", "list"],
    "database": null
  }
}
```
- `allowed_tools`: if non-nil, restricts the agent to only these tools
- `disabled_tools`: tools to remove from the available set
- `allowed_mcp`: maps MCP server names to allowed tool list (`null` = all tools from that server)

---

## Hooks (`hooks`)

PreToolUse hooks intercept tool calls before they execute. They can allow, deny, rewrite input, or inject context.

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

Each hook receives the tool call details via stdin and environment variables, then returns an exit code + optional JSON to signal the decision.

---

## Tools (`tools`)

Currently only `ls` has a config section:

```json
{
  "tools": {
    "ls": {
      "ignore": ["node_modules", ".git", "vendor"],
      "max_files": 100
    }
  }
}
```

Controls how the `ls` tool traverses directories (ignored patterns and max file count).

---

## Agents / Modes

Agent modes are configured in separate files (e.g. `.mocode/agents/skplan.md`). Each mode can define:

| Field | Description |
|-------|-------------|
| `allowed_tools` | Restrict to specific tools |
| `disabled_tools` | Remove specific tools |
| `allowed_mcp` | Per-MCP-server tool access control |
| System prompt | Custom instructions for the mode |
| File triggers | Auto-activate when certain files are present |

**Built-in modes:** `coder`, `task`, `git`, `rust`, `plan`, `skplan`.

---

## All Available Tools

Tools are organized by category. This is the complete list of built-in and plugin tools available in Mocode.

### File Operations
| Tool | Description |
|------|-------------|
| `view` | Read file contents with line numbers |
| `read_files` | Batch-read multiple files |
| `write` | Create or overwrite files |
| `edit` | Find-and-replace edits |
| `multiedit` | Batch multiple edits in one file |
| `ls` | Directory listing with tree view |

### Execution
| Tool | Description |
|------|-------------|
| `bash` | Execute shell commands (cross-platform, `mvdan/sh`) |
| `job_output` | Read background job output |
| `job_kill` | Kill a background job |

### Search
| Tool | Description |
|------|-------------|
| `grep` | Search file contents by regex |
| `glob` | Find files by name pattern |
| `sourcegraph` | Search across GitHub repositories |

### Network
| Tool | Description |
|------|-------------|
| `fetch` | HTTP GET with format selection |
| `agentic_fetch` | Intelligent web content extraction |
| `crawl` | Recursive website crawling |
| `download_docs` | Download documentation from GitHub repos |

### Session
| Tool | Description |
|------|-------------|
| `todos` | Structured task list management |
| `session_export` | Export session transcript |
| `message_export` | Export individual messages |
| `session_summary` | Session summarization |

### Memory
| Tool | Description |
|------|-------------|
| `memory_add` | Store a memory entry |
| `memory_update` | Update a memory entry |
| `memory_delete` | Delete a memory entry |
| `memory_search` | Search memories |
| `memory_load` | Load memories into context |
| `memory_clear` | Clear cached memories |

### LSP
| Tool | Description |
|------|-------------|
| `diagnostics` | Get file diagnostics |
| `references` | Find symbol references |
| `lsp_restart` | Restart an LSP server |

### MCP Meta
| Tool | Description |
|------|-------------|
| `list_mcp_resources` | List MCP server resources |
| `read_mcp_resource` | Read a specific MCP resource |

### Mocode
| Tool | Description |
|------|-------------|
| `mocode_info` | Get runtime state (model, provider, LSP/MCP/skills status) |
| `mocode_logs` | Retrieve system logs |

### Reasoning
| Tool | Description |
|------|-------------|
| `think` | Structured multi-step reasoning |

### Coordination
| Tool | Description |
|------|-------------|
| `agent` | Delegate tasks to sub-agents |
| `transfer_to_agent` | Transfer control to another agent |

### Gitea
| Tool | Description |
|------|-------------|
| `gitea_issues` | List/search Gitea issues |
| `gitea_pulls` | List/search pull requests |
| `gitea_notifications` | Get Gitea notifications |

### WeChat (plugin)
| Tool | Description |
|------|-------------|
| `send_wechat_image` | Send image via WeChat |
| `send_wechat_file` | Send file via WeChat |
| `screenshot_to_wechat` | Screenshot + send to WeChat |

---

## System Capabilities

| Capability | Description |
|------------|-------------|
| **Multi-provider** | Anthropic, OpenAI, Gemini, Azure, VertexAI, any openai-compatible endpoint |
| **LSP integration** | Go, TypeScript, Rust, Python — auto-detected by project files |
| **MCP protocol** | Extend with external tool servers via stdio/HTTP/SSE |
| **Skills system** | Agent instructions in SKILL.md format, auto-discovered from multiple paths |
| **Memory** | Persistent key-value store with vector search |
| **Session recording** | Record tool calls for replay/audit |
| **Sub-agents** | Parallel task delegation via `agent` tool |
| **Agent modes** | Switchable personalities: coder, task, plan, git, rust, etc. |
| **Hooks** | PreToolUse interception for security/guardrails |
| **Permissions** | Granular tool-level allow/deny with session persistence |
| **Attribution** | Optional "assisted by Mocode" footer in outputs |
| **Notifications** | Desktop notifications on completion |
| **Network proxy** | HTTP proxy support for restricted environments |
| **Cross-platform** | Windows, macOS, Linux — same config format |

---

## Environment Variables

| Variable | Description |
|----------|-------------|
| `MOCODE_GLOBAL_CONFIG` | Override global config file location |
| `MOCODE_GLOBAL_DATA` | Override data directory location |
| `MOCODE_SKILLS_DIR` | Override default skills directory |
| `CATWALK_URL` | Custom provider list URL (for auto-update) |
| `LOCALAPPDATA` | (Windows) Config location base |
| `XDG_CONFIG_HOME` | (Linux/macOS) Config location base |
| `XDG_DATA_HOME` | (Linux/macOS) Data location base |

---

## Quick Reference — Common Tasks

| Task | What to do |
|------|-----------|
| **Add a custom provider** | Add to `providers` with `type`, `base_url`, `api_key`, `models` |
| **Switch models** | Update `models.large` and/or `models.small` |
| **Disable a skill** | Add skill name to `options.disabled_skills` |
| **Disable a tool** | Add tool name to `options.disabled_tools` |
| **Add an MCP server** | Add to `mcp` with `type` and `command` (stdio) or `url` (http/sse) |
| **Configure LSP** | Add to `lsp` with `command` and language-specific `options` |
| **Enable auto-approve tools** | List tools in `permissions.allowed_tools` |
| **Change UI** | Tweak `options.tui` — `compact_mode`, `diff_mode`, `transparent` |
| **Set up proxy** | Use `options.network` with `proxy_url` |
| **Record sessions** | Enable `options.records.enabled` |
| **Use custom agent mode** | Set `options.active_mode` to the mode name |
