---
name: bun
description: >-
  Use when 使用 Bun 作为一体化 JavaScript/TypeScript 运行时、包管理器、打包器或测试器，包括用
  Bun.serve() 构建 HTTP/WebSocket 服务、bun install/add/remove 管理依赖、bun build 打包、
  bun test 跑测试、Bun.file/Bun.write 文件 I/O、Bun.password 做认证、bun:sqlite 嵌入式存储，
  以及从 Node.js 迁移到 Bun。
---

# Bun 一体化运行时

Bun 用单个二进制替代 Node.js + npm + webpack + Jest：原生 TypeScript/TSX、`Bun.serve()` 高性能 HTTP、极快的包管理器、打包器与 Jest 兼容测试器。核心原则：**能用一个内置 API 解决的，就不要引入 npm 依赖。**

## 命令速查

| 命令 | 用途 |
|------|------|
| `bun run <script>` | 跑 package.json 脚本（也可 `bun <file.ts>` 直接执行） |
| `bun install` / `bun i` | 安装依赖（10–30x 快于 npm） |
| `bun add <pkg>` / `bun add -d <pkg>` | 加依赖 / devDependency |
| `bun remove <pkg>` | 移除依赖 |
| `bun x <pkg>` / `bunx <pkg>` | 执行包二进制，无需安装 |
| `bun test` | 运行测试（`--watch`、`--coverage`） |
| `bun build ./src/index.ts --outdir dist` | 打包 |
| `bun --watch run dev` | 文件变更自动重启 |

- 锁定文件用文本格式 `bun.lock`（比二进制 lockfile 更适合 git diff），提交进仓库。
- `.env` / `.env.local` 自动加载，**不要**再装 `dotenv`。
- `bun` 可作为 `node` 的 drop-in，`package.json` 无需改动。

## HTTP 服务：Bun.serve()

```ts
Bun.serve({
  port: 3000,
  async fetch(req) {
    const url = new URL(req.url);
    if (url.pathname === "/api/users" && req.method === "POST") {
      const body = await req.json();
      return Response.json({ ok: true, body });
    }
    return new Response("Not Found", { status: 404 });
  },
  websocket: {
    open(ws) {},
    message(ws, msg) { ws.send(`echo: ${msg}`); },
    close(ws) {},
  },
});
```

- 返回标准 `Response`/`Request`（Web 标准），流式响应用 `ReadableStream`。
- WebSocket：在 `fetch` 中调用 `server.upgrade(req)` 完成升级，再于 `websocket` 处理器收发消息。
- 单进程 100K+ req/s；新服务优先用它，而不是 Express on Bun。

## 文件与内置工具

```ts
const file = Bun.file("data.json");      // 惰性引用
if (await file.exists()) await file.json();
await Bun.write("out.txt", "hello");     // 自动建目录，支持 Response/Blob
const glob = new Bun.Glob("**/*.ts");
for await (const path of glob.scan(".")) console.log(path);
```

- `Bun.file()` / `Bun.write()` 比 Node `fs` 快很多，优先使用。
- 密码哈希用内置 `Bun.password.hash()` / `Bun.password.verify()`（bcrypt/argon2），免装原生依赖。
- 嵌入式数据库用 `bun:sqlite`：`new Database("app.db")` + `db.query(...).all()` / `db.run(...)`。

## 打包：Bun.build()

```ts
await Bun.build({
  entrypoints: ["./src/index.tsx"],
  outdir: "./dist",
  target: "browser",   // "browser" | "bun" | "node"
  splitting: true,
  minify: true,
  sourcemap: "linked",
});
```

## 测试：bun test

```ts
import { test, expect, describe } from "bun:test";

describe("math", () => {
  test("adds", () => { expect(1 + 1).toBe(2); });
});
// mock.module("./db", () => ({ get: () => "fake" }));
```

- Jest 兼容 API（`describe`/`it`/`expect`/快照/`mock.module`），现有 Jest 测试通常可直接跑。
- `--coverage` 出覆盖率，`--watch` 增量重跑，执行速度远快于 Jest。

## 从 Node.js 迁移

1. CI 与本地把 `npm install` 换成 `bun install`，提交 `bun.lock`。
2. `package.json` 脚本里的 `node` 换成 `bun`（或 `bun run`）。
3. 删除 `dotenv`（内置 .env）、`bcrypt`/`argon2`（用 `Bun.password`）、SQLite npm 包（用 `bun:sqlite`）。
4. 测试从 Jest 切到 `bun test`，多数测试文件保持原样。
5. 逐步把 `fs` 换成 `Bun.file`/`Bun.write`；`node:*` 内置模块仍然可用。

## 踩坑表

| 现象 | 处理 |
|------|------|
| 依赖装不上/版本漂移 | 用 `bun install --frozen-lockfile`（CI）；核对 `bun.lock` |
| `Bun.serve` 端口被占 | 设 `reusePort: true` 或改端口；检查是否已有实例 |
| WebSocket 升级失败 | `fetch` 中先 `server.upgrade(req)` 并返回 undefined |
| 打包后 Node 模块报错 | target 选 `"bun"`/`"node"`；浏览器产物勿引 `node:*` |
| 原生依赖编译失败 | 优先找 Bun 内置等价 API，或使用预编译包 |

## 相关技能

- `rust-backend`：性能或系统级热路径可下沉到 Rust 服务。
- `vercel-react-best-practices`：用 Bun 构建前端时的包体积与渲染性能优化。
- `web-design-guidelines`：交付前端界面前做无障碍与交互审查。
