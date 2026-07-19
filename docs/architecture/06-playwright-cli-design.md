# Playwright CLI Design Notes

> 来源：`tmp/libs/playwright-cli`（Microsoft）

## Repository Role
薄分发层 + Skill 包装器。真正的引擎来自 `playwright-core` 的 CLI client，此仓库只负责版本检查、skill 校验和安装。

```
playwright-cli.js          ← 薄入口
    ├── program()          ← 来自 playwright-core/lib/tools/cli-client/program
    │       ├── 所有命令注册（open/goto/click/type/snapshot/find 等）
    │       └── 会话管理、网络拦截、视频录制
    └── skillCheck.js      ← 校验本地安装的 SKILL.md 是否与 CLI 版本一致
            └── bundled SKILL.md（来自 playwright-core 内部）

skills/playwright-cli/
    └── SKILL.md           ← 给 Coding Agent 的"使用说明书"
    └── references/        ← 8 个专题参考文档
```

## Key Design Patterns

### 1. CLI over MCP Philosophy
- 现代 coding agents 偏好 **CLI-based workflows** 暴露为 Skills，因为更 token-efficient
- CLI 调用避免加载大型 tool schemas 和 verbose accessibility trees 到模型上下文
- 适合高吞吐量 coding agents，需在代码库、测试和推理之间平衡上下文窗口

### 2. Skill as Tool Manual
- `SKILL.md` 本质是给 LLM agent 的 tool usage guide
- 定义了 `allowed-tools`（Bash 模式匹配）
- Quick start + 命令参考 + 专题深度文档

### 3. Version Consistency Check
- `skillCheck.js` 对比 `.claude/skills/playwright-cli/SKILL.md` 与 bundled skill
- 版本不一致时提示运行 `playwright-cli install --skills`
- 避免 agent 用过时文档操作工具

### 4. Session Isolation
- `-s=name` 支持多浏览器实例
- `PLAYWRIGHT_CLI_SESSION` 环境变量全局指定
- `--persistent` 持久化 profile 到磁盘
- `list`/`close-all`/`kill-all` 会话管理

### 5. Snapshot-First Interaction
- 每次操作后输出页面快照（YAML）
- agent 用 ref（如 `e15`）定位元素
- `find` 命令在 snapshot 内搜索文本/正则，返回匹配节点 + 上下文
- 不依赖 CSS selector，降低 selector 失效风险

### 6. Raw Output & Piping
- `--raw` 剥离状态/快照，只返回值
- `--json` 结构化输出
- 支持管道到 `jq`、重定向到文件

## Commands
- Core: open, goto, close, type, click, dblclick, fill, drag, drop, hover, select, upload, check, uncheck, snapshot, find, eval, dialog-accept/dismiss, resize
- Navigation: go-back, go-forward, reload
- Keyboard: press, keydown, keyup
- Mouse: mousemove, mousedown, mouseup, mousewheel
- Save as: screenshot, pdf
- Tabs: tab-list, tab-new, tab-close, tab-select
- Storage: state-save/load, cookie-*, localstorage-*, sessionstorage-*
- Network: route, route-list, unroute
- DevTools: console, requests, request, run-code, tracing-start/stop, video-*, show, generate-locator, highlight

## Config & Env
- 配置文件: `.playwright/cli.config.json`
- 环境变量: `PLAYWRIGHT_MCP_*` 系列（ALLOWED_HOSTS, BLOCKED_ORIGINS, TIMEOUT_ACTION 等）
- 支持 `--config` 指定配置

## Mocode Takeaways
1. 可在 `internal/core/agent/tools/` 的工具旁配套 `.md` skill 文件
2. CLI over MCP 的哲学适用于 token-sensitive 场景
3. 版本一致性检查机制值得借鉴
4. Session 隔离设计（named sessions + env var）
5. Snapshot-first 交互降低元素定位失败率
