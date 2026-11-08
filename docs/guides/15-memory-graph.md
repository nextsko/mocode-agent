# 图谱记忆：人格、用户画像与可追溯推理

上下文记忆不是把聊天记录塞进窗口，而是维护一张**可解释、可演化**的图。

## 为什么是图

- 事实/偏好/决策天然是关系：`用户 -[prefers]-> 结构化归档`；
- 每条信念带 **provenance**（session/message/task/url + 引用 + 时间），可回答“你为什么这样认为”；
- 冲突显式存在（同主体同关系指向不同客体），用 `supersedes` 演化而不是删历史；
- 置信度 + 时间有效性（valid_from/valid_to）支持“以前喜欢、现在不喜欢”；
- 可导出 Mermaid/HTML 做可视化，也能被外部系统消费。

## 模型

节点：`user / persona / preference / fact / goal / decision / task / skill / project / event / artifact`
边：`prefers / knows / decided / produced / derived_from / supersedes / contradicts / relates_to / part_of / evidenced_by`

```text
.nextsko(user) --prefers--> dark-mode(preference) --evidenced_by--> event("I prefer dark mode")
.nextsko(user) --knows--> ship-mocode(goal)
.mocode(persona) --relates_to--> nextsko(user)
```

## 工具

```text
memgraph action=remember kind=user name=nextsko props={"tone":"direct"}
memgraph action=remember kind=preference name="结构化归档" source_kind=message source_ref=m42 source_quote="记忆要以文档结构化存储"
memgraph action=relate from=<user-id> to=<pref-id> edge_kind=prefers
memgraph action=explain id=<pref-id>       # 证据链
memgraph action=persona                    # 人格 + 用户画像卡
memgraph action=conflicts                  # 待澄清矛盾
memgraph action=decay half_life_days=30    # 陈旧信念衰减
memgraph action=export                     # graph.json / graph.mmd / graph.html
```

## 存储与追溯

```text
.mocode/memory/
  graph.json      # 当前快照
  events.jsonl    # 追加式变更日志（upsert/forget/supersede/decay）
  exports/<ts>/   # 可视化导出
```

`events.jsonl` 是事件溯源的基础：任何时候都能回放“记忆是怎么长出来的”。
合并规则：重复观测会合并 props、累加证据、平滑置信度，而不是覆盖。

## 与飞轮的关系

- `flywheel` 记录**过程**（任务→最优路径→技能）；
- `memgraph` 记录**人与信念**（人格、画像、偏好、决策及证据）；
- 两者都以 Markdown/JSON 落盘、可组合、可导出：过程沉淀为技能，信念沉淀为画像，
  共同构成下一次推理的上下文底座。

## 隐私

`forget` 会删除节点与所有关联边；`events.jsonl` 保留操作痕迹（不含被删内容），
便于审计“何时被要求忘记”，但不再承载原始证据。

## TUI 命令

- `/persona`：人格 + 用户画像卡；
- `/conflicts`：记忆冲突及 supersedes 处理建议。
