---
name: zustand
description: >-
  Use when 在 React 应用里做全局或跨组件状态管理，用 Zustand 的 hook store、selector、middleware（persist/devtools/immer）、异步 action、computed 值，或在 React 组件外访问与订阅状态；也适用于用 Zustand 替代 Redux/Context 的样板代码。
---

# Zustand — 极简 React 状态管理

< 1KB、无 Provider 的 hook 式全局状态库。store 是模块级单例，任意位置 import 即用。

## 安装与最小 store

```bash
npm install zustand
```

```ts
import { create } from "zustand";

interface Counter {
  count: number;
  inc: () => void;
}

const useCounter = create<Counter>((set) => ({
  count: 0,
  inc: () => set((s) => ({ count: s.count + 1 })), // 函数式 set 基于旧值
}));
```

## 中间件组合（persist + immer + devtools）

```ts
import { create } from "zustand";
import { persist, devtools } from "zustand/middleware";
import { immer } from "zustand/middleware/immer";

interface Todo { id: string; text: string; done: boolean }

interface TodoStore {
  todos: Todo[];
  filter: "all" | "active" | "done";
  addTodo: (text: string) => void;
  toggleTodo: (id: string) => void;
  removeTodo: (id: string) => void;
  setFilter: (filter: TodoStore["filter"]) => void;
  fetchTodos: () => Promise<void>;
}

const useTodoStore = create<TodoStore>()(
  devtools(
    persist(
      immer((set) => ({
        todos: [],
        filter: "all",
        addTodo: (text) =>
          set((s) => { s.todos.push({ id: crypto.randomUUID(), text, done: false }); }),
        toggleTodo: (id) =>
          set((s) => { const t = s.todos.find((x) => x.id === id); if (t) t.done = !t.done; }),
        removeTodo: (id) => set((s) => { s.todos = s.todos.filter((t) => t.id !== id); }),
        setFilter: (filter) => set({ filter }),
        fetchTodos: async () => {
          const todos = await (await fetch("/api/todos")).json();
          set({ todos });
        },
      })),
      { name: "todo-storage" },   // localStorage key
    ),
    { name: "TodoStore" },        // Redux DevTools 标签
  ),
);
```

> 顺序：`devtools(persist(immer(...)))`。`persist` 加 `partialize` 只持久化需要保留的字段。

## Selector 与性能

- **总是选具体字段**：`useTodoStore((s) => s.todos)`，返回整个 store 会导致任何字段变化都重渲染。
- **多字段用 `useShallow`**（不要就地返回新对象）：

```ts
import { useShallow } from "zustand/react/shallow";

const { a, b } = useStore(useShallow((s) => ({ a: s.a, b: s.b })));
```

- 派生 / 过滤在组件内基于 selector 结果计算；不要在 selector 里 `map`/`filter` 制造新数组。
- 需要稳定引用时用 `useMemo`，而不是让 selector 每次新建对象。

## 异步 action

异步逻辑直接写在 store 里，`await` 之后照常调用 `set`。用 loading/error 字段表达状态：

```ts
fetchTodos: async () => {
  set({ loading: true, error: null });
  try {
    const todos = await (await fetch("/api/todos")).json();
    set({ todos, loading: false });
  } catch (e) {
    set({ error: (e as Error).message, loading: false });
  }
},
```

## React 组件外访问

```ts
const { addTodo, todos } = useTodoStore.getState();  // 一次性读取
useTodoStore.setState({ todos: [] });                 // 外部写入
const unsub = useTodoStore.subscribe((s) => console.log("len:", s.todos.length));
```

适合 API client、WebSocket handler、定时任务、测试。配合 `subscribeWithSelector` 可订阅子状态。

## 组织方式

- **多 store 按域拆分**（auth、cart、ui），每个保持聚焦；zustand store 间可直接互相 import 读取。
- **单 store 过大用 slice 模式**：每个 slice 是 `(set, get) => ({...})`，最后 `create<Store>()((...a) => ({ ...createA(...a), ...createB(...a) }))`。

## 常见陷阱

- selector 返回新对象/新数组，且未用 `useShallow` → 无限重渲染。
- 未用 `immer` 却直接 mutate state → 不触发更新（`set({...})` 必须返回新引用或走 immer）。
- `persist` 存了函数或瞬时 UI 状态 → 用 `partialize` 白名单。
- SSR/Next.js 中 `persist` 引发 hydration mismatch → 用 `skipHydration` 后在客户端手动 `rehydrate()`。
- 在 store 外闭包捕获 `get()` 结果 → 读到旧值，实时读应调用 `getState()`。

相关技能：React 组件与渲染性能见 `vercel-react-best-practices`；Provider 组合与 Context 边界见 `vercel-composition-patterns`。
