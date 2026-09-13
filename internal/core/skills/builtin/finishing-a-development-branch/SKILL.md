---
name: finishing-a-development-branch
description: >-
  Use when 实现已完成、测试通过，需要决定如何整合这份工作（merge 回主干、开 PR、保留分支或丢弃）时。适用于开发分支收尾、worktree 清理、以及需要给出结构化选项而非开放问题的场景。核心流程：验证测试 → 探测环境 → 给出选项 → 执行选择 → 清理 workspace。
---

# 收尾开发分支（Finishing a Development Branch）

## 概述

实现完成、测试通过后，用一组**结构化选项**引导用户决定工作如何整合，并正确处理分支与 worktree 的清理。

**核心原则**：Verify tests → Detect environment → Present options → Execute choice → Clean up。

**开始时报备**："I'm using the finishing-a-development-branch skill to complete this work."

## Step 1：验证测试

**给出任何选项之前，先确认测试通过：**

```bash
npm test / cargo test / pytest / go test ./...
```

**测试失败则停止**，不要进入 Step 2：

```
Tests failing (<N> failures). Must fix before completing:

[展示失败]

Cannot proceed with merge/PR until tests pass.
```

测试通过才继续。

## Step 2：探测环境

先判定 workspace 形态，决定展示哪个菜单、如何清理：

```bash
GIT_DIR=$(cd "$(git rev-parse --git-dir)" 2>/dev/null && pwd -P)
GIT_COMMON=$(cd "$(git rev-parse --git-common-dir)" 2>/dev/null && pwd -P)
```

| 状态 | 菜单 | 清理 |
|------|------|------|
| `GIT_DIR == GIT_COMMON`（普通 repo） | 标准 4 选项 | 无 worktree 可清理 |
| `GIT_DIR != GIT_COMMON`，命名分支（worktree） | 标准 4 选项 | 按 provenance 判定（见 Step 6） |
| `GIT_DIR != GIT_COMMON`，detached HEAD | 精简 3 选项（无 merge） | 不清理（外部管理） |

## Step 3：确定 base branch

```bash
git merge-base HEAD main 2>/dev/null || git merge-base HEAD master 2>/dev/null
```

不确定就问："This branch split from main - is that correct?"

## Step 4：给出选项

**普通 repo 与命名分支 worktree —— 恰好给出这 4 个：**

```
Implementation complete. What would you like to do?

1. Merge back to <base-branch> locally
2. Push and create a Pull Request
3. Keep the branch as-is (I'll handle it later)
4. Discard this work

Which option?
```

**Detached HEAD —— 恰好给出这 3 个：**

```
Implementation complete. You're on a detached HEAD (externally managed workspace).

1. Push as new branch and create a Pull Request
2. Keep as-is (I'll handle it later)
3. Discard this work

Which option?
```

**不要附加解释**——保持选项简洁。

## Step 5：执行选择

### Option 1：本地 merge

```bash
# 切到主 repo 根目录，保证 CWD 安全
MAIN_ROOT=$(git -C "$(git rev-parse --git-common-dir)/.." rev-parse --show-toplevel)
cd "$MAIN_ROOT"

git checkout <base-branch>
git pull
git merge <feature-branch>

# 在合并结果上重新验证测试
<test command>
```

**确认 merge 成功、测试通过后**才清理 worktree（Step 6），最后删分支：

```bash
git branch -d <feature-branch>
```

若 merge 产生冲突，转 `resolving-merge-conflicts`。

### Option 2：Push 并创建 PR

```bash
git push -u origin <feature-branch>
```

**不要清理 worktree**——用户需要它继续迭代 PR 反馈。

### Option 3：保留分支

报备："Keeping branch <name>. Worktree preserved at <path>." **不要清理 worktree。**

### Option 4：丢弃

**先确认：**

```
This will permanently delete:
- Branch <name>
- All commits: <commit-list>
- Worktree at <path>

Type 'discard' to confirm.
```

等待用户**逐字输入 `discard`**。确认后切到主 repo 根目录，清理 worktree（Step 6），再强删分支：

```bash
git branch -D <feature-branch>
```

## Step 6：清理 workspace

**仅 Option 1 与 4 执行。** Option 2、3 始终保留 worktree。

```bash
GIT_DIR=$(cd "$(git rev-parse --git-dir)" 2>/dev/null && pwd -P)
GIT_COMMON=$(cd "$(git rev-parse --git-common-dir)" 2>/dev/null && pwd -P)
WORKTREE_PATH=$(git rev-parse --show-toplevel)
```

- **`GIT_DIR == GIT_COMMON`**：普通 repo，无 worktree，结束。
- **worktree 路径位于 `.worktrees/` 或 `worktrees/` 下**：这是我们（工具/本流程）创建的，负责清理：

  ```bash
  MAIN_ROOT=$(git -C "$(git rev-parse --git-common-dir)/.." rev-parse --show-toplevel)
  cd "$MAIN_ROOT"
  git worktree remove "$WORKTREE_PATH"
  git worktree prune
  ```

- **其他路径**：workspace 归宿主环境所有，**不要删除**。平台若提供退出 workspace 的工具就用它，否则原地保留。

## 快速参考

| 选项 | Merge | Push | 保留 Worktree | 清理分支 |
|------|-------|------|---------------|----------|
| 1. 本地 merge | 是 | - | - | 是 |
| 2. 创建 PR | - | 是 | 是 | - |
| 3. 保留 | - | - | 是 | - |
| 4. 丢弃 | - | - | - | 是（强删） |

## 常见错误

- **跳过测试验证** → 合并坏代码、开失败 PR。**修复：** 给选项前必须验证测试。
- **开放问题** → "接下来做什么？" 语义模糊。**修复：** 恰好 4 个（detached HEAD 为 3 个）结构化选项。
- **为 Option 2 清理 worktree** → 删掉用户迭代 PR 所需的工作区。**修复：** 仅 1、4 清理。
- **先删分支再移 worktree** → `git branch -d` 因 worktree 仍引用而失败。**修复：** 先 merge，再移 worktree，最后删分支。
- **在 worktree 内执行 `git worktree remove`** → CWD 在被删目录里，命令静默失败。**修复：** 先 `cd` 到主 repo 根目录。
- **清理 harness 拥有的 worktree** → 造成 phantom state。**修复：** 只清理 `.worktrees/` / `worktrees/` 下的。
- **丢弃无确认** → 误删工作。**修复：** 要求逐字输入 `discard`。

## 红旗

**Never：** 测试失败仍推进；未在合并结果上验证测试就宣告完成；无确认删除工作；未经明确要求 force-push；merge 成功前移除 worktree；清理非自己创建的 worktree；在 worktree 内执行 `git worktree remove`。

**Always：** 给选项前验证测试；给菜单前探测环境；恰好 4 个选项（detached 3 个）；Option 4 需逐字确认；仅 1、4 清理 worktree；移除前 `cd` 到主 repo 根；移除后 `git worktree prune`。

## 相关技能

- `using-git-worktrees`：理解 worktree 的创建与 provenance 判定。
- `resolving-merge-conflicts`：本地 merge 发生冲突时的处理流程。
- `code-review-and-quality`：收尾前把好质量关。
- `verification-before-completion`：宣告完成前必须提供新鲜证据。
