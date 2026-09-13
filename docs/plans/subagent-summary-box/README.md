# subagent-summary-box — 子代理摘要盒遮挡输入框与「完成后自动隐藏」

> 状态：已修复（2026-09-14）
> 范围：`internal/ui/model/`（`ui_layout.go` / `ui_editor.go` / `chat_msgs.go`）、
> `internal/ui/chat/tools.go`、`internal/ui/components/subagent_summary.go`

## 现象

召唤子代理后**主输入框被遮住一部分**；且子代理**结束后界面仍显示 `N running`**，
看起来一直「卡」在那个状态。

## 根因（两个独立 bug）

### 1. 布局高度未计入摘要盒 → 输入行被裁

| 环节 | 事实 |
|------|------|
| 预留 | `generateLayout`：`editorHeight = textarea.H + editorHeightMargin(3)`，3 行仅覆盖「分隔线1 + 附件行1 + 底边距1」 |
| 渲染 | `renderEditorView` 有盒子时输出 `盒子3 + 分隔1 + 输入H + 底边距1` |
| 结果 | 实际比预留多 2–3 行 → 编辑器内容溢出矩形 → **底部输入行被裁** |

### 2. `SubagentCounts` 用了永不更新的原始状态

- 渲染侧用 `computeStatus()`（由工具结果推导 `Success/Error`），所以聊天项能正确显示 ✓。
- 但 `SubagentCounts` 用的是 `Status()`，它只返回字段 `t.status`，而该字段**只被设为
  `Running`/`AwaitingPermission`，结果到达时从不更新为 `Success`**。
- 于是 Agent 项**恒为 running** → 盒子永不消失 → 与现象完全吻合。

## 修复

| 提交 | 改动 |
|------|------|
| `🐛 fix(ui)` | `components.SummaryHeight=3` + `UI.subagentSummaryHeight()`；`generateLayout` 把盒子高度并入 `editorHeight`（盒子出现时聊天区相应收缩） |
| `✨ feat(ui)` | 新增 `baseToolMessageItem.EffectiveStatus()`（=`computeStatus()`），`SubagentCounts` 改用它；`renderEditorView`/`subagentSummaryHeight` 仅在 `running>0` 时渲染并预留 → **全部完成后盒子自动隐藏** |

## 验证

- `TestEditorReservesSubagentSummaryHeight`：运行中 → 编辑器 +3、聊天区 −3。
- `TestEditorHidesSummaryWhenSubagentsFinish`：完成后 → 编辑器恢复基线（此前**失败**，正好复现根因）。
- `TestEffectiveStatusDerivesFromResult`：锁定 `EffectiveStatus()` 与原始 `Status()` 的差异。
- `go vet` / `gofumpt` / golangci-lint（CI 等价，0 新增告警）通过。

## 经验沉淀

- 布局高度必须**逐行对齐** `renderEditorView` 的实际输出（见 `internal/ui/AGENTS.md`
  的 Common Gotchas）。
- 判断工具是否完成，用 `EffectiveStatus()`，不要用原始 `Status()`。
