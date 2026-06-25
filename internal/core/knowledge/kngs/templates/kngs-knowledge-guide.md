---
name: kngs-knowledge-guide
description: 如何正确使用 kngs 知识库系统
tags: [kngs, guide, knowledge]
---

# Kngs 知识库使用指南

## 什么是 kngs?

kngs 是一个**渐进式参考资料库**。存放在 `.agents/kngs/` 或 `.mocode/kngs/`
目录中的 Markdown 文件会被 fsnotify 自动监控。当文件发生创建、修改、删除时，
内容会自动同步到助手的 memory 中，实现**热更新知识库**。

## 存储什么

适合放入 kngs 的内容：

| 类型 | 示例 |
|------|------|
| **架构决策** | 为什么选择某个技术栈、整体架构设计 |
| **代码规范** | 项目特有的编码约定、命名规范 |
| **API 文档** | 内部 API 接口定义、调用方式 |
| **开发流程** | 构建、测试、部署步骤 |
| **领域知识** | 业务逻辑说明、专业术语解释 |
| **教训记录** | 踩过的坑、已知问题、规避方案 |

## 文件格式

每个 `.md` 文件推荐包含 YAML frontmatter：

```yaml
---
name: short-name
description: 一句话描述这条知识
tags: [相关, 标签]
---
正文内容...支持 Markdown 格式。
```

## 命名规范

- 文件名：英文小写短横线，如 `api-design.md`
- `name` 字段：英文小写短横线
- `tags` 字段：数组，用于检索分类

## 最佳实践

1. **每文件一主题** — 方便检索和复用
2. **描述要精准** — 帮助模型判断何时该引用
3. **标签要全面** — 至少包含所属领域标签
4. **内容要完整** — 自包含，不需要引用外部文件
5. **定期清理** — 过时的知识应及时删除或更新

## 工作原理

```
文件变更 → fsnotify 捕获事件 → 500ms 去抖
  → kngs.Store.AddOrUpdate() → memory.AddMemory()
  → 模型在下次会话中自动加载相关记忆
```

删除文件 → `kngs.Store.Remove()` → `memory.DeleteMemory()` → 知识自动移除。
