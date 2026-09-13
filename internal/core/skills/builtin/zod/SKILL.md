---
name: zod
description: >-
  Use when 在 TypeScript 中做运行时数据校验与类型推导：API 输入校验、表单校验、环境变量与配置文件校验，或任何数据边界（DB、URL query、webhook）。使用 z.object/parse/safeParse、z.infer、discriminatedUnion、transform/refine/coerce 与 schema 组合，避免重复定义类型和校验器。
---

# Zod — TypeScript-First Schema 校验

在一个 schema 里同时完成**运行时校验**和**编译期类型推导**，类型与校验器不再各写一份。

## 安装

```bash
npm install zod
```

## Schema 定义与类型推导

```ts
import { z } from "zod";

const userSchema = z.object({
  name: z.string().min(1, "Name is required").max(100),
  email: z.string().email("Invalid email"),
  age: z.number().int().min(18, "Must be 18+").optional(),
  role: z.enum(["user", "admin", "moderator"]).default("user"),
  tags: z.array(z.string()).max(10).default([]),
  address: z
    .object({
      street: z.string(),
      city: z.string(),
      country: z.string().length(2),            // ISO 国家代码
      zip: z.string().regex(/^\d{5}(-\d{4})?$/),
    })
    .optional(),
  metadata: z.record(z.string(), z.unknown()).optional(),
});

// 单一事实来源：由 schema 推导类型，不要再手写 interface
type User = z.infer<typeof userSchema>;
```

## parse vs safeParse

```ts
// parse：失败抛 ZodError
const user = userSchema.parse(requestBody);

// safeParse：返回判别结果，API 边界首选
const result = userSchema.safeParse(requestBody);
if (result.success) {
  result.data;                 // 已推导为 User
} else {
  result.error.flatten();      // { formErrors, fieldErrors }
  result.error.issues;         // 细粒度 path + message
}
```

错误展示：`flatten().fieldErrors` 适合表单字段；`error.issues` 适合映射到多个字段或 i18n。

## 高级模式

```ts
// 判别联合：API 事件、多态数据，TS 能正确收窄
const eventSchema = z.discriminatedUnion("type", [
  z.object({ type: z.literal("click"), x: z.number(), y: z.number() }),
  z.object({ type: z.literal("scroll"), offset: z.number() }),
  z.object({ type: z.literal("keypress"), key: z.string(), modifiers: z.array(z.string()) }),
]);

// transform：解析同时转换
const dateSchema = z.string().transform((s) => new Date(s));
const csvSchema = z.string().transform((s) => s.split(",").map((v) => v.trim()));

// refine：自定义校验，可链式
const passwordSchema = z
  .string()
  .min(8, "At least 8 characters")
  .refine((p) => /[A-Z]/.test(p), "Must contain uppercase")
  .refine((p) => /[0-9]/.test(p), "Must contain number");

// lazy：递归类型
const categorySchema: z.ZodType<Category> = z.object({
  name: z.string(),
  children: z.lazy(() => z.array(categorySchema)).default([]),
});

// coerce：把字符串转成数字（query / env 常用）
const port = z.coerce.number().default(3000);

// pipe：串联变换与校验
const numberFromString = z.string().pipe(z.coerce.number().positive());
```

## 环境变量校验（启动即失败）

```ts
const envSchema = z.object({
  DATABASE_URL: z.string().url(),
  API_KEY: z.string().min(1),
  PORT: z.coerce.number().default(3000),
  NODE_ENV: z.enum(["development", "production", "test"]).default("development"),
});

export const env = envSchema.parse(process.env);  // 缺失变量立即崩溃，而非运行到一半
```

## API 中间件

```ts
function validate<T extends z.ZodType>(schema: T) {
  return (req: Request, res: Response, next: NextFunction) => {
    const result = schema.safeParse(req.body);
    if (!result.success) {
      return res.status(400).json({ errors: result.error.flatten().fieldErrors });
    }
    req.body = result.data;   // 后续拿到已校验且带类型的 body
    next();
  };
}

app.post("/api/users", validate(userSchema), (req, res) => { /* ... */ });
```

## Schema 复用与组合

| 方法 | 作用 |
|---|---|
| `.extend({ ... })` | 增加 / 覆盖字段 |
| `.pick({ a: true })` / `.omit({ b: true })` | 选取或排除字段（典型：DTO/更新用 partial） |
| `.partial()` / `.required()` | 全部可选 / 全部必填 |
| `.merge(other)` | 合并两个 object schema |
| `z.intersection(a, b)` | 交集 |

## 最佳实践与陷阱

- **单一事实来源**：schema 定义一次，用 `z.infer<typeof S>` 出类型，禁止再手写一遍 interface。
- **API 边界用 safeParse**，不要靠 try/catch 包 `parse`。
- **字符串来源一律 `z.coerce.*`**：URL query、env、表单 value 都是 string。
- `.default()` 让 schema 同时承担转换职责；`.optional()` 与 `.default()` 语义不同，注意区分。
- 自定义 message 写在链式方法第二参：`z.string().min(1, "Required")`。
- 校验外部不可信数据（webhook、第三方 API）时用 `.strict()` 拒绝多余字段。
- **Zod 4 版本注意**：字符串格式改为顶层 API（`z.email()`、`z.url()`、`z.uuid()`），`z.string().email()` 等已弃用；`z.record` 需显式传 key 与 value 两个 schema。写代码前确认项目锁定的 zod 大版本。

相关技能：表单与受控组件见 `vercel-react-best-practices`；数据边界的错误呈现与交互态见 `design-taste-frontend`。
