# sandbox-and-mru — TS/uv 双沙箱工具 + slash MRU

> 状态：执行中
> 创建：2026-09-13
> 范围：`tools/core/sandbox/`（新）、`ui/completions`（MRU）
> 参考：grok-build `xai-grok-sandbox`（内核级 profile，本次取其 profile/超时/输出有界思想做语言级轻量版）；`xai-grok-pager/src/slash/mru.rs`（MRU 排序）

## 决策记录

- bubbles list 底层已是 `sahilm/fuzzy` 模糊匹配（slash 补全现状 OK）——grok 补全的增量吸收点为 **MRU 排序**（最近使用优先）；参数级补全（arg items）列入后续
- grok 的内核级沙箱（nono/landlock）超出 Go CLI 合理边界；本沙箱为**语言级隔离**：临时目录 + 超时 + 输出上限 + 依赖临时环境（uv --with）
- 双工具：`ts_run`（bun 优先，node 回退）、`py_run`（uv 优先，python 回退；deps 临时依赖是核心卖点）

## 实现要点

1. `sandbox/shared.go`：temp 工作目录、超时（默认 60s/上限 300s）、合并输出 1MiB head-tail 有界、退出码/错误分类、目录清理
2. `sandbox/ts.go` / `sandbox/python.go`：runtime 探测缓存（bun/uv/node/python）、权限审批（同 bash 工具）、UV deps → `uv run --with a --with b`
3. MRU：`ui/completions` 选择时记录（`infra.DataDir()/slash-mru.json`，上限 32），空查询时组内 MRU 优先排序

## 验收

- build/vet/test 绿；sandbox 工具单测（echo/超时/输出截断）；MRU 排序单测
- 注册进 allToolNames（registry 清单测试同步）
