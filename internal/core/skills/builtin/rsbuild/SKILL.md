---
name: rsbuild
description: >-
  Use when 使用 Rsbuild（基于 Rspack/Rust）搭建或优化前端构建，包括配置 rsbuild.config.ts、
  React/Vue/Svelte/Solid 插件、开发服务器与代理、代码分割与包体积、Module Federation、
  多环境构建、按需 polyfill，以及从 webpack/Vite/CRA/Vue CLI 迁移。
---

# Rsbuild 构建工具

Rsbuild 是 Rspack（Rust）驱动的构建工具，提供 webpack 语义与插件/loader 兼容性，构建速度比 webpack 快 5–10x，内置 TypeScript/CSS/静态资源支持，配置远比 webpack 精简。核心原则：**优先用官方配置与插件，只有能力缺失时才通过 `tools.rspack` 下沉到原始 Rspack 配置。**

## 快速开始

```bash
npm create rsbuild@latest                # 新建项目
npm install -D @rsbuild/core @rsbuild/plugin-react
npx rsbuild dev                          # 开发服务器（HMR）
npx rsbuild build                        # 生产构建
npx rsbuild preview                      # 本地预览产物
```

## 配置示例

```ts
// rsbuild.config.ts
import { defineConfig } from "@rsbuild/core";
import { pluginReact } from "@rsbuild/plugin-react";
import { pluginSass } from "@rsbuild/plugin-sass";
import { pluginTypeCheck } from "@rsbuild/plugin-type-check";

export default defineConfig({
  plugins: [pluginReact(), pluginSass(), pluginTypeCheck()],
  source: {
    entry: { index: "./src/index.tsx" },
    alias: { "@": "./src" },
  },
  output: {
    target: "web",              // "web" | "node" 等
    distPath: { root: "dist" },
    polyfill: "usage",          // 按 browserslist 注入，需装 core-js v3
    cleanDistPath: true,
    assetPrefix: process.env.CDN_URL || "/",
  },
  html: {
    title: "My App",
    favicon: "./src/assets/favicon.ico",
    template: "./public/index.html",
  },
  server: {
    port: 3000,
    proxy: { "/api": { target: "http://localhost:8080", changeOrigin: true } },
  },
  performance: {
    chunkSplit: { strategy: "split-by-experience" }, // 自动按经验拆包
    bundleAnalyze: process.env.ANALYZE === "true"
      ? { analyzerMode: "static" }
      : undefined,
  },
});
```

## 配置分区

| 前缀 | 关键项 |
|------|--------|
| `source` | `entry`、`alias`、`define`、`include`/`exclude`、`transformImport` |
| `output` | `target`、`distPath`、`assetPrefix`、`polyfill`、`cleanDistPath`、`sourceMap`、`externals` |
| `html` | `title`、`favicon`、`template`、`meta`、`tags` |
| `server` | `port`、`host`、`proxy`、`base`、`https`、`headers`、`open`、`setup` |
| `performance` | `chunkSplit`、`bundleAnalyze`、`buildCache`、`removeConsole`、`preload`/`prefetch` |
| `dev` | `hmr`、`liveReload`、`lazyCompilation`、`watchFiles`、`progressBar` |
| `tools` | `rspack`、`swc`、`postcss`、`cssLoader`、`htmlPlugin` |

## 插件生态

- 框架：`@rsbuild/plugin-react`、`plugin-vue`、`plugin-preact`、`plugin-svelte`、`plugin-solid`。
- 样式：`plugin-sass`、`plugin-less`、`plugin-tailwindcss`（Tailwind v4）。
- 质量：`plugin-type-check`（并行 TS 检查，不拖慢构建）、`plugin-svgr`。
- 高级：Module Federation 通过 `@module-federation/rsbuild-plugin` 或 `moduleFederation.options`。

## 常用能力

- **代码分割**：`performance.chunkSplit.strategy` 默认 `split-by-experience`，自动拆 React/lodash 等；也可 `split-by-module` 或自定义。
- **包体积分析**：`performance.bundleAnalyze` 产出静态报告；或接入 Rsdoctor 做构建分析。
- **多环境构建**：`environments` 可在一次运行内并行产出 web/node/SSR 等多份配置。
- **按需 polyfill**：`output.polyfill: "usage"` + browserslist，需安装 `core-js@3`。
- **开发代理**：`server.proxy` 转发 `/api` 避免 CORS；`server.setup` 可挂自定义中间件。
- **环境变量**：`source.define` 做编译期替换；用 `loadEnv` 或 `PUBLIC_` 前缀注入。
- **复用 webpack loader**：在 `tools.rspack` 里 push 规则，如 `graphql-tag/loader`。

## 迁移

- **webpack / CRA / Vue CLI**：官方文档提供逐项对照；多数 loader 与 plugin 可直接复用。
- **Vite**：配置语义不同（Rsbuild 用 `source`/`output` 分区），`import.meta.env` 等按文档替换。
- **Vite 插件**：需改写为 Rsbuild 插件（使用 `plugins/dev` 的 hook API）。

## 调试与排错

```bash
DEBUG=rsbuild npx rsbuild build     # 打印调试日志
RSDOCTOR=1 npx rsbuild build        # 接入构建分析
```

| 现象 | 处理 |
|------|------|
| 样式未生效 | 检查 `content`/`@source` 覆盖、CSS Modules 命名、`tools.cssExtract` |
| 产物过大 | 开 `bundleAnalyze`；检查 barrel 导入与重复依赖（`resolve.dedupe`） |
| 构建慢 | 开 `performance.buildCache`；收窄 `source.include` 编译范围 |
| 老浏览器报错 | 配 browserslist + `output.polyfill` |
| HMR 失效 | 检查 `dev.hmr` 与 `dev.client` 配置 |

## 相关技能

- `vercel-react-best-practices`：React 应用的包体积、请求瀑布与渲染性能优化。
- `web-design-guidelines`：构建产物交付前的无障碍与交互审查。
