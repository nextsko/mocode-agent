# internal-restructure — internal 目录分层重构

> 状态：R1–R3 已完成（2026-09-13），R4 蓝图待续
> 创建：2026-09-13
> 范围：`internal/` 全域，首批聚焦死包清除 + `core/agent` 域目录化 + 上帝文件拆解
> 参考：grok-build（一事一包，crates 制）、pi-mono（形态分层：core 领域 / modes 交互 / extensions 扩展 / utils 横切）

## 现状诊断（2026-09-13 实测）

1. **假目录**：`core`、`domain`、`integration`、`transport`、`util` 五个根目录仅挂 4–6 行 `doc.go`，零代码
2. **死包**：`tools/external/connector`（45 行 port.go，全仓无引用）
3. **agent 域平铺**：20 个 .go 文件糊在 `core/agent/` 根，职责横跨编排/生命周期/工具/上下文工程/横切
4. **上帝文件**：`ui/model/ui.go` 1589、`transport/cmd/doctor.go` 1150、`agent/coordinator.go` 1101、`agent/agent_lifecycle.go` 909（规范阈值 350）

## 分层原则（吸收两家理念，Go 化）

- **目录即架构**：一个目录一个职责域，包名=域名；域内文件按职责命名
- **依赖单向**：coordinator（编排）→ lifecycle/tools/context（领域）→ notify/failover/toolutil（横切）；拆包先验证无环
- **阈值**：非生成文件 >350 行必拆；平铺文件归位到职责目录

## 批次

### R1 死包与假目录清除
删 5 个 doc.go 空壳根目录 + `connector/`（45 行无引用）。

### R2 agent 域目录化（无循环风险的先迁）
| 现位置 | 去向 | 说明 |
|---|---|---|
| `prompts.go` + `agent_prompt.go` | `agent/prompt/`（已有包，合并） | 系统提示词域 |
| `context_compress.go` | `agent/ctxcompress/`（已有包，合并） | 上下文压缩域 |
| `session_summary_queue.go`（类型）| `agent/summary/` | `drainQueuedSummaries`（coordinator 方法）留在原处 |
| `background_notify.go` | `agent/jobs/` | 后台任务通知域 |
| `loop_detection.go` | `agent/loopdetect/` | 纯函数 |
| `agent_convert.go` + `agent_helpers.go` | `agent/messages/` | 消息转换与过滤域 |

### R3 上帝文件拆解（同包文件级，先立秩序）
- `coordinator.go`（1101）→ `coordinator.go`（结构+构造+Run）+ `coordinator_tools.go`（buildTools/buildAgentModels/withFailover）+ `coordinator_sessionlog.go`（sessionLogSink 族）+ `coordinator_skills.go`（discover/logTurn/logDiscovery）+ `coordinator_providers.go`（已有，吸收 getProviderOptions/mergeCallOptions）
- `agent_lifecycle.go`（909）→ 按函数族拆 `agent_lifecycle_run.go` / `agent_lifecycle_notify.go`（拆时按实际函数定界）

### R4 蓝图（后续轮次）
1. `agent/` 剩余平铺（agent.go/agent_tool.go/agentic_fetch_tool.go/callbacks.go/event.go/errors.go）进一步子包化：`lifecycle/` 与 `coordinator/` 完全分包需先立接口（当前互访私有字段，需回调注入重构）
2. `tools/external` 重组（plugins/sshcommon+netcommon 混置）
3. `ui/model` 拆包：ui.go(1589) 上帝文件按 chat/editor/layout/dialog 路由拆
4. `transport/cmd/doctor.go`(1150) 按检查项拆
5. `store/`、`domain/theme` 归位审查

## 实施记录（2026-09-13，R1–R3 + R4 首批完成）

| 批 | 结果 |
|----|------|
| R1 | 删 5 个 doc.go 空壳根目录 + `tools/external/connector`（45 行死包） |
| R2 | `core/agent` 域目录化：`loopdetect/`、`jobs/`、`summary/`、`prompt/`（吸收 prompts.go+3 模板）、`messages/`；删 420 行死代码 `context_compress.go` |
| R3 | `coordinator.go` 1101→459+482+26+167；`agent_lifecycle.go` 996→776+59+58+27；方法论：顶层声明切块 + goimports |
| R4a | `doctor.go` 1150→163+352（checks）+359（providers）+308（helpers）；`ui.go` 1589→956+358（messages）+230（view）+69（question） |
| R4b | `runTurn` 475 行手术：回调闭包（PrepareStep/OnReasoning*/OnText/OnTool*/OnStepFinish/StopWhen）提取为 `agent_turn_callbacks.go`（297 行，turnState 状态载体），runTurn 缩为编排骨架（agent_lifecycle 776→564）；`Update` 473 行巨型路由按消息族提取 `ui_update_handlers.go`（295 行：service 事件/mouse/anim 三族），ui.go 956→718 |
| R4c | **background.go 域拆分**：一次 `git checkout --` 误操作把未提交的增强打回原版；凭会话记录按四域完整重建并原计划拆分——`background_buffer.go` 142（head-tail+overflow）/`background_job.go` 369（类型+方法）/`background_manager.go` 322（单例+Start/Kill/持久化/epoch）/`background_notify.go` 102（通知/promote/sanitize/开关）。此前全部增强测试一次通过，复原完整性由测试证明。**教训入库：会话内改动应及时分批 commit，任何 checkout 前先 `git status` 确认** |
| R4d | **四主题分批 commit**（bg-jobs/subagent-ui/ask-user/restructure，工作入版本保护）；上帝文件批次：`ui_dialogs` 892→231+471（actions）+206（openers）、`chat` 872→500+205（scroll）+180（msgs）、`app` 853→658+207（store services）。quickstyle 三次脚本尝试后判定为**声明式样式表**（上游 crush 同为 1051 行单文件），拆分收益低于风险，维持同构不拆 |
| R4e | `plugins/*common` → `common/{gitea,gitops,net,search,ssh}`（共享库命名正名，21 处引用重写）；`wechat/bot` 746→405+353（media 域）、`diffview` 726→643+85（builders 域）、`question_form` 719→383+345（draw 域）；`handleDialogAction` 471 判定为宽浅路由表（40+ case 平均 10 行）记档不拆 |
| R4f | **agent 根分包落地**：sessionAgent 全家（14 文件：session/run/queue/control/model/convert/prompt/turn_callbacks/callbacks/event/errors + 5 测试）迁 `core/agent/lifecycle/`；根包保留 API 面——type alias + `NewSessionAgent`/`Err*` 转发，**全部调用方零改动**；agent 根现只剩编排域（coordinator×7）与工具域（agent_tool/agentic_fetch/agent_tool_context），平铺 22→12 文件 |

验证：全仓 build/vet/test 绿。R4b 教训：switch 内提取必须保留 case 标签转发（裸 if 插入会静默丢失事件路由，测试当场抓出）。

验证：全仓 build/vet/test 绿。工具沉淀：`scripts/gosplit.ps1`（切块函数，需同进程 dot-source）。

## R4 剩余蓝图（按序）
1. ~~agent 根完全分包~~（R4f 完成 lifecycle 域；coordinator 域可同法后续分包——alias 面已验证）
2. 残余 >350（收益递减区，按需）：quickstyle 964（已判定不拆）、ui.go 718、config.go 691、permissions.go 690、admin/server 677、wechat/channel 677、question_choice_base 668、app.go 658、diffview 643

## 相关
- [[../bg-jobs-fix/README]]（background_notify 的来历）
- [[../ask-user/README]]
