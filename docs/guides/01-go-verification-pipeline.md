# mocode Go 验证流水线

> 整理时间：2026-06-12  
> 背景：经过多次依赖升级、孤儿代码清理、SSH 工具开发，沉淀出的**必跑**流水线

---

## 🚦 5 步验证流水线

| 步骤 | 命令 | 期望 | 检查什么 | 耗时 |
|---|---|---|---|---|
| **1. Build** | `go build ./...` | exit 0，**0 stdout** | 编译通过、无符号丢失 | ~3s |
| **2. Vet** | `go vet ./...` | exit 0，**0 stdout** | 静态检查（结构、API 误用、shadowing） | ~3s |
| **3. Test Compile** | `go test -count=1 -run='^$' ./...` | exit 0，所有包 `ok`/`? [no test files]` | 测试文件**编译**通过 | ~10s |
| **4. Test Run (short)** | `go test -count=1 -short ./internal/config/... ./internal/agent/... ./internal/ui/... ./internal/cmd/...` | exit 0，所有包 `ok` | 核心包单元测试通过 | ~30s |
| **5. Test Run (full)** | `go test -count=1 ./...` | exit 0，所有包 `ok` | 完整测试套件 | ~5min |

---

## 🎯 6 个最小行为测试

每次代码变更后**必跑**这 6 个动作（不依赖单元测试）：

| # | 命令 | 验证什么 |
|---|---|---|
| 1 | `mocode --version` | 二进制可执行，版本号正常 |
| 2 | `mocode --help` | CLI 解析正常，所有子命令注册成功 |
| 3 | `mocode doctor` | 配置文件可解析，Provider 加载正常 |
| 4 | `mocode models` | 配置模型枚举，Provider 协议层正常 |
| 5 | `mocode dirs` | 用户目录检测，跨平台路径处理 |
| 6 | `mocode session list` | SQLite/file 会话存储可读 |

**这 6 个动作能覆盖 80% 的回归**（编译失败 / import cycle / 配置 schema 错 / Provider 注册失败 / 平台路径 bug / 存储层破坏）。

---

## 🚨 行为测试失败的分级处理

| doctor 输出 | 含义 | 行动 |
|---|---|---|
| ✅ All passed | 无回归 | 通过 |
| ⚠️ 1-2 warn (e.g. 配置文件不存在) | 用户态问题 | 接受 |
| ❌ 1 fail: 网络 403/502 | **网络问题**，不是代码 | 接受，记录 |
| ❌ 1 fail: 配置文件解析错误 | **代码问题** | 立即修复 |
| ❌ 1 fail: 二进制自身 panic | **代码问题** | 立即回滚 |

---

## 📊 当前 mocode 验证基线（2026-06-12）

| 指标 | 值 |
|---|---|
| Go 版本 | 1.26.4 |
| 包总数 | 64+ |
| 测试覆盖 | core 包全有（config/agent/cmd/ui/tools） |
| `go build ./...` | ✅ 0 错误 |
| `go vet ./...` | ✅ 0 错误 |
| `go test -short` | ✅ 0 失败 |
| linter 警告 | 11 个（gopls QF1003 等风格建议，与功能无关）|

---

## 🔧 一次性快速验证脚本

```bash
# 步骤 1+2：必跑，3 秒
go build ./... && go vet ./... && echo "✓ BUILD+VET OK"

# 步骤 3：删除/重构后必跑，验证测试可编译
go test -count=1 -run='^$' ./...

# 步骤 4：核心包快测，30 秒
go test -count=1 -short ./internal/config/... ./internal/agent/... ./internal/ui/... ./internal/cmd/...

# 步骤 5：完整测试，5+ 分钟（CI 跑）
go test -count=1 ./...
```

---

## 🧪 行为测试不能替代的

| 场景 | 正确工具 |
|---|---|
| 网络/IO 边界 | `httptest.NewServer` 单测 |
| UI 渲染 | 手动 `mocode --continue` 启动 TUI 验证 |
| 大文件处理 | 手动传 100MB 文件给相关工具 |
| 并发竞争 | `go test -race ./...` |

---

## 📋 验证流水线使用规范

| 现象 | 含义 | 行动 |
|---|---|---|
| `?  	pkg [no test files]` | 包里**没有**测试文件 | ✅ 正常（业务包允许无测试）|
| `ok  	pkg` | 测试全部通过 | ✅ 正常 |
| `FAIL pkg` | 有测试失败 | ❌ 必须立即修复 |
| `undefined: X` | 删除导致悬挂引用 | ❌ 立即回滚删除或补回 |
| `compiles but not used` | 未使用的 import/var | ⚠️ 需清理 |
| `import cycle` | 包循环依赖 | ❌ 架构问题 |
