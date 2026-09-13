# shell-parity — bash 工具的跨平台命令补齐（Layer 3）

> 状态：L1/L2 已落地；L3 Phase 1+2 已实现（Phase 3 待定）
> 创建：2026-09-14
> 范围：`internal/core/shellruntime/shell/`（执行中间件）+ `internal/core/tools/core/shell/`（路由表联动）
> 依据：`mvdan.cc/sh/moreinterp/coreutils`、`github.com/u-root/u-root/pkg/core`

## 背景

bash 工具的执行后端是 **mvdan/sh 解释器**（无系统 shell）。Go coreutils 中间件只提供：

```
cat chmod cp find ls mkdir mv rm touch xargs base64 gzip mktemp shasum tar
```

常见文本工具 **grep / rg / head / tail / sed / awk / wc / sort / uniq / cut / tr** 均缺失：
在 macOS/Linux 会落到系统二进制，在 **Windows 直接 `executable file not found in $PATH`**。
模型（我）惯性地在管道里用 `| head -20`、`| grep`，于是反复失败。

**已落地（前置）**：

- **L1**：修正 `shellruntime/shell/shell.go` 顶部原本声称「grep/head/tail/sed/awk 全平台可用」的**虚假注释**，改为如实列出现状。
- **L2**：`tools/core/shell/bash.go` 新增 `commandRoutingHint()`——当命令因缺失而失败时，在工具响应里追加一行指路提示（`head`→`view`、`grep`→`grep` 工具、`find`→`glob` …），让失败**当场自纠错**，而非仅由 `errcoll` 事后埋点。

## 目标

让「纯流式文本过滤」类命令在 Windows 也能跑，消除 L1/L2 之外的**残留摩擦**；对**有更好的专用工具**的命令，继续用 L2 提示引导而非实现。

## 非目标

- ❌ 实现 `grep` / `rg`：`grep` 工具提供正则、ignore 文件、结构化输出，明显更优——**保持路由**。
- ❌ 实现 `sed` / `awk`：文本改写应走 `edit` 工具，脚本变换走 `ts_run`/`py_run`——**保持路由**。
- ❌ 追求 GNU 全量 flag 兼容：只覆盖**agent 管道里真实高频**的子集。
- ❌ 实现 `find` / `ls` / `cat`：已由 coreutils 提供；仅在 coreutils 关闭时由 L2 提示兜底。

## 命令分界线（关键设计）

| 类别 | 命令 | 策略 | 理由 |
|------|------|------|------|
| **Parity（实现）** | `head` `tail` `wc` `tee` `sort` `uniq` `cut` `tr` | Go 内置中间件 | 纯流式过滤，无专用替代品，是命令管道的一部分（如 `go build 2>&1 \| tail -20`） |
| **Routing（提示）** | `grep` `egrep` `fgrep` `rg` `sed` `awk`（+ coreutils 关闭时的 `cat`/`find`/`ls`） | 保留 L2 提示 | 有语义更强的专用工具 |

> 分界原则：**「哑流过滤」实现，「聪明/带语义的工具」路由。**

## 架构

### 1. 新中间件

新增 `internal/core/shellruntime/shell/pipeutils.go`，镜像上游 `coreutils.ExecHandler` 的写法：

```go
func ExecHandler(next interp.ExecHandlerFunc) interp.ExecHandlerFunc {
    return func(ctx context.Context, args []string) error {
        program, programArgs := args[0], args[1:]
        newCmd, ok := commandBuilders[program]
        if !ok {
            return next(ctx, args) // 不认识的命令，交回下游
        }
        c := interp.HandlerCtx(ctx)
        cmd := newCmd()
        cmd.SetIO(c.Stdin, c.Stdout, c.Stderr)
        cmd.SetWorkingDir(c.Dir)
        cmd.SetLookupEnv(func(k string) (string, bool) { v := c.Env.Get(k); return v.Str, v.Set })
        return cmd.RunContext(ctx, programArgs...)
    }
}
```

命令实现复用 u-root 的 `core.Base`（`pkg/core`），实现 `core.Command` 接口
（`SetIO/SetWorkingDir/SetLookupEnv/Run/RunContext`），与 `u-root/pkg/core/cat` 同构。

### 2. 注册点

`shell.go` 的 `execHandlers()`：

```go
handlers := []func(...){ s.builtinHandler(), s.blockHandler() }
if useGoCoreUtils { handlers = append(handlers, coreutils.ExecHandler) }
if usePipeUtils    { handlers = append(handlers, pipeutils.ExecHandler) } // 新增
return handlers
```

放在最后：优先级低于内建与安全拦截，高于 OS exec 默认。

### 3. 开关与可测性

- 新增 `usePipeUtils`，默认与 `useGoCoreUtils` 一致（仅 Windows 开），可用 `MOCODE_PIPE_UTILS` 覆盖。
- **重构点**：`coreutils.go` 目前用 `init()` 读环境变量，导致测试无法用 `t.Setenv` 覆盖。改为**惰性求值**
  （首次调用 `execHandlers()` 时读取，或提供 `SetUseGoCoreUtils/SetUsePipeUtils(bool)` 供测试注入）。
  否则集成测试无法在 macOS/Linux 上验证这些内置命令。

## 实现记录（2026-09-14）

Phase 1 + 2 已落地：

| 文件 | 内容 |
|------|------|
| `internal/core/shellruntime/shell/pipeutils.go` | `pipeEnv` + 8 个命令 + `pipeUtilsHandler` 中间件 |
| `internal/core/shellruntime/shell/pipeutils_test.go` | 逐命令单测 + 经解释器的端到端管道测试 |
| `internal/core/shellruntime/shell/coreutils.go` | 开关改为**惰性求值**：`goCoreUtilsEnabled()` / `pipeUtilsEnabled()` |
| `internal/core/shellruntime/shell/shell.go` | `execHandlers()` 注册 `pipeUtilsHandler`；注释更新 |

- 开关：`MOCODE_PIPE_UTILS`（默认同 coreutils，仅 Windows）。
- flag 覆盖：`head -n/-c/-q/-v`、`tail -n/+N/-c`、`wc -l -w -c -m -L`、`tee -a`、
  `sort -n -r -u -f -k -t`、`uniq -c -d -u`、`cut -d -f -c -b`、`tr -d -s -c`。
- 未实现（按分界线保持路由）：`grep/rg/sed/awk`。
- 校验：`go build ./...`、`go vet`、`gofumpt`、golangci-lint v2（新增文件 0 告警）均通过。

## 分阶段

### Phase 1 — 高频四件套（`head` `tail` `wc` `tee`）

| 命令 | 覆盖 flag | 备注 |
|------|-----------|------|
| `head` | `-n N`、`-n -N`（除末 N 行）、`-c N`、`-q`、`-v`、多文件、默认 10 行 | 见「坑 1/2」 |
| `tail` | `-n N`、`-n +N`、`-c N`、`-q`、`-v`、多文件 | `-f` **暂不做**（见非目标/坑 4） |
| `wc`   | `-l -w -c -m -L`、多文件合计行 | |
| `tee`  | `-a`、多文件 | 需要文件系统访问（走 `core.Base` 的路径解析） |

### Phase 2 — 文本变换（`sort` `uniq` `cut` `tr`）

| 命令 | 覆盖 flag |
|------|-----------|
| `sort` | `-n -r -u -f -k -t`（**不做** locale 相关 `-d/-i`，按字节序） |
| `uniq` | `-c -d -u`（仅相邻去重，POSIX 语义） |
| `cut`  | `-d -f`、`-c`、范围 `N-M`/`N-`/`-M`、多段 |
| `tr`   | 集合与范围（`a-z`）、`-d -s -c` |

### Phase 3 — 可选

- `tail -f`：与现有**后台作业**机制有天然重叠，若做需明确语义（跟随到作业结束）。
- `sed` **极小子集**（`-n` + `s///` + `p`）：**默认不做**，除非出现明确需求——它与 `edit` 工具路由冲突。

## 关键语义与坑

1. **早退 / EPIPE**：`head` 读满 N 行后应停止读取上游。在 mvdan 管道里上游写端会拿到 EPIPE，
   表现为 "broken pipe"。参考 GNU：`head` 自身 **exit 0**，需**吞掉**下游断管错误，且不对上游报错。
2. **CRLF**：Windows 输入按 `\n` 切行会**保留 `\r`**。与 GNU 行为一致即可（不自动剥离），
   但需在测试中显式断言，避免「看起来对、实际多一个 CR」。
3. **字节 vs 字符**：`wc -c` 计**字节**、`wc -m` 计**字符**（UTF-8 需按 rune 计），`head -c` 按字节。
4. **无参数的 `tail`/`head`**：无文件参数时读 stdin；有文件时逐文件处理并加 `==> file <==` 头（`-q/-v` 控制）。
5. **退出码**：读文件失败返回非零；管道正常结束返回 0。要与 interp 的 exit status 语义对齐。

## 与 L2 的联动（重要）

Phase 1/2 落地后，对应命令在启用时**不再「缺失」**，只会返回成功，L2 的
`shellCommandRouting` 对于这些命令**只会在 `MOCODE_PIPE_UTILS=false` 时才会触发**。

最终决定：**保留** `head/tail/wc/tee/sort/uniq/cut/tr` 的路由条目作为
「功能被显式关闭时」的兜底提示，而非删除。理由：提示本身是**失败条件触发**的
（命令真缺失才出现），保留零成本且能在 flag 关闭时继续引导；真正会**永不触发**的
死映射并不存在。真正的路由目标（`grep/rg/sed/awk`，以及 coreutils 关闭时的
`cat/find/ls`）保持不变。

## 测试策略

- **单元**：每个命令表驱动测 flag 解析与语义（含 `head -n -N`、`cut -f2-`、`tr -d` 等边界）。
- **集成**：经解释器真实管道验证，例如
  `NewShell(nil).Exec(ctx, "printf 'a\\nb\\nc\\n' | head -2")` → 断言输出与退出码。
  这依赖上面的**可测性重构**（env 不能在 init 里读死）。
- **回归**：确认 coreutils 原有命令不受影响；确认 `pipeutils` 不吞掉 `builtinHandler` 的命令。

## 风险与回滚

| 风险 | 缓解 |
|------|------|
| 语义偏差导致脚本行为与 GNU 不一致 | 只实现高频子集 + 逐条对照 GNU 的测试用例；flag 不认识时**显式报错**而非静默忽略 |
| EPIPE 处理不当弄坏管道退出码 | 专项测试上游/下游断管两种情形 |
| 与 coreutils 中间件重复/冲突 | `pipeutils` 只注册自己的命令名；不覆盖 coreutils 已有的名字 |
| 体积与维护成本 | 分 Phase、按需推进；始终**默认关闭**（Windows 优先），可整体回退 |

## 验收标准

1. `go build ./...` + `go vet ./...` 全绿。
2. Windows 上 `echo x | head -1`、`... | tail -n 5`、`... | wc -l`、`... | tee log` 正常。
3. Phase 2 命令的单元 + 集成测试通过。
4. L2 路由表已同步删减，无死映射。
5. 文档：`shell.go` 注释中的可用命令清单更新为「coreutils + pipeutils」。

## 工作量估算

| 阶段 | 实现 | 测试 | 小计 |
|------|------|------|------|
| 可测性重构（惰性开关） | 0.5d | 0.5d | 1d |
| Phase 1（head/tail/wc/tee） | 1d | 1d | 2d |
| Phase 2（sort/uniq/cut/tr） | 1.5d | 1.5d | 3d |
| L2 联动 + 文档 | 0.25d | 0.25d | 0.5d |
