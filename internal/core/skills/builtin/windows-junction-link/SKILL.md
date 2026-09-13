---
name: windows-junction-link
description: >-
  Use when 需要在 Windows 上创建目录 Junction 链接（mklink /J），让两个目录指向同一份磁盘
  数据，例如把 `~/.workbuddy/skills` 指向 `~/.agents/skills`、跨工具同步配置目录、或让某个
  工具直接看到另一处已有文件而无需复制。Junction 是双向的，任一边的改动立即可见于另一边。
  触发词：创建 junction 链接、windows 目录链接、mklink、目录映射、同步两个目录、junction link。
---

# Windows Junction 目录链接

Junction（目录联接 / directory junction）把一个目录链接到磁盘上的另一个目录：所有文件与子
目录在两个位置同时可见，且是**同一份数据**（非副本）。适合：

- 让 `~/.workbuddy/skills` 指向 `~/.agents/skills`
- 跨工具同步配置目录
- 让工具无需复制即可访问另一位置的现有文件

> 通过 mocode 的 `bash` 工具执行 PowerShell 时，注意 `$` 变量剥离等坑，详见 `powershell-on-windows`。

## 前提

- **目标目录（target，真实数据所在）必须已存在**，且里面已有内容。
- **链接目录（link，将被创建）必须不存在**。
- 若 link 位置已有文件：先把它们 `Move-Item` 到 target，再创建链接。
- Junction 内部以**绝对路径**记录 target。

## 创建

**PowerShell（推荐）**：

```powershell
New-Item -ItemType Junction -Path "C:\Users\username\.workbuddy\skills" -Target "C:\Users\username\.agents\skills"
```

**CMD**：

```cmd
mklink /J C:\Users\username\.workbuddy\skills C:\Users\username\.agents\skills
```

`/J` 表示 junction（目录级），Windows 10+ 通常**无需管理员权限**。

## 验证

用 `fsutil` 查询重分析点，确认是真 junction 而非普通目录：

```cmd
fsutil reparsepoint query "C:\path\to\link"
```

输出应包含（`0xa0000003` = `IO_REPARSE_TAG_MOUNT_POINT`）：

```
重分析标记值 : 0xa0000003
标记值: 装入点
替换名称: \??\C:\Users\username\.agents\skills
```

PowerShell 侧也可看（经 `bash` 工具时改用 `.ps1` 文件）：

```powershell
Get-Item "C:\path\to\link" | Select-Object LinkType, Target
```

## 删除（重要）

- 删除**链接目录本身**：`Remove-Item "C:\path\to\link"` 或 `rmdir "C:\path\to\link"`。
- 删链接**不会**删除 target 里的真实数据，但删除前务必确认路径指向 link 而非 target。
- **绝不要从 target 侧删除**——那会删掉真实数据。破坏性操作的安全习惯见 `security-and-hardening`。

## 常见坑

| 坑 | 说明 |
|----|------|
| 在 target 侧删除 | 会删掉**真实数据**；永远只删 link 侧 |
| 链接目录已存在 | `New-Item` / `mklink` 会失败；先移除或换链接位置 |
| junction vs symlink | `/J`（Junction）目录级、通常免管理员；`/D`（符号链接）可能需开发者模式或管理员 |
| 以为是副本 | Junction 双向共享同一数据源，一边改动另一边立即可见 |
| 路径漂移 | target 被移动/重命名后链接失效；target 用稳定路径 |
