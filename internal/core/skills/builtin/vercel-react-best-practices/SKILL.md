---
name: vercel-react-best-practices
description: >-
  Use when 编写、审查或重构 React / Next.js 代码，覆盖数据获取、消除请求瀑布、包体积、Server Component
  性能、客户端缓存、重渲染、渲染性能与 JavaScript 微优化。适用于组件编写、页面开发、数据获取实现与性能优化相关任务。
---

# Vercel React 最佳实践

源自 Vercel Engineering 的性能优化规则集（70 条，8 大类），按影响面优先级排序，用于指导重构与代码生成。规则 ID 前缀对应类别，便于在评审意见中直接引用。

## 何时使用

- 编写新的 React 组件或 Next.js 页面
- 实现客户端 / 服务端数据获取
- 审查或重构既有 React / Next.js 代码
- 优化包体积、首屏加载与交互性能

> 在评审中引用具体规则 ID，并与 `code-review-and-quality` 配合；涉及 Server Action 鉴权、密钥或不可信输入时联动 `security-and-hardening`。

## 类别优先级（高影响优先处理）

| 优先级 | 类别 | 影响 | 前缀 |
|---|---|---|---|
| 1 | 消除请求瀑布 | CRITICAL | `async-` |
| 2 | 包体积优化 | CRITICAL | `bundle-` |
| 3 | 服务端性能 | HIGH | `server-` |
| 4 | 客户端数据获取 | MEDIUM-HIGH | `client-` |
| 5 | 重渲染优化 | MEDIUM | `rerender-` |
| 6 | 渲染性能 | MEDIUM | `rendering-` |
| 7 | JavaScript 性能 | LOW-MEDIUM | `js-` |
| 8 | 高级模式 | LOW | `advanced-` |

## 1. 消除请求瀑布（最高优先）

独立操作一律并行；`await` 推迟到真正使用处。

```ts
// BAD：串行，耗时相加
const user = await getUser();
const posts = await getPosts();

// GOOD：并行
const [user, posts] = await Promise.all([getUser(), getPosts()]);
```

- `async-cheap-condition-before-await`：先做同步廉价判断，再 await 远程标志位。
- `async-defer-await`：把 await 移进真正用到它的分支。
- `async-dependencies`：存在部分依赖时用 `better-all` 表达依赖图。
- `async-api-routes`：在 handler 顶部先发起 promise，晚一点再 await。
- `async-suspense-boundaries`：用 Suspense 边界流式输出内容。

## 2. 包体积优化

- `bundle-barrel-imports`：直接引用具体模块，避免 barrel 文件把整包拖进来。
- `bundle-analyzable-paths`：导入与文件系统路径保持静态可分析，避免宽泛打包与追踪。
- `bundle-dynamic-imports`：重型组件用 `next/dynamic` 懒加载。
- `bundle-defer-third-party`：分析 / 日志脚本在 hydration 之后再加载。
- `bundle-conditional`：仅在功能被激活时加载对应模块。
- `bundle-preload`：在 hover / focus 时预加载，提升感知速度。

## 3. 服务端性能

- `server-auth-actions`：Server Action 必须像 API 路由一样做鉴权（详见 `security-and-hardening`）。
- `server-cache-react`：用 `React.cache()` 做同一请求内的去重。
- `server-cache-lru`：跨请求缓存用 LRU。
- `server-dedup-props`：避免 RSC props 中重复序列化。
- `server-hoist-static-io`：字体、logo 等静态 I/O 提升到模块级。
- `server-no-shared-module-state`：禁止在 RSC/SSR 使用模块级可变请求状态。
- `server-serialization`：传给 Client Component 的数据最小化。
- `server-parallel-fetching`：重构组件结构以并行发起 fetch。
- `server-parallel-nested-fetching`：嵌套 fetch 按条目放进 `Promise.all`。
- `server-after-nonblocking`：非阻塞副作用用 `after()`。

## 4. 客户端数据获取

- `client-swr-dedup`：用 SWR 做请求自动去重。
- `client-event-listeners`：全局事件监听器去重（共享单个监听器）。
- `client-passive-event-listeners`：滚动监听使用 passive。
- `client-localstorage-schema`：localStorage 数据加版本并最小化。

## 5. 重渲染优化

- `rerender-defer-reads`：只在回调里用到的状态不要订阅。
- `rerender-memo`：把昂贵计算抽到 `memo` 组件。
- `rerender-memo-with-default-value`：非原始类型默认值提升到组件外。
- `rerender-dependencies`：effect 依赖使用原始值。
- `rerender-derived-state`：订阅派生布尔值，而非原始值。
- `rerender-derived-state-no-effect`：在渲染期派生状态，不要用 effect。
- `rerender-functional-setstate`：`setState(prev => ...)` 保持回调稳定。
- `rerender-lazy-state-init`：昂贵初始值用 `useState(() => ...)`。
- `rerender-simple-expression-in-memo`：简单原始值计算不要用 memo。
- `rerender-split-combined-hooks`：拆分依赖相互独立的 hook。
- `rerender-move-effect-to-event`：交互逻辑放事件处理器，不放 effect。
- `rerender-transitions`：非紧急更新用 `startTransition`。
- `rerender-use-deferred-value`：用 `useDeferredValue` 保持输入响应。
- `rerender-use-ref-transient-values`：高频瞬态值用 ref。
- `rerender-no-inline-components`：不要在组件内部定义组件。

## 6. 渲染性能

- `rendering-content-visibility`：长列表使用 `content-visibility`。
- `rendering-hoist-jsx`：静态 JSX 抽到组件外。
- `rendering-animate-svg-wrapper`：动画加在 div 包装层，而非 SVG 本身。
- `rendering-conditional-render`：条件渲染用三元而非 `&&`（避免渲染 `0`）。
- `rendering-hydration-no-flicker`：仅客户端数据用内联脚本避免闪烁。
- `rendering-hydration-suppress-warning`：确认的差异才用 `suppressHydrationWarning`。
- `rendering-usetransition-loading`：加载态优先用 `useTransition`。
- `rendering-resource-hints`：用 React DOM 资源提示预加载。
- `rendering-script-defer-async`：`<script>` 使用 defer 或 async。
- `rendering-svg-precision`：降低 SVG 坐标精度。

## 7. JavaScript 性能

- `js-set-map-lookups`：查找用 `Set` / `Map` 得到 O(1)。
- `js-index-maps`：重复查找先构建 Map 索引。
- `js-combine-iterations`：合并多次 filter/map 为一次循环。
- `js-early-exit`：函数尽早返回。
- `js-cache-property-access`：循环内缓存对象属性。
- `js-cache-function-results`：模块级 Map 缓存函数结果。
- `js-hoist-regexp`：正则创建提升到循环外。
- `js-length-check-first`：昂贵比较前先检查数组长度。
- `js-min-max-loop`：求 min/max 用循环而非排序。
- `js-tosorted-immutable`：用 `toSorted()` 保持不可变。
- `js-flatmap-filter`：`flatMap` 一次完成 map + filter。
- `js-batch-dom-css`：通过 class 或 `cssText` 批量改样式。
- `js-request-idle-callback`：非关键工作推迟到浏览器空闲。

## 8. 高级模式（LOW）

- `advanced-effect-event-deps`：不要把 `useEffectEvent` 的结果放进 effect 依赖。
- `advanced-event-handler-refs`：事件处理器存 ref。
- `advanced-init-once`：应用级初始化每次加载只执行一次。
- `advanced-use-latest`：用 `useLatest` 提供稳定的回调 ref。

## 落地检查

- [ ] 独立请求是否已并行？是否有可提前的 await？
- [ ] 重型组件是否动态导入？是否直接引用模块而非 barrel？
- [ ] Server Action / API 是否有鉴权，是否最小化传给客户端的数据？
- [ ] effect 依赖是否稳定，是否用派生值替代冗余状态？
- [ ] 长列表与静态 JSX 是否已优化？
