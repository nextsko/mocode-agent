---
name: nu-shell-helper
description: >-
  Use when 回答 Nushell（nu）相关问题、编写 nu 命令/脚本/管道，或用户提到 nushell、`nu`、
  `nu.exe`、nu 的表格/dataframe 操作、nu 插件命令时。也适用于工作目录中存在 `nu.exe` 或 nu
  插件的场景。不用于一般 shell/bash/PowerShell 问题，除非用户明确要求 Nushell 方案。
---

# Nu Shell Helper

## 核心原则

回答**任何** Nushell 问题时：**先给命令，再解释**（command first, explanation after）。用户要的是能立刻复制执行的片段，不要用整段文字把答案埋起来。

## 何时使用

- Nushell 命令、语法、管道（`ls`、`where`、`select`、`get`、`each` 等）
- 编写或调试 `.nu` 脚本
- Nushell 数据类型（table、record、list、range）
- Nu 插件（`nu_plugin_polars`、`nu_plugin_query`、`nu_plugin_formats` 等）
- 把 bash/POSIX 命令迁移为 Nushell 等价写法
- 任何提到 "nu"、"nushell"、`nu.exe` 的问题

**不要**用于一般 shell/bash/PowerShell 问题（那类走 `powershell-on-windows`），除非用户明确要 Nushell 方案。

## 回答格式（始终遵守）

每个答案都用两段式：

**Command:** 可复制执行的 `` ```nu `` 代码块。

**Explanation:**
- 每个关键部分做什么（一个 bullet 一项）。
- 为什么这么做（仅当不直观时）。
- 变体 / 坑（仅当相关时）。

示例：

**Command:**

```nu
ls | where size > 10mb | select name size
```

**Explanation:**
- `ls` 产出表格；`where` 按条件过滤行；`select` 只保留指定列。
- 10mb 是 nu 的字节单位字面量，无需手写乘法。

## 常用 nu 命令速查

| 任务 | 命令 |
|------|------|
| 列文件 | `ls` 或 `ls -a` |
| 过滤行 | `ls \| where size > 10mb` |
| 选列 | `ls \| select name size` |
| 取一列 | `ls \| get name` |
| 逐行映射 | `ls \| each { \|row\| $row.name }` |
| 排序 | `ls \| sort-by size --reverse` |
| 前 N 行 | `ls \| first 10` |
| 计数 | `ls \| length` |
| 保存输出 | `ls \| save data.json` |
| 字符串匹配 | `ls \| where name =~ "txt"` |
| 管道 + JSON | `open data.json \| where age > 18 \| select name` |

## Nushell vs Bash 速查

| Bash | Nushell |
|------|---------|
| `ls -la` | `ls -a` |
| `cat file` | `open file` |
| `grep "x"` | `where $it =~ "x"` |
| `head -n 10` | `first 10` |
| `wc -l` | `length` |
| `find . -name "*.ts"` | `ls **/*.ts` |
| `echo $PATH` | `$env.PATH` |
| `export FOO=bar` | `$env.FOO = "bar"` |
| `command1 \| xargs command2` | `each { \|x\| command2 $x }` |

## 环境中的 Nu 插件

| 插件 | 用途 |
|------|------|
| `nu_plugin_formats.exe` | 额外数据格式 |
| `nu_plugin_gstat.exe` | Git 状态信息 |
| `nu_plugin_inc.exe` | 版本号自增 |
| `nu_plugin_polars.exe` | DataFrame（polars） |
| `nu_plugin_query.exe` | 查询 web/JSON/XML |

注册插件：

```nu
plugin add ./nu_plugin_polars.exe
```

## 常见错误

| 错误 | 修正 |
|------|------|
| 在管道外使用 `$it` | `$it` 只在管道内有效；写显式闭包 `{ \|row\| ... }` 更清晰 |
| 把字符串当路径 | 用 `path parse` / `path join`，别用裸字符串操作 |
| 期待 bash 风格参数 | nu 用结构化 flag（`-a`、`--reverse`），不是 POSIX 参数链 |
| 忘记 `open` 会解析 | `open data.json` 返回 table/record，不是原始文本 |

## Red Flags —— 出现这些说明你在违反本 skill

- 在给出任何命令**之前**先写大段解释。
- 只用散文描述命令，没有可运行代码块。
- 用户问 Nushell 却给 bash 命令（或反之）且未标明目标 shell。
- 只给解释、不给可复制片段。

**以上任一情况：立即重排答案——命令在前，解释在后。**
