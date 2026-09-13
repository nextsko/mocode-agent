---
name: assistant-ui
description: >-
  Use when 用 React 构建 AI 聊天界面：从 assistant-ui 的可组合原语与运行时出发，做选包、选 runtime（useChatRuntime / useExternalStoreRuntime / useLangGraphRuntime / useLocalRuntime）、理解分层模型（RuntimeCore / Runtime / aui client / primitives）与消息模型、多线程与 branching。覆盖 @assistant-ui/react 0.15.x 搭配 AI SDK v7。
---

# assistant-ui

用可组合原语构建 AI chat UI 的 React 库。**实现前先查最新 API：[assistant-ui.com/llms.txt](https://www.assistant-ui.com/llms.txt)。**

## 分层架构

```
UI 原语          ThreadPrimitive / MessagePrimitive / ComposerPrimitive
      │
aui client + hooks   useAui / useAuiState / useAuiEvent
      │
Runtime（公开 API）   AssistantRuntime → ThreadRuntime → MessageRuntime
      │
RuntimeCore（内部）   LocalRuntimeCore / ExternalStoreRuntimeCore / ThreadListRuntimeCore
      │
适配器 / 后端         AI SDK · LangGraph · 自定义 · Cloud
```

- **Runtime** 是交给 `AssistantRuntimeProvider` 的对象；`thread` / `threads` 是**属性**。
- **aui client** 是应用代码应使用的 API：`useAui()` 每个 scope 一个属性访问器（0.15+ 起为属性，scope 上的方法保留括号）。
- 注册 scope：`threads` `threadListItem` `thread` `message` `part` `composer` `attachment` `modelContext` `suggestions` `chainOfThought` `queueItem` `tools` 等。

## 选 Runtime

```
用 AI SDK？
├─ 是 → useChatRuntime（首选）
└─ 否
   ├─ 外部状态（Redux/Zustand）→ useExternalStoreRuntime
   ├─ LangGraph agent        → useLangGraphRuntime
   ├─ AG-UI 协议             → useAgUiRuntime
   ├─ A2A 协议               → useA2ARuntime
   └─ 自定义 API             → useLocalRuntime
```

## 核心包

| 包 | 用途 |
|---|---|
| `@assistant-ui/react` | UI 原语、hooks、runtimes（需 React 18/19） |
| `@assistant-ui/core` | 框架无关的核心 runtime |
| `@assistant-ui/store` | `useAui` / `AuiProvider` 状态层 |
| `@assistant-ui/react-ai-sdk` | Vercel AI SDK v7 适配（1.4.x） |
| `@assistant-ui/react-langchain` / `-langgraph` | LangChain `useStream` / LangGraph 适配 |
| `@assistant-ui/react-markdown` | Markdown 渲染（`MarkdownTextPrimitive`） |
| `assistant-stream` | 流式协议与编解码器 |
| `assistant-cloud` | 云端持久化与鉴权 |
| `@assistant-ui/react-native` / `-ink` | Expo / 终端（Ink）绑定 |

预构建组件来自 registry（`npx assistant-ui@latest add thread`），落到 `@/components/assistant-ui/*`。

## 快速开始

```bash
npm install @assistant-ui/react @assistant-ui/react-ai-sdk ai@^7 @ai-sdk/react@^4
```

```tsx
import { AssistantRuntimeProvider } from "@assistant-ui/react";
import { Thread } from "@/components/assistant-ui/thread";
import { useChatRuntime, AssistantChatTransport } from "@assistant-ui/react-ai-sdk";

function App() {
  const runtime = useChatRuntime({
    transport: new AssistantChatTransport({ api: "/api/chat" }),
  });
  return (
    <AssistantRuntimeProvider runtime={runtime}>
      <Thread />
    </AssistantRuntimeProvider>
  );
}
```

## 状态访问

scope 访问器是属性，scope 上的方法保留括号：

```tsx
import { useAui, useAuiState, useAuiEvent } from "@assistant-ui/react";

const aui = useAui();
aui.thread.append({ role: "user", content: [{ type: "text", text: "Hi" }] });
aui.thread.cancelRun();
aui.thread.composer().send();
aui.threads.switchToNewThread();

const messages = useAuiState((s) => s.thread.messages);
const isRunning = useAuiState((s) => s.thread.isRunning);
useAuiEvent("thread.modelContextUpdate", (e) => console.log(e));
```

- **不可用 scope 不抛错**：`aui.thread` 恒为真、`source` 为 `null`，其余读取才抛。用 `aui.thread.source != null` 守卫，或在 `useAuiState` 里走 `s.optional.thread`。

## 消息模型

```ts
type ThreadAssistantMessage = {
  id: string;
  role: "assistant";
  content: readonly ThreadAssistantMessagePart[];
  status: MessageStatus;   // 是对象，不是字符串；按 status.type 分支
  metadata: { steps: readonly ThreadStep[]; custom: Record<string, unknown>; /* ... */ };
  createdAt: Date;
};

type MessageStatus =
  | { type: "running" }
  | { type: "requires-action"; reason: "interrupt" | "tool-calls" }
  | { type: "complete"; reason: "stop" | "unknown" }
  | { type: "incomplete"; reason: "cancelled" | "content-filter" | "error" | "length" | "tool-calls"; error?: unknown };
```

- user 与 assistant 的 part 联合不同：assistant 含 `text` `reasoning` `tool-call` `source` `file` `image` `data` `generative-ui`；user 含 `text` `image` `file` `data`。
- UI 层通常读 `MessageState`：`ThreadMessage` 加上 `parentId` `index` `isLast` `branchNumber` `branchCount` `parts`（`PartState[]`，含 per-part status）`composer` `isCopied` `isHovering`。
- **branching**：消息构成树，编辑会生成分支；用 `BranchPickerPrimitive` 或 runtime API 在分支间导航。

## 原语与工具（节选）

- 原语：`ThreadPrimitive` `MessagePrimitive` `ComposerPrimitive` `ActionBarPrimitive` `BranchPickerPrimitive` `AttachmentPrimitive` `ThreadListPrimitive` `MessagePartPrimitive` `ChainOfThoughtPrimitive` `SuggestionPrimitive` `ErrorPrimitive`。
- 条件渲染：`AuiIf`；Provider：`AuiProvider`。
- 工具：`tool` `defineToolkit` `makeAssistantTool` `makeAssistantToolUI` `useAssistantTool` `useAssistantToolUI` `hitl` `humanTool`。
- Copilots（让助手理解当前应用）：`useAssistantInstructions` `useAssistantContext` `makeAssistantVisible` `Interactables`。

## 选包指引

| 场景 | 包 |
|---|---|
| Next.js + AI SDK | `@assistant-ui/react` + `react-ai-sdk` + `ai@^7` + `@ai-sdk/react@^4` |
| LangGraph | `@assistant-ui/react` + `react-langgraph`（或 `react-langchain`） |
| 自定义后端 | `@assistant-ui/react` + `assistant-stream` |
| 需要 Markdown | 加 `react-markdown` 或 `react-streamdown` |
| 生产持久化 | 加 `assistant-cloud` |

## 版本与陷阱

- `@assistant-ui/react` 需要 `react@^18 || ^19`。
- AI SDK 代际要匹配适配器版本：v7→`react-ai-sdk` 1.4.x；`ai@^6`→`1.3.40`；`ai@^5`→`1.1.21`；`ai@^4`→`react-data-stream`。
- AI SDK 要求 `zod@^3.25.76 || ^4.1.8`，Zod 3.25+ 与 Zod 4 均可用（见 `zod`）。
- 不要手搓消息 UI 与滚动逻辑，优先原语；chat 组件也可用 shadcn 的 `MessageScroller` 体系（见 `shadcn`）。

相关技能：React 性能与 hooks 见 `vercel-react-best-practices`；组合边界与 Provider 设计见 `vercel-composition-patterns`。
