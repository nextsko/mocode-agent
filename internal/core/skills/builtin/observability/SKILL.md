---
name: observability
description: >-
  Use when 为 assistant-ui 后端接入 tracing / telemetry / observability：把 AI SDK 路由
  （streamText/generateText、toUIMessageStream、createUIMessageStreamResponse）接到 Langfuse
  （OpenTelemetry + LangfuseSpanProcessor + NodeSDK + experimental_telemetry + propagateAttributes +
  serverless forceFlush）、LangSmith（wrapAISDK(ai) + createLangSmithProviderOptions +
  awaitPendingTraceBatches）或 Helicone（createOpenAI baseURL https://oai.helicone.ai/v1 +
  Helicone-Auth 头）；以及用 @assistant-ui/react-o11y 无头原语（SpanResource、SpanPrimitive、
  SpanByIndexProvider、SpanData/SpanState）绘制 trace waterfall。用于 trace 缺失/为空、edge 与
  nodejs 运行时差异、serverless flush、trace 瀑布排查。
---

# assistant-ui Observability

**始终以 [assistant-ui.com/llms.txt](https://www.assistant-ui.com/llms.txt) 为最新 API 来源。**

## 接入点

Telemetry 挂在**调用 `streamText`/`generateText` 的服务端 route** 上，不在 React runtime。前端（`useChatRuntime`、`Thread`）保持不变；`react-o11y` 是可选的前端 span 渲染层。

```
Thread (frontend) ──> /api/chat (streamText) ──> tracing backend
                                              └─> react-o11y (optional UI)
```

## Provider 路由

| Provider | 机制 |
|----------|------|
| Langfuse | OTel span processor + `experimental_telemetry` + `propagateAttributes` |
| LangSmith | `wrapAISDK(ai)` 包装，无需 OTel |
| Helicone | provider 上覆盖 proxy `baseURL`，无需 telemetry flag |
| react-o11y | 渲染已采集的 spans |

## 共享：AI SDK telemetry

Langfuse 与任何 OTel 后端都复用 `experimental_telemetry`，**每次调用**都要开启：

```ts
import { openai } from "@ai-sdk/openai";
import {
  streamText,
  convertToModelMessages,
  createUIMessageStreamResponse,
  toUIMessageStream,
} from "ai";
import type { UIMessage } from "ai";

export async function POST(req: Request) {
  const { messages }: { messages: UIMessage[] } = await req.json();
  const result = streamText({
    model: openai("gpt-4o"),
    messages: await convertToModelMessages(messages),
    experimental_telemetry: { isEnabled: true },
  });
  return createUIMessageStreamResponse({
    stream: toUIMessageStream({ stream: result.stream }),
  });
}
```

## Langfuse（OTel）

```bash
# 环境变量：LANGFUSE_PUBLIC_KEY / LANGFUSE_SECRET_KEY / LANGFUSE_BASE_URL
npm install @langfuse/tracing @langfuse/otel @opentelemetry/sdk-node
```

```ts
// instrumentation.ts
import { NodeSDK } from "@opentelemetry/sdk-node";
import { LangfuseSpanProcessor } from "@langfuse/otel";

export const langfuseSpanProcessor = new LangfuseSpanProcessor();

export async function register() {
  if (process.env.NEXT_RUNTIME !== "nodejs") return; // OTel 不在 edge 跑
  new NodeSDK({ spanProcessors: [langfuseSpanProcessor] }).start();
}
```

用 `propagateAttributes({ traceName, userId, sessionId }, () => streamText(...))` 给 trace 打标，便于按用户/会话分组。
Serverless 响应前必须 `await langfuseSpanProcessor.forceFlush()`，否则函数先退出、buffer 丢失。

## LangSmith（包装 ai）

```bash
# LANGSMITH_TRACING=true / LANGSMITH_API_KEY=... / LANGSMITH_PROJECT=...
npm install langsmith
```

```ts
import * as ai from "ai";
import { wrapAISDK } from "langsmith/experimental/vercel";

const { streamText } = wrapAISDK(ai);
```

- `convertToModelMessages`、`toUIMessageStream`、`createUIMessageStreamResponse` **不在包装内**，直接从 `ai` 命名空间调用。
- 分组元数据：`providerOptions: { langsmith: createLangSmithProviderOptions({ name, metadata: { userId, threadId } }) }`。
- Serverless：响应前 `await new Client().awaitPendingTraceBatches()`。

## Helicone（代理，无需 OTel）

覆盖 provider 的 `baseURL` 并加认证头即可，流式/工具/附件行为不变：

```ts
import { createOpenAI } from "@ai-sdk/openai";

const openai = createOpenAI({
  baseURL: "https://oai.helicone.ai/v1",
  headers: {
    "Helicone-Auth": `Bearer ${process.env.HELICONE_API_KEY}`,
    "Helicone-User-Id": "user_123",              // 可选：按用户分组
    "Helicone-Property-App": "support-bot",      // 可选：自定义可过滤维度
  },
});
```

`OPENAI_API_KEY` 仍由 `createOpenAI` 读取，Helicone 转发给 OpenAI。密钥仅服务端使用。

## react-o11y 可视化

`@assistant-ui/react-o11y` 提供无头原语，把 `SpanData[]` 渲染成 trace 瀑布。用 `useAui({ span: SpanResource({ spans }) })` 挂载，`AuiProvider` 提供。

```tsx
import { SpanPrimitive, SpanResource, type SpanData } from "@assistant-ui/react-o11y";
import { AuiProvider, useAui } from "@assistant-ui/store";

function SpanRow() {
  return (
    <SpanPrimitive.Root>
      <SpanPrimitive.Indent />
      <SpanPrimitive.CollapseToggle />
      <SpanPrimitive.StatusIndicator />
      <SpanPrimitive.TypeBadge />
      <SpanPrimitive.Name />
    </SpanPrimitive.Root>
  );
}

export function TraceView({ spans }: { spans: SpanData[] }) {
  const aui = useAui({ span: SpanResource({ spans }) });
  return (
    <AuiProvider value={aui}>
      <SpanPrimitive.Children components={{ Span: SpanRow }} />
    </AuiProvider>
  );
}
```

- `SpanData`：`id`、`parentSpanId`（根为 `null`）、`name`、`type`、`status`（`running | completed | failed | skipped`）、`startedAt`、`endedAt`、`latencyMs`。
- `SpanPrimitive.Root` 暴露 `data-span-status`、`data-span-type`、`data-span-depth`、`data-collapsed` 供样式选择。
- `SpanPrimitive.Children` 会把树摊平成可见列表，并自动包 `SpanByIndexProvider`。

## 常见坑

| 现象 | 处理 |
|------|------|
| Vercel/Lambda 无 trace | 函数在 OTel flush 前退出：Langfuse 用 `forceFlush()`，LangSmith 用 `awaitPendingTraceBatches()` |
| Langfuse trace 为空 | 每次 `streamText`/`generateText` 都要 `experimental_telemetry: { isEnabled: true }`；span processor 仅在 nodejs runtime 注册 |
| LangSmith 不追踪 | 用 `wrapAISDK(ai)` 解构出的方法，别用原始 `ai`；确保 `LANGSMITH_TRACING=true` |
| Helicone 请求仍直连 OpenAI | 确认走 `oai.helicone.ai`，且同时带 `Helicone-Auth` 与 `Authorization` |
| react-o11y 渲染为空 | 原语必须在 `AuiProvider` 内，资源经 `useAui` 挂载 |

## 相关技能

- `streaming`：telemetry 挂载的 route handler 与 stream 响应。
- `security-and-hardening`：密钥仅服务端使用，切勿写进客户端。
- `code-review-and-quality`：合并前的质量门禁。
