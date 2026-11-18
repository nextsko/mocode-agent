# 08 · 视口可控与视频录制（browser 工具）

> 目标：让 browser 工具能**按指定尺寸布局页面**（含 2500x1600 设计画布），并能把页面**录成视频文件**。
> 状态：已实现（核心层 + 工具层 + 测试）

---

## 1. 起点：浏览器能力里缺的两块

在 v0.27.0 之前，browser 工具（见 [07-native-browser-runtime.md](07-native-browser-runtime.md)）的能力边界是：

| 能力 | 现状 | 缺口 |
|---|---|---|
| 视口 | 完全由浏览器窗口决定 | 想看 2500x1600 的布局，只能改操作系统窗口大小；attach 到用户已开的浏览器时这条路根本不存在 |
| 录制 | 无 | `screenshot` 只有一张静态 PNG；滚动、hover、过渡、动效过程都留不下证据 |

这两件事都属于「agent 需要看见的东西」而不是「agent 需要写的东西」，所以它们应该在浏览器运行时里解决，而不是让每个任务各自想办法（改窗口、装 ffmpeg、外挂截图工具）。

---

## 2. 设计决策

### 2.1 视口 = CDP 设备度量覆盖，而不是改窗口

用 `Emulation.setDeviceMetricsOverride`：页面按指定的 CSS 像素布局，**不动操作系统窗口**。

| 备选 | 为什么不用 |
|---|---|
| `Browser.setWindowBounds` | headless 没有窗口；有头时会把用户的窗口真的拽大 |
| 启动参数 `--window-size` | attach 模式用不上；会话中途也改不了 |
| 截图后裁剪 | 页面是按视口布局的，裁剪改变不了布局 |

两个容易忽略的细节：

1. **emulation 绑定的是 page session**，不是浏览器。切标签页后要重放覆盖，否则新页会退回窗口尺寸 —— 见 `session.go` 的 `applyViewport`。
2. **`screenWidth` / `screenHeight` 一起设**，让 `position: fixed` 元素与媒体查询看到与视口一致的屏幕，而不是真实窗口。

尺寸上限 10000、缩放 0.1–8：把参数错误挡在浏览器之前，而不是让 Chrome 用一个荒谬的尺寸分配一帧像素。

### 2.2 2500x1600 是显式预设，不是魔法默认值

设计评审用的画布尺寸是**需求**，不是隐含默认：所以它是 `corebrowser.DesignCanvas()` 这个具名预设，工具侧对应 `op=design`。

如果把 2500x1600 设成「不传参数时的默认」，那么「忘了设置」和「故意设置」就再也分不开了——而这两者对截图证据的可信度影响完全不同。

### 2.3 录制 = screencast 帧 + MJPEG AVI

Chrome 的 `Page.startScreencast` 持续推送 JPEG 帧。关键约束有两条：

1. **帧必须逐帧 ack**（`Page.screencastFrameAck`），否则浏览器发一帧就停。
2. **ack 必须在事件处理器里发出去，但不能等回复** —— 回复正是由正在执行这个处理器的读循环派发的，等回复就是死锁。

因此新增 `cdpConn.Notify`：只写不等（`Call` 的孪生兄弟）。

容器选 **AVI/MJPEG**，理由是它是唯一能**原样携带 JPEG 帧**的视频容器：不需要编解码器、不需要外部二进制、`CGO_ENABLED=0` 依然成立、整个实现只有几十行。代价是体积大、浏览器不能直接播；需要 MP4 时 `ffmpeg -i take.avi take.mp4` 一步到位。

单次录制上限 600 帧（10fps 约一分钟）：screencast 是推送式的，不设上限就是「带视频文件的内存泄漏」。

---

## 3. 落地位置

| 文件 | 作用 |
|---|---|
| `internal/core/browser/viewport.go` | `Viewport` 类型、2500x1600 预设、设置/清除/查询、CDP 参数构造 |
| `internal/core/browser/recorder.go` | screencast 录制器：订阅帧、ack、帧数上限、停止与取帧 |
| `internal/core/browser/video.go` | JPEG 尺寸解析 + MJPEG AVI 写出（RIFF / hdrl / movi / idx1 索引） |
| `internal/core/browser/cdp.go` | 新增 `Notify`：只发不等的命令，用于帧 ack |
| `internal/core/browser/session.go` | 记住视口与录制器；切标签页时重放视口 |
| `internal/core/tools/external/systems/browser/browser_viewport_record.go` | 工具层：`viewport` 与 `record` 两个 action |
| `internal/core/tools/external/systems/browser/browser.go` | 新参数、schema、动作分发、`launch` 时直接设定视口 |

---

## 4. 工具用法

```
viewport: op=set    + width/height (+ scale, mobile)   按指定尺寸布局
viewport: op=design                                    2500x1600 设计画布
viewport: op=reset                                     回到浏览器窗口尺寸
viewport: op=get                                       报告当前尺寸

record:   op=start  (+ fps, quality, max_width, max_height)   开始录制
record:   op=stop   (+ path)                                结束并写出视频
record:   op=status                                        查看已录帧数
```

`launch` 也接受 `width`/`height`，让第一个页面一开始就在目标尺寸上。

---

## 5. 测试

| 测试 | 断言 |
|---|---|
| `viewport_test.go` | CDP 参数正确（2500x1600@1x）、非法尺寸在到达浏览器前被拒、清除后状态归零 |
| `recorder_test.go` | 帧被抓到、**每帧都被 ack**、停止后帧内容与时间戳正确 |
| `video_test.go` | 写出的 AVI 能被逐块解析（hdrl/movi/idx1）、帧原样保存、索引指向帧 |
| `browser_test.go` | schema 暴露两个新 action 与新参数；无会话时是软错误 |

录制器的测试全部跑在 `fakeChromium` 上：没有浏览器也能回归「协议理解」这件事，CI 在没装浏览器的机器上保持绿色。

---

## 6. 边界与不做的事

- **AVI/MJPEG 不是 MP4**：不引入编解码依赖是这个仓库的硬约束（`CGO_ENABLED=0`）。要 MP4 请用 ffmpeg 转一次。
- **录制只覆盖主 frame**：iframe 内部与浏览器原生弹窗（alert）不会出现在帧里（与上游 Playwright 的限制一致）。
- **不录制音频**：CDP screencast 没有音频轨。
- **不自动开视频编辑器**：录制只是产出证据文件，不接管任何播放流程。

---

## 7. 核验清单

```bash
go build -buildvcs=false ./...
go test ./internal/core/browser/... ./internal/core/tools/external/systems/browser/... -count=1
gofmt -l internal | grep -v '^$'
```

人工验收：

1. `launch` 后 `viewport` `op=design` → `screenshot` 得到 2500x1600 的图。
2. `record` `op=start` → 操作页面 → `op=stop` → 得到可播放的 `.avi`。
3. 切到另一个标签页，视口仍然是 2500x1600。