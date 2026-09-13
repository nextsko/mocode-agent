---
name: shipping-gitea-prs
description: >-
  Use when Gitea/Forgejo 托管的 Git 仓库里已验证的改动准备交付，需要整理分支与远端、push 或提 PR，
  或需要清理多余 worktree 且只保留唯一 origin 时。核心是先重跑权威验证命令，再做分支/远端正交化、
  push、用 tea 建 PR，并删除临时登录凭证。
---

# 交付 Gitea PR（Shipping Gitea PRs）

## 概述

以「证据先行」的方式完成一个 Git/Gitea 分支：重跑权威验证命令 → 整理分支与远端 → push → 建 PR → 移除临时认证。

本技能只负责**交付**，不负责继续实现。GitHub 原生工作流、force-push、历史重写不适用本技能（GitHub 场景见 `github-issue-fix-pr`）。隔离工作区的创建/清理遵循 `using-git-worktrees`；交付前的质量门参照 `code-review-and-quality`。

## 何时使用

- 仓库托管在 Gitea 或 Forgejo，交付走 `git` + `tea`。
- 工作已完成，需要一次干净的 push 或 PR，而不是继续实现。
- 分支名过时或误导，应在发布前改名。
- 用户希望只保留一个 `origin`，或清理多余 worktree / 本地分支。
- Windows + `http://` 远端可能打印 Git Credential Manager 的 OAuth 警告，需要独立核验。

## 工作流

### 1. 交付前重跑验证

跑**真正能证明工作就绪**的那条命令，没有新鲜输出就不 push、不声称可提 PR：

- Go 后端：`go test ./...`
- 仅 gateway 后端：`cd gateway-sever && go test ./...`
- Monorepo：跑受影响包自己的权威测试或构建命令

### 2. 审计仓库形态

```powershell
git status --short --branch
git branch -vv
git remote -v
git worktree list --porcelain
```

确认当前分支、tracking 状态、远端 URL、是否存在多余 worktree。

### 3. 发布前正交化

- 用户要求单一远端时，只保留预期的 `origin`。
- 分支名不准确，push 前改名：

```powershell
git branch -m feature/<accurate-name>
```

- 清理多余 worktree **必须先确认干净**：

```powershell
git -C <worktree-path> status --short --branch
git worktree remove <worktree-path>
git branch -D <worktree-branch>
```

**绝不**删除有未审查本地改动的 worktree 或分支。

### 4. Push 并核验远端状态

```powershell
git push -u origin <branch>
git branch -vv
git ls-remote --heads origin <branch>
```

Windows + `http://` 远端下，Git Credential Manager 可能在 push 实际成功时仍打印 OAuth 警告。**只有**当 branch tracking 与 `git ls-remote` 都确认分支存在于远端，才判定 push 成功。

若用户只要求 push，到此为止并汇报分支状态。

### 5. 用 `tea` 建 PR

优先 `tea pulls create`。

若 `tea login list` 为空但 `git push` 已可用，可从 Git 已存凭证临时引导一个登录：

```powershell
@"
protocol=http
host=<host:port>
path=<owner>/<repo>.git
username=
"@ | git credential fill
```

用返回的 `username` / `password` 建临时登录：

```powershell
tea logins add --name temp-gitea --url http://<host:port> --user <user> --password <password>
```

建 PR：

```powershell
tea pulls create -l temp-gitea -r <owner>/<repo> --base <base> --head <branch> --title "<title>" --description "<body>"
```

**PR 创建后立即删除临时登录**：

```powershell
tea logins delete temp-gitea
```

### 6. 汇报结果

始终报告：

- 最终分支名
- 重跑的准确验证命令
- PR URL；若仅 push 则给出远端分支名
- 出现的任何警告及其独立核验方式

## PR 形态

标题：简短祈使句，贴合实际改动。例：`refactor: complete gateway Gin migration`、`fix: stabilize workspace RPC middleware`。

正文保持紧凑：

```markdown
## Summary
- <2-3 concrete changes>

## Verification
- <exact command>
```

## CGRA Gateway 默认值

对 `cgra-eda-gateway`，除用户另有说明外：

- 只保留一个名为 `origin` 的远端
- base 分支 `main`
- 分支名用 `feature/<topic>` 或 `fix/<topic>`
- 仅后端 gateway 的验证命令是 `cd gateway-sever && go test ./...`
- 若存在临时集成 worktree 且不属于本次交付物，确认干净后再移除

## 常见错误

- 没重跑权威验证命令就建 PR。
- 遗留过时分支名（旧的 merge 或 migration 标签）。
- 看到吓人的 HTTP/OAuth 警告就放弃，却没检查分支是否真的落地。
- 把临时 `tea` 登录留在磁盘上。
- 未检查本地改动就删除多余 worktree。
