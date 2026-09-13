---
name: rust-backend
description: >-
  Use when 用 Rust 开发后端服务、API 或系统级组件：异步编程（tokio）、HTTP 客户端（reqwest）、
  序列化（serde）、错误处理（anyhow/thiserror）、结构化日志（tracing）、Web 抓取
  （scraper/html2md）、配置与数据库（config/sqlx），以及生产级测试与交付检查。
---

# Rust 后端开发

用 tokio + reqwest + serde + tracing 构建健壮的后端服务。核心原则：**可失败的操作一律返回 `Result`，错误带上文，日志用 `tracing`，绝不在生产代码里 `unwrap()`。**

## 依赖速查

| 任务 | crate | 说明 |
|------|-------|------|
| 异步运行时 | `tokio` | async/await、TCP/UDP、定时器 |
| HTTP 客户端 | `reqwest` | REST 调用、抓取（启用 `rustls-tls`） |
| 序列化 | `serde` + `serde_json` | JSON 解析、配置文件 |
| 错误处理 | `anyhow`（应用）/ `thiserror`（库） | 见下节 |
| 日志 | `tracing` + `tracing-subscriber` | 结构化日志、`EnvFilter` |
| CLI | `clap` | 参数解析 |
| 测试 | `tokio-test` + `wiremock` + `mockall` | 异步测试、HTTP mock |
| 数据库 | `sqlx` | 连接池、编译期校验 SQL |
| 配置 | `config` | 环境变量驱动的配置 |

```toml
[dependencies]
tokio = { version = "1", features = ["full"] }
reqwest = { version = "0.12", features = ["json", "rustls-tls"] }
serde = { version = "1", features = ["derive"] }
serde_json = "1"
anyhow = "1.0"
thiserror = "2.0"
tracing = "0.1"
tracing-subscriber = { version = "0.3", features = ["env-filter"] }
```

## 异步与 Tokio

```rust
use anyhow::Result;

#[tokio::main]
async fn main() -> Result<()> {
    tracing_subscriber::fmt()
        .with_env_filter(tracing_subscriber::EnvFilter::from_default_env())
        .init();
    if let Err(e) = run().await {
        tracing::error!("application error: {e:?}"); // {:?} 展开完整错误链
        std::process::exit(1);
    }
    Ok(())
}
```

- 并发：`tokio::spawn` 返回 `JoinHandle`，`.await??` 同时解包 JoinError 与业务错误。
- 通信：`mpsc::channel::<T>(cap)`（有界 channel 自带背压）、`oneshot::channel`（单次）。
- 共享状态：优先 `Arc<tokio::sync::Mutex<T>>`；持锁跨 `.await` 时保持临界区尽量短。

## HTTP 客户端（reqwest）

```rust
let client = reqwest::Client::builder()
    .timeout(std::time::Duration::from_secs(30))
    .user_agent("my-app/1.0")
    .build()?;

let user: User = client.get(url).send().await?.error_for_status()?.json().await?;
```

- **复用客户端**：`Client` 内部维护连接池，构造一次跨请求复用，不要每请求 `Client::new()`。
- 统一 `.error_for_status()?`，否则 4xx/5xx 会被当成正常响应继续解析。
- 重试：只对幂等请求 + 可重试错误（超时/5xx）做指数退避，如 `sleep(Duration::from_millis(100 * (1 << attempt)))`，并设上限与 jitter。

## 序列化（serde）

```rust
#[derive(Debug, Serialize, Deserialize)]
struct Config {
    name: String,
    #[serde(default)]                              // 缺字段时用 Default
    features: Vec<String>,
    #[serde(rename = "poolSize", default = "default_pool_size")]
    pool_size: u32,
}
```

- 字段名与 JSON 不一致用 `#[serde(rename)]`；可选字段用 `#[serde(default)]` 或 `Option<T>`。
- 时间统一 RFC3339：用 `serialize_with` / `deserialize_with` 处理 `chrono::DateTime<Utc>`。

## 错误处理

- **应用层用 `anyhow`**：`with_context(|| format!("read {path}"))?` 给错误加上下文；日志用 `{:?}` 打印完整链。
- **库/公共 API 用 `thiserror`**：定义 `enum AppError`，用 `#[from]` 自动转换，暴露 `pub type Result<T>`。

```rust
#[derive(thiserror::Error, Debug)]
pub enum AppError {
    #[error("io error: {0}")]
    Io(#[from] std::io::Error),
    #[error("user not found: {id}")]
    UserNotFound { id: u32 },
}
```

- 生产代码禁止 `unwrap()`/`expect()`；用 `?` 或显式处理。
- 不要用 `Box<dyn Error>` 作为库的公共错误类型。

## 日志与 tracing

```rust
#[tracing::instrument(skip(data), fields(len = data.len()))]
async fn process(data: &[u8]) -> anyhow::Result<String> { /* ... */ }
```

- 入口初始化一次 `tracing_subscriber`，用 `EnvFilter`（如 `RUST_LOG=my_app=info`）。
- 用 `info!/warn!/error!/debug!` 输出结构化字段，避免字符串拼接。
- **切勿记录** token、密钥、密码等敏感数据（见 `security-and-hardening`）。

## 配置与数据库

```rust
#[derive(Debug, Deserialize)]
struct AppConfig { database_url: String, #[serde(default = "default_port")] port: u16 }
// config::Config::builder().add_source(Environment::with_prefix("APP")).build()?.try_deserialize()?
```

```rust
let pool = PgPoolOptions::new().max_connections(10)
    .acquire_timeout(Duration::from_secs(30))
    .connect(&database_url).await?;
```

## Web 抓取（scraper / html2md）

- `scraper::{Html, Selector}` 解析 DOM：`Html::parse_document(html)` + `document.select(&selector)`。
- 抽取链接：`element.value().attr("href")`；正文转 Markdown 用 `html2md::parse_html(html)`。
- Selector 构造可能失败，缓存/提前 `Selector::parse` 并处理错误，别在循环里重复解析。

## 测试

```rust
#[tokio::test]
async fn parses_config() { /* serde_json::from_str(...) */ }

// HTTP mock：wiremock 起本地 server
// Mock::given(method("GET")).and(path("/users/1"))
//     .respond_with(ResponseTemplate::new(200).set_body_string(r#"{"id":1}"#))
//     .mount(&mock_server).await;
```

覆盖正常路径 + 错误路径 + 边界；异步测试用 `#[tokio::test]`。

## QA 清单

- [ ] 可失败操作全部返回 `Result`，错误有上下文
- [ ] 生产路径无 `unwrap()`/`expect()`
- [ ] 日志级别合理且不含敏感数据
- [ ] `cargo fmt` 与 `cargo clippy -- -D warnings` 通过
- [ ] `cargo test` 覆盖正常/错误路径
- [ ] `cargo audit` 无已知漏洞
- [ ] 提交前按 `code-review-and-quality` 走一遍评审

## 排错表

| 现象 | 处理 |
|------|------|
| async fn 不编译 | 返回类型用 `async fn`/`impl Future`；确保在 tokio 上下文内 `.await` |
| reqwest TLS 报错 | 启用 `rustls-tls`（或 `native-tls`）feature |
| serde 反序列化失败 | 字段名不匹配 → `#[serde(rename)]`；可选 → `#[serde(default)]` |
| tokio 运行时 panic | `#[tokio::main]` 缺失，或手动 `Runtime::new()` 未正确启动 |
| 结构体生命周期报错 | 改用 `Arc` 或 owned 类型 |
| 编译慢 | `sccache`、精简依赖 feature、拆分 crate |

## 相关技能

- `security-and-hardening`：输入校验、密钥管理、依赖审计。
- `code-review-and-quality`：合并前的质量门禁。
