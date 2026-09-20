# 子计划 06 — Provider 链骨架

> [!info] 元信息
> **状态**：pending
> **创建日期**：2026-09-14
> **所属计划**：[[MASTER_PLAN]] Web Search Provider 链吸收
> **依赖**：无（基础层）
> **后续被依赖**：07 / 08 / 09 / 10 / 11

---

## 目标

实现 mocode Go 版的 Web Search Provider 链骨架，作为后续 5 个子计划的基础设施。

## 范围

### 必须实现（核心 4 件套）

1. **`ProviderV2` 接口**
   - `ID() string`
   - `Label() string`
   - `IsAvailable(ctx, broker) (bool, error)`
   - `IsExplicitlyAvailable(ctx, broker) (bool, error)`
   - `Search(ctx, params) (*SearchResponse, error)`

2. **`Registry` 注册表**
   - `Register(meta ProviderMeta)`
   - `Load(id) (ProviderV2, error)` — 懒加载 + 单例缓存（`sync.Map`）
   - `Order() []string` — 返回候选顺序
   - `Excluded(id) bool` — 黑名单检查

3. **`Chain` Fallback 循环**
   - `Execute(ctx, params) (*SearchResponse, error)`
   - 状态机：构建候选 → 懒加载 → 探测可用性 → search → 失败聚合 → 错误返回

4. **`CredentialBroker` 凭证 broker**
   - `Resolve(ctx, providerID, opts) (string, error)`
   - env 优先 → authStorage fallback
   - force-refresh 协议（与 mocode 现有 AuthStorage 对接）

### 数据结构

```go
type SearchParams struct {
    Query            string
    Limit            int
    Recency          string  // "day"|"week"|"month"|"year"|""
    SystemPrompt     string
    Signal           context.Context  // 取代 AbortSignal
    Fetch            *http.Client     // 注入用
    MaxOutputTokens  int
    NumSearchResults int
    Temperature      float64
    AuthStorage      CredentialBroker
    ModelRegistry    *ModelRegistry
    SessionID        string
}

type SearchResponse struct {
    Provider         string
    Answer           string
    Sources          []SearchSource
    Citations        []SearchCitation
    SearchQueries    []string
    RelatedQuestions []string
    Usage            *SearchUsage
    Model            string
    RequestID        string
    AuthMode         string
}

type SearchSource struct {
    Title         string
    URL           string
    Snippet       string
    Author        string
    PublishedDate string
    AgeSeconds    int64
}
```

### 错误处理

- `SearchProviderError{Provider, Code, Message}` 结构
- `classifyHTTPError(resp) *SearchProviderError` — 正则识别 credits/quota/insufficient + 401/402/403
- `formatFailures(failures) string` — 多 provider 错误聚合

## 文件清单

```
pkg/search/
├── provider.go         # ProviderV2 接口
├── registry.go         # Registry 注册表
├── chain.go            # Chain fallback 循环
├── errors.go           # SearchProviderError + classifyHTTPError
├── types.go            # SearchParams / SearchResponse / SearchSource
├── credentials.go      # CredentialBroker 接口
├── credentials_env.go  # env 实现
├── credentials_db.go   # authStorage 对接
├── broker.go           # broker 组合器（env 优先 + db fallback + 单飞）
└── tests/
    ├── chain_test.go
    └── credentials_test.go
```

## 验收标准

- [ ] `go build ./pkg/search/...` 通过
- [ ] 单测：`Chain` fallback 行为测试（全部失败 / 部分失败 / 单个成功）
- [ ] 单测：`Registry` 懒加载 + 单例缓存测试
- [ ] 单测：`CredentialBroker` env vs authStorage 优先级测试
- [ ] 文档：`docs/architecture/web-search-provider-design.md` 写总体架构图 + 接口定义

## 工期估

3 个工作日

## 风险

- **现有 netcommon.Provider 链改造**：是否需要保证向后兼容？建议新建 `pkg/search/` 独立目录，不动 `netcommon`
- **CredentialBroker 接口锁定**：与 mocode AuthStorage 对接可能需要先调研现有认证体系（建议先看 `internal/auth/`）

## 设计原则（铁律）

> [!warning] 本计划必须严格遵循
> 1. **置顶向下作规划**：本计划已是 Phase 1 顶层骨架，向下分解为 types → broker → registry → chain → errors 五层
> 2. **至底向上搞实现**：先实现 `types.go`（SearchParams/Response/Source DTO）→ `credentials.go`（broker 接口）→ `provider.go`（ProviderV2 接口）→ `registry.go`（注册表）→ `chain.go`（fallback 循环）→ `errors.go`（分类器）
> 3. **向上发展作拼装**：`Registry.Register(meta)` 在 init() 中拼装；`Chain.Execute()` 通过 Provider 接口拼装多 provider
> 4. **逐层验证和仿真**：types 单测 → broker 单测 → registry 单测（mock provider）→ chain e2e（mock 全 provider）
> 5. **铁律约束 LLM 超边界**：LLM 只允许创建/修改 `pkg/search/` 目录下 plan 中列出的文件，禁止触碰 `internal/auth/`、`internal/netcommon/`、其他模块

## 执行模型（推荐）

- **主执行模型**：**Sonnet**（复杂接口设计 + 多文件协作 + 测试编排）
- **辅助模型**：Haiku（用于生成单测 fixture 数据）
- **不做的事**：不得用 opus（过重），不得用 haiku 主导接口设计

## 相关笔记

- [[provider-chain-design]] — oh-my-pi 完整设计参考
- [[MASTER_PLAN]] — 上层计划