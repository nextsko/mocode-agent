# 子计划 12 — 测试与可观测性

> [!info] 元信息
> **状态**：pending
> **创建日期**：2026-09-14
> **所属计划**：[[MASTER_PLAN]] Web Search Provider 链吸收
> **依赖**：06-11 全部完成
> **后续被依赖**：无

---

## 目标

为 Provider 链添加完整的测试基础设施与可观测性（metrics + tracing + 错误分类告警）。

## 范围

### 必须实现

1. **单元测试（每个 Provider）**
   - HTTP client mock（`httptest.Server`）
   - 成功路径 + 失败路径 + 部分失败路径
   - 覆盖率 ≥ 70%

2. **集成测试（Chain）**
   - 多 provider fallback 行为
   - credential broker env vs db 优先级
   - 全部失败聚合错误

3. **E2E 测试**
   - CI：mock provider（不依赖真实网络）
   - 手动：真实调用 3 个免费 provider 至少 1 次

4. **Metrics（Prometheus）**
   ```go
   // Counter
   web_search_requests_total{provider, status}
   // Histogram
   web_search_duration_seconds{provider, phase}
   // Gauge
   web_search_provider_available{provider}
   ```

5. **Tracing（OpenTelemetry）**
   - 每个 provider search() 一个 span
   - 子 span：HTTP request / SSE parse / result extract
   - attributes：provider id, status code, source count, latency

6. **错误分类告警**
   - 高频 401/403 → "凭证失效"告警（建议用户重新 /login）
   - 高频 429 → "限流"告警（建议切换 provider）
   - 高频 credits exhausted → "余额耗尽"告警
   - 全部 provider 失败 → "搜索能力完全不可用"严重告警

## 文件清单

```
pkg/search/
├── observability/
│   ├── metrics.go         # Prometheus collectors
│   ├── tracing.go         # OTel spans
│   └── alerts.go          # 错误分类告警
├── tests/
│   ├── chain_e2e_test.go  # 端到端
│   ├── chain_unit_test.go # 单元
│   └── fixtures/          # 真实 provider 响应样本
│       ├── exa_success.json
│       ├── ddg_success.html
│       ├── zai_success.txt
│       └── ...
└── docs/
    └── architecture/
        └── search-observability.md
```

## 验收标准

- [ ] 每个 Provider 单测覆盖率 ≥ 70%
- [ ] Chain e2e 测试：mock 全部 provider，验证 fallback 行为
- [ ] Prometheus metrics 暴露在 `/metrics`
- [ ] OTel spans 在 Jaeger/Zipkin 中可见
- [ ] 5 种错误分类告警规则文档化

## 工期估

2 个工作日

## 风险

- **真实网络依赖**：e2e 测试若依赖真实 provider，CI 稳定性差；建议默认 mock + nightly 真实调用
- **PII 风险**：tracing 中可能包含 query 内容，需做 PII 过滤

## 设计原则（铁律）

> [!warning] 本计划必须严格遵循
> 1. **置顶向下作规划**：先 metrics → tracing → alerts，三层递进
> 2. **至底向上搞实现**：`metrics.go`（Prometheus collectors）→ `tracing.go`（OTel spans）→ `alerts.go`（错误分类告警）→ 测试 fixtures
> 3. **向上发展作拼装**：通过 Chain 装饰器模式统一注入 metrics/tracing（不侵入 provider）
> 4. **逐层验证和仿真**：metrics 单测（Prometheus testutil）→ tracing span 验证 → alerts 规则测试 → e2e（mock provider）
> 5. **铁律约束 LLM 超边界**：测试代码独立 `tests/` 子目录，禁止修改 provider 主体（除非显式声明装饰器）

## 执行模型（推荐）

- **主执行模型**：**Haiku**（测试与可观测性是模板化工作）
- **辅助模型**：无

## 相关笔记

- [[provider-chain-design]] §8 错误分类器
- [[MASTER_PLAN]]