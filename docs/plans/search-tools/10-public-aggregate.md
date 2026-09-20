# 子计划 10 — Public 聚合 Provider

> [!info] 元信息
> **状态**：pending
> **创建日期**：2026-09-14
> **所属计划**：[[MASTER_PLAN]] Web Search Provider 链吸收
> **依赖**：06-provider-chain-skeleton / 09-duckduckgo-scraper
> **后续被依赖**：无

---

## 目标

实现 Public 聚合 provider：5 个免费子引擎 fan-out + soft/hard deadline + straggler abort + 共识排序。

## 范围

### 必须实现

1. **5 子引擎**
   - startpage（POST HTML）
   - google（GET HTML + browser fallback）
   - duckduckgo（已完成，04 子计划）
   - ecosia（GET HTML + browser fetch）
   - mojeek（GET HTML + 可选 Puppeteer/rod + ALTCHA solve）

2. **Fan-out 编排**
   - `errgroup.WithContext` + `context.WithTimeout`
   - soft deadline：5s（短结果先到先返回，但 goroutine 继续）
   - hard deadline：30s（所有 goroutine cancel）

3. **Straggler abort**
   - 软截止后，剩余子引擎显式 cancel context
   - 已收到的子结果保留，丢弃未到的

4. **共识排序**
   ```go
   func rankSources(sources []SubEngineResult) []SearchSource {
       urlCounts := map[string]int{}
       bestRank := map[string]int{}
       firstSeen := map[string]int{}
       for _, r := range sources {
           for _, s := range r.Sources {
               key := s.URL
               urlCounts[key]++
               if _, ok := bestRank[key]; !ok {
                   bestRank[key] = r.Rank
                   firstSeen[key] = r.Order
               }
           }
       }
       // sort: engineCount desc → bestRank asc → firstSeen asc
   }
   ```

5. **错误处理**
   - 单子引擎失败不影响其他
   - 全部失败 → 抛 SearchProviderError("public", 204, "no result")

## 文件清单

```
pkg/search/providers/public/
├── public.go              # PublicProvider 主体
├── fanout.go              # 5 子引擎并发编排
├── ranking.go             # 共识排序算法
├── engines/
│   ├── startpage.go
│   ├── google.go
│   ├── duckduckgo.go      # 复用 04 子计划
│   ├── ecosia.go
│   └── mojeek.go
└── tests/
    ├── public_test.go
    └── ranking_test.go
```

## 验收标准

- [ ] 5 子引擎 fan-out 并发编排正确
- [ ] soft deadline 5s / hard deadline 30s 实际触发
- [ ] straggler abort 后剩余 context cancel
- [ ] 共识排序：3 个子引擎都返回相同 URL → 排第一
- [ ] 单子引擎失败不阻塞其他

## 工期估

2 个工作日

## 风险

- **CAPTCHA 求解成本**：mojeek ALTCHA 求解依赖 proof-of-work 特性，若升级到图像 captcha 会失效
- **子引擎各自 cooldown**：每个子引擎需要独立的 cooldown 机制，避免连环触发 bot 检测
- **排序权重**：consensus > bestRank > insertion order 是 oh-my-pi 默认，mocode 是否要加权（如按引擎可信度）需业务侧决定

## 设计原则（铁律）

> [!warning] 本计划必须严格遵循
> 1. **置顶向下作规划**：先设计 fan-out 编排（errgroup + soft/hard deadline）→ 共识排序算法 → 子引擎注册接口
> 2. **至底向上搞实现**：`engines/startpage.go` → `engines/google.go` → `engines/duckduckgo.go`（复用 09 子计划）→ `engines/ecosia.go` → `engines/mojeek.go` → `ranking.go`（共识排序）→ `fanout.go`（errgroup 编排）→ `public.go`（Provider 主类）
> 3. **向上发展作拼装**：`PublicProvider` 通过注册多个子引擎实例，统一 fan-out 编排
> 4. **逐层验证和仿真**：单子引擎 mock 单测 → ranking 算法单测 → fan-out 编排测试（mock 部分子引擎超时）→ 真实调用 1 次
> 5. **铁律约束 LLM 超边界**：子引擎实现各自独立目录，避免相互依赖；`duckduckgo.go` 必须 import `09` 的 package（白名单）

## 执行模型（推荐）

- **主执行模型**：**Sonnet**（5 子引擎 + fan-out + 共识排序，复杂度中等偏高）
- **辅助模型**：Haiku（生成各子引擎 fixture HTML）

## 相关笔记

- [[free-providers-research]] §4 Public 聚合
- [[MASTER_PLAN]]