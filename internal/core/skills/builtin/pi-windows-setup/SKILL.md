---
name: pi-windows-setup
description: >-
  Use when 在 Windows 上安装、配置或排查 pi coding agent：shellPath、PowerShell 兼容别名、
  MCP server、自定义模型、system prompt、packages 等。触发词包括 "setup pi on windows"、
  "configure pi windows"、"pi shell not working"、"pi mcp setup"、"pi model
  configuration"，或任何 pi 的 Windows 配置问题。
---

# Pi Windows Setup Guide

在 Windows 上配置 pi coding agent：shell、PowerShell 兼容、MCP、模型、system prompt、包管理。

## 1. 核心路径

| 用途 | 路径 |
|------|------|
| 全局配置目录 | `~/.pi/agent/` |
| 全局设置 | `~/.pi/agent/settings.json` |
| 全局认证 | `~/.pi/agent/auth.json` |
| System prompt（追加） | `~/.pi/agent/APPEND_SYSTEM.md` |
| System prompt（替换） | `~/.pi/agent/SYSTEM.md` |
| 自定义模型 | `~/.pi/agent/models.json` |
| MCP 配置（全局） | `~/.config/mcp/mcp.json` |
| 项目配置 | `./.pi/settings.json` |

## 2. shellPath：改用 PowerShell 7

**问题**：默认 Nu shell 对 `||`、`cat`、Windows 路径（`C:\Users\`）和中文编码支持不佳。

1. 定位 pwsh：

```powershell
Get-Command pwsh | Select-Object -ExpandProperty Source
```

常见路径：`C:\Program Files\WindowsApps\Microsoft.PowerShell_7.6.4.0_x64__8wekyb3d8bbwe\pwsh.exe`

2. 写入 `settings.json`（JSON 中反斜杠必须双写）：

```json
{
  "shellPath": "C:\\Program Files\\WindowsApps\\Microsoft.PowerShell_7.6.4.0_x64__8wekyb3d8bbwe\\pwsh.exe"
}
```

3. **完全重启 pi** 才生效。

> 通过 mocode 的 `bash` 工具调试 PowerShell 时见 `powershell-on-windows`；想直接用 Nushell 见 `nu-shell-helper`。

## 3. PowerShell Bash 兼容别名

Profile 位置：`C:\Users\<username>\Documents\PowerShell\Microsoft.PowerShell_profile.ps1`

关键：**先移除内置别名，再定义同名函数**，否则 `ls`/`cat` 函数不会被调用。

```powershell
Remove-Item Alias:\ls  -ErrorAction SilentlyContinue
Remove-Item Alias:\cat -ErrorAction SilentlyContinue

# ls - bash 风格列表（支持 -l, -a, -la）
function ls {
    param([string]$Path = ".", [switch]$l, [switch]$a, [switch]$la)
    $showAll = $a -or $la
    $params = @{ Path = $Path }
    if ($showAll) { $params.Force = $true }
    Get-ChildItem @params | Format-Table Name, Mode, Length, LastWriteTime
}
function cat { param([string]$Path) Get-Content -Path $Path }

# find -> fd，grep -> rg（更快）
if (Get-Command fd -ErrorAction SilentlyContinue) { Set-Alias -Name find -Value fd }
if (Get-Command rg -ErrorAction SilentlyContinue) {
    function grep {
        param([string]$Pattern, [string]$Path = ".", [switch]$r, [switch]$i, [switch]$n)
        $args = @($Pattern)
        if ($i) { $args += "-i" }
        if ($n) { $args += "-n" }
        if ($r) { $args += "-r" }
        $args += $Path
        rg @args
    }
}
Set-Alias -Name rm -Value Remove-Item
Set-Alias -Name cp -Value Copy-Item
Set-Alias -Name mv -Value Move-Item
```

重载 profile：`. $PROFILE`

## 4. 安装必需包

```bash
pi install npm:pi-mcp-adapter          # MCP server 必需
pi install npm:pi-powershell           # PowerShell 翻译层（推荐）
pi install npm:pi-custom-system-prompt # 自定义 system prompt（可选）

pi list      # 列出已装包
pi update    # 更新
pi update pi # 更新 pi 自身
```

## 5. MCP Server

位置 `~/.config/mcp/mcp.json`：

```json
{
  "mcpServers": {
    "MiniMax": {
      "command": "uvx",
      "args": ["minimax-coding-plan-mcp", "-y"],
      "env": {
        "MINIMAX_API_KEY": "your-api-key",
        "MINIMAX_API_HOST": "https://api.minimaxi.com"
      }
    }
  }
}
```

配置优先级（高 → 低）：`~/.config/mcp/mcp.json`（用户全局，推荐）→ `~/.pi/agent/mcp.json`（pi 全局覆盖）→ `./.mcp.json`（项目本地）→ `./.pi/mcp.json`（pi 项目覆盖）。

验证：

```bash
pi
/mcp        # 查看连接状态
/mcp tools  # 查看可用工具
```

## 6. 自定义模型（models.json）

位置 `~/.pi/agent/models.json`：

```json
{
  "providers": {
    "minimax-cn": {
      "type": "api_key",
      "key": "$MINIMAX_API_KEY",
      "models": [
        {
          "id": "MiniMax-M2.7-highspeed",
          "name": "MiniMax M2.7 高速版",
          "input": ["text"],
          "contextWindow": 100000,
          "maxTokens": 8000
        }
      ]
    }
  }
}
```

环境变量插值：`"apiKey": "$MY_API_KEY"` 或 `"${KEY_PREFIX}_${KEY_SUFFIX}"`。

## 7. System Prompt 定制

- **追加模式（推荐）**：`~/.pi/agent/APPEND_SYSTEM.md`，内容追加到默认 prompt，保留工具提示。
- **替换模式**：`~/.pi/agent/SYSTEM.md`，整体替换默认 prompt，**不推荐**（可能丢失工具提示）。

APPEND 典型内容：

```markdown
# Windows Shell Tools
- `fd` (find) over Get-ChildItem -Recurse
- `rg` (grep) over Select-String
Available bash-style commands: ls, cat, rm, cp, mv, mkdir, touch, find, grep
```

## 8. settings.json 完整示例

```json
{
  "defaultProvider": "minimax-cn",
  "defaultModel": "MiniMax-M2.7-highspeed",
  "defaultThinkingLevel": "medium",
  "theme": "dark",
  "shellPath": "C:\\Program Files\\WindowsApps\\Microsoft.PowerShell_7.6.4.0_x64__8wekyb3d8bbwe\\pwsh.exe",
  "packages": ["npm:pi-mcp-adapter", "npm:pi-powershell"],
  "enabledModels": ["claude-*", "gpt-4o", "minimax-*"]
}
```

- `defaultThinkingLevel`：`off`/`minimal`/`low`/`medium`/`high`/`xhigh`/`max`；`enabledModels` 支持 glob（如 `minimax-*`）。

## 9. 排查

| 症状 | 处理 |
|------|------|
| `shellPath not found` | `Test-Path "C:\path\to\pwsh.exe"` 验证；用 `Get-Command pwsh` 的真实路径 |
| `ls -la` 不工作 | 先 `Remove-Item Alias:\ls -ErrorAction SilentlyContinue` 再定义函数 |
| MCP 连不上 | 校验 `mcp.json` 是合法 JSON；`pi list` 确认已装；加 MCP 后重启 pi |
| `Model not found` | 校验 `models.json`；确认 API key（环境变量或 `auth.json`）；重启 pi |

## 安全提示

API key（`MINIMAX_API_KEY` 等）优先放环境变量或 `auth.json`，**不要提交到版本控制**；密钥管理与泄露处置见 `security-and-hardening`。
