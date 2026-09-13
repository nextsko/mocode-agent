---
name: writing-skills
description: >-
  Use when creating a new skill, editing an existing skill, or verifying that a
  skill actually changes agent behavior before deploying it — including writing
  SKILL.md front matter, designing triggering descriptions, and pressure-testing
  guidance against rationalization.
---

# Writing Skills：像 TDD 一样写 skill

写 skill 就是 **把 TDD 应用到流程文档**：先看没有 skill 时 agent 怎么失败（RED），再写最小 skill 让它通过（GREEN），最后堵住新冒出的合理化借口（REFACTOR）。

**铁律：没有失败的测试，不写 skill。** 新增和修改都适用。先写 skill 再测？删掉，重来。

配套阅读：用 `using-agent-skills` 了解 skill 如何被发现与调用；skill 定稿后，用 `writing-plans` 把落地步骤排进计划。

## 何时创建

创建：技巧对你并不显然、以后会跨项目复用、模式足够通用、别人也受益。

不创建：一次性方案、已有充分文档的标准做法、项目专属约定（写进 AGENTS.md 等指令文件）、纯机械约束（能用校验/脚本强制的就别写文档）。

## RED-GREEN-REFACTOR

| TDD 概念 | skill 创建 |
|---|---|
| 测试用例 | 带压力的子代理场景 |
| 生产代码 | `SKILL.md` |
| RED | 无 skill 时 agent 违规（基线行为） |
| GREEN | 有 skill 时 agent 遵守 |
| REFACTOR | 补漏洞、保持遵守 |

- **RED**：不含 skill 跑压力场景，逐字记录它做了什么选择、用了哪些借口。
- **GREEN**：只针对这些借口写最小 skill，再用同一场景验证。
- **REFACTOR**：出现新借口就加显式反制，重测到无懈可击。

## SKILL.md 结构

```
<name>/
  SKILL.md          # 必需
  references/       # 仅当有 100+ 行重型参考
```

Front matter（YAML，总长 ≤1024 字符）：

```yaml
---
name: verb-first-name      # 仅字母、数字、连字符
description: >-
  Use when <触发条件与症状>。
---
```

### Skill Discovery Optimization（SDO）

- `description` **只写"何时用"，绝不总结流程**。一旦描述里写了流程，agent 会照描述抄近路而不读正文。
- 用第三人称，覆盖症状、错误信息、同义词，便于被检索到。
- 命名动词优先：`condition-based-waiting` 优于 `async-test-helpers`。
- 令牌效率：常加载的 skill 尽量短；细节外置到 `references/` 或 `--help`，用交叉引用代替重复。

## 匹配失败类型（Match the Form to the Failure）

先判断基线失败属于哪一类，再选形式；形式选错会起反效果。

| 基线失败 | 正确形式 | 错误形式 |
|---|---|---|
| 知道规则却在压力下违反 | 禁令 + 借口表 + red flags | 软性建议（"prefer/consider"） |
| 遵守但输出形状不对 | 正向配方/契约（说明产物由哪些部分组成） | 禁令清单（"don't/never"） |
| 产物漏掉必需元素 | 模板里加 REQUIRED 字段/槽位 | 正文里的散文提醒 |
| 行为应随条件变化 | 绑定到可观察谓词的条件规则 | 无条件规则 + 豁免子句 |

选好形式后：**不加 nuance 子句**（"除非重要否则别 X" 会重新打开讨价还价）；**豁免子句无法限定作用域**，需要豁免就重构结构。

## 对抗合理化（仅纪律类 skill）

1. **显式堵死每个变体**：不只说规则，还要禁止具体绕法。
   - 反例：`Write code before test? Delete it.`
   - 正例：`...Delete it. Start over.` + "No exceptions: 不留作参考 / 不边改边测 / 不看它 / 删就是删"。
2. **早早写下**："违反规则的字面就是违反规则的精神"，封杀"我在意精神"这类借口。
3. **借口表**：把基线测试里出现的每句借口写进 `| Excuse | Reality |` 表。
4. **Red Flags 清单**：列出"即将违规"的自我检查信号。
5. 把违规症状补进 `description` 触发词。

## 微测措辞（Micro-Test）

完整压力场景是最终关卡但很慢。先做廉价措辞微测：

1. 每次调用一个全新上下文样本（system prompt = 真实承载环境全文；user message = 诱发失败的任务）。
2. **必须有"不给指导"的对照组**；对照组不复现失败，就没有要修的问题。
3. 每个变体跑 5+ 次，单次样本会骗人。
4. 命中的样本全部人工阅读，模板回声和引用式反例会被误判为命中。
5. 关注方差：措辞生效时结果收敛到同一形状。

微测只验证措辞，不能替代纪律 skill 的压力场景。

## 检查清单

RED：

- [ ] 设计 3+ 叠加压力（时间、沉没成本、权威、疲惫）
- [ ] 无 skill 跑基线，逐字记录失败与借口

GREEN：

- [ ] 名称仅字母数字连字符；front matter 有 `name` / `description`
- [ ] `description` 以 "Use when" 开头、第三人称、含症状触发词、不总结流程
- [ ] 形式匹配失败类型；纪律类做措辞微测（对照 + 5 次 + 人工阅读）
- [ ] 只给一个高质量示例；代码内联或链接到独立文件
- [ ] 有 skill 复跑场景，确认遵守

REFACTOR：

- [ ] 收集新借口并加反制；建借口表与 red flags；重测到无懈可击

## 常见错误

- 叙事化（"某次会话我们如何解决…"）——无法复用。
- 多语言稀释（example-js/py/go 各一份）——质量差、维护重。
- 流程图里放代码；标签用 step1/helper2 等无语义名字。
- 批量创建多个 skill 而不逐个测试。

**底线**：创建 skill 就是为流程文档做 TDD。没跑失败测试，就不写。
