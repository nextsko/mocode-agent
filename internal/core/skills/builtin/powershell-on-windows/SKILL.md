---
name: powershell-on-windows
description: Use when operating on a Windows environment and needing to write or debug PowerShell commands via the bash tool. Covers the `$` variable stripping issue, script-file workaround, common pitfalls, and correct patterns for file sizing, encoding, pipeline formatting, and directory recursion.
---

# PowerShell on Windows — Correct Usage & Common Pitfalls

When running inside the `bash` tool on a Windows host, PowerShell commands are passed through a Go shell interpreter (`mvdan/sh`). This creates several non-obvious pitfalls that differ from running PowerShell in a native terminal.

## The `$` Variable Stripping Problem

**Symptom**: Inline PowerShell commands (via `powershell -Command "..."`) silently lose `$` variable references.

**Root cause**: The `bash` tool's shell interpreter (`mvdan/sh`) interprets `$` before PowerShell sees it. Variables like `$_, $sum, $dir, $g, $results` get stripped or mangled, causing seemingly valid PowerShell to fail with syntax errors like:

```
MissingStatementInHashLiteral
MissingExpressionAfterToken
MissingEndParenthesisInMethodCall
EmptyPipeElement
```

### ❌ Wrong — Inline with `$` variables

```powershell
powershell -Command "Get-ChildItem $dir -Recurse -File | ForEach-Object { $_.FullName }"
#                                                        ^^  ^^  stripped!
```

### ✅ Correct — Write a `.ps1` script file, then execute

```powershell
# 1. Write the script to a temp file
write @"
`$dir = "C:\path\to\scan"
`$files = Get-ChildItem `$dir -Recurse -File
foreach (`$f in `$files) {
    Write-Output `$f.FullName
}
"@ C:\path\to\_tmp_script.ps1

# 2. Run it
powershell -NoProfile -ExecutionPolicy Bypass -File C:\path\to\_tmp_script.ps1
```

> **Note**: When writing `.ps1` via the `write` tool, use the backtick `` ` `` to escape `$` in the file content string, or better yet, write the `.ps1` file directly with the `write` tool (no escaping needed in the file itself).

## Recommended Patterns

### Pattern 1: Write `.ps1` file with the `write` tool, then execute

This is the most reliable approach — the file content is written verbatim, no escaping issues.

```
Step 1: write tool -> path/to/script.ps1 (write the PowerShell script cleanly)
Step 2: bash tool -> powershell -NoProfile -ExecutionPolicy Bypass -File path/to/script.ps1
```

### Pattern 2: Simple one-liners without `$`

For trivially simple commands that don't use variables:

```powershell
powershell -NoProfile -Command "Get-ChildItem 'C:\path' -Recurse -File | Group-Object Extension"
```

Works as long as there are **no `$` signs** in the command.

### Pattern 3: Group-Object + computed properties (use script files)

Complex pipelines with calculated properties MUST use a `.ps1` file:

```powershell
# script.ps1
$dir = "C:\path"
$items = Get-ChildItem $dir -Recurse -File
$groups = $items | Group-Object Directory
$results = @()
foreach ($g in $groups) {
    $sum = ($g.Group | Measure-Object Length -Sum).Sum
    $cnt = $g.Group.Count
    $results += [PSCustomObject]@{
        Folder = $g.Name
        Files = $cnt
        KB = [math]::Round($sum/1KB, 1)
    }
}
$results | Sort-Object KB -Descending | Format-Table -AutoSize
```

## Common Pitfalls

| Pitfall | ❌ Wrong | ✅ Correct |
|---------|----------|------------|
| **Inline `$` vars** | `powershell "... $_.Name ..."` | Use `.ps1` file instead |
| **Missing paths** | `Get-ChildItem $dir -Recurse` where `$dir` isn't set | Fully qualify paths, avoid relative |
| **ExecutionPolicy** | `powershell -File script.ps1` (might be blocked) | Always add `-ExecutionPolicy Bypass` |
| **Chinese chars in script** | Inline Chinese in `-Command` gets garbled | Write `.ps1` file with UTF-8 encoding |
| **Nesting quotes** | `powershell "...'...\"..."` | Use script file, avoid quote hell |
| **`$_` in calculated properties** | `@{N='Size';E={$_.Length}}` inline | Move to `.ps1` file |

## File Size Best Practices

When using PowerShell to analyze disk usage:

```powershell
# Get directory sizes recursively
$dir = "C:\target"
Get-ChildItem $dir -Recurse -File | 
    Group-Object Directory | 
    ForEach-Object {
        $sum = ($_.Group | Measure-Object Length -Sum).Sum
        [PSCustomObject]@{
            Path = $_.Name
            Files = $_.Group.Count
            MB = [math]::Round($sum / 1MB, 2)
        }
    } | Sort-Object MB -Descending
```

## Verification

- Run script: `powershell -NoProfile -ExecutionPolicy Bypass -File script.ps1`
- Check for syntax errors: `powershell -NoProfile -Command "Get-Command -Syntax Get-ChildItem"`
