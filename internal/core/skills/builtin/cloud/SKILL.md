---
name: cloud
description: >-
  Use when 为 assistant-ui 应用接入 Cloud 持久化与授权（assistant-cloud 的 AssistantCloud + useChatRuntime 的 cloud 选项）：跨会话 / 多设备 thread & message 历史、文件上传、JWT/authToken、API key + userId/workspaceId、anonymous 模式，以及 NextAuth / Clerk / Firebase / better-auth 接入；cloud.threads.list/get/create/update/delete、messages.list/create、files.generatePresignedUploadUrl、aui/v0 消息格式、自定义适配器（CloudMessagePersistence、createFormattedPersistence、ThreadHistoryAdapter、RemoteThreadListAdapter）、自动标题、external_id/metadata 映射，环境变量 NEXT_PUBLIC_ASSISTANT_BASE_URL / ASSISTANT_API_KEY。线程列表侧边栏 UI 用 thread-list。
---

# assistant-ui Cloud

**始终以 [assistant-ui.com/llms.txt](https://www.assistant-ui.com/llms.txt) 为最新 API 来源。**

Cloud 为 threads / messages / files 提供持久化，换来跨会话历史、多设备同步、线程管理（归档 / 删除）与自动标题。

## 安装与快速开始

```bash
npm install assistant-cloud
```

```tsx
import { AssistantCloud } from "assistant-cloud";
import { useChatRuntime, AssistantChatTransport } from "@assistant-ui/react-ai-sdk";
import { AssistantRuntimeProvider } from "@assistant-ui/react";

const cloud = new AssistantCloud({
  baseUrl: process.env.NEXT_PUBLIC_ASSISTANT_BASE_URL,
  authToken: async () => getAuthToken(),
});

function Chat() {
  const runtime = useChatRuntime({
    transport: new AssistantChatTransport({ api: "/api/chat" }),
    cloud, // 打开持久化
  });
  return (
    <AssistantRuntimeProvider runtime={runtime}>
      <ThreadList />
      <Thread />
    </AssistantRuntimeProvider>
  );
}
```

传入 `cloud` 后：新消息自动保存、首条消息时创建线程、标题 / 时间戳自动更新、编辑分支持久化。

## 认证方式

| 方式 | 场景 | 安全级别 |
|------|------|---------|
| JWT（`authToken`） | 生产客户端 | 高 |
| API key + userId/workspaceId | 仅服务端 | 中 |
| `anonymous: true` | 公开演示 | 低 |

```tsx
// JWT（推荐）：token 动态获取
new AssistantCloud({ baseUrl: process.env.NEXT_PUBLIC_ASSISTANT_BASE_URL, authToken: async () => session?.accessToken });

// API key（服务端）：绝不进客户端
new AssistantCloud({ baseUrl: process.env.ASSISTANT_BASE_URL, apiKey: process.env.ASSISTANT_API_KEY, userId: user.id, workspaceId: user.workspaceId });

// 匿名
new AssistantCloud({ baseUrl: process.env.NEXT_PUBLIC_ASSISTANT_BASE_URL, anonymous: true });
```

`authToken` 内处理 token 过期刷新；401 时刷新，429 时退避重试。密钥只走服务端环境变量。

## Cloud API

```tsx
// threads.list 光标分页（after 为上一页最后一条 id，非 offset）
const { threads } = await cloud.threads.list({ is_archived: false, limit: 50, after: cursor });
const thread = await cloud.threads.get(threadId);

// create：last_message_at 必填，其余可选
const { thread_id } = await cloud.threads.create({
  last_message_at: new Date(), title: "New Chat",
  external_id: "your-id", metadata: { source: "web" },
});
await cloud.threads.update(threadId, { title: "Updated", is_archived: true });
await cloud.threads.delete(threadId);

// messages 挂在 threads 上，threadId 为第一参数；无分页，只接受 { format? }
const { messages } = await cloud.threads.messages.list(threadId, { format: "aui/v0" });
await cloud.threads.messages.create(threadId, {
  parent_id: null, format: "aui/v0",
  content: { role: "user", content: [{ type: "text", text: "Hello" }] },
});

// 文件直传
const { signedUrl, publicUrl } = await cloud.files.generatePresignedUploadUrl({ filename: "document.pdf" });
await fetch(signedUrl, { method: "PUT", body: file });
```

`external_id` 用于把线程映射回你的系统；`metadata` 存自定义数据。标题可手动触发：`api.threads.item({ id }).generateTitle()`。

## aui/v0 消息格式

```ts
interface AUIv0Message {
  role: "user" | "assistant" | "system";
  content: MessagePart[];
  status?: "running" | "complete" | "incomplete" | "requires-action";
  attachments?: Attachment[];
}
type MessagePart =
  | { type: "text"; text: string }
  | { type: "image"; image: string }
  | { type: "tool-call"; toolCallId: string; toolName: string; args: unknown; argsText: string; result?: unknown; isError?: boolean }
  | { type: "reasoning"; text: string }
  | { type: "source"; sourceType: "url"; id: string; url: string; title?: string };
```

## 自定义持久化

不想用 Cloud 托管时，用两个适配器（来自 `@assistant-ui/react`）：

- `RemoteThreadListAdapter`：线程元数据（`list / initialize / rename / archive / unarchive / delete / fetch / generateTitle / unstable_Provider`）。
- `ThreadHistoryAdapter`：单线程消息（`load / append / withFormat`）。

`useChatRuntime` 路径必经 `withFormat`：`fmt.decode({id,parent_id,format,content})` 还原 `UIMessage`，`fmt.encode(item)` 生成待存 `content`，`fmt.getId(item.message)` 取 id，`fmt.format` 写 `format` 列。存储契约固定四列：`id / parent_id / format / content`，`parent_id` 链保留分支。`unstable_Provider` 里用 `RuntimeAdapterProvider` 挂载 `{ history }`；`append` 前先 `await aui.threadListItem.initialize()`，`load` 用 `getState()` 取 `remoteId` 并在缺失时退出。用 Cloud 托管时可直接以 `CloudMessagePersistence` / `createFormattedPersistence`（来自 `assistant-cloud`）作为适配器后端。

## Auth 集成（后端）

Cloud 托管时可自建 token 端点：服务端算出 `workspaceId`（如 `orgId_userId`），用 `assistantCloud.auth.tokens.create()` 签发后返回，前端把它作为 `authToken`。自建持久化时：better-auth 用 `auth.api.getSession({ headers: await headers() })`，Clerk 用 `@clerk/nextjs/server` 的 `auth()`，都先判空返回 401；线程查询一律按 `session.user.id` / `userId`（Clerk Orgs 再加 `orgId`）过滤。首屏可能在会话解析前渲染，用 `ReloadOnAuth` 组件在登录态就绪后调 `aui.threads.reload()`。

## 环境变量

```env
NEXT_PUBLIC_ASSISTANT_BASE_URL=https://api.assistant-ui.com
ASSISTANT_BASE_URL=https://api.assistant-ui.com
ASSISTANT_API_KEY=your-api-key      # 仅服务端
ASSISTANT_WORKSPACE_ID=your-workspace
```

## 常见坑

| 现象 | 处理 |
|------|------|
| 线程不持久化 | 确认把 `cloud` 传给 runtime；检查认证是否就绪 |
| 认证报错 | `authToken` 返回有效 token、`baseUrl` 正确 |
| 401 / 429 | 401 刷新 token；429 退避重试 |
| 越权看到他人线程 | 所有线程查询按登录用户 id 过滤，勿信 client 传参 |
| API key 泄漏 | 密钥仅服务端，客户端一律用 JWT / anonymous |

## 相关技能

- `observability`：给同一条 `streamText` 路由加 tracing / telemetry。
- `security-and-hardening`：密钥管理、越权与输入校验。
- `rig-core-llm-integration`：LLM 调用层集成。
