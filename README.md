# mocode

Terminal-based AI coding assistant with multi-model support, LSP integration, and MCP extensibility.

## Project

- **Module**: `github.com/package-register/mocode`
- **Go Version**: 1.26.2
- **Binary Name**: `mocode`

## Configuration

### Config File Priority

1. `mocode.json` / `.mocode.json` from parent directories to the working directory
2. `.mocode/mocode.json` workspace data config
3. `$HOME/.config/mocode/mocode.json` global config
4. `$HOME/.local/share/mocode/mocode.json` data override config

### Data Location

```bash
# Unix
$HOME/.local/share/mocode/mocode.json

# Windows
%LOCALAPPDATA%\mocode\mocode.json
```

Override with env vars:
- `MOCODE_GLOBAL_CONFIG`
- `MOCODE_GLOBAL_DATA`

### Core Config Structure

```json
{
  "$schema": "https://charm.land/mocode.json",
  "providers": {},
  "lsp": {},
  "mcp": {},
  "permissions": {},
  "options": {}
}
```

### Environment Variables for Providers

| Variable | Provider |
|----------|----------|
| `ANTHROPIC_API_KEY` | Anthropic |
| `OPENAI_API_KEY` | OpenAI |
| `GEMINI_API_KEY` | Google Gemini |
| `GROQ_API_KEY` | Groq |
| `OPENROUTER_API_KEY` | OpenRouter |
| `AWS_ACCESS_KEY_ID` | Amazon Bedrock |
| `AWS_SECRET_ACCESS_KEY` | Amazon Bedrock |
| `AWS_REGION` | Amazon Bedrock |
| `VERTEXAI_PROJECT` | Google VertexAI |
| `VERTEXAI_LOCATION` | Google VertexAI |

### LSP Configuration

```json
{
  "lsp": {
    "go": {
      "command": "gopls",
      "env": {
        "GOTOOLCHAIN": "go1.24.5"
      }
    },
    "typescript": {
      "command": "typescript-language-server",
      "args": ["--stdio"]
    }
  }
}
```

### MCP Configuration

```json
{
  "mcp": {
    "filesystem": {
      "type": "stdio",
      "command": "node",
      "args": ["/path/to/mcp-server.js"],
      "timeout": 120,
      "disabled": false,
      "disabled_tools": ["some-tool-name"]
    },
    "remote": {
      "type": "http",
      "url": "https://example.com/mcp/",
      "timeout": 120,
      "headers": {
        "Authorization": "Bearer $REMOTE_MCP_TOKEN"
      }
    }
  }
}
```

### Options

```json
{
  "options": {
    "debug": false,
    "debug_lsp": false,
    "disabled_tools": ["bash", "sourcegraph"],
    "disabled_skills": ["mocode-config"],
    "disable_notifications": false,
    "disable_provider_auto_update": false,
    "initialize_as": "AGENTS.md",
    "skills_paths": ["~/.config/mocode/skills"],
    "attribution": {
      "trailer_style": "assisted-by",
      "generated_with": true
    }
  }
}
```

### Permissions

```json
{
  "permissions": {
    "allowed_tools": ["view", "ls", "grep", "edit"]
  }
}
```

### Custom Provider (OpenAI-compatible)

```json
{
  "providers": {
    "deepseek": {
      "type": "openai-compat",
      "base_url": "https://api.deepseek.com/v1",
      "api_key": "$DEEPSEEK_API_KEY",
      "models": [
        {
          "id": "deepseek-chat",
          "name": "Deepseek V3",
          "context_window": 64000,
          "default_max_tokens": 5000
        }
      ]
    }
  }
}
```

### Custom Provider (Anthropic-compatible)

```json
{
  "providers": {
    "custom-anthropic": {
      "type": "anthropic",
      "base_url": "https://api.anthropic.com/v1",
      "api_key": "$ANTHROPIC_API_KEY",
      "extra_headers": {
        "anthropic-version": "2023-06-01"
      },
      "models": [
        {
          "id": "claude-sonnet-4-20250514",
          "name": "Claude Sonnet 4",
          "context_window": 200000,
          "default_max_tokens": 50000,
          "can_reason": true,
          "supports_attachments": true
        }
      ]
    }
  }
}
```

## Build

```bash
task build    # go build -buildvcs=false
task test     # go test -race -failfast ./...
task lint     # CI lint profile on changed files
task lint:strict # full legacy golangci-lint suite
task fmt      # gofumpt -w .
task dev      # MOCODE_PROFILE=true go run -buildvcs=false .
```

If you build from a source snapshot without usable Git metadata, use:

```bash
go build -buildvcs=false -o ./mocode .
```

## Skills Paths

Global:
- `$MOCODE_SKILLS_DIR`
- `~/.config/agents/skills`
- `~/.config/mocode/skills`
- `%LOCALAPPDATA%\agents\skills` (Windows)
- `%LOCALAPPDATA%\mocode\skills` (Windows)

Project:
- `.agents/skills`
- `.mocode/skills`
- `.claude/skills`
- `.cursor/skills`

## Logging

Location: `./.mocode/logs/mocode.log`

```bash
mocode logs           # Last 1000 lines
mocode logs --tail 500
mocode logs --follow
```
