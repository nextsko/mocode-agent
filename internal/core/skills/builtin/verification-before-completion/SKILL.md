---
name: verification-before-completion
description: >-
  Use when 准备声明工作已完成、已修复或已通过测试，或在 commit、创建 PR、结束任务之前。
  它要求先运行验证命令并确认输出，再做出任何成功声明——evidence before assertions always。
  适用于任何形式的完成/成功表述、满意表达、对工作状态的正面陈述，以及委托给 agent 后的独立核验。
---

# 完成前验证（Verification Before Completion）

## 概述

未经验证就宣称工作完成，是欺骗而非效率。

**核心原则：先证据，后声明（Evidence before claims, always）。**

**违反规则的字面，就是违反规则的精神。**

## 铁律

```
NO COMPLETION CLAIMS WITHOUT FRESH VERIFICATION EVIDENCE
```

如果本条消息里没有跑过验证命令，你就不能声称它通过。

## 门禁函数（The Gate Function）

```
在声称任何状态或表达满意之前：

1. IDENTIFY：什么命令能证明这个声明？
2. RUN：执行完整命令（新鲜、完整）
3. READ：读完输出，检查 exit code，数失败数
4. VERIFY：输出是否支持该声明？
   - 否：陈述实际状态，附证据
   - 是：带证据陈述声明
5. ONLY THEN：才做声明

跳过任一步 = 撒谎，不是验证
```

## 常见失败对照

| 声明 | 需要的证据 | 不充分的依据 |
|------|-----------|--------------|
| Tests pass | 测试命令输出：0 failures | 上一次运行、"应该通过" |
| Linter clean | linter 输出：0 errors | 部分检查、外推 |
| Build succeeds | build 命令：exit 0 | linter 通过、日志好看 |
| Bug fixed | 复现原症状的测试通过 | 改了代码、假设已修 |
| Regression test works | 已验证 red-green 循环 | 测试只通过一次 |
| Agent completed | VCS diff 显示改动 | agent 报告 "success" |
| Requirements met | 逐条对照 checklist | 测试通过 |

## 红旗 — STOP

- 使用 "should"、"probably"、"seems to"
- 验证前表达满意（"Great!"、"Perfect!"、"Done!"）
- 准备在未验证时 commit / push / PR
- 信任 agent 的成功报告
- 依赖部分验证
- 想"就这一次"
- 累了想赶紧收工
- **任何暗示成功却未运行验证的措辞**

## 反合理化

| 借口 | 现实 |
|------|------|
| "现在应该能用了" | RUN 验证命令 |
| "我有信心" | 信心 ≠ 证据 |
| "就这一次" | 没有例外 |
| "linter 过了" | linter ≠ compiler |
| "agent 说成功了" | 独立核验 |
| "我累了" | 疲惫不是借口 |
| "部分检查够了" | 部分证明不了任何事 |
| "换种说法规则就不适用" | 精神高于字面 |

## 关键模式

**Tests：**
```
✅ [运行测试命令] [看到：34/34 pass] "All tests pass"
❌ "Should pass now" / "Looks correct"
```

**Regression tests（TDD Red-Green）：**
```
✅ 写 → 跑（pass）→ 回退修复 → 跑（MUST FAIL）→ 恢复 → 跑（pass）
❌ "我写了回归测试"（未经 red-green 验证）
```

**Build：**
```
✅ [运行 build] [看到：exit 0] "Build passes"
❌ "Linter passed"（linter 不检查编译）
```

**Requirements：**
```
✅ 重读 plan → 建 checklist → 逐条验证 → 报告缺口或完成
❌ "测试通过，阶段完成"
```

**Agent delegation：**
```
✅ agent 报告成功 → 查 VCS diff → 验证改动 → 报告真实状态
❌ 直接信任 agent 报告
```

## 为什么要紧

来自 24 条 failure memory：

- 人类搭档说 "I don't believe you" —— 信任被破坏
- 未定义的函数被发布 —— 会崩溃
- 缺失的需求被发布 —— 功能不完整
- 假完成导致返工、重定向、时间浪费
- 违反："Honesty is a core value. If you lie, you'll be replaced."

## 何时适用

**ALWAYS，在以下动作之前：**

- 任何形式的成功/完成声明
- 任何满意表达
- 任何关于工作状态的正面陈述
- commit、创建 PR、任务完成
- 进入下一个任务
- 委托给 agent

**规则覆盖：** 精确措辞、同义改写、成功暗示、任何暗示完成/正确的沟通。

## 底线

**验证没有捷径。**

运行命令。读完输出。**然后**才声明结果。

这不可协商。

## 相关技能

- `systematic-debugging`：先定位根因，再用本技能验证修复。
- `test-driven-development`：提供 red-green 循环作为回归测试的验证依据。
- `receiving-code-review`：评审反馈的处理同样以验证为先，而非盲从。
