---
name: verify-and-stop
description: >-
  Use when 只做验证/验收——证明既有工作满足 acceptance conditions 而不扩大 scope：
  验证型任务、完成度检查、聚焦 gate 运行、last-mile proof。核心纪律是把 acceptance
  翻译成最小充分证明集，精确区分 pass/fail/unavailable/blocked，证明完成即停。
---

# Verify and Stop（只验证，即停止）

## 概述

把 **acceptance conditions 翻译成最小充分证明集（smallest sufficient proof set）**，运行它，然后停下。

核心一句话：**只证明，不扩张；证明完成即停。**

## 何时使用

- 验证型任务（validation-only）：只确认现状是否满足验收
- 完成度检查（completion checks）：声明 done 前的最后把关
- 聚焦 gate 运行（focused gate runs）：跑指定测试 / 构建 / lint
- last-mile proof：主体工作已完成，只差证明

**不适用**：需要修 bug（见 `surgical-patch`）；需要实现新行为（见 `lean-build`）。

## 铁律

```
不做 acceptance 之外的任何编辑
不把「没验证」当成「已验证」
证明完成即停
```

- **不改产品代码**，除非验证请求明确包含修复（included fixes）。此时也按最小修复处理，并遵守 `surgical-patch`。
- **不在 criteria 通过后**追加打磨、清理或无关测试。

## 工作流程

### 1. 把 acceptance 翻译成最小充分证明集

- 逐条列出 acceptance condition，各自映射到**最小**能证明它的命令 / 检查。
- 证明集要**充分**（覆盖全部 acceptance）且**最小**（不做无关的额外验证）。
- 明确每条证明的**预期输出**：什么算 pass、什么算 fail。

### 2. 复用仍然有效的证据

- 若既有验证结果是在**匹配的 repository state** 下产生的，且此后代码未变，可以复用，避免重复运行。
- 一旦代码 / 依赖 / 配置发生变化，旧证据失效，必须重跑。
- 不要复用「相似但不匹配」的结果来充当证明。

### 3. 先跑聚焦检查，再跑更宽的 gate

- 顺序：先 focused checks（直接对应 acceptance），再 wider gates（全量测试 / build）。
- 优先用能最快证伪的检查暴露问题，避免在大 gate 上浪费周期。
- 记录实际运行的命令与原始输出。

### 4. 精确区分四种状态

| 状态 | 含义 | 报告方式 |
|------|------|----------|
| **pass** | 命令成功，输出支持 acceptance | 附命令与结果 |
| **fail** | 命令失败或输出不支持 acceptance | 附失败输出与定位 |
| **unavailable** | 无法运行（环境/依赖/权限缺失） | 明确说不可用，不给结论 |
| **blocked** | 依赖前置条件未满足（如等待他人改动） | 说明阻塞点 |

**不要**把 unavailable / blocked 混成 pass 或 fail；不要用「应该可以」代替结论。

### 5. 证明完成即停

- acceptance proof 完成的那一刻**立即停止**。
- 不再编辑产品代码（除非请求含修复），不追加打磨 / 清理 / 无关测试。
- 报告只包含：**运行的命令、实际结果、未解决风险（unresolved risk）**。

## Checklist

- [ ] acceptance 逐条映射到最小充分证明集，且写明 pass/fail 判据
- [ ] 复用的证据确实基于当前 repository state
- [ ] 先跑聚焦检查，再跑更宽 gate
- [ ] 每条结论精确标注 pass / fail / unavailable / blocked
- [ ] 未在验证中修改产品代码（除请求明确含修复）
- [ ] criteria 通过后未追加打磨 / 清理 / 无关测试
- [ ] 报告包含命令、结果与 unresolved risk，无成功暗示

## 常见合理化

| 合理化 | 现实 |
|--------|------|
| 「顺手把这个小问题也改了」 | 验证任务不等于修复任务；改代码即超出 scope。 |
| 「上次跑过了，不用再跑」 | 只有 repository state 匹配时才可复用；变了就失效。 |
| 「跑不过但环境问题，算通过吧」 | 环境问题应报 unavailable / blocked，不是 pass。 |
| 「反正测试都过了，再加个清理」 | criteria 通过即停，追加动作违反 scope。 |
| 「看起来没问题」 | 需要命令与原始输出作为证据，见 `verification-before-completion`。 |

红旗：验证中擅自改 code；把 unavailable / blocked 报告成 pass；复用过期结果；通过后继续扩张；结论无命令输出支撑。

## 相关技能

- `verification-before-completion`：本技能是其「last-mile proof」的聚焦形态，共享「先证据后声明」铁律。
- `surgical-patch`：当验证请求确实包含 fix 时，用其最窄层修复纪律。
- `test-driven-development`：需要新增证明用的测试时，遵循 red-green。
- `code-review-and-quality`：验收证明属于评审的 Verification 轴。
