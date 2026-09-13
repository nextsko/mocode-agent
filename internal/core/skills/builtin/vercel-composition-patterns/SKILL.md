---
name: vercel-composition-patterns
description: >-
  Use when 重构布尔 prop 泛滥的组件、构建可复用组件库或设计组件 API，覆盖 compound components、
  显式变体、children 组合、状态上提、context 依赖注入与 React 19 API 变更。适用于组件架构设计与评审。
---

# React 组合模式

用组合构建灵活、可维护的 React 组件。核心目标：**消灭布尔 prop 泛滥**，让状态可依赖注入，让代码对人类与 AI agent 都更易读写。

## 何时使用

- 重构带大量布尔 prop 的组件
- 构建可复用组件库
- 设计灵活的组件 API
- 评审组件架构
- 使用 compound component 或 context provider

> 架构改动影响公共 API 时，按 `code-review-and-quality` 走评审；组件涉及输入校验、鉴权边界时参考 `security-and-hardening`。

## 类别优先级

| 优先级 | 类别 | 影响 | 前缀 |
|---|---|---|---|
| 1 | 组件架构 | HIGH | `architecture-` |
| 2 | 状态管理 | MEDIUM | `state-` |
| 3 | 实现模式 | MEDIUM | `patterns-` |
| 4 | React 19 API | MEDIUM | `react19-` |

## 1. 不要用布尔 prop 定制行为

每个布尔 prop 让可能状态翻倍，组合出无法维护的条件分支。

```tsx
// BAD：isThread / isEditing / isForwarding 组合爆炸
<Composer isThread isEditing={false} channelId="abc" showAttachments />

// GOOD：显式变体，各自说明渲染了什么
<ThreadComposer channelId="abc" />
<EditMessageComposer messageId="xyz" />
```

- `architecture-avoid-boolean-props`：需要新行为时，用组合而非新增布尔开关。
- 判断规则：当同一组件出现 `isX`、`showY` 这类开关超过 1 个，停下来改为组合。

## 2. Compound Components（共享 context）

把复杂组件拆成一组通过 context 共享状态的子组件，消费者只组装自己需要的部分。

```tsx
const ComposerContext = createContext<ComposerContextValue | null>(null);

function ComposerInput() {
  const { state, actions: { update }, meta: { inputRef } } = use(ComposerContext);
  return (
    <TextInput
      ref={inputRef}
      value={state.input}
      onChangeText={(text) => update((s) => ({ ...s, input: text }))}
    />
  );
}

const Composer = {
  Provider: ComposerProvider,
  Frame: ComposerFrame,
  Input: ComposerInput,
  Submit: ComposerSubmit,
  Header: ComposerHeader,
  Footer: ComposerFooter,
};
```

```tsx
<Composer.Provider state={state} actions={actions} meta={meta}>
  <Composer.Frame>
    <Composer.Header />
    <Composer.Input />
    <Composer.Footer>
      <Composer.Formatting />
      <Composer.Submit />
    </Composer.Footer>
  </Composer.Frame>
</Composer.Provider>
```

- `architecture-compound-components`：子组件从共享 context 取状态，不从 props 层层传递。
- 没有隐藏条件分支；消费者显式组装。

## 3. 显式变体优于布尔模式

- `patterns-explicit-variants`：`ThreadComposer` / `EditMessageComposer` / `ForwardMessageComposer` 各自组合所需部件，实现自解释，无不可能状态。
- 变体之间可复用共享内部件，但不是共享一个巨型父组件。

## 4. children 优于 render props

```tsx
// BAD：renderX 回调签名难读、不灵活
<Composer renderHeader={() => <CustomHeader />} renderFooter={...} />

// GOOD：children 自然组合
<Composer.Frame>
  <CustomHeader />
  <Composer.Input />
  <Composer.Footer><Composer.Formatting /><SubmitButton /></Composer.Footer>
</Composer.Frame>
```

- `patterns-children-over-render-props`：静态结构用 children。
- 例外：父组件需要把数据 / 状态回传给子项时，render props 才合适，例如
  `<List data={items} renderItem={({ item, index }) => <Item ... />} />`。

## 5. 通用 Context 接口：state / actions / meta

定义**泛型接口**作为契约，任何 provider 都能实现，从而让同一套 UI 适配不同状态实现。

```tsx
interface ComposerState { input: string; attachments: Attachment[]; isSubmitting: boolean }
interface ComposerActions {
  update: (updater: (s: ComposerState) => ComposerState) => void;
  submit: () => void;
}
interface ComposerMeta { inputRef: React.RefObject<TextInput> }

interface ComposerContextValue {
  state: ComposerState;
  actions: ComposerActions;
  meta: ComposerMeta;
}
const ComposerContext = createContext<ComposerContextValue | null>(null);
```

- `state-context-interface`：UI 只依赖接口，不依赖具体 hook 实现。
- 本地临时状态、全局同步状态可以是不同 provider，UI 组件不变。
- Provider 边界（而非视觉嵌套）决定谁能访问状态：`ForwardButton`、`MessagePreview` 可以位于 `Composer.Frame` 之外，只要在同一 provider 内。

## 6. 状态实现与 UI 解耦 / 状态上提

- `state-decouple-implementation`：只有 provider 知道状态来自 `useState`、Zustand 还是服务端同步。
- `state-lift-state`：把状态移入 provider，供兄弟组件访问；避免 prop drilling、`useEffect` 向上同步、提交时读 ref 这些反模式。

```tsx
function ForwardMessageProvider({ children }: { children: React.ReactNode }) {
  const [state, setState] = useState(initialState);
  const inputRef = useRef(null);
  return (
    <Composer.Provider state={state} actions={{ update: setState, submit }} meta={{ inputRef }}>
      {children}
    </Composer.Provider>
  );
}
```

## 7. React 19 API（React 19+ 才适用）

> 使用 React 18 及以下请跳过本节。

- `react19-no-forwardref`：`ref` 已是普通 prop，不再需要 `forwardRef`。
- 用 `use(Context)` 取代 `useContext()`；`use()` 允许条件调用。

```tsx
// React 19
function ComposerInput({ ref, ...props }: Props & { ref?: React.Ref<TextInput> }) {
  return <TextInput ref={ref} {...props} />;
}
const value = use(MyContext);
```

## 检查清单

- [ ] 是否新增了布尔 prop 开关来定制行为？改为组合或显式变体。
- [ ] 复杂组件是否用共享 context 的 compound component？
- [ ] 静态结构是否用 children，而非 render props？
- [ ] context 是否定义 `state` / `actions` / `meta` 泛型接口？
- [ ] 状态管理细节是否被隔离在 provider 内？
- [ ] 需要共享状态的兄弟组件是否已通过 provider 上提获得？
- [ ] React 19 项目是否去掉了 `forwardRef` 与 `useContext`？
