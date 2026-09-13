---
name: tauri
description: >-
  Use when 用 Tauri 构建轻量跨平台桌面/移动应用：系统 WebView + Rust 后端、`#[tauri::command]`
  IPC 与事件、capabilities 权限、官方插件（fs/dialog/shell/notification/store/updater）、
  `tauri build` 打包分发与自动更新。Trigger: tauri, rust desktop, system webview, tauri commands, tauri plugins.
---

# Tauri 跨平台应用

Tauri 用系统 WebView 渲染前端、Rust 处理后端，取代打包 Chromium 的 Electron：产物 <10MB、内存 30–80MB，具备能力（capability）安全模型、类型安全 IPC 与原生插件生态。核心分工：**UI 与交互放前端框架，系统访问与重计算放 Rust。**

## 架构

- 前端：任意 Web 框架（React/Vue/Svelte/Solid），构建后由 WebView 加载；前端框架无关，性能与规范分别参考 `vercel-react-best-practices`、`web-design-guidelines`。
- 后端：Rust crate（`src-tauri/`），通过命令（command）与事件（event）暴露能力。
- 前端调用 v2 API：`import { invoke } from "@tauri-apps/api/core"`（v1 的 `@tauri-apps/api/tauri` 已废弃）。

## IPC：命令与事件

```rust
// src-tauri/src/lib.rs
#[tauri::command]
async fn read_note(id: String, state: tauri::State<'_, AppState>) -> Result<String, String> {
    Ok(state.store.get(&id).ok_or("not found")?.to_string())
}

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    tauri::Builder::default()
        .manage(AppState::new())
        .invoke_handler(tauri::generate_handler![read_note])
        .run(tauri::generate_context!())
        .expect("error while running tauri application");
}
```

```ts
import { invoke } from "@tauri-apps/api/core";
const note = await invoke<string>("read_note", { id: "1" });
```

- **命令**用于请求-响应，**事件**用于后端→前端推送，两者不要混用。
- Rust 端用 `Result<T, String>`，`Err` 会在 JS 端变成 rejected promise。
- 共享可变状态用 `tauri::State<Mutex<T>>`（或 `tokio::sync::Mutex`），Rust 保证线程安全。

```rust
use tauri::{Emitter, Manager};
app.emit("metrics", payload)?;                                       // 全局广播
app.get_webview_window("main").unwrap().emit("metrics", payload)?;  // 定向
```

## 权限与安全

- 前端可访问的 API 必须在 `src-tauri/capabilities/*.json` 中按最小权限声明，未声明即不可用。
- 配置 CSP；高敏感场景使用 isolation pattern 做沙箱隔离。
- 插件权限同样要显式加入 capabilities（如 `fs:allow-read-text-file` 加对应路径 scope）。

## 官方插件

| 插件 | 用途 |
|------|------|
| `@tauri-apps/plugin-fs` | 读写文件（需配 scope） |
| `@tauri-apps/plugin-dialog` | 原生打开/保存对话框 |
| `@tauri-apps/plugin-shell` | 执行命令（严格白名单） |
| `@tauri-apps/plugin-notification` | 系统通知 |
| `@tauri-apps/plugin-store` | 持久化键值（跨版本存活，优于 `localStorage`） |
| `@tauri-apps/plugin-updater` | 自动更新（签名校验） |
| `@tauri-apps/plugin-tray` | 系统托盘 |

- 安装：`npm run tauri add <plugin>`（或手动加 Rust crate + JS 包 + capabilities）。
- `plugin-store` 优于 `localStorage`：后者在应用更新或清理缓存时可能丢失。

## 构建与分发

```bash
cargo tauri build          # 或 npm run tauri build
# Windows: NSIS/MSI   macOS: DMG(.app)   Linux: AppImage/deb
```

- 分发前做代码签名：Windows 代码签名证书、macOS 签名 + notarization。
- 自动更新用 `plugin-updater` + GitHub Releases 或自建服务，**必须**校验签名防篡改。
- 移动端（Android/iOS）见 `rust-android-apk`：Tauri v2 全流程、NDK/JDK 约束、模拟器、签名与验证。

## 踩坑表

| 现象 | 处理 |
|------|------|
| `invoke` 报 command not found | 命令未注册进 `generate_handler!`，或名称/参数大小写不匹配 |
| 插件 API 权限被拒 | capabilities 未声明对应 permission/scope |
| 前端状态丢失 | 改用 `plugin-store`，不要依赖 `localStorage` |
| 构建产物异常大 | 检查 debug/release profile、未用 feature、assets 是否误打包 |
| 更新后旧数据消失 | 持久化到 `plugin-store`/应用数据目录，而非 WebView 缓存 |
| 移动端初始化失败 | 见 `rust-android-apk`（JDK ≤21、`NDK_HOME`、identifier 先行） |

## 相关技能

- `rust-backend`：Tauri 后端的 Rust 工程规范（错误处理、tracing、测试）。
- `rust-android-apk`：Tauri v2 移动端打包、模拟器、签名与验证。
- `vercel-react-best-practices` / `web-design-guidelines`：前端性能与界面规范。
