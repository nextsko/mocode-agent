---
name: red-team-animation-verification
description: >-
  Use when「能跑」还不够，需要证明 LLM 聊天 / 交互式 UI 及其动画「打不坏、且真的在动」：
  对聊天集成做对抗测试（XSS、prompt injection、超长输入、Unicode/RTL、流式卸载、
  监听器泄漏），用 ffmpeg 逐帧差分验证 CSS 动画真的在播放（typewriter、spinner、
  bubble 入场、hover），用视觉模型做 UI 终端验证，并守护 LOCKED testid 契约。
---

# Red Team + Animation Verification

适用场景：「能跑吗」不是终点，要证明「它打不坏，而且用户看到的是**真的动画**，不是假象」。
三层缺一不可：**对抗式 red team**（逻辑/安全）、**逐帧差分动画验证**（运动）、**视觉模型终端验证**（画面真相）。

相关：`playwright-cli`（录制视频 / 驱动 Chromium）、`ui-replication`（验收流程）、`screenshot-to-ui`（视觉核对）。

## 何时用

- 聊天/LLM 功能落地，要证明它不是 XSS 可注入、超长输入不卡死、不泄漏监听器、流式过程中卸载不炸 → **red team 攻击矩阵**。
- 新增/改动了 CSS 动画（typewriter、spinner、fade-in、hover）→ **动画验证协议**。
- 安装 APK / web 构建后要证明 UI 真的渲染了（不是白屏 splash）→ **视觉模型终端验证**。
- 动了 hook/组件，要确认没破坏自动化锚点契约 → **LOCKED testid 守护**。

**不适用**：跨浏览器兼容（只跑 Chromium）、性能基准（帧差分不是 profiling）、纯函数单测。

## Red team 攻击矩阵（12 例，当 checklist 跑）

每例：**全新 Playwright page**、仅 Chromium、`domcontentloaded` + `waitForSelector`；失败 → 截图到 `docs/acceptance/red-team-llm-{N}.png`。

| # | 向量 | 关键断言 |
|---|---|---|
| R1 | 空输入 | `sendDisabled=true userDelta=0` |
| R2 | 纯空白 | `trim()` 拒绝 + 按钮禁用 |
| R3 | Enter 发送 | `msg-user-0` + `msg-assistant-1` testid 出现 |
| R4 | Shift+Enter 换行 | textarea 含 `\n`，不产生新消息 |
| R5 | 快速连发两次 | 2 个 user bubble，0 监听器泄漏（看 console 警告） |
| R6 | **XSS** `<img src=x onerror=alert(1)>` | 按文本渲染，**0 个 `<img>` 节点**，`alertFired=false` |
| R7 | 路径穿越 + prompt injection | 不崩溃 |
| R8 | 10000 字符输入 | 填充 ~19ms、发送 ~63ms，不卡死 |
| R9 | Unicode+emoji+RTL `你好 🌍 שלום` | 字面渲染完整 |
| R10 | 流式中途卸载 | useEffect cleanup，0 条 React 警告 |
| R11 | `msg-{role}-{i}` testid 稳定性 | 3 轮后 testid 齐全 |
| R12 | `chat-input/chat-send/chat-disclaimer` LOCKED | 始终存在 |

**输出协议**：每例结束打印 `[R6 xss] PASS — onerror rendered as text`；套件结束打印 `---SUMMARY--- PASS=X FAIL=Y TOTAL=12`，便于 grep。

**断言纪律**：断言要带数字（`expect(fillMs).toBeLessThan(200)`），不要用 `Infinity`；监听 `page.on('console')` 抓警告，不能只看 DOM。

## 动画验证协议（逐帧差分，**不是**看文件大小）

**永远不要**因为 `.webm` 存在或体积合理就判定动画通过。文件大小是假代理——静止帧可以 163KB，编码噪声也能骗过体积检查。唯一可信信号是**逐帧像素变化**。

Pipeline：

1. Playwright `recordVideo` → `chat-{name}.webm`
2. `ffmpeg` 在已知时间点抽帧（如 `t=0,0.5,1,1.5,2s`）
3. 计算 ROI 相邻帧像素差：`diff% = Σ|a-b| / (w·h·255) · 100`
4. 全部 `diff% = 0` → **FAIL**（静止，动画没跑）
5. `diff%` 序列形状符合预期 → **PASS**

校准基线（阈值参考）：

| 动画 | 时长 | diff% 序列 | 通过判据 |
|---|---|---|---|
| typewriter | 2.08s | 0.55→0.16→0.13→0.06→0.02 | 严格递减 |
| send-spinner | 6.12s | 3.4→3.6→3.7→**15.7** | 低抖动 + 一次 >15 跳变（图标切换） |
| bubble-entrance | 6.52s | 5.5→0.8→0.4 | 首帧 >1、末帧 <0.5（衰减） |
| codeblock-hover | 1.96s | **40.0**→0→22.2 | 至少一次 >15 跳变（hover 触发） |

该协议在源头跑时抓到过真 bug：`<Message>` 缺入场 class → 首帧 diff% 仅 5.5（真实 fade-in 应 >20）→ 复用项目已有 `mavis-fade-in-up` keyframe 加 `animate-fade-in-up`（自动感知 reduced-motion）→ 重建 APK → 视觉模型终端验证 → PASS。**只看文件大小会完全漏掉。**

## 视觉模型 UI 终端验证

安装构建或录屏后，**不要**用文件大小或 DOM testid 宣称「UI 渲染了」。用视觉模型读图，给强提示词：

```text
images_path: <absolute path or URL>
query_prompt: "Describe in detail what's visible on this Android screen.
               1. Is there a working app UI, or is it blank/splash/error?
               2. List top-level components (TopAppBar, BottomNav tabs, FAB, lists).
               3. Quote any legible headings.
               4. Any rendering glitches (overlap, cut-off, missing icons)?"
role: apk        # ← Android 优化的 system prompt，关键参数
format: markdown
```

- **`role: apk` 是关键**：注入 Android UI 感知的 system prompt，能正确识别状态栏、导航栏、WebView；默认 role 会误判。
- 强提示词返回组件级细节（“TopAppBar 显示 'MAVIS' + 4-tab BottomNav + FAB”），可与设计稿逐项比对；弱提示词（“这是什么”）只会回“一部手机屏幕”，无用。

## LOCKED testid 守护（每次改动前后）

这些 testid 是所有 verify/red-team 脚本依赖的契约：
`chat-input`（恰好 1）、`chat-send`（恰好 1）、`chat-disclaimer`（恰好 1）、`msg-user-{i}` / `msg-assistant-{i}`（i 从 0 起，无空洞）。

改动聊天组件前后跑：

```bash
rg --count 'data-testid="chat-input"'      src/   # expect 1
rg --count 'data-testid="chat-send"'       src/   # expect 1
rg --count 'data-testid="chat-disclaimer"' src/   # expect 1
```

计数变了就是破坏契约——**改源码，不改测试**。改 testid 名的代价比改 hook 高一个数量级。

## 常见坑

- **`waitUntil: 'networkidle'` 在 Rsbuild/Vite HMR 下永不 resolve**：HMR websocket 长连接保活，`networkidle` 等 30s 后整套超时。用 `domcontentloaded` + 显式 `waitForSelector('[data-testid="..."]')`，并**总是显式传 timeout**（静态 5s / 流式 15s / 动画 3s），别用默认 30s。
- **冷启动截图陷阱**：截太早只会拿到 ~21KB 白屏 splash。等 5–10s 让 WebView 真正渲染；更好的是轮询直到连续两次 `screencap` 字节数差 <5%。“94KB 截图=有内容”是经典假阳性——渐变白屏也能 94KB。
- **用文件大小当「动画跑了 / UI 渲染了」的代理**：两者都是假代理。动画用帧差分，UI 用视觉模型。
- **red team 各例复用同一个 Playwright page**：监听器泄漏检测（R5）会污染 XSS 检查（R6）。**每例新 page**。
- **用 sleep 代替等待**：`waitForSelector` 更快更可信；`sleep(2000)` 只会掩盖 flakiness。
- **跨引擎 red team**：锁定 Chromium——HMR 在那儿最稳，要的是可复现而不是浏览器兼容。
- **重命名 LOCKED testid**：一步弄坏所有 `verify-*.mjs` 与 `red-team-*.mjs`。改 hook，永不改 testid。

## 反模式

- 用「文件存在 / 体积合理 / DOM 有 testid」代替真正的像素与视觉证据。
- 断言无数字、无 timeout、用默认 30s。
- 发现 flat diff% 或 red team 失败后直接改测试掩盖问题，而不是 root-cause。
- 一次跑完所有 red team 用例却共用状态（page、listener、localStorage）。
