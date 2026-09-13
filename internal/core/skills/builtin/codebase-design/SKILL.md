---
name: codebase-design
description: >-
  Use when designing or improving a module's interface, hunting for deepening
  opportunities, deciding where a seam belongs, or making code more testable and
  AI-navigable. Also use when another skill needs the deep-module vocabulary
  (module, interface, depth, seam, adapter, leverage, locality).
---

# Codebase Design（深模块设计）

设计**深模块（deep module）**：小接口背后藏着大量行为，落在干净的**seam**上，并可通过该接口测试。凡是在设计或重构代码的地方，都使用这套语言与原则。目标：给调用方**leverage**，给维护者**locality**，给所有人可测试性。

## 术语表

术语必须严格照用，不要替换成「component / service / API / boundary」。语言一致本身就是重点。

| 术语 | 定义 | 不要用 |
|------|------|--------|
| **Module** | 任何有 interface 和 implementation 的东西，刻意与尺度无关：函数、类、包，或跨层切片 | unit、component、service |
| **Interface** | 调用方正确使用该模块必须知道的一切：类型签名，以及不变量、顺序约束、错误模式、必需配置、性能特征 | API、signature（过窄） |
| **Implementation** | 模块内部的代码主体。与 **Adapter** 区分：小 adapter 可以有大 implementation（如 Postgres 仓储） | — |
| **Depth** | 接口处的杠杆。调用方/测试每学一单位接口所能撬动的行为量。行为多而接口小＝**深**；接口几乎和实现一样复杂＝**浅** | — |
| **Seam** | 可以在不修改该处的情况下改变行为的位置；即模块 interface 所在之处（Michael Feathers）。「seam 放哪」是独立的设计决策 | boundary（与 DDD 的限界上下文冲突） |
| **Adapter** | 在 seam 上满足 interface 的具体物。描述**角色**（填哪个槽），而非内在实质 | — |
| **Leverage** | 调用方从 depth 得到的东西：每单位接口学到更多能力。一份实现被 N 个调用点、M 个测试复用 | — |
| **Locality** | 维护者从 depth 得到的东西：变更、bug、知识、验证集中在一处，而非散落在调用方 | — |

## 深 vs 浅

**深模块** = 小接口 + 大实现：

```
┌─────────────────────┐
│   Small Interface   │  ← 方法少、参数简单
├─────────────────────┤
│   Deep Impl         │  ← 复杂逻辑被隐藏
└─────────────────────┘
```

**浅模块** = 大接口 + 小实现（避免）：

```
┌─────────────────────────────────┐
│       Large Interface           │  ← 方法多、参数复杂
├─────────────────────────────────┤
│  Thin Implementation            │  ← 只是透传
└─────────────────────────────────┘
```

设计接口时自问：能否减少方法数？能否简化参数？能否把更多复杂度藏进去？

## 核心原则

- **Depth 是接口的属性，不是实现的属性。** 深模块内部可以由小而可 mock、可替换的部件组成，它们只是不属于接口。模块可以有**内部 seam**（实现私有、供自身测试用）以及接口处的**外部 seam**。
- **删除测试（deletion test）。** 假想删掉这个模块：若复杂度随之消失，它只是透传；若复杂度在 N 个调用方处重新出现，它在挣自己的位置。
- **接口即测试面。** 调用方与测试跨过同一个 seam。若你想「绕过」接口去测试，多半是模块形状错了。
- **一个 adapter 只是假设的 seam，两个 adapter 才是真实的 seam。** 没有东西真正在 seam 上变化时，别引入 seam。

## 按依赖分类加深

评估加深候选时，先给它的依赖分类，分类决定如何在 seam 上测试加深后的模块。

| 类别 | 特征 | 策略 |
|------|------|------|
| **1. In-process** | 纯计算、内存状态、无 I/O | 总能加深：合并模块，直接穿过新接口测试，无需 adapter |
| **2. Local-substitutable** | 有本地测试替身（PGLite 代替 Postgres、内存文件系统） | 替身存在即可加深；seam 属于内部，外部接口不需要 port |
| **3. Remote but owned** | 跨网络的自家服务（微服务、内部 API） | 在 seam 定义 **port**；逻辑归深模块，传输作为 **adapter** 注入。生产用 HTTP/gRPC/queue adapter，测试用内存 adapter |
| **4. True external** | 不掌控的第三方（Stripe、Twilio…） | 深模块把外部依赖作为注入的 port，测试提供 mock adapter |

推荐措辞示例：*「在 seam 定义 port，生产实现 HTTP adapter，测试实现内存 adapter；即使跨网络部署，逻辑仍留在同一个深模块里。」*

**seam 纪律：** 除非至少两个 adapter（通常是生产 + 测试）成立，否则不要引入 port；单 adapter 的 seam 只是间接层。不要因为测试用到就把内部 seam 暴露进接口。

## 为可测试性设计

好接口让测试自然发生：

1. **接受依赖，而不是创建依赖。**

   ```typescript
   // 可测
   function processOrder(order, paymentGateway) {}
   // 难测
   function processOrder(order) {
     const gateway = new StripeGateway();
   }
   ```

2. **返回结果，而不是产生副作用。**

   ```typescript
   // 可测
   function calculateDiscount(cart): Discount {}
   // 难测
   function applyDiscount(cart): void { cart.total -= discount; }
   ```

3. **小表面积。** 方法越少，需要的测试越少；参数越少，测试搭建越简单。

## 测试策略：替换，而非叠加

- 深模块接口处的测试一旦存在，针对浅模块的旧单测就成了废料，**删除**。
- 在深模块接口处写新测试。**接口即测试面。**
- 断言接口的可观测结果，而不是内部状态。
- 测试应能在内部重构后存活（描述行为而非实现）。实现一变测试就得改，说明它在测接口之外的东西。

## Design It Twice（探索替代接口）

用户想为某个候选探索不同接口时，用并行子代理模式（源自 Ousterhout：第一直觉多半不是最优）：

1. **先框定问题空间**：约束、依赖分类、一个用于把约束具体化的代码草稿（不是方案），展示给用户后立即进入下一步。
2. **并行 spawn 3+ 子代理**，每个产出**截然不同**的接口，配独立技术简报（文件路径、耦合细节、依赖类别、seam 背后是什么）：
   - Agent 1：最小化接口，1–3 个入口，最大化每入口杠杆。
   - Agent 2：最大化灵活性，支持多用例与扩展。
   - Agent 3：为最常见的调用方优化，让默认路径变得平凡。
   - Agent 4（可选）：围绕 ports & adapters 设计跨 seam 依赖。
3. **呈现并对比**：按 **depth**、**locality**、**seam 位置**对比，给出自己的推荐（可提混合方案），要**有观点**，不要只给菜单。

每个子代理输出：接口（类型/方法/参数 + 不变量、顺序、错误模式）、用法示例、seam 背后隐藏了什么、依赖策略与 adapter、权衡。

## 被拒绝的表述

- **Depth = 实现行数 / 接口行数**（Ousterhout）：会奖励把实现灌水。我们采用 depth-as-leverage。
- **「Interface」指 TS 的 `interface` 关键字或类的 public 方法**：过窄；此处包含调用方必须知道的每一条事实。
- **「Boundary」**：与 DDD 的限界上下文冲突。请说 **seam** 或 **interface**。

## 与其他 skill 的关系

- 用 `improve-codebase-architecture` 在真实代码库中扫描并呈报加深机会。
- 用 `test-driven-development` 在加深后的接口上先写失败测试；替换旧单测时尤其如此。
- 术语（CONTEXT.md）由 `domain-modeling` 维护；接口用领域词命名，seam 才站得住。
- 调试时若发现「测试很难写」往往暴露了浅模块，见 `systematic-debugging`。
- 重构落地按 `safe-refactor` 的边界纪律执行。
