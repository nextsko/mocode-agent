# 依赖升级 SOP（含 dependabot 解读）

> 整理时间：2026-06-12  
> 背景：本次成功升级 10 个 Go 模块（含 1 个 minor 跳跃 8 版的 fantasy），并对齐 goreleaser-action 到 v9

---

## 🎯 dependabot 解读陷阱

### 陷阱 1：dependabot 报告的"版本滞后"现象

**现象**：PR 标题写 `bump v6 → v7`，但 merge 时发现本地已经是 v9 了。

**根因**：dependabot 在创建 PR 的那一刻扫描仓库，PR 一旦创建就不再更新。但**其他 PR / 人工提交**可能已经把版本升到比 PR 标题更高的版本。

**应对策略**：

| 场景 | 决策 |
|---|---|
| PR 标题 v6→v7，本地 v6 | 升到 v7 |
| PR 标题 v6→v7，本地已经是 v7 | 不动 |
| PR 标题 v6→v7，本地已经是 v9 | **不必看 PR 标题，直接升到 v9**（保持对齐） |
| PR 标题 v6→v7，本地 v8 是个有 bug 的版本 | 跳到 v9 |

**本会话实战**：PR #2 标题写 v6→v7，但 `release.yml` 已是 v9。我直接升 v6→v9（与 release.yml 对齐），并在 commit message 写明"dependabot 标题已过时"。

### 陷阱 2：dependabot 标题的"包数"与实际可能不符

PR #1 标题说"14 updates"，实际 `go.mod` 体现的**直接升级只有 10 个**。其余 4 个是：
- 隐式传递依赖被顺带升级（如 bubbles/lipgloss 跟随 bubbletea）
- dependabot 把"all group"内的所有 patch+minor 算上

**应对**：永远以 `git diff go.mod` 为准，不要盲信 PR 标题。

---

## 🔧 升级类 PR 的"安全"分级

| 升级类型 | 风险 | 安全措施 |
|---|---|---|
| patch (0.0.x) | 极低 | 直接升 |
| minor (0.x.0) | 中 | 跑全量测试 |
| major (x.0.0) | 高 | 读 CHANGELOG，必要时改代码 |
| 工具链 | 中 | 检查 CI 是否能跑新工具链 |

本会话实战：
- 9 个 patch/minor 直接升
- 1 个 minor（catwalk 0.39→0.44）跑全量测试
- 1 个 minor（fantasy 0.23→0.31）跑全量测试 + 工具链切换

---

## 📋 升级 Go 模块的标准 7 步

```bash
# 1. 先做基线检查
git status --short
go build ./...  # 确认基线可编译

# 2. 按风险分级升级（patch 先，minor 后，major 最后）
go get <pkg>@<version>  # 一个一个来

# 3. 同步 go.mod / go.sum
go mod tidy

# 4. 验证 5 步流水线
go build ./... && go vet ./...
go test -count=1 -run='^$' ./...           # 测试编译
go test -count=1 -short ./internal/...     # 核心包测试

# 5. 行为测试
go build -o tmp/mocode-test.exe .
tmp/mocode-test.exe --version
tmp/mocode-test.exe --help
tmp/mocode-test.exe doctor
tmp/mocode-test.exe models
tmp/mocode-test.exe dirs
tmp/mocode-test.exe session list
rm tmp/mocode-test.exe

# 6. 如果有 breaking change，改业务代码
# 7. 分批 commit
```

---

## 🔄 Go 工具链自动升级机制

**现象**：
```
go: charm.land/fantasy@v0.31.0 requires go >= 1.26.4; switching to go1.26.4
go: downloading go1.26.4 (windows/amd64)
```

**机制**：当依赖要求的 Go 版本 > 当前 `go.mod` 声明的版本时，Go 会：
1. 自动下载对应版本的工具链（到 `GOPATH` 缓存）
2. 用新工具链执行 `go get`
3. 不会自动改 `go.mod` 的 `go 1.26.2` 行
4. 但**`go env GOTOOLCHAIN`** 会显示新版本

**应对**：
- 在 `go.mod` 顶部手动加 `toolchain go1.26.4` 声明（最佳实践）
- 或在 CI 中明确 `go-version-file: go.mod` 配合 `actions/setup-go@v6` 自动读取
- 不要让 CI 缓存旧工具链 — 否则会出现"本地 go1.26.4 编，CI 1.26.2 编失败"

---

## 🧹 `go mod tidy` 的 3 件事

1. **删除**所有 import 链上未使用的 require
2. **添加**所有 transitively imported 的依赖到 require/indirect
3. **重新计算**所有 hash 并更新 go.sum

**注意**：tidy 是**唯一权威**的"声明 vs 实际" 同步器。其他命令（`go get`、`go build`）都可能留下死依赖。

---

## 🎨 升级 GitHub Actions 的规范

### 1. 同一项目不同 workflow 用同一 action 应当对齐版本

mocode 的 `build.yml` 用 `goreleaser-action@v6`，`release.yml` 用 `goreleaser-action@v9` — **不一致**。

**应对**：升到与最新 workflow 一致的版本（v9），不要被 dependabot PR 标题"v7"束缚。

### 2. v9 之后不需要 `version: latest` 参数

```yaml
# ❌ 旧写法
- uses: goreleaser/goreleaser-action@v6
  with:
    version: latest
    args: release --clean

# ✅ v9 写法（action release tag 自动指向最新）
- uses: goreleaser/goreleaser-action@v9
  with:
    args: release --clean
```

---

## 📜 提交模板

### 依赖升级

```
🔧 chore(deps): bump 10 个 Go 模块 (catwalk 0.39→0.44, fantasy 0.23→0.31, bubbletea 2.0.6→2.0.7 等)

批量升级 10 个 Go 模块依赖（来自 dependabot all group）：
- charm.land/bubbletea/v2: 2.0.6 → 2.0.7
- charm.land/catwalk: 0.39.3 → 0.44.14
- charm.land/fantasy: 0.23.0 → 0.31.0（要求 Go ≥ 1.26.4）
- ...

副作用：Go 工具链 1.26.2 → 1.26.4（fantasy@v0.31.0 强制要求）。
所有 catwalk/fantasy API 升级在源代码层面无破坏，构建/测试/行为全绿。
```

### CI 升级

```
🔧 chore(ci): bump goreleaser-action v6 → v9 (对齐 release.yml)

build.yml 与 release.yml 之前使用不同版本的 goreleaser-action (v6 vs v9)，
本次统一升级到 v9，并移除 build.yml 中冗余的 version: latest 参数
（v9 通过 action 的 release tag 自动指向最新版）。

dependabot 原 PR 升级到 v7，但 v9 已是新稳定版且与 release.yml 一致。
```

---

## 🚀 本次升级实战记录

| 包 | From | To | 风险等级 |
|---|---|---|---|
| `charm.land/bubbletea/v2` | 2.0.6 | 2.0.7 | ✅ patch |
| `charm.land/catwalk` | 0.39.3 | 0.44.14 | ⚠️ 5 minor（无 API 破坏）|
| `charm.land/fantasy` | 0.23.0 | 0.31.0 | ⚠️ 8 minor（无 API 破坏）|
| `github.com/alecthomas/chroma/v2` | 2.24.1 | 2.26.1 | ✅ patch |
| `github.com/charmbracelet/x/powernap` | 0.1.4 | 0.1.6 | ✅ patch |
| `github.com/charmbracelet/ultraviolet` | 489999b90468 | 948f4557a654 | ✅ patch |
| `github.com/fsnotify/fsnotify` | 1.9.0 | 1.10.1 | ✅ minor |
| `github.com/modelcontextprotocol/go-sdk` | 1.6.0 | 1.6.1 | ✅ patch |
| `github.com/sahilm/fuzzy` | 0.1.1 | 0.1.2 | ✅ patch |
| `github.com/tidwall/gjson` | 1.18.0 | 1.19.0 | ✅ minor |
| `modernc.org/sqlite` | 1.50.0 | 1.52.0 | ✅ minor |
| **Go 工具链** | **1.26.2** | **1.26.4** | fantasy 强制要求 |

**验证结果**：build/vet/test 0 错误，6/6 行为测试全过，0 行为层面回归。
