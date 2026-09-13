---
name: primitives
description: >-
  Use when 用 @assistant-ui/react 的 composable、unstyled primitives 组装或定制聊天 UI
  （Thread、Composer、message rendering、action bar、branch picker 等），或需要确认
  part 组合、children render function、part grouping、条件渲染 AuiIf 与 RuntimeProvider
  等 API 规则时。适用于从 building blocks 自建界面，而非使用预置 drop-in UI。
---

# assistant-ui Primitives

## 概述

`@assistant-ui/react` 提供一组 **composable、unstyled** 的组件，遵循 **Radix-style part composition**：每个 primitive 是一个带 `.Part` 子组件的命名空间，组合出完整的聊天 UI。

- **始终查阅 [assistant-ui.com/llms.txt](https://www.assistant-ui.com/llms.txt) 获取最新 API。**
- Primitives **默认无样式**，必须自行加 `className` 并用应用的 Tailwind / CSS 体系美化。
- 必须在 `AssistantRuntimeProvider` 内使用。

## Import

```tsx
import {
  AuiIf,
  ThreadPrimitive,
  ComposerPrimitive,
  MessagePrimitive,
  ActionBarPrimitive,
  BranchPickerPrimitive,
  AttachmentPrimitive,
  ThreadListPrimitive,
  ThreadListItemPrimitive,
} from "@assistant-ui/react";
```

## Primitive Parts

| Primitive | Key Parts |
|-----------|-----------|
| `ThreadPrimitive` | `.Root`, `.Viewport`, `.ViewportFooter`, `.Messages`, `.Empty`, `.ScrollToBottom`, `.Suggestions`, `.Suggestion` |
| `ComposerPrimitive` | `.Root`, `.Input`, `.Send`, `.Cancel`, `.Attachments`, `.AddAttachment`, `.AttachmentDropzone`, `.Quote`, `.QuoteText`, `.QuoteDismiss`, `.Dictate`, `.StopDictation`, `.DictationTranscript`, `.Queue` |
| `MessagePrimitive` | `.Root`, `.Parts`（`.Content` 为 deprecated alias）, `.GroupedParts`, `.Attachments`, `.Quote`, `.GenerativeUI`, `.Error` |
| `ActionBarPrimitive` | `.Root`, `.Copy`, `.Edit`, `.Reload`, `.Speak`, `.StopSpeaking`, `.FeedbackPositive`, `.FeedbackNegative`, `.ExportMarkdown` |
| `BranchPickerPrimitive` | `.Root`, `.Previous`, `.Next`, `.Number`, `.Count` |
| `ThreadListPrimitive` | `.Root`, `.New`, `.Items`, `.LoadMore` |
| `ThreadListItemPrimitive` | `.Root`, `.Trigger`, `.Title`, `.Archive`, `.Unarchive`, `.Delete` |
| `AttachmentPrimitive` | `.Root`, `.Name`, `.Remove`, `.unstable_Thumb` |

另导出（各有独立 reference / skill）：`ChainOfThoughtPrimitive`、`SelectionToolbarPrimitive`、`SuggestionPrimitive`、`QueueItemPrimitive`、`ErrorPrimitive`、`MessagePartPrimitive`、`AssistantModalPrimitive`、`ActionBarMorePrimitive`、`ThreadListItemMorePrimitive`。

## 自定义 Thread 示例

```tsx
function CustomThread() {
  return (
    <ThreadPrimitive.Root className="flex flex-col h-full">
      <ThreadPrimitive.Empty>
        <div className="flex-1 flex items-center justify-center">
          Start a conversation
        </div>
      </ThreadPrimitive.Empty>

      <ThreadPrimitive.Viewport className="flex-1 overflow-y-auto p-4">
        <ThreadPrimitive.Messages>
          {({ message }) =>
            message.role === "user" ? <CustomUserMessage /> : <CustomAssistantMessage />
          }
        </ThreadPrimitive.Messages>
      </ThreadPrimitive.Viewport>

      <ComposerPrimitive.Root className="border-t p-4 flex gap-2">
        <ComposerPrimitive.Input className="flex-1 rounded-lg border px-4 py-2" />
        <ComposerPrimitive.Send className="bg-blue-500 text-white px-4 py-2 rounded-lg">
          Send
        </ComposerPrimitive.Send>
      </ComposerPrimitive.Root>
    </ThreadPrimitive.Root>
  );
}
```

## 条件渲染：优先 `AuiIf`

`AuiIf` 是新代码的首选；primitive 上的 `.If` 仍存在但已 **deprecated**。

```tsx
<AuiIf condition={({ message }) => message.role === "user"}>User only</AuiIf>
<AuiIf condition={({ thread }) => thread.isRunning}>Generating...</AuiIf>
<AuiIf condition={({ message }) => message.branchCount > 1}>Has edit history</AuiIf>

<AuiIf condition={({ thread }) => thread.isRunning}>
  <ComposerPrimitive.Cancel>Stop</ComposerPrimitive.Cancel>
</AuiIf>

<AuiIf condition={({ thread }) => thread.isEmpty}>No messages</AuiIf>
```

## children render function（0.14 起）

从 0.14 起，渲染列表的 primitives 改为接受 **children render function**，取代 `components` prop（后者仍可用但已 deprecated）：

`ThreadPrimitive.Messages`、`MessagePrimitive.Parts`、`ThreadPrimitive.Suggestions`、`ThreadListPrimitive.Items`、`ComposerPrimitive.Attachments`。

`MessagePrimitive.Parts` 是规范名；`MessagePrimitive.Content` 是 deprecated alias。

### Message Parts 渲染

```tsx
<MessagePrimitive.Parts>
  {({ part }) => {
    switch (part.type) {
      case "text":
        return <p>{part.text}</p>;
      case "image":
        return <img src={part.image} alt="" />;
      case "reasoning":
        return (
          <details>
            <summary>Thinking</summary>
            {part.text}
          </details>
        );
      case "tool-call":
        return part.toolUI ?? <div>Tool: {part.toolName}</div>;
      default:
        return null; // registered tool/data UIs still render
    }
  }}
</MessagePrimitive.Parts>
```

**关键规则**：

- 渲染函数返回 `null` → 让已注册的 tool / data UI 通过 registry 继续渲染。
- 返回 `<></>` → 显式什么也不渲染。
- 覆盖 `text`、`image`、`reasoning`、`tool-call`、`data`、`generative-ui` 等 part 类型。

## Part grouping

- 用 `groupPartByType` 对 part 分组。
- **0.15 变更**：`"mcp-app"` key 已移除，改用 `"standalone-tool-call"`。
- chain-of-thought UI 由 `ChainOfThoughtPrimitive` 承载。

## Branch Picker

```tsx
<AuiIf condition={({ message }) => message.branchCount > 1}>
  <BranchPickerPrimitive.Root className="flex items-center gap-1">
    <BranchPickerPrimitive.Previous>←</BranchPickerPrimitive.Previous>
    <span>
      <BranchPickerPrimitive.Number /> / <BranchPickerPrimitive.Count />
    </span>
    <BranchPickerPrimitive.Next>→</BranchPickerPrimitive.Next>
  </BranchPickerPrimitive.Root>
</AuiIf>
```

## 常见坑

**Primitives 不渲染**

- 用 `AssistantRuntimeProvider` 包住整棵树。
- 确认父 primitive 提供了所需的 context（子 part 不能脱离 Root 使用）。

**样式不生效**

- Primitives 默认无样式，需自行加 `className`。
- 用应用的 Tailwind / CSS 体系，而不是期待内置外观。

## Checklist

- [ ] 已核对 llms.txt 的最新 API（无过时 `.If` / `components` / `"mcp-app"` 用法）
- [ ] 组件树包在 `AssistantRuntimeProvider` 内，且 part 均在父 primitive 内
- [ ] 所有需要样式的 primitive 都带了 `className`
- [ ] 列表渲染使用 children render function，`MessagePrimitive.Parts` 而非 `.Content`
- [ ] 条件渲染优先 `AuiIf`
- [ ] 渲染函数 `null` / `<></>` 的语义符合预期（registry 渲染 vs 显式不渲染）
- [ ] 实际渲染验证过（见 `verification-before-completion`）

## 相关技能

- `verification-before-completion`：声称 UI 正常前先实际渲染并检查。
- `code-review-and-quality`：组件组合与引用正确性。
- `streaming`：assistant-ui 的流式渲染相关行为。
