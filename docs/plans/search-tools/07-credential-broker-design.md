# 子计划 07 — CredentialBroker 凭证 broker

> [!info] 元信息
> **状态**：pending
> **创建日期**：2026-09-14
> **所属计划**：[[MASTER_PLAN]] Web Search Provider 链吸收
> **依赖**：01-provider-chain-skeleton（接口骨架）
> **后续被依赖**：08 / 09 / 10 / 11

---

## 目标

实现 mocode 的 CredentialBroker：env 优先 + authStorage fallback + 单飞刷新 + force-refresh 协议。

## 范围

### 必须实现

1. **env-only broker**（无持久化场景）
   - `EnvBroker` 实现 `CredentialBroker` 接口
   - 静态查 `os.Getenv(envKey)`
   - 用于 Kagi / Brave / Tavily / Jina 等纯 env provider

2. **db-backed broker**（持久化场景）
   - `DBBroker` 实现 `CredentialBroker` 接口
   - 查 mocode `internal/auth` 的 AuthStorage
   - 支持 OAuth refresh

3. **multi-source broker**（默认组合）
   - `MultiBroker`：env 优先 → db fallback
   - 记录 miss 来源到 metrics

4. **单飞刷新（singleflight）**
   - `singleflight.Group` 防并发重复刷新
   - 凭证 refresh 时锁住，单 goroutine 拉新值，其他等待

5. **force-refresh 协议**
   - `Refresh(ctx, providerID, opts) (string, error)`
   - 主动失效 + 重拉 + 缓存
   - 用于 Provider 上报 401 后强制刷新场景

### 接口签名

```go
type CredentialBroker interface {
    Resolve(ctx context.Context, providerID string, opts ResolveOpts) (string, error)
    Refresh(ctx context.Context, providerID string, opts ResolveOpts) (string, error)
}

type ResolveOpts struct {
    EnvKey     string            // 单一 env 变量
    AuthID     string            // authStorage 中的 provider 标识
    SessionID  string            // 会话作用域
    Preferred  []string          // 多源优先级
    OnMiss     func(source string) // miss 回调（用于 metrics）
}
```

## 文件清单

```
pkg/search/credentials/
├── broker.go            # CredentialBroker 接口 + MultiBroker
├── env.go               # EnvBroker
├── db.go                # DBBroker (对接 internal/auth)
├── singleflight.go      # 包装 singleflight.Group
├── refresh.go           # force-refresh 协议
└── tests/
    ├── broker_test.go
    └── singleflight_test.go
```

## 验收标准

- [ ] `MultiBroker` env 优先测试通过
- [ ] 单飞刷新：10 并发请求同一 provider，只触发 1 次 refresh
- [ ] force-refresh：模拟 401 后自动重拉
- [ ] 单测覆盖率 ≥ 70%

## 工期估

1 个工作日

## 风险

- **mocode 现有 auth 模块耦合度**：需先看 `internal/auth/` 是否支持多 provider / OAuth refresh 协议
- **跨 session 凭证共享**：sessionID 隔离语义需与现有产品对齐

## 设计原则（铁律）

> [!warning] 本计划必须严格遵循
> 1. **置顶向下作规划**：本计划依赖 `06-provider-chain-skeleton` 的 CredentialBroker 接口，专注实现 env / db / multi 三种实现
> 2. **至底向上搞实现**：先 `env.go`（最简）→ `db.go`（次简）→ `singleflight.go`（包装）→ `refresh.go`（force-refresh）→ `broker.go`（MultiBroker 组合）
> 3. **向上发展作拼装**：MultiBroker 通过 embed EnvBroker + DBBroker 组合实现多源查找
> 4. **逐层验证和仿真**：env 单测 → db 单测（mock AuthStorage）→ singleflight 并发测试 → force-refresh 场景测试
> 5. **铁律约束 LLM 超边界**：只读 `internal/auth/` 的接口定义（必须明确白名单路径），不得修改该模块

## 执行模型（推荐）

- **主执行模型**：**Sonnet**（需要与现有 auth 模块对接，含 OAuth refresh 协议）
- **辅助模型**：无（本计划规模小，单一模型即可）

## 相关笔记

- [[provider-chain-design]] §6 凭证统一入口
- [[MASTER_PLAN]]