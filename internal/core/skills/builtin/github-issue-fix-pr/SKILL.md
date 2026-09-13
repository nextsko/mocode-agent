---
name: github-issue-fix-pr
description: >-
  Use when 用户想「修复 issue / pick an issue to fix / 帮他修 bug 并提 PR / 提交 PR」，
  需要从 GitHub 开放 issue 中挑一个、定位根因、写修复与测试、本地验证、生成 changeset 并按
  仓库贡献规范开出 PR 时。面向 TypeScript pnpm monorepo（kimi-code 类），六阶段顺序执行。
---

# GitHub Issue 修复 → PR

面向 GitHub 托管的 TypeScript pnpm monorepo（以 kimi-code 为原型，形态可推广）的端到端工作流：从开放 issue 中挑选，定位根因，写修复与测试，本地验证，生成 changeset，开出符合贡献规范的 PR。

六阶段**顺序执行，不得跳步**，每阶段的退出条件都是下一阶段的闸门。定位根因遵循 `investigate-first` 与 `systematic-debugging` 的纪律；交付前质量门参照 `code-review-and-quality`。

## Phase 0 — 验证环境

先确认工具链真实可用，否则后续每一步都白费。

```bash
node -v                  # 满足 engines（kimi-code 为 >=24.15.0）
pnpm -v                  # kimi-code 为 10.33.0
ls node_modules/.pnpm    # 不存在 → 需先 pnpm install（先问用户，很慢）
```

`node_modules` 缺失时**先征得用户同意再跑 `pnpm install`**——多分钟、重网络，不要静默执行。

## Phase 1 — 挑选值得修的 issue

```bash
gh issue list --repo <owner/repo> --state open --limit 30 \
  --json number,title,labels,createdAt
```

Triage 标准（**真正可修**的才算）：

- **根因可定位**：描述具体行为或报错，不是模糊感受。带复现步骤或 bisect 的是金矿。
- **范围小**：改一两个文件，不涉及跨包架构变更。
- **非重复**：`gh issue view <n>` 看评论是否有 maintainer 的「duplicate of #x」。
- **不是伪装成 bug 的 feature request**：`enhancement` 标签需先设计讨论，修 PR 直接跳过。

读 3–4 个最佳候选的完整正文 + 评论，选定后**告诉用户为什么选它**——这个「为什么」在训练 triage 直觉。

### 警惕「报告症状 ≠ 根因」陷阱

issue 标题常描述报告者**以为**看到的症状，而非真正的 bug。经典模式：报告者 bisect 出某个变量（如「description 长度」）就断定它是原因，而真正触发条件是相关效应（如长文本更可能含某个破坏 YAML 解析的字符）。留意报告者找到的 workaround（如「给值加引号就好了」）——那是最强的根因信号。

## Phase 2 — 定位根因（只读）

从 issue 关键词出发 `rg` 到相关模块。**改动前先读目录树中最近的 `AGENTS.md`**——它带有覆盖通用假设的目录级规则。

不要跳去编辑。追完整调用路径，直到能用一句话 + file:line 陈述根因，例如「`SessionSkillRegistry` 在 `session/index.ts:228` 构造时未传 `onWarning`，扫描器调用 `warn(...)` 被静默丢弃；`getSessionWarnings()`（:687）又只读 AGENTS.md，从不读 skill 层」。若压不成这种形式，说明还没理解 bug，继续读。

搜索面很广时（「这个类的所有构造点」「v2 引擎是否有并行路径」），派 Explore 子 Agent，而非自己 grep。

## Phase 3 — 设计修复（plan mode）

进入 plan mode，呈现：

1. **根因**：Phase 2 的一句话陈述（含 file:line）。
2. **修复**：逐文件具体改动，最小化；说明什么**不在**范围内及原因。
3. **测试**：扩展现有测试文件（组件已有测试文件时绝不新建），每个新用例证明什么。
4. **验证命令**：确切的 `pnpm --filter ... exec vitest` / `tsc` / `oxlint` 调用。
5. **Changeset**：包列表 + bump 级别（规则见 Phase 5）。

`AskUserQuestion` 只用于真正的分叉（范围决策、方案取舍）；不要问「这个计划行不行」——那是 `ExitPlanMode` 的用途。

### Karpathy 纪律（计划期）

- **最小改动**：20 行能修就别写 100 行，不做顺手重构。
- **贴合周边风格**：邻居用 `[...this.x]` 拷贝数组就照做，不强行套自己的偏好。
- **一个逻辑变更**：若修复与重构都诱人，那是两个 PR。

## Phase 4 — 实现（TDD-ish）

顺序：源码修复 → 测试 → 验证。此场景先改源码再写测试可接受，因为 bug 在既有代码中；目标是写出**本可捕获该 bug** 的测试。

1. 先建分支：`git switch -c fix/<short-slug>`。
2. 按计划做源码改动。
3. 在**既有**测试文件中写测试。
4. 先跑定向测试拿快速反馈：`cd "<package-dir>" && pnpm exec vitest run test/<module>/<file>.test.ts`
5. 测试因环境报错（Windows `symlink` 的 `EPERM`、缺二进制、端口占用）时，先判断是否既有失败，再假定是自己改坏的。

### Windows 专属坑

- Git Bash 中连续 `cd packages/foo` 常失败；用绝对路径 `cd "C:/.../packages/foo"`。
- `vitest` 不在 PATH，用 `pnpm exec vitest`。
- 创建 symlink 的测试在未开 Developer Mode 时报 EPERM——属既有失败，不是回归。

## Phase 5 — 验证

按序执行并报告**实际输出**：

```bash
# 1. 定向测试（快）
cd "<package-dir>" && pnpm exec vitest run test/<module>/
# 2. 类型检查
cd "<package-dir>" && pnpm run typecheck
# 3. lint（仓库根）
pnpm -w run lint
```

如实陈述「13/13 passed」「typecheck clean」「0 errors」。失败就带输出说明，**没有证据不声称成功**。既有的环境失败标注「既有、与本次改动无关」并给出证据。

## Phase 6 — 交付

### 6a. 生成 changeset

kimi-code 完整规则见 `.agents/skills/gen-changesets/SKILL.md`，关键几条：

- **进入 CLI bundle 的内部包源码必须显式列出 `@moonshot-ai/kimi-code`**：CLI inline-bundle `@moonshot-ai/*`，但在其视角它们只是 devDependencies，changesets 不会自动传导 bump。内部包改动若影响 CLI 输出，frontmatter 必须带它。
- **绝不自行写 `major`**：认为需要就停下来问用户。默认 `patch`；仅实质新能力用 `minor`。
- **ignored 包**（`vis`、`vis-server`、`vis-web`、`kimi-inspect`）不能与可发布包混在同一 frontmatter。
- 英文、一句话、不写文件名/类名/PR 号；新用户可见功能可补一行用法提示。

文件格式 `.changeset/<kebab-name>.md`：

```markdown
---
"@moonshot-ai/<pkg>": patch
"@moonshot-ai/kimi-code": patch
---

<One English sentence describing the user-visible change.>
```

### 6b. 提交前自审

```bash
git status                  # 确保没有 HANDOVER/*.html/.zcode 等垃圾被 staged
git diff --staged --stat    # 确认只有预期文件
```

移除 `HANDOVER-*.md`、`*-mockup.html`、`.zcode/` 等草稿产物。diff 里只应有源码、测试、changeset。

### 6c. 提交

Conventional Commit 标题（`fix(scope): ...`、`feat(scope): ...`）。无 co-author trailer、无任何 agent 身份。正文解释根因与修复，让 reviewer 能跟上推理。

### 6d. 账号 + fork + push + PR

先显式确认「我在以哪个账号行事」：

```bash
git config user.name                      # 提交作者
gh auth status                            # 当前 gh 账号
```

账号不对，push 前切换：`gh auth switch --user <correct-login>`，再 `gh auth setup-git --hostname github.com` 重新绑定 git 凭证。然后：

```bash
gh repo fork <owner/repo> --clone=false          # 每个 repo 只需一次
git remote add fork https://github.com/<your-login>/<repo>.git
git push -u fork fix/<short-slug>

gh pr create --repo <owner/repo> \
  --head <your-login>:fix/<short-slug> --base main \
  --title "<conventional-commit-title>" \
  --body "$(cat .github/pull_request_template.md 填好)"
```

诚实填写 PR 模板：关联 issue、陈述根因（而非仅症状）、解释方案、只勾选真正做过的检查项。

### 6e. 网络抖动

若 `git push` 报 `port 443 ... Could not connect` 但 `gh api` 可用，说明 git 的 HTTPS 路径被挡而 gh 的没被挡。重试 push——往往瞬时。重试前可用 `git ls-remote <remote>` 廉价探测路径是否恢复。

## Phase 7 — 向用户汇报

1. PR URL。
2. 一行确认作者账号（`gh pr view <n> --json author` 核验）。
3. 流水线状态：changeset 是否被识别？CI pending/passed？距离发布还差什么。

如实区分「bot 识别到 changeset」与「maintainer 已批准」——bot 评论是自动确认，不是人工 review。发布路径：CI green → maintainer review → merge → changeset release PR → publish。

## 快速参考

```bash
node -v; pnpm -v; ls node_modules/.pnpm                                  # 0. env
gh issue list --repo <owner/repo> --state open --json number,title,labels # 1. pick
rg "<keyword>" packages/                                                 # 2-3. investigate + plan
git switch -c fix/<slug>                                                 # 4. implement
cd "<pkg-dir>" && pnpm exec vitest run test/<module>/ && pnpm run typecheck
pnpm -w run lint                                                         # 5. verify
# 6. 写 .changeset/<slug>.md；git status && git diff --staged --stat；commit；
#    gh auth switch + setup-git；gh repo fork；push；gh pr create
```
