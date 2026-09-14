# 01 — Slash 补全弹窗 ANSI 残码根因分析

> 现象：输入 `/a` 后，内联 `/` 补全弹窗里每一行都出现形如 `[38;2;104;255;214m`、`;221m`、`2;223;219;221m` 的字面量乱码，且高亮行（`/admin`）正常，其余行全部损坏。

- **状态**：**✅ 已修复**（`internal/ui/completions/item.go`：高亮改为作用于纯文本；`item_test.go` 补 6 个回归测试）
- **结论立场**：**Adopt** —— 这是 `internal/ui/completions` 的 UI 渲染缺陷，**不是**终端/编码/字体问题
- **影响面**：`/` 内联补全弹窗的非选中行（unfocused rows）
- **调研基线**：`b979d00`（当时为只读调研，未修改被测源码）

---

## 0. 修复记录

**改动**：`internal/ui/completions/item.go:146-158` —— 把 `highlightRunes` 的作用对象从「已着色的 `TitleText.Render(label)`
输出」改为「纯文本 `label` / `desc`」，着色由 `highlightRunes` 内部对各分段完成；布局宽度在着色前先用纯文本量好。

**附带修掉的两个隐患**（原实现既有、未被报告）：

1. `highlightRunes` 在 `len(hits)==0` 时提前返回。原先因为入参已预先着色所以看不出问题，
   改为传纯文本后若不处理，无筛选词时整列会**丢掉主题色**。现在 `styled=true` 时无命中也会走 `base.Render(text)`。
2. 对已着色文本再次 `base.Render` 造成的**样式双重嵌套**（旧输出里的 `\x1b[m\x1b[m`、`\x1b[4;38;2;223;219;221;4m`）。

**测试是否真的能抓住这个 bug**：把修复 stash 掉后跑新测试，**11 条断言失败**，其中捕获到的实际残码为

```
escape parameters leaked as literal text (focused=false):
" [38;2;104;255;214m/agents        Switch agent mode                           "
query "a" altered the visible row (focused=false)
```

与报告截图逐字一致。修复恢复后全部通过。

**回归测试**（`internal/ui/completions/item_test.go`）：

| 测试 | 守住什么 |
|---|---|
| `Render_NoAnsiResidue` | 9 组命令/关键词 × focused/unfocused：`ansi.Strip` 后不得出现 SGR 残片，且行宽必须恰为 60 |
| `Render_HighlightDoesNotChangeLayout` | **核心不变量**：高亮只是装饰，可见文本与列对齐不得改变（旧实现每行宽出 5–18 格） |
| `Render_KeepsThemeStyling` | 修复后由 `highlightRunes` 负责着色，无命中时也必须保留主题色 |
| `Render_EmphasizesHits` | 别把残码 bug 换成「高亮静默消失」 |
| `Render_CachedResultIsStable` | 命中行与空查询行走同一渲染路径、缓存一致 |

---

## 1. 一句话结论

`SlashCompletionItem.Render()` 先把文本**渲染成带 ANSI 样式**的字符串，再用**按纯文本算出的**模糊匹配下标去「逐 rune」切分它。下标落进转义序列内部，把一个完整的 `ESC[38;2;…m` 从中间切开：ESC 被分到上一段，剩下的 `38;2;…m` 参数被当作普通字符渲染出来 —— 于是屏幕上出现字面量颜色码。

## 2. 证据：现象矩阵（截图 → 复现）

截图中的每一行都由探针**逐字复现**（`ansi.Strip` 后的可见文本）：

| 截图行 | 截图中的可见残码 | 探针复现结果 | 行宽（应为 60） |
|---|---|---|---|
| `/agents` | `[38;2;104;255;214m/agents` | `" [38;2;104;255;214m/agents        Switch agent mode"` | **78** |
| `/approve` | `[38;2;104;255;214m/approve` | `" [38;2;104;255;214m/approve       Toggle auto-approve (Yolo)"` | **78** |
| `/init` | `2;223;219;221mInitialize project` | `" /init          2;223;219;221mInitialize project"` | **74** |
| `/wechat` | `…221mManage WeChat accounts` | `" /wechat        ;221mManage WeChat accounts"` | **65** |
| `/copy` | `;221mCopy the last assistant reply to clip…` | `" /copy          ;221mCopy the last assistant reply to …"` | **65** |
| `/admin`（高亮选中行） | 干净 | `" /admin        Open Admin Panel"` | 60 ✅ |

- 残码颜色 `104;255;214` / `223;219;221` 分别等于主题里的 `Dialog.TitleText` 与 `Dialog.ListItem.InfoBlurred` 前景色 —— 与截图完全一致。
- 选中行干净、非选中行损坏 —— 与截图完全一致。

**行宽是第二个症状**：损坏行比预期宽 5–18 个单元格。因为 `gap` 是用**纯文本**（`lipgloss.Width(label)`）算的，而实际输出多出了残码字符，导致整行撑爆弹窗宽度 → 换行 → 截图中残码看起来「跑到别的行前面」。

## 3. 根因链路

### 3.1 触发点：`internal/ui/completions/item.go:146-152`

```go
} else {
    renderedLabel := s.t.Dialog.TitleText.Render(label)                              // :147 先上色 → 含 ESC
    renderedLabel = highlightRunes(renderedLabel, cmdHits, s.t.Dialog.TitleText,     // :148 再按下标切
        s.t.Completions.Match, true)
    renderedDesc := s.t.Dialog.ListItem.InfoBlurred.Render(desc)                     // :149 先上色
    renderedDesc = highlightRunes(renderedDesc, descHits, s.t.Dialog.ListItem.InfoBlurred,
        s.t.Completions.Match, true)                                                 // :150 再按下标切
```

`cmdHits` / `descHits` 由 `splitMatchIndexes(s.match, len(s.command))`（`:139`，定义于 `:162-173`）产生，其下标空间是**纯文本**（`command + " " + desc`，见 `completions/item.go:73` 的 `Filter()`）。

而传入 `highlightRunes` 的是**已着色字符串**，每个转义序列额外占 18–20 个 rune。两个下标空间错位。

### 3.2 切口：`internal/ui/completions/item.go:177-207`

`highlightRunes` 是纯 rune 遍历，**不感知 ANSI**：

```go
for _, r := range text {        // :202  text 里含 ESC/[ /数字/m
    isHit := hits[i]            // :203  用纯文本下标去索引「含转义的」rune 流
    if isHit != hitRun {
        flush()                 // :205  在这里把转义序列切成两段
        hitRun = isHit
    }
    seg.WriteRune(r)
    i++
}
```

`flush()`（`:185-201`）对每一段独立调用 `base.Render(out)` / `match.Render(out)`，于是：

- 第 0 段的 `"\x1b"` 被单独 `base.Render`；
- 第 1 段从 `"38;2;104;255;214m/agents"` 开始 —— **ESC 已经留在上一段了**。

### 3.3 实测中间产物（`/agents`，query=`a`，命中下标 `[1]`）

```
RAW: " \x1b[38;2;223;219;221m\x1b[38;2;104;255;214m\x1b\x1b[m\x1b[4;38;2;223;219;221;4m[\x1b[m\x1b[38;2;104;255;214m38;2;104;255;214m/agents\x1b[m\x1b[m  ..."
                                                                  ^^^^^^^^^^^^^^^^^^^^^
                                                                  重复且已失去 ESC 前缀的参数
```

注意 `\x1b[38;2;104;255;214m` 之后紧跟 `38;2;104;255;214m/agents`：**同一串参数被输出了两遍**，第二遍没有 ESC，于是显示为字面文本。这就是乱码的全部来源。

### 3.4 完整调用链

```
Completions.Render()                       internal/ui/completions/completions.go:669
└─ renderSlashContainer()                  :384   ← 补上 "Commands" 标题与分隔线（即截图外框）
   └─ list.Render()                        internal/ui/list/list.go:280
      └─ item.Render(width)                (getItem :133 → :148)
         └─ SlashCompletionItem.Render()   internal/ui/completions/item.go:113
            └─ highlightRunes()            :177  ← 缺陷所在
```

命令文本来自 `internal/ui/model/ui_completions.go` 的 `slashCompletionGroups()`（`:206` `/agents`、`:268` `/approve`、`:295` `/copy` —— 与截图描述逐字一致）。

## 4. 关键判别：为什么只坏一部分行

| 分支 | 输入 | 是否损坏 | 原因 |
|---|---|---|---|
| **unfocused + 命中标签列** | 已着色文本 | ❌ 必坏 | 标签转义前缀占 18–20 rune，而命令很短，命中下标几乎必然落在前缀内 |
| **unfocused + 只命中描述列** | 已着色文本 | ⚠️ 残码视下标而定 | `splitMatchIndexes` 会按 `cmdLen+1` 重定基（`:169`）；下标 0 恰好落在描述转义序列的 ESC 上 → 产生 `;221m…`（即截图 `/wechat`、`/copy` 的形态） |
| **focused**（`:142-145`） | `lipgloss.NewStyle()` + **纯文本** | ✅ 正常 | 下标空间一致，走 `\x1b[1m…\x1b[22m` 粗体回退（`:194`） |
| **无筛选词**（query 为空） | — | ✅ 正常 | `highlightRunes` 在 `:178-180` 提前返回 |

> 因此：**必须先输入筛选词才触发**。截图里输入框是 `> /a`，完全吻合。

`Commands` 对话框（`internal/ui/dialog/commands_item.go:217-218`）虽然同样先上色，但**从不调用 `highlightRunes`**，所以不受影响 —— 这也解释了为什么全屏命令面板没坏。

## 5. 与既有 `CleanCommandText` 的关系（重要）

`internal/ui/slash/sanitize.go:27-33` 已有一个 `CleanCommandText`，用正则清理「ESC 丢失后的 SGR 残片」：

```go
s = ansi.Strip(s)
s = bracketSGR.ReplaceAllString(s, "")   // `\[[0-9;]*m`
s = bareSGR.ReplaceAllString(s, "")      // `;?\d{1,3}(?:;\d{1,3})+m`
```

**立场：Reject 它作为本 bug 的解法。** 理由：

1. **层次错了**。它是文本清洗，只对**自定义命令**（用户 markdown 文件）生效，调用点仅 `DisplayLabel()` / `ui_completions.go:373`。而本 bug 的残码是**渲染时现场生成**的，源数据完全干净（`/agents`、`/copy` 都是硬编码字符串）。清洗源数据不可能修好它。
2. **误诊的痕迹**。`internal/ui/slash/sanitize_test.go:18-19/24/30` 的夹具恰好是：
   - `"\x1b[38;2;104;255;214m/history"`（= 标签残码形态）
   - `"[38;2;223;219;221mShow help & key bindings"`（= `/help` 描述残码形态）
   - `"8;2;104;255;214mBrowse past sessions"`（= `/history` 描述残码形态）

   这些形状与本 bug 的输出**完全一致**。也就是说：同一个现象被归因成了「用户粘贴终端彩色文本到文件里」，于是在错误的位置加了一层文本清洗，而真正的渲染缺陷至今未被覆盖。

> 保留 `CleanCommandText` 仍有价值（它确实挡掉了文件里真实存在的脏数据），但它**不能替代**本修复。

## 6. 可核验的复现方法

在模块内建一个临时包（不改动任何现有文件）：

```go
// zz_probe/probe_test.go
st := styles.ThemeForProvider("")
v  := completions.SlashCompletionValue{Command: "/agents", Desc: "Switch agent mode"}
it := completions.NewSlashCompletionItem(v, &st)
it.SetFocused(false)                                  // 非选中行
ms := fuzzy.Find("a", []string{"/agents Switch agent mode"})
it.SetMatch(ms[0])
out := it.Render(60)
fmt.Printf("%q\n", ansi.Strip(out))
// → " [38;2;104;255;214m/agents        Switch agent mode"   宽度 78（预期 60）
```

运行：`go test ./zz_probe/ -run TestProbe -v`（Go 1.26.5，已验证）。

## 7. 修复建议

**最小正确改动**：把「高亮」放到「着色」之前，让 `hits` 始终作用在纯文本上。

```go
} else {
    // 先在纯文本上高亮（下标空间一致），再一次性着色
    labelHL := highlightRunes(label, cmdHits, s.t.Dialog.TitleText, s.t.Completions.Match, true)
    descHL  := highlightRunes(desc,  descHits, s.t.Dialog.ListItem.InfoBlurred, s.t.Completions.Match, true)
    gap := strings.Repeat(" ", max(0, lineWidth-lipgloss.Width(label)-len(labelGap)-lipgloss.Width(desc)))
    row = labelHL + labelGap + descHL + gap
}
```

要点：

- `highlightRunes` 在 `styled=true` 时已对非命中段 `base.Render`、命中段 `match.Render`（`:191-198`），喂纯文本即产出正确结果，**无需改 `highlightRunes` 本身**。
- 顺带修掉一个隐性缺陷：现状对已着色文本再次 `base.Render` 会造成样式**双重嵌套**（RAW 里的 `\x1b[m\x1b[m` / `\x1b[4;38;2;223;219;221;4m`）。
- 顺带修掉错位高亮：即使命中的是可见字符（如描述列命中），当前实现也会因前缀偏移而**强调到错误的字**。
- 修复后 `gap` 计算仍然正确（它本就用纯文本宽度），行宽回到 `lineWidth`，撑爆/换行消失。

**不推荐**：给 `highlightRunes` 加 ANSI 感知 tokenizer。虽然也能修，但接口会变复杂，且当前唯一的两个调用点（`:143`、`:148/150`）只要统一喂纯文本问题就消失。

## 8. 回归测试建议

在 `internal/ui/completions/item_test.go` 增加（现有 `TestHighlightRunes` 只测了纯文本 `"abc"`，恰好绕过了这个 bug）：

1. `Render()` 非选中行 + 命中标签列 → 断言 `ansi.Strip(out)` **不包含** `[38;2;` 与独立的 `;` `m` 残片；
2. 断言 `lipgloss.Width(out) == width`（守住行宽不溢出）；
3. 参数化覆盖 `label 命中` / `desc 命中` / `两者都命中` / `空 query` 四类；
4. 断言输出里 `\x1b[` 之后**不带**裸参数（可用 `ansi.Strip(out)` 与原文比对）。

## 9. 未验证 / 边界

- 截图中的 `/think`、`/plan` 两行未逐行跑探针；但其残码形状（`38;2;223;219;221m`、`8;2;104;255;214m`）与机制推导、与 §2 已复现行一致，且同样出现在 `sanitize_test.go` 夹具中。标注为**同机制、未逐行复现**。
- 未在真实 TUI 中端到端验证（需要交互式终端）；结论基于对真实 `SlashCompletionItem.Render` 的直接调用。
- `AtCompletionItem.Render`（`item.go:288-289`）先上色但不调用 `highlightRunes`，**推断**不受影响；未构造 `@` 场景实测。
