---
name: ooml-rulebook
description: >-
  Use when 在 OOMOL（oomol-lab）代码库中写代码、新建仓库、做架构决策、设计
  API/SDK/CLI、写 React/Tailwind/shadcn 或 SwiftUI 界面、处理错误与重试、组织
  测试、发版、写 AGENTS.md/README，或评审某设计是否符合 OOMOL 工程观念时。
  本 skill 是 common-rulebook 方法论在 oomol-lab 的实例层（参数集）：具体工具
  选型、命名、色值、参数以本文件为准；方法论与跨栈普适原则见 common-rulebook，
  两者冲突时本文件实例参数优先。蒸馏自 oomol-lab 64 仓库（2026-08-25 快照）。
---

# ooml-rulebook：OOMOL 公司级编码与设计指南

> **两层结构中的实例层**：本 skill 是 `common-rulebook` 方法论应用于 oomol-lab 的参数集——工具选型、命名、色值、参数等具体值以本文件为准；方法论与普适原则见 `common-rulebook`。蒸馏自 oomol-lab 工作区 64 仓库实测分析（2026-08-25 快照），证据均以 `仓库名/路径` 标注。

## 0. 总纲：五条设计哲学（评审先对照）
1. **凭证永不进 Agent**——OAuth/密钥生命周期收拢在网关（open-connector）或主进程 safeStorage，Agent/渲染进程/技能只拿结构化结果与安全账户标签（accountId/displayName/grantedScopes），永远拿不到原始 token。
2. **渐进式发现，不注册爆炸**——千级 provider 压缩为少量发现工具（MCP 5 工具、wanta Link 4 工具 list→search→inspect→call、运行时 `oo connector schema` 读权威 schema），而不是把所有 action 注册成常驻工具。
3. **借协议不建轮子**——包管理寄生 npm 协议（oopm）、发布借 GitHub OIDC、Agent 内核外购（OpenCode/Hermes，"发行版而非 fork"）、认证借 OIDC 联邦。先问"有没有成熟协议可寄生"，再考虑自建。
4. **类型即契约 / 产物即契约**——类型从单一事实源生成（fusion 的 openapi snapshot、connector-types 的 declaration merging）；中间产物做成显式持久化契约（pdf-craft DocumentPackage、.wikg、Command Artifact v2 三重校验、不可变 ProjectRevision）。
5. **白标分层**——开源底座与托管增值切开：品牌单一来源（branding.ts）+ build-time endpoint 常量（`__OO_ENDPOINT__`），白标 = 改两个文件重新分发。

## 1. 代码风格

### 1.1 工具链按仓库世代选型（三代并存，新仓一律第三代 oxlint/oxfmt）
| 世代 | 工具 | 代表仓库 | 状态 |
|---|---|---|---|
| 老一代 | `@oomol-lab/eslint-config-basic/ts`（.eslintrc） | ovm-js、sparse-file-js、code-style | ❌ 弃用，勿模仿 |
| 中一代 | `@antfu/eslint-config` flat config | oo-cli、skills、oopm、alicloud-tablestore | ✅ CLI/工具线现役 |
| 新一代 | **oxlint + oxfmt**（.oxlintrc.json + .oxfmtrc.json） | open-flow、wanta、open-connector、connector-sdk | ✅✅ 平台/产品线现役，新仓默认 |

**oxlint/oxfmt 签名配置**（open-flow/wanta/open-connector/connector-sdk 四仓一致）：`categories.correctness=error`；plugins `typescript/unicorn/oxc/import`（react 按需）；`import/consistent-type-specifier-style: ["error","prefer-top-level"]`。
- **禁 Prettier**（open-connector AGENTS.md："Use `oxfmt` and `oxlint`; do not add Prettier"）。
- **不手动折行**："Do not manually wrap code to 80 columns. Let `oxfmt` decide formatting."（open-flow printWidth 160）。
- **禁 barrel 文件**："Do not create barrel files such as `index.ts`"（open-connector/open-flow AGENTS.md）——用直接导入，不给宽泛 barrel 和掩盖归属的 path alias。

### 1.2 签名风格（两派并存，风格随仓库，先看仓库现状）
| 维度 | antfu 系（oo-cli/skills/oopm/tablestore） | oxfmt 系（open-flow/wanta/open-connector） |
|---|---|---|
| 缩进 | TS **4 空格**；JSON/YAML 2 空格 | 2 空格 |
| 引号 | **双引号** | 单引号 |
| 分号 | **有** | **无**（semi: false） |
| 尾逗号 | antfu 默认 | `"all"` |
| import 排序 | antfu 排序器 | 自定义 groups：side-effect → type → value |

### 1.3 Python（pdf-craft / epub-translator / epub2speech 一致）
- **pyright + pylint + ruff** 三件套；无 mypy/black。ruff：`line-length = 120, target-version = "py311"`，`select = ["E","W","F","I","UP"]`，format `quote-style = "double"`；pylint `.pylintrc`，`disable=C,R,missing-*-docstring,...`，`max-line-length=120`。
- 构建：开发者库用 **poetry-core**；需兼容老 Python 的 SDK 用 setuptools（py3.7/3.8 下限）。

### 1.4 Go / Swift
- **Go（ovm 系）**：golangci-lint v2 formatters 显式 `gofmt+goimports`；staticcheck + gocritic + nilerr 精选；`testifylint: enable-all`；`nolintlint.require-specific: true`；错误包装 `fmt.Errorf("...: %w", err)` + `errors.Is/As`；Makefile 用 `##@` 自文档注释。
- **Swift（LockIME / CloseUp，质量标杆）**：Swift 6 + `SWIFT_STRICT_CONCURRENCY: complete`；XcodeGen（project.yml）+ Makefile，无 SwiftLint——纪律靠 DesignSystem 强制（§13）；可测逻辑进 `<App>Kit` target（CloseUpKit / LockIMEKit），平台胶水薄壳分离。

## 2. 工程脚手架与构建
- **包管理器**：CLI/工具/服务端用 **Bun**（`.bun-version` 钉版本）；库/桌面应用用 **pnpm**（`packageManager` 钉版本）；原生 Node TS 服务用裸 npm（Node 22+ 原生跑 TS，禁加 tsx）。**打包器演进**：库类 `tsup`（老）→ `tsdown`（新：connector-sdk、alicloud-tablestore）→ 薄 SDK 直接 `tsc`；产物 mjs/cjs 双格式 + `exports` 多入口。
- **monorepo**：pnpm-workspace 或 bun workspaces；deployables 归 `apps/`，可复用合同归 `packages/`。
- **CI 三件套**（GitHub Actions）：`pr-check`（pull_request→main）+ push-main 检查 + release。release 触发 tag `v*.*.*` 或 workflow_dispatch（带 `version_bump: patch|minor|major`）；release job 一律 `concurrency: { group: release-<name>, cancel-in-progress: false }`；macOS 产品加 nightly cron 出 beta。

## 3. 测试规范
- **框架与组织**：Bun 仓用内置 `bun test`；Vite/Electron 仓用 **vitest**；Python 用 pytest（`testpaths=["tests"]`）。测试与源文件同目录（oo-cli），共享 helper 放 `__tests__/helpers.ts`；或 `test/**/*.test.ts`（connector-sdk）；Python 统一顶层 `tests/`。⚠️ open-flow AGENTS.md：monorepo 根目录**禁用 `bun test`**（会用 Bun runner 错误加载 Vitest 文件），在 package 层跑各自脚本。
- **架构测试（公司特色，三类必学）**：① **遥测决策清单测试**（oo-cli `telemetry-decisions.test.ts`）——每个注册命令必须在清单里有显式遥测决策，漏登记 CI 直接挂，并拒绝敏感属性名；② **双语言对齐测试**（task-sdk-py `test_aligned_types_are_exported`）——断言 Python 导出与 TS 对齐，把"一致性"变成被测断言；③ **API conformance 测试**（open-flow `conformance.ts`）——`ControlApiConformanceCase` + `exact(value, expectedFields)` 做线上 API 字段集契约校验。
- **coverage 门槛**：SDK 类 90/85/90/90（statements/branches/functions/lines，connector-sdk）；平台类 70/80/80（open-flow）。

## 4. 错误处理
### 4.1 TS 判别联合错误类范式（标准写法）
```typescript
export class ConnectorError extends Error {
  readonly code: "rate_limited" | "credential_expired" | (string & {});  // 开放联合：向前兼容
  readonly status?: number;
  constructor(...) { super(...); Object.setPrototypeOf(this, ConnectorError.prototype); }  // 修 prototype 链
}
export function isRetryable(err: unknown): boolean { /* RETRYABLE_CODES + 429/5xx/status 0 */ }
```
- 基类用 `this.name = new.target.name`；`toJSON()` + `static fromUnknown(error)` 收口未知错误。
- 错误码**两派**（按 SDK 系归类，勿混用）：连接器系 **snake_case**（`rate_limited`）；云任务/fusion 系 **SCREAMING_SNAKE**（`INSUFFICIENT_QUOTA`、`TASK_TIMEOUT`）。新服务端返回结构化错误码，**禁止客户端靠文案子串启发式分类**（教训：两个 task SDK 用"余额/点数/费用"匹配判余额不足，后端文案一变即失效）。

### 4.2 Python 镜像错误与重试参数
- Python：双命名兼容（错误属性同时挂 `task_id`/`taskID`、`status_code`/`statusCode`）；`from_unknown()` 与 TS 版逐逻辑对齐；pdf-craft 定义领域错误（`PDFError/OCRError/InterruptedError`）+ `is_inline_error()` 谓词。
- 重试/退避标准参数（oo-cli `retrying-fetcher.ts` 范本）：可重试状态码 `[429, 502, 503, 504]`（fusion 另含 408/5xx/网络错误）；指数退避 base 1s，`delay = min(30_000, 2 ** attempt * 1000)`，默认 max 2 次重试；`AbortError` 与已 abort 的 signal **不重试**；`sleep` 可注入便于测试。
- 轮询语义：**请求级失败（网络抖动/5xx）不终止轮询**，只有明确终态、超时或外部 abort 才停；超时消息附最近一次轮询错误。

## 5. 命名与代码组织
- **npm 命名**：内部/工具包 `@oomol-lab/*` scope；面向公众 SDK 无 scope 裸名（oomol-fusion-sdk、oomol-cloud-task-sdk）；**仓库语言后缀不进包名**（ovm-js→`@oomol-lab/ovm`，`-ts/-py` 只属仓库）。⚠️ 同一 npm 包名永远只有一个 owner 仓（task-sdk-ts 与 block-sdk-ts 同名事故，旧仓必须 archive + deprecate）。
- **目录分层**：CLI `src/{adapters,application,i18n}`，application 下按域分 `commands/telemetry/auth/contracts/shared/logging`；服务端 `src/{core,server,providers,oauth}`，provider 固定四件套 `providers/<service>/{definition,actions,executors}.ts`；三层所有权 `common/browser/node` 强制分层（common 不 import browser/node，browser 不 import node，`check:boundaries` 静态强制）。
- **类型**：`strict: true` + `noUncheckedIndexedAccess: true`（严仓加 `exactOptionalPropertyTypes: true`），新仓方向 `verbatimModuleSyntax: true` + `isolatedDeclarations: true`；"Prefer `interface` for object-shaped contracts. Keep unions and mapped/utility compositions as `type`."（open-connector AGENTS.md）；Python 源码全量标注 + CI 跑 pyright，新 SDK 必须放 `py.typed`。

## 6. 注释与文档
- **代码注释一律英文**（oo-cli："Comments must be in English"；open-flow："plain English sentence style with terminal punctuation"）；测试 fixture 与本地化文案可留中文。
- **多语言 README**：中文版文件名标准化为 **`README.zh-CN.md`**（历史有 `README_zh-CN.md`/`README_ZH.md`/`README-ZH_CN.md` 四种分裂，新仓用第一种）；头部 badge 墙（CI/registry/license）+ `English | [中文](./README.zh-CN.md)` 切换行。
- **AI 文档层级**（公司差异化优势）：① `AGENTS.md`——硬性开发规则的唯一载体（禁 barrel、prefer-top-level type import 等只写这里，不进 lint）；② `CLAUDE.md`——薄入口，按任务索引 `docs/ai/*.md`；③ 消费者 AI 文档 `README.ai.md` + `AGENT_GUIDE.md`（含 "Preferred Usage"）；④ 契约文档 `docs/commands.md` 只写用户可见契约；⑤ 架构文档 `docs/architecture.md`（owner 模型 + 反模式清单）。**诚实性文档**明确区分"目录里有/本地可执行/已验证"三态。

## 7. 版本与发布
- **源码 package.json 常驻占位版本 `0.0.0`**，真实版本由 CI 从 git tag 计算（LockIME："Local builds are always 0.0.0-development; CI overrides at archive time"）。
- **npm publish 走 OIDC Trusted Publishing**：`permissions: { id-token: write }` + `pnpm publish --access public --provenance`；GitHub Release 用 `generate_release_notes: true`。⚠️ OIDC JWT **不要放命令行参数**（进程列表/CI 日志泄漏面）。
- **PyPI 黄金范本**（pdf-craft release.yml）：workflow_dispatch + `environment: pypi` + pypa/gh-action-pypi-publish；发布前四重校验（必须 main、版本无 v 前缀、`docs/changelog/v{VERSION}.md` 存在且非空、远程 tag 不得已存在）→ pyright→pylint→test→poetry build→publish。
- **changelog 无 changesets**，手写每版本一个 `docs/changelog/<tag>.md`；Go 库发 tag 带 module major 后缀（`/v3`），纯二进制仓可不带。

## 8. Git 惯例
- **Conventional Commits 高度一致**：`feat(scope):` / `fix(scope):` / `chore(ci):` / `docs:` / `refactor(ipc)!:`（破坏性加 `!`），尾缀 PR 号；样例 `feat(flow): update Open Flow command to alpha.5 (#345)`。分支全部 `main`（无 master），主干 + 短 PR。
- 卫生文件新仓标配：`.editorconfig`（TS 4 空格/JSON·YAML 2 空格，md 关 trailing trim）、`.gitattributes`、`.bun-version` 或 `packageManager`、`SECURITY.md`（对外产品）。

## 9. 架构观念十律（设计评审硬标准）
1. **不可变事实，可变指针**——历史不改写：ProjectRevision 不可变 + 至多一个 Live pointer，Rollback 是新建记录而非修改；版本化 release 每次发新版本，compat 别名保活而非改名。
2. **fail-closed Capability**——用户代码/插件只在隔离 realm 拿到窄能力，Task 结束即收权；Connector 能力缺失时等待宿主配置而非降级放行（isolated-vm、oo-guard）。
3. **中间产物显式契约化**——长管线中间状态落盘为稳定格式（DocumentPackage 的 chapters/*.xml+toc.xml、.wikg、resume_state），每阶段可独立重放/调试/续跑。
4. **断点续跑是默认**——请求级内容哈希缓存（只缓存成功响应、原子写入）、页级/块级 checkpoint（索引+原文双校验防漂移）、`ocr/done` 标记跳过。
5. **懒加载隔离上游故障**——执行器懒加载（open-connector 1441 个 executors 动态 import）、OCR 后端延迟导入且不暴露上游 factory——上游问题在上游仓库修，pin 住接入点。
6. **单二进制/黑盒二进制分发**——运行时资产 `go:embed` 进二进制；上层产品把 CLI 当黑盒驱动（env 注入 + 隔离 config 目录），不 link 内部 API。
7. **兼容层平滑换代**——新一代运行器保留旧参数"仅兼容解析"并文档化覆盖优先级；事件协议写明 "for compatibility ovm-js"。
8. **双语言 SDK 同源生成**——TS 仓的 OpenAPI 快照是唯一事实源，py 从同一文件生成类型；语义（期望状态码/错误码/重试判定/默认超时）逐项镜像；差异写成明文策略（TS declaration merging vs py TypedDict）。
9. **状态机显式建模**——任务六态（queued→scheduling→scheduled→running→success/failed）、判别联合类型；TS 用 discriminated union 即 schema（zod 单进单出契约）。
10. **边界即合同**——架构文档定义 owner 模型（谁拥有什么目录/包），静态检查强制归属（`check:boundaries`），越界 PR 直接挂。

## 10. 安全观念（安全评审清单）
- **凭证三级隔离**：bootstrap token（env，仅引导）→ 持久 token（`oct_` 前缀，库存哈希、仅显示一次、四维策略 allowedActions/allowedProxies/allowedConnections/blockedActions）→ 短时 JWT（JWKS）；桌面端 token 走 safeStorage 且**不回渲染进程**。
- **策略只能收窄不能扩展**：部署 env → runtime_policy → token 授权三层叠加，每层只能减权限。
- **SSRF 守卫是体系级强制**：所有出站经守卫 fetcher——URL + 每个重定向 Location + DNS 解析后实际地址逐一校验；云元数据/链路本地地址黑名单；私网访问需显式 `allowPrivateNetwork` 并线程化贯通；WebSocket 走同一 hop 校验。
- **静态加密为默认**：凭证/OAuth state/幂等载荷 AES-256-GCM，key 不落库、支持 rotate-key 重编码。⚠️ open-connector 无 key 时明文落库仅告警——新部署必须设 key。**OAuth 纪律**：PKCE、state 一次性 take + 加密存储 + 15 分钟 TTL、requestedScopes 只能收窄、回调统一单端点。
- **进程与边界**：sidecar 走 loopback + 随机进程密码，凭证经环境变量注入不落盘，退出走两级进程树 reap；受管 artifact 目录校验 symlink 防逃逸；HTTP Basic Auth 用 `timingSafeEqual` 常量时间比较；CORS 不得开 `*`（sign-server 反面教材）；单飞 promise 防缓存击穿式并发刷新，幂等键 + 请求指纹 + 限时可重放。

## 11. API 与协议设计观念
- **统一响应信封** `{success, message, data, meta}`（open-connector /v1），meta 携带 executionId/审计标记/分页 nextToken。
- **MCP 服务设计**：无状态 JSON-RPC POST（不开 SSE）；只暴露 5 个左右发现型工具（list/search/guide/execute）而非全量 action；工具 handler 必须用 inputSchema/zod 校验（⚠️ mcp-sdk 全 `args: any` 是待修短板）；长任务用 submit + 轮询分离，不在单请求内长阻塞。
- **异步任务 API 形态**：submit→state→result 三端点，用期望状态码驱动轮询（200 完成/202 进行中/404 未就绪）；`wait/run/waitData/runData` 归一化完成语义。**认证双模**：浏览器走 httpOnly cookie（`oomol-token`），程序化走 Bearer；SDK 两者都支持且默认明确。
- **扩展三层**：内置 registry → 运行时 registerTask/registerAction → 裸 `request()`；TS 用 declaration merging 开放类型联合（`(keyof Registry & string) | (string & {})`），类型包滞后不阻塞编译。
- **类型包按需子路径化**：万级类型拆 per-provider `.d.ts` 子路径 + `exports: "./*"`，避免全量入图。**幂等**：写操作支持 Idempotency-Key，key 哈希 + 请求指纹 + 24h 可重放响应。

## 12. LLM 应用工程规范（全线家法）
1. **请求级内容哈希缓存**：key = sha256(消息+参数+协议版本)，磁盘缓存、仅成功响应原子写入。
2. **并发限流**：BoundedSemaphore/AsyncSemaphore **上限 6**，有序并发保章节顺序。
3. **重试时温度/top_p 递增**：预设区间内线性递增以跳出坏样本，区别于盲目同参重试。
4. **两段式结构化生成**：生成 LLM（高温）产内容 + 回填 LLM（低温）按模板/结构对齐，修复回路设上限（pdf-craft ≤5 次 hill climbing）。
5. **结构保持优先**：XML 模板回填 + 相似度评分对齐；翻译不改排版；时间戳/索引全程保持（字幕 cue 双重校验）。
6. **成本计量内建**：token 用量分列（input/cache/output）、metering + aborted 检查、请求 JSON 日志。
7. **LLM 是增强不是依赖**：核心功能启发式优先（TOC 字号分析），断网/无 key 也能跑核心链路。
8. **数据完整性优先于展示**：只清洗确认的伪影，绝不静默改写用户/翻译内容；错误分级回调（可恢复 vs 致命）。
9. **prompt 回归用 evals**：对比 prompt 修改前后（wiki-graph），fixture 固定输入。

## 13. UI 风格观念
- **Web 栈（新项目标准）**：React 19 最新 patch + Vite 8 + TypeScript + oxlint/oxfmt（React 18 只在弃用代码）；**Tailwind 4 全员**（`@tailwindcss/vite` 插件、**零 tailwind.config**、`@custom-variant dark (&:is(.dark *))` + `@theme inline` 映射 CSS 变量；UnoCSS 勿用于新仓）。
- **组件**：shadcn/ui + Radix，组件 vendored 进 `src/components/ui/`，不追 registry 更新（本地改过的变体标注"升级时不得覆盖"）；辅助 cva + clsx + tailwind-merge；toast 用 sonner；动效用 motion（尊重 `useReducedMotion`）；AI Markdown 用 streamdown + shiki；图标界面一律 lucide-react、品牌 logo 走 iconify、macOS 用 SF Symbols template。
- **主题 Token（wanta theme.css 范本）**：oklch + `color-mix(in oklab)` 派生；中性色极克制（蓝灰基底、几乎无彩度），**主色 = 前景色**（黑白主按钮），彩色只做语义四件套 `--destructive/--success/--warning/--info`；自研 `--oo-*` token 层（字号 caption 0.75rem→page-title 1.5rem、控件高度 compact 1.75/标准 2/comfortable 2.25rem、圆角以 `--radius: 0.5rem` 派生）；间距 4pt 网格全局通用（SwiftUI 同名 xxs2 xs4 sm6 md8 lg12 xl16 xxl24 section32）；暗色 `.dark` + `prefers-color-scheme` + localStorage 三态、是独立 token 表不是滤镜；附件/类型色按类型固定 hue（pdf 红、video 紫、code 靛…）+ 12%/18% 透明 surface + 同 hue 前景。
- **布局交互**：左侧固定侧边栏 + 主内容（wanta 264px 三层壳、open-connector 248px grid 两栏），不用顶部导航；侧边栏可折叠（79px），item 高 32px、图标 16px；**克制加页**（加页面前先问是否真需要 router，优先 AppShell 内部状态）；空状态 lucide 图标 + muted 文案；表格 shadcn table + 行 hover token；状态徽章 tone 三态 success/warning/error；AI 聊天专属组件目录（`ai-elements/`）与通用 shadcn 原语分开，视觉层级固定产物卡→review 卡→过程次级入口，流式期间 Enter 只许发送。
- **macOS SwiftUI（LockIME/CloseUp DESIGN.md，公司最完整设计规范）**：北极星 "calm, native, system-native utility"（不像系统原生控件就不做；反模式：黑色胶囊 toast，改 NSAlert + 内联结果）；**Liquid Glass 只用于导航层**（内容区永不上 glass、禁手动 NSVisualEffectView、glass-on-glass 包 `GlassEffectContainer`、pre-26 降级 `.bordered`）；**DS 命名空间禁内联字面量**（间距/圆角/动效参数全进 `DS` caseless enum，嵌套容器同心圆角）；**品牌三杠杆**——一个 accent（LockIME "Lock Indigo" #3A5BD9/#5B7BF0 明暗双值，只许出现在锁定态/唯一主按钮/链接三处）、一个满版 app icon、一个签名动效（参数写死：toggle spring(0.3,0.85)、confirmIn easeOut(0.18)，手动 spring 必须 gate `reduceMotion`）；排版 macOS 正文 13pt（禁 iOS 17pt），只用语义字阶 + `.primary~.quaternary` 梯度，禁 `.system(size:)`；菜单栏原生 NSMenu（零自定义色），Settings 顶部 tab 拒绝 sidebar。
- **i18n**：zh-CN + en 是基线（wanta 仅此两语，zh-CN 基准/默认），公众 web 加 zh-TW/ja/ru/fr、macOS 加 de/es/pt；实现从轻（自研 context flat dot key + `{var}` 占位，或自研库），**不引 i18next 重框架**；繁简判定手写 subtag 优先于 region；macOS 用 xcstrings + in-app 覆盖 + 本地化守卫测试；web 按 `html[lang]` 切字体栈。
- **字体视觉基调**：**无网络字体**，系统栈为王（-apple-system/SF Pro/PingFang SC），mono 用 SFMono/Consolas；**品牌色 = 蓝紫/靛蓝系**（#3A5BD9 / #4F63D8 / #7C9DFF / #6366f1），内容卡片 = 毛玻璃白卡 + 135° 鲜艳渐变 + indigo 点缀；**动效克制是底色**（禁硬切/全屏 shake/通用 ring；宣传物料状态由绝对帧派生、禁 `Date.now()`/`Math.random()`；品牌前景图保原色、不重上色、不裁剪）。

## 14. 产品形态与分发观念
- **形态阶梯**（按消费者选形态，不是都做库）：核心库（pdf-craft）→ 云 API + SDK（fusion）→ CLI 枢纽（oo-cli）→ 工作流 block/flow 包（package.oo.yaml）→ **Agent Skill**（SKILL.md 纯文档 + 运行时 schema 发现，无代码无凭证）→ **MCP Server**。新能力优先问"能不能只是一个 SKILL.md"。
- **Skill 设计铁律**：纯文档、零代码、零凭证；`allowed-tools: [Bash(oo *)]` 钉死通道；契约运行时发现（`oo connector schema`）使上游 API 演进不腐化技能；动作标 `[write]`/`[destructive]`。
- **fork 四姿态**（按投入递增）：浅壳 fork（❌ cline 教训：百行改动后必弃坑漂移）→ 小步跟随 fork（documented 上游合并流程）→ **发行版而非 fork**（hermes：upstream.lock.json SHA pin + `git apply --check` fail-closed 唯一小补丁 + /opt 只读 + /data 可变 + 非 root）→ **配置级寄生**（wanta×OpenCode：零源码修改，纯配置/工具注入，升级只需动 pin）。新项目默认后两种。**精确 pin 三方同版**（wanta opencode-ai@1.18.21），升级是显式联动动作。
- **分发渠道**：npm/PyPI OIDC 直发；macOS 工具走自建 homebrew-tap（repository_dispatch 全自动 bump：下载产物→双架构重算 sha256→`brew audit --online --strict` 通过才 push；beta 永不进 tap）；桌面 App Sparkle 自更新 + Developer ID + Hardened Runtime；Windows 走 Inno Setup 静默卸载（/VERYSILENT）+ UKey 远程签名服务。

## 15. 仓库治理与许可证
- **许可证矩阵**：平台/产品 Apache-2.0 或 MIT；基础设施小库 MIT/Apache；面向公众的 macOS 工具 GPL-3.0（独立产品线，与 Apache 主线物理隔离）；**GPL/LGPL 代码绝不混入 Apache 产品**（flexpilot 教训）。
- **弃用纪律**：死仓库 archive + README 顶部声明迁移去向（paper-rag "migrate to knowledge-base" 正面示范）；同一生态只保留一条活跃路线（kwbase vs wiki-graph 未收敛是教训）。
- **组织化收编**：CI badge、构建源、fork 依赖必须指向 org 仓库，不留个人仓库单点（ihexon 依赖是现状风险）。仓库命名 kebab-case；`-ts/-py` 后缀标语言实现；`oomol-*` 前缀标公司出品。

## 16. 反模式禁令清单（每条源自真实教训）
| ❌ 禁止 | 依据 |
|---|---|
| 同一 npm 包名出现两个 owner 仓 | task-sdk-ts/block-sdk-ts 同名双仓事故 |
| 客户端用文案子串猜测错误类型 | task SDK "余额/点数"启发式 |
| 凭证/OAuth 进 Agent 进程、渲染层或镜像层 | 全线红线；hermes 刻意 strip provider env |
| 把全部 action 注册为常驻工具 | dsh-oomol AGENTS.md 明令渐进发现 |
| 加 Prettier / 手动折行 / 建 barrel index.ts | open-connector/open-flow AGENTS.md |
| 破坏性变更不写兼容层直接切换 | ovm-next legacy 参数规范 |
| 双语言 SDK 各自手写类型不同源 | fusion 用同一 openapi snapshot 生成 |
| fork 后不声明改动、不记 upstream.lock | hermes upstream.lock.json 模式 |
| 弃用仓库不 archive 不声明去向 | cline/luna 教训 |
| CORS `*` + "内网专用"假设并存 | sign-server 教训 |
| 长管线无断点续跑/无缓存 | 全线家法反证 |
| UI 内联色值/字号/间距字面量 | LockIME DS 禁令 |
| 不尊重 reduceMotion 的自定义动效 | LockIME/wanta 双侧规定 |

## 17. 新仓库启动 checklist
- [ ] 选定形态（库/CLI/服务/Skill/MCP）与语言，按 §2 选包管理器与构建器
- [ ] lint/format：新仓默认 oxlint+oxfmt（§1.1）；Python pyright+pylint+ruff（§1.3）
- [ ] tsconfig：strict + noUncheckedIndexedAccess（+ verbatimModuleSyntax）
- [ ] AGENTS.md 写硬规则（风格、禁 barrel、注释英文、契约文档边界）
- [ ] README（英文主文档 + README.zh-CN.md）+ badge 墙 + 语言切换行
- [ ] CI 三件套（pr-check / push-main / release），release 走 OIDC、concurrency 防并发
- [ ] package.json 占位版本 0.0.0，tag 触发计算真实版本
- [ ] 错误类按 §4.1 范式；重试按 §4.3 参数
- [ ] 测试：框架按 §3；涉及隐私/契约时写架构测试
- [ ] UI 仓：Tailwind 4 + shadcn vendored + oklch token 层 + 暗色三态 + i18n（zh-CN+en 基线）
- [ ] 涉外部服务：SSRF 守卫 + 凭证不落盘 + 结构化错误码
- [ ] 卫生文件：.editorconfig/.gitattributes/.bun-version 或 packageManager

## 相关技能
- `common-rulebook`：本 skill 的方法论层与普适原则来源；冲突时本文件实例参数优先，但实例若违反普适原则应标记为"实例层债务"。
- `code-review-and-quality`：评审五轴与批准标准；本 skill 的架构十律与安全清单是其领域化补充。
- `writing-plans`：把规范要求落成可执行的实现计划。
- `systematic-debugging`：反模式事故蒸馏与根因调查的方法支撑。
