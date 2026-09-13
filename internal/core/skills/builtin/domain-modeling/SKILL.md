---
name: domain-modeling
description: >-
  Use when discussing a codebase's terminology, writing or editing a
  CONTEXT.md glossary, or recording and editing an ADR. Also use when code and
  stated domain rules disagree and the model needs to be clarified, sharpened,
  or written down.
---

# Domain Modeling（领域建模）

在设计过程中**主动**构建并打磨项目的领域模型：挑战术语、发明极端场景、在术语和决策结晶的当下就写下来。仅仅「读一下 `CONTEXT.md` 拿词汇」不是本 skill——那是任何 skill 都能做的一行习惯；本 skill 用于**改变模型**，而非仅消费模型。

## 文件结构

多数仓库只有一个上下文：

```
/
├── CONTEXT.md
├── docs/
│   └── adr/
│       ├── 0001-event-sourced-orders.md
│       └── 0002-postgres-for-write-model.md
└── src/
```

若根目录存在 `CONTEXT-MAP.md`，说明有多个上下文，map 指向各自位置：

```
/
├── CONTEXT-MAP.md
├── docs/
│   └── adr/                          ← 系统级决策
├── src/
│   ├── ordering/
│   │   ├── CONTEXT.md
│   │   └── docs/adr/                 ← 上下文专属决策
│   └── billing/
│       ├── CONTEXT.md
│       └── docs/adr/
```

**惰性创建文件：** 有东西可写时才创建。没有 `CONTEXT.md`，就在第一个术语确定时创建；没有 `docs/adr/`，就在第一条 ADR 需要时创建。目录与归档纪律参见 `docs-rulebook`。

## 会话中要做的事

- **对照术语表挑战用词。** 用户说的词与 `CONTEXT.md` 冲突，立即指出：「你的术语表把 cancellation 定义为 X，但你现在像是 Y。到底是哪个？」
- **锐化模糊语言。** 出现含糊或过载的词，提出精确的规范术语：「你说的 account，是指 Customer 还是 User？这是两回事。」
- **讨论具体场景。** 关系被讨论时，用具体场景压力测试，逼出概念边界：「那如果订单在支付前被部分取消会怎样？」
- **与代码交叉核对。** 用户描述某处如何工作时，检查代码是否同意。有矛盾就摊开：「你的代码会取消整个 Order，但你刚说支持部分取消。哪个对？」
- **内联更新 `CONTEXT.md`。** 术语一确定就当场写进去，不要攒批。格式见下。
- **谨慎提供 ADR。** 仅当下面三条**全部**成立才提议（见 ADR 判定）。

`CONTEXT.md` 必须**完全不含实现细节**。它不是规格、不是草稿本、不是实现决策的仓库——它只是术语表。

## CONTEXT.md 格式

```md
# {Context Name}

{一两句话说明这是什么上下文、为何存在。}

## Language

**Order**:
{A one or two sentence description of the term}
_Avoid_: Purchase, transaction

**Invoice**:
A request for payment sent to a customer after delivery.
_Avoid_: Bill, payment request

**Customer**:
A person or organization that places orders.
_Avoid_: Client, buyer, account
```

规则：

- **有观点。** 同一概念有多个词时，选最好的一个，其余列在 `_Avoid_` 下。
- **定义要紧凑。** 最多一两句，定义它**是什么**，不是它**做什么**。
- **只收本项目上下文特有的术语。** 通用编程概念（timeout、error type、utility pattern）即使项目大量使用也不该进，先问：这是本上下文独有的概念，还是通用编程概念？
- **成簇时用子标题分组。** 若所有术语同属一个内聚领域，平铺列表即可。

### 单上下文 vs 多上下文

- **单上下文（多数仓库）：** 根目录一个 `CONTEXT.md`。
- **多上下文：** 根目录 `CONTEXT-MAP.md` 列出各上下文、位置与关系：

```md
# Context Map

## Contexts

- [Ordering](./src/ordering/CONTEXT.md): receives and tracks customer orders
- [Billing](./src/billing/CONTEXT.md): generates invoices and processes payments
- [Fulfillment](./src/fulfillment/CONTEXT.md): manages warehouse picking and shipping

## Relationships

- **Ordering → Fulfillment**: Ordering emits `OrderPlaced` events; Fulfillment consumes them to start picking
- **Ordering ↔ Billing**: Shared types for `CustomerId` and `Money`
```

推断顺序：有 `CONTEXT-MAP.md` → 读它找上下文；只有根 `CONTEXT.md` → 单上下文；都没有 → 在第一个术语确定时惰性创建根 `CONTEXT.md`。多上下文时推断当前话题属于哪个，不清楚就问。

## ADR 格式

ADR 放在 `docs/adr/`，顺序编号：`0001-slug.md`、`0002-slug.md`……先扫 `docs/adr/` 找最大编号再加一。

模板（就这些，一段话也行）：

```md
# {Short title of the decision}

{1-3 sentences: what's the context, what did we decide, and why.}
```

可选小节**仅在确有价值时**才加，多数 ADR 用不上：

- **Status** frontmatter（`proposed | accepted | deprecated | superseded by ADR-NNNN`）：决策会被重新审视时有用
- **Considered Options**：被否的方案值得记住时
- **Consequences**：有非显而易见的下游影响需要点明时

价值在于记录**做过某决策**以及**为什么**，而非填满小节。

## 何时提供 ADR

三条**必须同时成立**，缺一即跳过：

1. **难以逆转**：以后改主意的成本可观。
2. **无上下文会令人意外**：未来的读者会想「他们究竟为什么这么做？」
3. **是真实权衡的结果**：存在真正的备选，你为具体理由选了其一。

容易逆转就跳过（你迟早会逆转它）；不意外就没人会问；没有真正备选，就只剩「我们做了显而易见的事」。

**够格的例子：**

- **架构形态**：「用 monorepo。」「写模型是事件溯源的，读模型投影进 Postgres。」
- **上下文间的集成方式**：「Ordering 与 Billing 通过领域事件通信，而非同步 HTTP。」
- **带锁定成本的技术选型**：数据库、消息总线、认证提供方、部署目标——不是每个库，而是换掉要花一个季度的那些。
- **边界与范围决策**：「客户数据归 Customer 上下文所有，其他上下文只用 ID 引用。」明确的「不」和「是」同样有价值。
- **对显而易见路径的有意偏离**：「因为 X 用手写 SQL 而非 ORM。」凡是合理读者会假设相反的地方，能阻止下一个人「修好」本是有意为之的东西。
- **代码里看不见的约束**：「因合规要求不能用 AWS。」「因合作方 API 合约，响应必须 < 200ms。」
- **被否方案（当否决理由不显然时）**：若为微妙原因否掉 GraphQL 选了 REST，就记下来，否则半年后有人会再提一次。

## 反模式

- 把 `CONTEXT.md` 写成规格或实现笔记 → 术语表被稀释，没人再读。
- 攒批更新术语 → 会话结束就忘了，模型失真。
- 每个决策都提 ADR → 噪音淹没真正重要的记录。
- 术语表里塞 `Config`、`Handler`、`Repository` 这类通用词。

## 与其他 skill 的关系

- 目录生命周期与计划文档规范见 `docs-rulebook`。
- 架构词汇（module / interface / seam / adapter）见 `codebase-design`；命名深模块时优先复用 `CONTEXT.md` 的领域词。
- 扫描并呈报架构改进时，`improve-codebase-architecture` 会读取 `CONTEXT.md` 与 ADR，并把新术语/新决策写回。
- 术语与代码矛盾时，先用 `systematic-debugging` 的方法确认事实，再改模型。
