# 能力飞轮：归档 → 最优路径 → 技能进化 → 导出可视化

`flywheel` 工具把“做完一件事”变成“下一次更快”，是一条可积累的能力生产线。

## 设计来源

| 来源 | 吸收的点 |
|---|---|
| Voyager（2305.16291） | 持续增长的技能库 + 迭代提示 + 自验证 |
| Reflexion（2303.11366） | 用语言化反馈从试错中学习，而不是改权重 |
| MemGPT（2310.08560） | 分层记忆：热上下文 + 冷归档，按需调页 |
| A-MEM（2502.12110） | 记忆条目动态链接，而不是固定结构 |

## 工作流

```text
完成长任务
  → flywheel archive（type / goal / outcome / 实际步骤 / lessons，失败步骤如实标记）
  → flywheel distill（抽最简成功路径：去失败、去重试、去重复动作）
  → flywheel skill（引擎判定：create / update / upgrade / skip）
  → 下次同类任务：flywheel context（拉摘要）→ 复用 → flywheel use（记分）
  → flywheel report / export（飞轮报表 + Mermaid 图）
```

## 技能判定规则（由引擎执行，模型不手改版本）

- **create**：没有相似技能（同类型 + 名称/触发词相似度 ≥ 0.6）；
- **upgrade**：新路径更短（步数减少）；
- **update**：路径不变但有新步骤/经验可合并；
- **skip**：没有任何新增，避免版本通胀。

每次变更写入 `versions/vN.md`，`index.json` 记录 uses / successes / failures / history。

## 产物

```text
.mocode/flywheel/
  tasks/2026-11/<id>.json|md      # 归档：实际路径 + 最优路径 + 坑 + 经验
  skills/index.json               # 技能注册表（版本、复用、成功率）
  skills/<name>/SKILL.md          # 当前版本
  skills/<name>/versions/vN.md    # 历史版本
  exports/<ts>/report.md|json|graph.mmd
```

`graph.mmd` 是 Mermaid 图：任务 → 技能的复用关系，可直接渲染或嵌入网页。

## 上下文自调控

- `flywheel context` 返回紧凑摘要（任务数、技能排行、上一条最优路径），
  默认 600 字符预算，模型可自行决定是否拉取；
- 摘要/接口是给模型的“调页”入口：需要细节时再 `distill` 或读 `SKILL.md`，
  避免把整段历史塞进上下文。

## 可组合与导出

- 任务、技能、报表全部是 JSON/Markdown，可被 CI、知识库、Web 面板消费；
- `export` 产出自包含目录，便于归档、对比或可视化；
- 与 `ui_dissect` 的 `skill`/`evolve` 输出可以合并进同一技能目录，形成统一的
  能力库（UI 规则与通用流程共用一套版本/记分模型）。
