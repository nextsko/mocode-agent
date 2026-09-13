---
name: rig-core-llm-integration
description: >-
  Use when 需要把 rig-core crate（v0.40）集成进 Rust / Tauri 应用，经 OpenAI 兼容端点流式输出 LLM chat completion（opencode-go、one-api、vllm、ollama 桥、Azure OpenAI 等）。当用户提到 rig、rig-core、streaming LLM output in Rust、Tauri 流式 command、自定义 base_url provider、修复 rig 的 404、`stream_chat` / `completions_api` / import 报错时使用。提炼自一次真实跑通的 rig-core 0.40 + opencode-go + Tauri 2.x 集成。
---

# rig-core LLM 集成（0.40 + openai-compat + Tauri streaming）

## 何时使用

- 要把 LLM 流式回复集成进 Rust / Tauri 应用（关键词 rig、rig-core、streaming、token streaming）。
- 接 OpenAI 兼容端点（opencode-go、one-api、vllm、ollama openai 桥、deepseek、moonshot、zhipu、Azure OpenAI 等），使用自定义 `base_url`。
- 遇到 rig 报错：`unresolved import`、`stream_chat not found`、HTTP 404、`pub(crate)` 引用失败、`MultiTurnStreamItem` 找不到。
- 需要在 Tauri command 里把 token 增量推给前端。
- 处理 API key 加载（`.env.local`）或密钥脱敏。

## 版本陷阱（先验证）

**rig-core 当前最新是 0.40.0**（用 `cargo search rig-core` 确认）。网上 99% 的教程和 LLM 记忆停留在 0.10 / 0.11，那时 crate 名还叫 `rig`，API 完全不同。

**第一步永远是 `cargo search rig-core` 锁定版本**，然后以本地 registry 源码为权威（见 pitfall #8），不要信博客。

## Workflow

### 1. Cargo.toml

```toml
[dependencies]
rig-core = "0.40"
tokio = { version = "1", features = ["full"] }
futures = "0.3"          # StreamExt
anyhow = "1"
serde = { version = "1", features = ["derive"] }
dotenvy = "0.15"
```

首次 `cargo build` 约 1 分钟（243 deps）。aws-lc-rs 1.17.3 自带 builder，**无需安装 NASM**。

### 2. 加载 `.env.local`（不是 `.env`）

```rust
const MANIFEST: &str = env!("CARGO_MANIFEST_DIR");
let env_path = std::path::Path::new(MANIFEST).join(".env.local");
let _ = dotenvy::from_path(&env_path);   // ⚠️ dotenv() 不读 .env.local
let api_key = std::env::var("MY_API_KEY")?;
```

### 3. 构造客户端（关键：`.completions_api()`）

```rust
use rig_core::{client::CompletionClient, providers::openai::Client as OpenAIResponsesClient};

let client = OpenAIResponsesClient::builder()
    .api_key(api_key)
    .base_url(base_url)        // 例：https://opencode.ai/zen/go/v1
    .build()?                  // 返回 Result，要 ?
    .completions_api();        // ⚠️ 必须：切到 POST /chat/completions
```

### 4. 流式拉取

```rust
use rig_core::agent::MultiTurnStreamItem;
use rig_core::completion::Message;
use rig_core::streaming::{StreamedAssistantContent, StreamingChat};
use futures::StreamExt;

let agent = client.agent("deepseek-v4-flash").build();
let history: Vec<Message> = vec![];        // ⚠️ 类型必须显式
let mut stream = agent.stream_chat(prompt, history).await;  // await 后直接是 stream

while let Some(item) = stream.next().await {
    match item {
        Ok(MultiTurnStreamItem::StreamAssistantItem(StreamedAssistantContent::Text(t))) => {
            // t.text 是 delta 字符串，emit 出去
        }
        Ok(MultiTurnStreamItem::FinalResponse(_)) => break,
        Ok(_) => {}
        Err(e) => { /* redact 后再返回 */ }
    }
}
```

### 5. Tauri command 包装（流推到前端）

- Rust 侧字段 `request_id`；JS 传 `requestId`（Tauri 自动 camelCase 转换）。
- 事件用 `#[serde(tag = "kind")]` 区分 `delta` / `done` / `error`。
- 用全局 channel + `request_id` 做多路复用（multiplex）。

## 常见陷阱

1. **openai-compat 网关返回 404** → rig 默认 POST `/responses`（OpenAI Responses API），第三方网关通常只实现 `/chat/completions`。**必须加 `.completions_api()`** 切到 CompletionsClient。诊断：`curl -o /dev/null -w "%{http_code}" $BASE/chat/completions -X POST ...`。
2. **`dotenvy::dotenv()` 读不到 key** → 它只找 `.env`，不找 `.env.local`。用 `dotenvy::from_path(CARGO_MANIFEST_DIR/.env.local)`。不要用编译期 `dotenv!` 宏（key 会被编进二进制）。
3. **`stream_chat not found`** → `stream_chat` 来自 `StreamingChat` trait（不是 `StreamingPrompt`，后者只有 `stream_prompt`）。`use rig_core::streaming::StreamingChat;`。
4. **给 `stream_chat(...).await` 包 `match Ok/Err`** → 错。它不返回 `Result`，await 后直接是 stream；错误在 `while let Some(item) = stream.next().await` 的 `Err(e)` 分支处理。
5. **`use rig_core::agent::prompt_request::streaming::MultiTurnStreamItem` 报 `pub(crate)`** → 嵌套路径是 `pub(crate)`。用顶层 re-export：`use rig_core::agent::MultiTurnStreamItem;`。
6. **Message 构造** → 用 `Message::system/assistant/user(&str)`，参数 `impl Into<String>`，三者都存在。不要找 `Message::new`。
7. **`history` 类型推断失败** → `let history: Vec<Message> = vec![];` 必须显式标注，否则 `stream_chat` 泛型推断不出来。
8. **docs.rs 失效 / 网上例子写 `use rig::...`** → 那是 0.10 的旧 crate 名，0.40 是 `rig_core`。**以本地 registry 源码为准**：
   ```bash
   SRC=~/.cargo/registry/src/index.crates.io-*/rig-core-0.40.0/src
   rg 'fn stream_chat' $SRC
   rg 'enum MultiTurnStreamItem' $SRC -A 10
   rg 'pub(crate)' $SRC/agent        # 这些路径不能跨 crate 用
   ```
9. **错误消息泄露 API key** → reqwest/hyper 错误体常含 `Authorization: Bearer sk-xxx`。emit / return 前必须 `s.replace(key, "***")`。
10. **Tauri 参数 camelCase** → Rust `request_id`，JS 必须 `requestId`（Tauri 自动转换）；JS 侧写 `snake_case` 不识别。

## 验证

```bash
# 1. 编译通过
cd src-tauri && cargo check

# 2. live LLM smoke（标 #[ignore] 避免 CI 花钱）
cargo test --test llm_smoke -- --ignored
# 期望：deltas=2, chars=4, reply_prefix="PONG" → 真实流式回复

# 3. 端点连通性（先 curl，不写代码）
curl -s -o /dev/null -w "%{http_code}" $BASE/models          # 应 200
curl -s -o /dev/null -w "%{http_code}" -X POST $BASE/chat/completions \
  -H "Authorization: Bearer $K" -H "Content-Type: application/json" \
  -d '{"model":"deepseek-v4-flash","messages":[{"role":"user","content":"hi"}],"stream":true}'
# 应 200，且 body 含 `data:` SSE 行
```

任何一步不是预期值，**先停在该步定位**，不要往下走。

## 相关技能

- `systematic-debugging`：遇到诡异 404 / 流断 / 类型推断失败时，按其方法论隔离变量。
- `security-and-hardening`：API key 的更深一层硬化（vault、keyring、运行时注入）。
- `rust-android-apk`：若该 LLM 集成要打包进 Tauri Android APK，用其处理交叉编译与打包。
