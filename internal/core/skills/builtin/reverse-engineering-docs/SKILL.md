---
name: reverse-engineering-docs
description: >-
  Use when 用户要「把项目逆向成可复刻的文档 / 生成架构文档 / 用自然语言描述整个设计 /
  全面扫一下还有什么没弄的」，目标是让人只看文档就能完整复刻项目时。产物是 `docs/<project>-arch/`
  下的一套自然语言文档，强调可执行命令、样式系统、权限/自动导入等易遗漏项，并做「删掉代码能否复刻」验证。
---

# 逆向工程为可复刻文档（Reverse Engineering to Reproducible Docs）

## 概述

把现有项目逆向拆解成**完整的自然语言文档**，目标是：任何人看了文档都能完整复刻这个项目。

核心原则：**代码 → 自然语言 + 图 + 流程 = 可复刻**。

与 `project-teardown` 的区别：teardown 偏「理解与评估」，本技能偏「复刻」——文档必须可执行、无遗漏，能作为重建项目的唯一依据。文档目录纪律参照 `docs-rulebook`。

## 何时使用

满足任一即触发：

- 「拆解项目成文档」「生成可复刻的架构文档」「把这个项目逆向成 md」
- 「我想把项目删了，只保留文档」
- 「用自然语言描述整个设计」
- 「全面扫一下还有什么没弄的」

## 交付物

```
docs/<project-name>-arch/
├── README.md                      # 索引目录 / 导航
├── 01-design-philosophy.md        # 设计哲学 (why)
├── 02-architecture-overview.md    # 架构总览 (what)
├── 03-module-design.md            # 模块设计详解 (how)
├── 04-component-interactions.md   # 组件交互设计
├── 05-data-flow.md                # 数据流与状态管理
├── 06-frontend-stack.md           # 前端技术栈 ⭐
├── 07-backend-stack.md            # 后端技术栈 ⭐
├── 08-commands-collection.md      # 运行命令集合
├── 09-build-scripts.md            # 构建脚本详解 ⭐
├── 10-deployment-guide.md         # 部署指南
├── 11-persistence-strategy.md     # 持久化策略 ⭐
└── 12-boundary-conditions.md      # 边界条件处理
```

⭐ = 容易被遗漏，必须补充。

## Phase 1：Discovery（发现）

扫描完整项目结构，排除噪音目录：

```powershell
Get-ChildItem -Recurse -File -Include *.ts,*.vue,*.rs,*.json,*.toml,*.html,*.css,*.scss |
  Where-Object { $_.FullName -notmatch 'node_modules|target|\.git|dist' } |
  Select-Object -ExpandProperty FullName | Sort-Object
```

识别技术栈与关键目录：

```bash
cat go.mod / package.json / Cargo.toml / pyproject.toml / Cargo.lock   # 语言/框架依赖
cat vite.config.ts / webpack.config.js / Makefile                     # 构建配置
ls .github/workflows/                                                 # CI/CD ⭐
```

```
├── src/ / src-tauri/ / frontend/   # 源代码
├── .github/workflows/              # CI/CD ⭐
├── .vscode/                        # IDE 配置 ⭐
├── build/ / scripts/               # 构建脚本
└── assets/ / public/ / static/     # 静态资源 ⭐
```

## Phase 2：Analysis（分析）

### 必读文件清单（扩展版）

| 类型 | 文件 | 读取原因 |
|------|------|----------|
| **入口** | main.go / main.ts / app.ts / main.rs | 启动流程 |
| **配置** | package.json / Cargo.toml / pyproject.toml | 完整依赖清单 |
| **构建** | vite.config.ts / webpack.config.js / Makefile | 构建配置 |
| **框架配置** | tauri.conf.json / wails.json / next.config.js | 框架特定配置 |
| **权限配置** | capabilities/*.json / permissions.json ⭐ | 桌面应用权限 |
| **自动导入** | auto-imports.d.ts / components.d.ts ⭐ | 组件/API 自动导入 |
| **样式** | *.css / *.scss / tailwind.config.js ⭐ | 样式系统 |
| **主题** | theme.ts / theme.css / *_theme.ts ⭐ | 主题配置 |
| **路由** | router/ / routes/ / _routes.ts | 页面结构 |
| **状态** | stores/ / context/ / reducers/ | 状态管理 |
| **核心业务** | core/ / services/ / agentcore/ | 主要逻辑 |
| **工具** | utils/ / helpers/ / lib/ | 辅助函数 |
| **组件** | components/ / ui/ / widgets/ | UI 组件 |
| **Composables** | composables/ / hooks/ / use*.ts ⭐ | 组合式函数 |
| **类型** | types/ / models/ / interfaces/ | 数据结构 |
| **CI/CD** | .github/workflows/*.yml ⭐ | 自动构建发布 |
| **IDE 配置** | .vscode/extensions.json / settings.json ⭐ | 开发环境 |

## Phase 3：Documentation（文档化）

每个模块必须包含：

1. **模块职责** — 做什么
2. **核心接口** — 公开方法签名
3. **数据结构** — 关键类型定义
4. **交互流程** — 调用关系
5. **边界处理** — 错误、超时、空值
6. **代码示例** — 关键逻辑的简化示例，并配「人话」解释

质量标准：

| 维度 | 要求 |
|------|------|
| 完整性 | 别人看文档能跑起来 |
| 可执行性 | 命令、脚本都经过验证 |
| 自然语言 | 不直接贴大段代码，用「人话」解释为什么这样配 |
| 图解 | 关键流程用 ASCII 图 |
| 索引 | README 有导航 |
| 遗漏检查 | 扫描后再确认无遗漏 ⭐ |

## Phase 4：Verification（验证）

问自己：**「如果我把所有代码删了，只保留这些文档，能复刻出来吗？」**

```
完整复刻清单：
□ 设计哲学清晰 (为什么这样做)        □ 前端技术栈明确 (用什么 UI)
□ 架构图完整 (整体结构)              □ 后端技术栈明确 (用什么框架)
□ 目录结构详细 (每个文件夹)          □ 样式系统完整 (CSS/主题/变量)
□ 模块设计详细 (每个组件)            □ 自动导入配置 (Vite/UnoCSS 等)
□ 交互流程明确 (怎么协作)            □ Composables 说明 (组合式函数)
□ 数据流清楚 (数据怎么传)            □ 公共组件说明 (可复用组件)
□ 持久化策略明确 (怎么存数据)        □ 运行命令完整 (怎么跑起来)
□ 边界条件说明 (错误怎么处理)        □ 构建脚本齐全 (怎么打包)
                                     □ CI/CD 配置 (GitHub Actions 等)
                                     □ IDE 配置 (.vscode)
                                     □ 部署指南可执行 (怎么发布)
```

全部 YES = 文档完整；有 NO = 补全缺失部分。

## 常见错误

| 错误 | 问题 | 解决 |
|------|------|------|
| 只贴代码不解释 | 难以理解意图 | 用自然语言描述「为什么」 |
| 遗漏边界条件 | 复刻后遇到问题 | 显式说明错误处理 |
| 遗漏样式系统 | UI 无法复刻 | 包含 CSS/SCSS/主题配置 |
| 遗漏权限配置 | 桌面应用无法运行 | 包含 capabilities/*.json |
| 遗漏自动导入 | 组件无法使用 | 包含 auto-imports.d.ts 说明 |
| 遗漏 Composables | 逻辑无法复用 | 说明 useXXX.ts 用途 |
| 遗漏 CI/CD | 发布流程缺失 | 包含 GitHub Actions 配置 |
| 遗漏 IDE 配置 | 开发体验差 | 包含 .vscode/extensions.json |
| 假设读者懂技术 | 新手无法复刻 | 解释每个工具的作用 |

## 扫描后核对清单

```
[ ] package.json → 检查依赖版本
[ ] vite.config.ts → 检查自动导入配置
[ ] capabilities/ → 检查权限配置
[ ] .github/workflows/ → 检查 CI/CD
[ ] .vscode/ → 检查 IDE 配置
[ ] assets/ 或 public/ → 检查静态资源
[ ] composables/ 或 hooks/ → 检查组合式函数
[ ] theme/ → 检查主题配置
[ ] *.d.ts → 说明自动导入声明
```

## 最后一步

拆解完成后，主动问用户：

> 我已经完成了文档拆解。还有什么遗漏的内容吗？

这样确保没有遗漏重要内容。
