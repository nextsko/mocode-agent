---
name: streaming
description: >-
  Use when 构建或调试 assistant-ui 的流式后端与 wire protocol（assistant-stream 包）：用
  createAssistantStreamResponse / createAssistantStreamController 自建端点，通过
  appendText/appendReasoning/appendSource/appendFile/addToolCallPart/setResponse 发出分片；
  在 AI SDK UI-message 格式（toUIMessageStream + createUIMessageStreamResponse）与原生
  Assistant Transport 格式之间选型；用 DataStreamEncoder/Decoder、AssistantTransportEncoder/Decoder、
  PlainTextEncoder、UIMessageStreamDecoder 编解码；接入 useLocalRuntime / useChatRuntime；
  排查 text-delta、part-start、result 事件、text/event-stream、SSE、tool call 不渲染、部分文本不显示；
  以及用 assistant-stream/resumable 实现可续传流。
---

# assistant-ui Streaming

**始终以 [assistant-ui.com/llms.txt](https://www.assistant-ui.com/llms.txt) 为最新 API 来源。**
`assistant-stream` 负责把后端流式响应规范化成 UI 可消费的分片。

## 选型

```
用 Vercel AI SDK？
├─ 是 → createUIMessageStreamResponse + toUIMessageStream（不需要 assistant-stream）
└─ 否 → assistant-stream 自建后端
        ├─ 只关心最简文本 → PlainTextEncoder
        ├─ 要完整特性（reasoning/source/file/tool）→ Assistant Transport
        └─ 要与 AI SDK 生态兼容 → Data Stream
```

## 自建流式响应

```ts
import { createAssistantStreamResponse } from "assistant-stream";

export async function POST(req: Request) {
  return createAssistantStreamResponse(async (stream) => {
    stream.appendText("Hello ");
    stream.appendText("world!");

    const tool = stream.addToolCallPart({ toolCallId: "1", toolName: "get_weather" });
    tool.argsText.append('{"city":"NYC"}');
    tool.argsText.close();
    tool.setResponse({ result: { temperature: 22 } });

    stream.close();
  });
}
```

### Controller 方法

- `appendText(text)` / `appendReasoning(reasoning)`
- `appendSource({ sourceType: "url", id, url, title?, parentId? })`
- `appendFile({ data, mimeType })`
- `addToolCallPart({ toolCallId, toolName, parentId? })` → `{ argsText.append/close, setResponse({ result, artifact?, isError? }), close }`
- `close()` 结束消息

## 编码器 / 解码器

| Encoder | 格式 | 场景 |
|---------|------|------|
| `DataStreamEncoder` | AI SDK Data Stream（SSE） | 默认，`toUIMessageStream` 背后的 wire format |
| `AssistantTransportEncoder` | 原生 SSE `data: {chunk}` | 自建后端，要全部 chunk 类型 |
| `PlainTextEncoder` | 纯文本 | 极简 demo |
| `UIMessageStreamDecoder` | 累积成完整消息态 | 直接喂给 UI |

```ts
const response = AssistantStream.toResponse(stream, new DataStreamEncoder());
const decoded = AssistantStream.fromResponse(response, new DataStreamDecoder());
for await (const chunk of decoded) console.log(chunk);
```

## 事件类型

- `part-start`，`part.type = "text" | "reasoning" | "tool-call" | "source" | "file"`
- `part-finish`、`tool-call-args-text-finish`
- `text-delta`、`annotations`、`data`
- `result`（工具结果）
- `step-start` / `step-finish` / `message-finish`
- `error`

Data Stream 的 SSE 行前缀：`0:` 文本、`9:`/`b:`/`c:`/`a:` 工具调用、`d:` 消息结束、`e:` 步骤结束、`3:` 错误、`h:` source、`k:` file、`aui-*` assistant-ui 扩展。

## 接入 useLocalRuntime

`useLocalRuntime` 的 `model.run` 期望 `ChatModelRunResult` 分片；流式时逐个 yield content：

```tsx
const runtime = useLocalRuntime({
  model: {
    async *run({ messages, abortSignal }) {
      const res = await fetch("/api/chat", {
        method: "POST",
        body: JSON.stringify({ messages }),
        signal: abortSignal,
      });
      const reader = res.body?.getReader();
      const decoder = new TextDecoder();
      let buffer = "";
      while (reader) {
        const { done, value } = await reader.read();
        if (done) break;
        buffer += decoder.decode(value, { stream: true });
        const parts = buffer.split("\n");
        buffer = parts.pop() ?? "";
        for (const text of parts.filter(Boolean)) {
          yield { content: [{ type: "text", text }] };
        }
      }
    },
  },
});
```

原生 Assistant Transport 分片可用 `AssistantStream.fromResponse(res, new AssistantTransportDecoder())` 解码后转成 content parts（`text-delta` → text；`part-start`/`result` → tool-call）。

## 可续传流（resumable）

`assistant-stream/resumable` 在**编码后的字节层**持久化进行中的响应，让客户端重连/刷新/新标签页继续读同一流，适用于任意 encoder，流以不透明 `streamId` 寻址。

- Context：`createResumableStreamContext({ store, waitUntil, ttlMs, onAcquire/onAppend/onFinalize/onError })`，每进程构造一次复用。
- POST：`resumableContext.run(streamId, () => body)`，首个调用者成为 producer 并写入 store；响应头带 `x-resumable-stream-id`（`RESUMABLE_STREAM_ID_HEADER`）。
- GET resume：`resumableContext.resume(streamId)` 重放；`null`/404 表示流不存在；`requireResume` 则在缺失时抛 `ResumableStreamError`。
- 客户端：`AssistantChatTransport({ api, resumable: { storage, resumeApi } })`，配合 `createResumableSessionStorage()`；`useChatRuntime` 挂载时若 storage 有待续 id 会自动 `resumeStream()`。
- 存储：开发用 `createInMemoryResumableStreamStore`；生产用 Redis/ioredis adapters（`assistant-stream/resumable/redis`），或实现 `ResumableStreamStore` 的 `acquire/append/finalize/read/status/delete`，其中 `acquire` 必须原子（Redis `SET NX EX`、Postgres `ON CONFLICT DO NOTHING`）。

## 调试

```ts
// 1) 看原始字节
const reader = response.body?.getReader();
while (reader) {
  const { done, value } = await reader.read();
  if (done) break;
  console.log("Raw:", new TextDecoder().decode(value));
}

// 2) Content-Type 必须是 text/event-stream
response.headers.get("Content-Type");

// 3) 逐事件解码
for await (const e of AssistantStream.fromResponse(response, new DataStreamDecoder())) {
  console.log("Event:", e);
}
```

## 常见坑

| 现象 | 处理 |
|------|------|
| 流不更新 UI | Content-Type 必须是 `text/event-stream`；检查 CORS |
| tool call 不渲染 | `addToolCallPart` 必须同时给 `toolCallId` 与 `toolName`；用 `makeAssistantToolUI` 注册 UI |
| 部分文本不显示 | 用 `text-delta` 事件承载增量，勿只发 `result` |
| 事件顺序/结构异常 | 先解码打印原始 chunk，对照上面事件类型表 |
| 可续传重连拿到 404 | 流已过期/不存在；检查 store TTL 与 `streamId` 是否合法 |

## 相关技能

- `runtime`：`useLocalRuntime` / `ChatModelRunResult` 如何消费这些分片。
- `observability`：route handler 的 trace 接入。
- `security-and-hardening`：流式端点同样要鉴权、限流、校验输入。
