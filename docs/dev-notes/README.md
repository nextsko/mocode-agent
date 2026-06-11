# mocode 开发笔记（dev-notes）

> 沉淀自 2026-06-12 一次完整的代码考古 + 依赖升级实战。

## 📚 笔记清单

| 文件 | 主题 | 适用场景 |
|---|---|---|
| [go-verification-pipeline.md](./go-verification-pipeline.md) | Go 5 步验证流水线 + 6 个行为测试 | 每次代码变更后必跑 |
| [agent-cooperation.md](./agent-cooperation.md) | 子代理协作的 7 条铁律 | 派子代理做发现任务时 |
| [gfw-workarounds.md](./gfw-workarounds.md) | GFW 环境下网络工具 + Go 临时程序模板 | 拉取 Gitea/GitHub API 时 |
| [dependency-upgrade-procedure.md](./dependency-upgrade-procedure.md) | 依赖升级 SOP（含 dependabot 解读） | dependabot PR 或手动升级时 |

---

## 🎯 5 条核心金句

1. **永远二次核验子代理报告** — 错误删除的代价比子代理节省的时间更大
2. **临时 Go 程序是 GFW 环境唯一可靠的网络工具** — 显式 ProxyURL + ResponseHeaderTimeout
3. **行为测试 ≥ 单元测试** — `doctor` 1 分钟能发现 `go test` 1 小时找不到的回归
4. **commit message 写"为什么"不写"做了什么"** — 让 git log 成为项目历史文档
5. **删除前先 git status** — 工作区基线是所有"清理"操作的前提

---

## 🔄 快速决策树

### "我该派子代理做这个任务吗？"

```
是"发现型"任务（扫描/统计/查询）  → 可以派，但要贴证据
是"决策型"任务（删除/重构/迁移）  → 自己来，不派
需要二次核验                       → 派完自己再 grep 一次
```

### "我该删这个文件吗？"

```
1. 用 grep 确认 0 包外引用           ← 必做
2. 用 grep 确认 0 struct field 引用  ← 必做
3. 用 grep 确认 0 接口实现引用        ← 必做
4. 检查 //go:embed / init() 引用    ← 必做
5. git status 确认基线              ← 必做
6. 备份或在新分支操作                ← 推荐
7. 删除                              ← 现在可以
8. go build / go vet 验证           ← 必做
9. go test -short 验证              ← 必做
10. 6 行为测试验证                   ← 必做
```

### "我该升这个依赖吗？"

```
patch (0.0.x)     → 直接升
minor (0.x.0)     → 跑全量测试
major (x.0.0)     → 读 CHANGELOG + 改代码
工具链             → 同步 go.mod toolchain 行
```

### "我该升 GitHub Action 吗？"

```
dependabot 标题是 v6→v7，本地 v6 → 升 v7
dependabot 标题是 v6→v7，本地已是 v9 → 直接升 v9 对齐
两个 workflow 用不同版本 → 对齐到最新
v6/v7 有 `version: latest` 参数 → v9 之后不需要，删掉
```

---

## 📊 本次会话沉淀统计

| 资产 | 数量 |
|---|---|
| 错误工具陷阱 | 10+ |
| Go 验证流水线步骤 | 5 步 + 6 行为 |
| 子代理协作铁律 | 7 条 |
| GFW 工作模板 | 5 关键点 |
| 升级分级表 | 4 类 |
| dependabot 解读陷阱 | 2 类 |
| 本次实战 commit | 2 个 |
| 删除孤儿总数 | 95 文件 / -6938 行 |
| 升级包总数 | 10 个 + 1 工具链 |

---

## 🔗 相关文档

- [docs/ci-fix-log.md](../ci-fix-log.md) — 历史 CI 修复记录
- [docs/slash-command-arch.md](../slash-command-arch.md) — slash command 架构
- [AGENTS.md](../../AGENTS.md) — 项目总开发指南
