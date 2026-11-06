# 12 · 发布检查清单（Release Checklist）

> 用途：把"准备发布"变成一组可勾选、可验证的步骤。适用于从当前开发分支
> （相对上游基线 25 个提交）发布 **v0.12.0**。
>
> 版本真相源是 **git tag**；`internal/util/version/version.go` 只是 `go install`
> 的兜底显示，必须与最新 tag 保持一致。

---

## 1. 发布前门禁（必须全绿）

| 检查 | 命令 | 期望 |
|---|---|---|
| 构建 | `CGO_ENABLED=0 go build ./...` | 成功 |
| 单元/集成测试 | `CGO_ENABLED=0 go test -short ./internal/... ./pkg/...` | 无 FAIL |
| 完整测试（发布前） | `CGO_ENABLED=0 go test -count=1 ./internal/... ./pkg/...` | 无 FAIL |
| 格式 | `gofmt -l internal pkg scripts` | 无输出 |
| 层规则 | `go run ./scripts/layercheck` | 无违规 |
| 全量 lint | `bash scripts/lint-full.sh` | 无 **新** 错误；存量债务见下 |
| 配置 schema | `./bin/mocode schema` 与上一发布版本比较 | properties 不变 |
| MCP 冒烟 | `printf ... \| ./bin/mocode mcp serve` | initialize/tools/call 正常 |
| 密钥扫描 | `git log -p --all \| grep -i 'sk_'` | 无命中 |

### 当前已知的 lint 存量（不阻塞 0.12.0）

```
wrapcheck 132 / nestif 65 / exhaustive 46 / unparam 45
goconst 38 / prealloc 36 / nilerr 33 / godox 27 / gosec 12
errcheck 0 / staticcheck 0
```

新代码由 errcheck + golangci-lint 的 per-diff 作业把关；上表是历史债务，
按包逐步清理，不要求一次清零。

## 2. 发布步骤

```bash
# 1) 工作区必须干净
git status --short          # 期望空

# 2) 版本与变更日志
#    - CHANGELOG.md 增加本次版本条目
#    - internal/util/version/version.go 的 Version 与最新 tag 同步
grep -n 'Version = ' internal/util/version/version.go

# 3) 提交收尾改动
git add CHANGELOG.md internal/util/version/version.go ...
git commit -m "🔖 chore: 准备 v0.12.0 发布"

# 4) 构建产物验证
CGO_ENABLED=0 go build -buildvcs=false -o bin/mocode .
./bin/mocode --version        # 期望 0.12.0
./bin/mocode mcp serve </dev/null  # 期望干净退出（无 stdout 垃圾）

# 5) 打 tag（发布动作，需维护者执行）
git tag -a v0.12.0 -m "mocode v0.12.0"
git describe --tags --always

# 6) 推送（tag 会触发 release workflow）
git push origin main
git push origin v0.12.0

# 7) 观察 CI
#    - Test job（test.yml）
#    - Race job（-race，仅 Linux CI 可运行）
#    - Lint job（per-diff）
#    - Release workflow（release.yml）

# 8) 发布后验证
#    - 下载 release 产物，核对 checksum
#    - 目标平台运行 `mocode --version` / `mocode doctor`
```

## 3. 当前状态（截至准备提交）

| 步骤 | 状态 | 备注 |
|---|---|---|
| 工作区干净 | ✅ | `git status` 为空 |
| 构建 / 测试 / 格式 / 层规则 | ✅ | 已本地验证 |
| CHANGELOG + 版本号 | ✅ | 0.12.0 |
| MCP 冒烟 | ✅ | initialize + tools/call 返回模型 |
| 推送 main | ⏳ 待维护者 | 本地领先 `origin/main` 25 个提交 |
| 打 tag `v0.12.0` | ⏳ 待维护者 | 仅发布时执行 |
| CI 实跑（race / lint-full / release） | ⏳ 待推送 | 本地 android/arm64 不支持 `-race` |
| 密钥轮换（tokenrhythm） | ⏳ 待维护者 | 明文在 DSH profile，不在仓库 |

## 4. 回滚

- tag 未推送：`git tag -d v0.12.0`。
- 已推送：`git push origin :refs/tags/v0.12.0` 后重新打 tag。
- 版本号回退：还原 `internal/util/version/version.go` 并重新提交。

## 5. 交接物

若无法直接推送（无远端凭据），使用：

```bash
git bundle create mocode-agent-0.12.0.bundle main
git format-patch 8e7ca8e..HEAD -o patches/
```

接收方 `git bundle verify` / `git am patches/*.patch` 即可复现。
