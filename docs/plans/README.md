# Plans 目录结构

> 改进的子目录组织方式，避免平铺过多文件。

## 组织原则

- **按主题分目录**：相关的研究/计划放在同一个子目录
- **编号前缀**：子目录内文件用数字编号保持顺序
- **README 索引**：每个子目录保留 README，父级 docs/README.md 同步索引

## 当前结构

```
plans/
├── README.md                          # 本文件，记录目录结构规范
├── search-tools/                      # 外部项目搜索工具深度研究
│   ├── 01-grep-tool-architecture.md
│   ├── 02-web-search-provider-chain.md
│   ├── 03-ast-grep-design.md
│   ├── 04-rust-native-grep-engine.md
│   └── 05-search-testing-observability.md
├── agentic-fetch/                     # agentic_fetch 工具端点不可用时的根因分析
│   ├── 01-context-canceled-failure.md
│   ├── 02-fallback-strategy.md
│   └── 03-context-isolation-plan.md
├── tool-call-arguments/               # 部分 provider 报 tool_calls.function.arguments is required
│   └── 01-empty-arguments-bad-request.md
└── summary-async/                     # /summary slash 命令同步阻塞 TUI 根因与异步化方案
    ├── 01-sync-blocking-root-cause.md
    ├── 02-async-fix-options.md
    ├── 03-implementation-plan.md
    ├── 04-pubsub-integration.md
    └── 05-testing-matrix.md
```

## 子目录说明

### search-tools/

来自 oh-my-pi 项目的搜索工具体系深度分析，作为 mocode 工具系统改进的参考设计。

| 文件 | 来源 | 核心内容 |
|------|------|----------|
| 01-grep-tool-architecture.md | `packages/coding-agent/src/tools/grep.ts` | TS 层完整架构、输入处理管道、路径解析、输出格式化 |
| 02-web-search-provider-chain.md | `packages/coding-agent/src/web/search/` | 22 Provider 懒加载 Registry、Fallback 策略、Credential 解析 |
| 03-ast-grep-design.md | `packages/coding-agent/src/tools/ast-grep.ts` | tree-sitter + ast-grep-core、DSL 语法、与 grep 协同 |
| 04-rust-native-grep-engine.md | `crates/pi-natives/src/grep.rs` | RE2/PCRE2 双引擎、两阶段搜索、NAPI FFI |
| 05-search-testing-observability.md | `scripts/session-stats/` + provider mocks | 测试金字塔、可观测性指标、会话级行为测试 |

### agentic-fetch/

agentic_fetch 工具在外部端点不可用时反复抛 `context canceled` 的根因分析与修复计划。

| 文件 | 主题 |
|------|------|
| 01-context-canceled-failure.md | 失败链路追踪：子 Agent 继承父 context → SDK RequestTimeout → 硬失败 |
| 02-fallback-strategy.md | 降级策略设计：Provider 链 + 静态快照 + DuckDuckGo 直退路 |
| 03-context-isolation-plan.md | 上下文隔离改造：子 Agent 独立 deadline + 软取消 vs 硬取消分类 |

### tool-call-arguments/

Step 3.7 Flash 等严格 OpenAI 兼容 provider 在第二轮请求时把 `tool_calls.function.arguments` 为空字符串的历史消息判为非法，导致 400 的根因分析与修复计划。

| 文件 | 主题 |
|------|------|
| 01-empty-arguments-bad-request.md | fantasy 库 Generate 路径透传空 arguments；Mocode 侧在 PrepareStep 钩子里归一化为 `{}` |

### summary-async/

`/summary` slash 命令在 TUI 中同步阻塞 Update 循环，导致 LLM 流式响应期间整个界面冻结。与 `session_summary` 工具调用路径（已异步）行为不一致。修复方案：复用现有 `summaryQueue`，slash 入口走 `AgentEnqueueSummary`，通过 pubsub 推回完成事件。

| 文件 | 主题 |
|------|------|
| 01-sync-blocking-root-cause.md | 同步阻塞根因：`tea.Cmd` 包裹 `AgentSummarize` 直调，Update 循环冻结；含 drainQueuedSummaries 触发时机澄清 |
| 02-async-fix-options.md | 三方案对比：就地异步 / 复用 summaryQueue（选定）+ 主动 drain / 假进度；含 busy 守卫语义差异（B1/B2/B3） |
| 03-implementation-plan.md | M0–M5 拆 PR：设计澄清 → API 暴露 → pubsub → UI 切换 → 测试 → 文档 |
| 04-pubsub-integration.md | 接入 app.events 总线：SummaryCompletedMsg 类型 + setupSubscriber 范式 + session 级隔离未来改进 |
| 05-testing-matrix.md | 单元 + 集成 + UI + 端到端手动测试矩阵；含 V1-V4 端到端验证步骤 |

## 添加新计划时的规范

1. 确定主题归属（如 `search-tools/`、`agent-architecture/`、`tui-redesign/`）
2. 创建或复用对应子目录
3. 子目录内文件使用数字编号前缀
4. 更新 `docs/README.md` 的「参考研究」表格
5. 如创建新子目录，在 README 的「当前结构」和「子目录说明」中追加条目
