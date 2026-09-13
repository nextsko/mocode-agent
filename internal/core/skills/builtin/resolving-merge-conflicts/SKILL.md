---
name: resolving-merge-conflicts
description: >-
  Use when 需要解决进行中的 git merge / rebase / cherry-pick conflict，遇到冲突标记（<<<<<<< ======= >>>>>>>）或 rebase 停在某一步时。适用于多分支并行开发后的集成分支、长生命周期分支落后于主干、以及需要把重放历史期间产生的冲突逐个理清的场景。目标是理解双方原始意图后完成合并，而不是 abort 或草率挑选一边。
---

# 解决合并冲突（Resolving Merge Conflicts）

## 概述

冲突是两条真实历史相遇的结果，双方改动通常都有正当理由。任务不是"消掉标记让命令跑过"，而是**理解双方意图、尽可能保留、然后完成合并**。

**核心原则**：看清状态 → 追溯 primary source → 逐 hunk 解决 → 跑自动化检查 → 收尾。**永远解决，绝不 `--abort`。**

## 何时使用

- `git merge` 报冲突，或 rebase / cherry-pick 停在冲突步骤。
- 文件里出现 `<<<<<<<`、`=======`、`>>>>>>>` 标记。
- 长生命周期分支落后主干，需要同步。
- 多人 / 多 agent 并行改动同一区域。

## 流程

### 1. 看清当前状态

先确认正在发生什么操作，再动手：

```bash
git status                          # 当前 merge/rebase 阶段、冲突文件
git log --oneline --graph --decorate -20
git diff --name-only --diff-filter=U   # 未解决的冲突文件
```

- 是 **merge** 还是 **rebase**？两者的收尾方式不同（见第 5 步）。
- rebase / cherry-pick 会停在中间态，必须 `--continue` 才能推进。
- 确认这是哪条分支并入哪条、merge 的 stated goal 是什么。

### 2. 为每个冲突追溯 primary source

**不要只看 diff 猜。** 对每个冲突 hunk：

- 读两侧的 `commit message`，理解改动动机。
- 查关联的 PR、issue / ticket，找原始需求与讨论。
- 用 `git log -p -- <file>`、`git blame <file>`、`git show <commit>` 定位引入改动的提交。
- 明确回答：这一边想解决什么？那一边想解决什么？两者是否其实在解决不同问题？

理解深度决定解决质量——不知道意图时的"随便选一边"就是引入 bug。

### 3. 逐个 hunk 解决

对每个冲突：

- **优先保留双方意图**（能融合就融合，而不是二选一）。
- **确实不可调和时**：选择与本次 merge stated goal 一致的一方，并在 commit message / PR 中记录 trade-off 与被放弃的一方。
- **绝不发明新行为**——不要借冲突之机重构、改语义、加功能。
- **删干净冲突标记**，包括三向 diff 的 base 段与辅助标记。
- **不要一把梭** `git checkout --ours/--theirs` 整文件覆盖，除非你已确认该文件所有 hunk 都该走同一边。

### 4. 发现并运行项目的自动化检查

合并会破坏原本各自通过的东西，必须验证：

1. **typecheck / lint**（如 `tsc --noEmit`、`cargo check`、`go vet`）
2. **test**（`npm test`、`cargo test`、`pytest`、`go test ./...`）
3. **format**（`prettier`、`cargo fmt`、`gofmt`）

修复 merge 引入的失败。若失败与本次合并无关（合并前就坏），记录并说明，不要顺手修无关问题。

### 5. 完成 merge / rebase

```bash
git add <resolved-files>
# merge:
git commit                          # 用项目约定的 merge commit message
# rebase / cherry-pick:
git rebase --continue               # 若还有后续 commit，重复直到全部重放完
```

- rebase 需要持续推进，直到所有 commit 重放完毕、无冲突残留。
- 完成后再次跑检查，确认最终状态绿。

## 红旗

**Never:**

- `git merge --abort` / `git rebase --abort` —— 逃避冲突，任务未完成。
- 在没读懂双方意图前解决 hunk。
- 借合并顺手重构或改变行为。
- 留下任何冲突标记就 commit。
- 用 `--ours/--theirs` 整文件覆盖来"解决"。
- 未跑测试就宣告合并完成。

**Always:**

- 先 `git status` 摸清处于哪个阶段。
- 每个冲突都能说清"为什么两边这么改"。
- 逐 hunk 解决并保留双方意图（可行时）。
- 跑 typecheck → test → format。
- rebase 一路 `--continue` 到结束。

## 相关技能

- `using-git-worktrees`：在隔离 workspace 里操作，避免污染当前工作区。
- `systematic-debugging`：合并后出现难以定位的回归时，用其方法论隔离变量。
- `code-review-and-quality`：合并结果在宣告完成前仍需按质量标准把关。
- `finishing-a-development-branch`：冲突解决、测试通过后，用它决定 merge / PR / 清理。
