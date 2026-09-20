# 07 · 原生浏览器运行时（browser 工具）

> 目标：让 mocode **原生**具备网页调试能力 —— 打开页面、看 DOM 结构、点元素、读 console、抓截图，
> 全部在一个进程内完成，不依赖用户预装 Node/Playwright CLI。
>
> 上游资产：`C:/Users/16143/projects/self/go-browser`（9,145 行 Go）、
> `internal/core/skills/builtin/playwright-cli/`（现存的 skill 文本）。
>
> 状态：**已实现 P1 并全部验证通过**（探测 + CDP 客户端 + session + snapshot/ref + browser 工具 + 三层测试）

---

## 1. 选型：为什么不直接移植 go-browser

对 `go-browser` 做了逐文件审计，结论是**不应该整体移植**，理由按严重度排序：

| # | 发现 | 证据 | 影响 |
|---|---|---|---|
| 1 | **它是 CLI + daemon，不是库**。全部实现在 `internal/` 下，Go 规则禁止外部 import | 9 个 package，31 个文件；`internal/daemon` 单包 3,117 行 | 要复用必须整包搬进 mocode 的 `internal/`，且**必须砍掉 daemon 层**（mocode 是 TUI 进程内调用） |
| 2 | **底层是 playwright-go**，需要 Playwright 的 Node driver + 各浏览器内核预装 | `go.mod`：`playwright-community/playwright-go v0.5700.1`；`playwright.Run()` 对未安装 driver 直接报错 | 与"原生"矛盾：要给用户装 ~500MB Node driver。仓库里**没有任何安装引导代码**（`playwright.Install()` 零引用） |
| 3 | **完全没有浏览器探测代码（0 行）** | 全仓仅 2 处 `exec.Command`：重启自身、跑 `netstat`。`LookPath`/`flatpak`/`snap`/`chromium-browser`/`Program Files`/`.app/Contents` 命中数 **0** | 用了 playwright 就由它自己下载内核，**探测本身没意义**。而你明确要"浏览器探测" |
| 4 | **每个命令两套实现**（daemon 分支 + local 分支） | 新增命令要改 protocol/server/client/commands **四个文件**（CLAUDE.md 自述） | 行为漂移已存在：local 用 `locator.Resolver` 支持 `getBy*`，daemon 的 `resolveLocator` 不认 |
| 5 | **`console` 命令永远输出空 —— 静默失效** | `setupConsoleHandler`（devtools.go:229）**从未被调用**，`consoleStore` 永远是空 | 比报错更危险：命令存在、不报错、永远无数据 |
| 6 | **9 个命令是假实现**，但都注册在 CLI 里对用户可见 | `route`/`route-list`/`unroute`/`network`/`tracing-*`/`video-*` 全部 `not implemented` | 网页调试最需要的 `network` 恰恰没实现 |
| 7 | **测试覆盖率 < 5%** | 测试 193 行 vs 生产 8,952 行；`internal/daemon`（3,117 行）0 测试 | "闭环测试"无从谈起，这正是你要求的那条 |
| 8 | Linux 取 PID 依赖 `netstat`（`net-tools`），Alpine/slim 镜像里没有；`LD_LIBRARY_PATH` hack 硬编码 `x86_64` | `local.go:120`、`browser.go:50-71` | Linux 上不可靠 |
| 9 | 无 `--no-sandbox` | `local.go:68-70` 只有 `--remote-debugging-port` | 容器/root 下 Chromium 直接启动失败 |

**但有 3 样东西值得吸收**：① snapshot + ref 的交互模型（`e0`/`e15` 短引用）；
② 会话/标签页的抽象；③ CLI 命令语义（可直接映射成工具 action）。
`docs/architecture/06-playwright-cli-design.md` 里已经记下了同一套判断（snapshot-first、session 隔离、`--raw`）。

## 2. 决策：CDP-native（纯 Go 直连 Chrome DevTools Protocol）

**用已安装的系统浏览器 + 直接讲 CDP，不引入 playwright / Node / 任何浏览器下载。**

这正是本仓库既有的「探测优先」范式（`rg`/`fd`/`jq`）：

| 现有范例 | 探测 | 兜底 |
|---|---|---|
| grep | `search/rg.go:17` `sync.OnceValue(exec.LookPath("rg"))` | `search.go:170` 纯 Go `filepath.Walk` |
| shell rg/fd | `shell/rg.go:28` `hostRgPath()` | `shell/rg.go:73` 纯 Go 子集 |
| jq | 无探测 | `shell/jq.go:44` 内嵌 `gojq` |
| **browser（本文）** | `detect.go` 跨平台找已装浏览器 | 找不到 → 工具返回可诊断的软错误，而不是崩 |

选它的收益，恰好逐条对上 §1 的缺陷：

| §1 缺陷 | CDP-native 的结果 |
|---|---|
| ② 需要 Node driver | **不需要**。纯 Go + `gorilla/websocket`（已在依赖图中） |
| ③ 没有探测 | **就是本方案的核心**，见 §3 |
| ⑤ `console` 静默失效 | CDP 的 `Runtime.consoleAPICalled` 是**事件推送**，天然可用 |
| ⑥ `network` 未实现 | CDP 的 `Network.*` 天生具备（P2 接入） |
| ⑦ 覆盖率 <5% | 三层闭环测试，见 §6 |
| ⑧ Linux netstat hack | 不取 PID：用 CDP 的 `Browser.close` 优雅关闭 + 进程句柄兜底 |
| ⑨ 无 `--no-sandbox` | 检测 root/euid==0 时自动补 `--no-sandbox` |

额外收益：**CGO_ENABLED=0 仍成立**，二进制仍是静态单文件，符合 `Taskfile.yaml:14` 与仓库发布方式。

## 3. 浏览器探测（本方案的重点）

契约：`Detect(opts) []Candidate`，按**优先级降序**返回所有可用候选，并带**可解释的探测来源**
（`source` 字段），方便 `doctor` 与报错信息说清"我是怎么找到/没找到的"。

### 3.1 各平台搜索位置

| 平台 | 来源 |
|---|---|
| **linux** | `PATH` 上的 `google-chrome`/`google-chrome-stable`/`chromium`/`chromium-browser`/`microsoft-edge`/`microsoft-edge-stable`/`brave-browser`/`brave`/`vivaldi`；`/usr/bin`、`/usr/local/bin`、`/opt/google/chrome`、`/snap/bin`；**flatpak**（`flatpak run com.google.Chrome` 这类需要的特殊处理见 3.3）；**snap**（`/snap/bin/chromium`）；`~/.local/bin` |
| **darwin** | `/Applications/Google Chrome.app/Contents/MacOS/Google Chrome`、`Chromium.app`、`Microsoft Edge.app`、`Brave Browser.app`、`Vivaldi.app`、`~/.local/share/...`；以及 `PATH` |
| **windows** | `%ProgramFiles%`、`%ProgramFiles(x86)%`、`%LOCALAPPDATA%` 下的 `Google\Chrome\Application\chrome.exe`、`Microsoft\Edge\Application\msedge.exe`、`BraveSoftware\Brave-Browser\Application\brave.exe`、`Vivaldi\Application\vivaldi.exe`、`Chromium\Application\chrome.exe`；以及 `PATH` |

**只接受 Chromium 系**（Chrome/Chromium/Edge/Brave/Vivaldi/Opera）。Firefox/WebKit 不走 CDP，**显式不支持**
并在错误信息里说明——这比静默失败好。

### 3.2 探测的「诚实」原则

- 每个候选带 `Source`（`path` / `flatpak` / `snap` / `env` / `playwright-cache`），
  以及 `Version`（尽力解析，失败为空而非报错）。
- **不做"存在即可用"的假设**：候选要经过 `probeVersion`（跑 `--version`）才算 `Verified`。
  未验证的候选仍返回，但排在后面 —— 这样"装了个壳但跑不起来"能被诊断出来。
- 找不到任何候选时，错误信息**列出所有尝试过的路径**（而不是干巴巴一句 "not found"）。

### 3.3 Linux 的两个坑（显式处理）

1. **snap 版 Chromium 的 user-data-dir**：snap 有 confinement，不能把 profile 放到 `~/` 之外的任意路径，
   只能用 `$SNAP_USER_DATA`。方案：检测到 snap 候选时，profile 目录改放
   `~/snap/chromium/common/.mocode-profile`，并在候选上标记 `NeedsSnapProfile=true`。
2. **flatpak 版**：不能直接 exec 沙箱内的二进制路径，必须 `flatpak run <app-id> ...`。
   方案：候选带 `PrefixArgs []string`（flatpak 候选为 `["run", "com.google.Chrome"]`），
   启动时统一 `exec.Command(bin, append(prefix, args...)...)`。
3. **容器/root**：当 `os.Geteuid() == 0` 时自动追加 `--no-sandbox`（go-browser 的缺陷 ⑨）。

### 3.4 显式覆盖

- `MOCODE_BROWSER_PATH` 环境变量：跳过探测直接用指定二进制（`source=env`）。
- `MOCODE_BROWSER_CDP` 环境变量：跳过启动，直接连已有 CDP 端点（attach 模式）。

## 4. 架构

```
internal/core/browser/            ← 引擎（新 core 包，layer 3）
├── detect.go      跨平台探测，返回 []Candidate（含 Source/Version/PrefixArgs）
├── launch.go      启动浏览器子进程：headless/--no-sandbox/profile/tmpdir/端口等待
├── cdp.go         CDP over WebSocket：请求-响应复用、事件订阅、目标/会话路由
├── session.go     会话生命周期：Page 句柄、ref 缓存、console 环形缓冲、关闭
├── snapshot.go    DOM 快照 + ref 分配（吸收 go-browser 的 snapshot/ref 思路）
└── types.go       共享类型与错误

internal/core/tools/external/systems/browser/   ← 工具层
├── browser.go     单个工具 + action 枚举（与 learn 同一范式）
└── browser.md     给模型的说明书（含房规：snapshot-first）
```

分层的理由（对应 `internal/core/tools/doc.go` 的约定）：**引擎归 `core/browser`，工具语义归 tools 子树**；
`skills` 包管 skill 格式同理。引擎可被 doctor/其他工具复用，不必绑死在 fantasy 上。

### 4.1 为什么是「一个工具 + action 枚举」

浏览器调试是**有状态的多步流程**（launch → goto → snapshot → click(ref) → console）。
拆成 N 个工具会让每个周期都重新声明 schema（token 成本），而 `learn` 已经验证了 action 枚举范式可用。
参考 `docs/architecture/06-playwright-cli-design.md` 记下的判断：CLI-over-MCP 在 token 敏感场景更优。

### 4.2 CDP 客户端的最小闭环

需要的最小能力（其余都是增量）：

- **握手**：HTTP `GET /json/version` 拿 `webSocketDebuggerUrl`（这是 CDP 的发现端点，不是猜端口）。
- **请求-响应**：`{id, method, params}` / `{id, result|error}`；用一个 `map[int64]chan` 做多路复用。
- **事件**：`{method, params}` 无 `id` → 分发给订阅者（`Runtime.consoleAPICalled`、`Page.loadEventFired`）。
- **会话路由**：attach 模式下有 `Target.attachToTarget` 得到的 `sessionId`，消息需带/校验 `sessionId`。

## 5. 工具表面（P1）

单个工具 `browser`，`action` 枚举：

| action | 作用 | 关键参数 |
|---|---|---|
| `status` | 探测报告：找到哪些浏览器、当前会话状态 | — |
| `launch` | 启动（或 attach）会话 | `url?`, `headless?`, `browser?` |
| `goto` | 导航 | `url` |
| `snapshot` | DOM 快照 + 分配 ref | `max_depth?` |
| `click` | 点击 | `ref` 或 `selector` |
| `fill` | 填表 | `ref`/`selector`, `text`, `submit?` |
| `press` | 按键 | `key` |
| `eval` | 执行 JS | `expression` |
| `screenshot` | 截图存文件 | `path?`, `full_page?` |
| `console` | 读 console 消息（**CDP 事件，真能用**） | `clear?` |
| `close` | 关闭会话 | — |

**房规（写进 `browser.md`）**：先 `snapshot` 拿 ref，再用 ref 操作；不要凭猜写 CSS selector。
这条直接来自 `docs/architecture/06-playwright-cli-design.md` 的 Snapshot-First 结论。

## 6. 闭环测试策略（三层，都要有）

这是本设计能成立的前提 —— 你要求的那条。

| 层 | 测什么 | 怎么做到无外部依赖 |
|---|---|---|
| **L1 单元** | 探测候选表、`PrefixArgs`、snap profile 改写、参数拼接、CDP 帧编解码 | 注入假 `GOOS`/假 PATH/假文件系统（`fs.FS` 或临时目录树） |
| **L2 协议闭环** | **对 mock CDP 服务**跑完整会话：握手 → 建 session → goto → snapshot → click → console 事件 → close | `httptest.Server` + `gorilla/websocket` 起一个**假 Chromium**，按真实 CDP 语义应答；断言收到的请求序列 |
| **L3 真浏览器 e2e** | 真启动本机 Chrome，打开一个本地 `httptest` 页面，验证 snapshot/click/fill/console 端到端 | 探测不到浏览器或 `MOCODE_TEST_NO_BROWSER=1` 时 **`t.Skip`**，CI 不红 |

L2 是核心：它把「我们的 CDP 客户端理解协议正确」这件事**变成可回归的断言**，而不依赖机器上有没有 Chrome。
L3 在本机跑得起来（已确认有 Chrome/Edge/Playwright Chromium），用来证明 L2 的 mock 不是自欺欺人。

## 7. 明确不做（P1 范围外，不是遗漏）

| 不做 | 原因 |
|---|---|
| Firefox / WebKit | 不是 CDP；需要另一套协议（Marionette/JWP）。显式报错 |
| 网络拦截 / 请求回放（`route`） | P2。CDP `Fetch.*` 可做，但语义面大 |
| tracing / video | P2。go-browser 里同样未实现，且 video 需要 ffmpeg |
| PDF 导出 | P2。CDP `Page.printToPDF` 一行就能加，但先验证 P1 闭环 |
| 多标签页 / 命名会话 | P2。P1 单会话单页，避免过早复杂化 |
| 云浏览器（browserless） | 不作为主路径；`MOCODE_BROWSER_CDP` 已能把 attach 到任意远端端点当作逃生口 |

## 8. 风险

| 风险 | 缓解 |
|---|---|
| CDP 是**未版本化**的协议，字段可能随 Chrome 版本漂移 | 只用稳定域（`Page`/`Runtime`/`DOM`/`Input`/`Target`）里的稳定方法；L3 在真 Chrome 上跑 |
| 无头模式在旧 Chrome 上行为不同 | 用 `--headless=new`，并对 `--version` 探测结果择优 |
| 启动端口竞态（go-browser 的 `findFreePort` 是 TOCTOU） | 用 `--remote-debugging-port=0` + 读 stderr 里的 `DevTools listening on ws://...`，**不猜端口** |
| 子进程泄漏 | `Session.Close()` 先 `Browser.close`（CDP 优雅）再 `Process.Kill` 兜底；`runtime.SetFinalizer` 不够可靠，靠显式 Stop 服务 |

> 端口竞态那条是本设计相对 go-browser 的一个实质改进：它先 `net.Listen` 再 `Close` 再把端口交给浏览器，
> 中间窗口期内端口可能被别的进程抢走；而 `--remote-debugging-port=0` 让浏览器自己选，再把它打印出来的
> ws URL 读回来，没有竞态。


---

## 9. 实现记录与实测发现

三层测试全部落地：

| 层 | 文件 | 说明 |
|---|---|---|
| L1 单元 | `internal/core/browser/detect_test.go` | 假 host（注入 `GOOS`/`stat`/`readDir`/`lookPath`/`probe`），**三个平台的查找表都在 Windows 上跑**，含 snap / flatpak 分支 |
| L2 协议闭环 | `internal/core/browser/cdp_test.go` | 自建 **mock Chromium**（`httptest` + WebSocket，按真实 CDP 语义应答），断言命令序列与参数 |
| L3 真机 e2e | `internal/core/browser/e2e_test.go` | 真启动本机 Chrome，打开本地页面，验证 snapshot → fill → click → console → screenshot → close |
| 工具层 | `internal/core/tools/external/systems/browser/browser_test.go` | 参数/权限/枚举 + **工具 → manager → CDP → 真 Chrome** 全栈集成 |

浏览器包语句覆盖率 **79.6%**（含真机启动）。

### 9.1 闭环测试实际抓到的缺陷

这些都不是设计阶段能想到的，全部由真机测试暴露：

| # | 缺陷 | 后果 | 修复 |
|---|---|---|---|
| 1 | **Windows 上 `chrome.exe --version` 不打印版本，而是启动浏览器窗口并挂住** | 探测耗时 **59 秒**、**弹出浏览器窗口**（探测有副作用！） | Windows 改为从安装目录读取版本号目录名（`…\Applicationj.0.7977.83\`），**完全不 spawn 进程**；59s → **0.03s**，且新增测试断言 Windows 上 probe 调用次数为 0 |
| 2 | 版本解析把日志时间戳当版本 | 报出 `chrome=101849.824`（实为 `0914/102041.503`） | 只解析首行 + 正则限定 `\d{1,4}(?:\.\d{1,4}){1,3}` + 要求前面是空白 |
| 3 | 快照的文本白名单只含 `H1/P/SPAN…`，**普通 `<div>` 的文本不可见** | 页面把结果渲染进 `<div>` 是最常见模式，却看不到——网页调试直接失效 | 改为**两趟扫描**：先收交互元素，再按「叶子且有文本」收任意标签，保证按钮/输入框永远不会被文本挤掉 |
| 4 | `Close()` 消费了 `waitCh`，之后 `Alive()` 永远为真 | 会话状态撒谎，泄漏的浏览器进程看不出来 | 改用**只关闭一次的 `exited` channel**，`Alive()` 只做非消费式检测 |
| 5 | `kindFromPath` 只看文件名 | Chromium 的可执行文件也叫 `chrome.exe`，被误判为 Chrome | 改为匹配**全路径** |

第 1 条尤为关键：它同时是**性能问题**和**产品事故**（用户只是想知道装了没有，却弹出一个浏览器）。这正是「必须有真机闭环测试」的直接证据。

### 9.2 相对 go-browser 的实质改进

- **端口竞态**：用 `--remote-debugging-port=0` + 读回浏览器打印的 `DevTools listening on ws://…`，不猜端口（go-browser 先 `net.Listen` 再 `Close` 再交给浏览器，中间有 TOCTOU 窗口）。
- **console 真的可用**：走 CDP 的 `Runtime.consoleAPICalled` + `Runtime.exceptionThrown` 事件；go-browser 的 `setupConsoleHandler` 从未被调用，`console` 命令永远输出空。**未捕获异常单独处理**——它不会经过 `console.error`，否则页面真正的 bug 看不见。
- **fill 对框架有效**：用原型上的原生 setter 赋值再派发 `input`/`change`；`el.value = x` 对 React/Vue 无效（它们自己存了一份值），会「看起来填上了、提交的还是旧值」。
- **click 是真指针事件**：先 `scrollIntoView` 再在元素中心派发 `mouseMoved/mousePressed/mouseReleased`；`element.click()` 跳过指针事件序列，对拖拽与 canvas UI 会撒谎。
- **引用不可注入**：ref 是唯一进入脚本的模型输入，用严格形状校验（`e0`…`e9999`）而非转义；CSS selector 走 JSON 编码嵌入。

### 9.3 验收清单

```bash
go build ./...                    # ✅
go vet ./internal/core/...        # ✅
go run ./scripts/layercheck       # ✅ no upward dependency violations
go test ./...                     # ✅ 全仓通过
go test ./internal/core/browser/ -cover        # ✅ 79.6%
```

真机闭环（在有 Chrome 的机器上自动运行，无浏览器则 `t.Skip`）：

```bash
go test ./internal/core/browser/ -run TestE2E -v
go test ./internal/core/tools/external/systems/browser/ -run TestBrowser_ToolDrivesARealBrowser -v
```

`MOCODE_TEST_NO_BROWSER=1` 可强制跳过；`-short` 也会跳过。

---

## 10. P2 实现记录（2026-09-14）

§7 中的 P2 项已按 CDP 原生路线落地，go-browser 的能力缺口基本闭合：

| 能力 | 实现 | 引擎文件 |
|---|---|---|
| forward / reload | `Page.getNavigationHistory`（entry id 是**数字**，真机修正）+ `Page.reload` | `actions.go` |
| hover / dblclick / check / uncheck / select | 真指针事件（clickCount=2）；check 族点击后**复核状态**；select 按 value/label 匹配并派发 input/change | `actions.go` |
| 多标签页 | `Target.getTargets/createTarget/closeTarget` + `switchPage` 重挂（域名重启用、路由重装）；新标签先建空白页再 `Navigate`（避免错过 load 事件空等 15s） | `tabs.go` |
| 存储 | cookies 走**页面会话**的 `Network.*`（真机教训：浏览器级连接报 `'Network.setCookie' wasn't found`）；`setCookie` 返回 `{success:bool}` 对象；localStorage/sessionStorage 走 Evaluate | `storage.go` |
| state save/load | cookies（全部）+ 当前 origin 的 web storage 序列化 JSON；跨 origin 的条目显式跳过并报告 | `storage.go` |
| PDF | `Page.printToPDF`（headless） | `actions.go` |
| 网络日志 | `Network.requestWillBeSent/responseReceived/loadingFailed` → 环形缓冲（500 条，按 requestId 更新） | `network.go` |
| 路由拦截 | `Fetch.enable`（catch-all pattern + 本地裁决）；`requestPaused` 处理**必须异步**（readLoop 内同步 Call 会死锁）；block=`Fetch.failRequest`，mock=`Fetch.fulfillRequest`；glob 无 scheme 时隐式前缀通配 + 容忍尾随 query/hash | `network.go` |

**架构改动**：`cdpConn.On` 返回退订函数（事件句柄化，防多次导航/切标签后 handler 泄漏与串扰）；持久事件注册（console/network/fetch）移出 `attachPage`，每次连接只注册一次；`waitLoad` 按 sessionID 过滤，别的标签页 load 不会提前释放等待。

**测试**：L1（glob 编译、网络缓冲、cookie 渲染）+ L2（fake Chromium 扩展 handler：历史/标签/cookies/PDF/网络/路由全链路）+ L3（真机 Chrome：交互/历史/存储/state/网络/路由/PDF/标签，其中 `TestE2E_NetworkLogAndRoutes` 验证 block 后 fetch 失败、mock 后返回注入体、unroute 后恢复）。真机抓到的缺陷：history id 数字解码、cookie 命令会话作用域、setCookie 响应形状、Chrome target 列表**顺序不稳定**（测试须按 URL 定位而非下标断言）。

**仍未做**（显式）：tracing/video（需 ffmpeg）、Firefox/WebKit（非 CDP）、命名多会话、低级鼠标事件（mousemove/down/up/wheel）。浏览器包语句覆盖率 75.9%（含真机）。
