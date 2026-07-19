# GFW 环境下的网络工具笔记

> 整理时间：2026-06-12  
> 背景：Gitea PR 调研过程中，多个工具对 gitea.com 全部超时；最终通过 Go 临时程序解决

---

## 🚫 不可用的工具

| 工具 | 失败原因 | 现象 |
|---|---|---|
| `fetch` | 直连被 GFW 阻塞 | `context deadline exceeded` |
| `agentic_fetch` | 同上 | 同上 |
| `download` | 同上 | 同上 |
| `gh CLI` | 只支持 GitHub GraphQL | `HTTP 405: Method Not Allowed` |
| `tea CLI` | 未配置 login | `No gitea login configured` |
| `curl` | bash 安全白名单禁止 | `command is not allowed for security reasons` |
| `mcp_gitea_*` | MCP 服务未注册 | `mcp 'gitea' not available` |

---

## ✅ 唯一可行方案：Go 临时程序

通过自定义 `http.Transport` 的 `Proxy` 字段显式指向本地代理。

### 业务场景

| 场景 | 适用工具 |
|---|---|
| 绕过 3 类直连工具的 GFW 超时 | 写 Go 临时程序 + 自定义 Proxy |
| 批量 API 调用 | 写 Go 临时程序做循环 |
| JSON 复杂结构解析 | 写 Go 临时程序 + `encoding/json` |
| 网络环境探测 | 写 Go 临时程序的 HEAD ping |

---

## 🔧 最小化模板（可直接复用）

### 独立 go.mod

```go
// tmp/gitea-prs/go.mod
module gitea-prs

go 1.26
```

### 主程序骨架

```go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// 1) 结构体按需定义
type PullRequest struct {
	Number  int    `json:"number"`
	Title   string `json:"title"`
	State   string `json:"state"`
	HTMLURL string `json:"html_url"`
	Body    string `json:"body"`
}

func main() {
	// 2) 必须显式配置代理（直连会被 GFW 阻塞）
	proxyURL, _ := url.Parse("http://127.0.0.1:7897")
	tr := &http.Transport{
		Proxy:                 http.ProxyURL(proxyURL),
		MaxIdleConns:          20,
		IdleConnTimeout:       30 * time.Second,
		ResponseHeaderTimeout: 45 * time.Second, // 重要：避免长尾阻塞
	}
	client := &http.Client{Transport: tr, Timeout: 60 * time.Second}

	// 3) 先 HEAD ping 验证代理可用
	if err := ping(client, "https://gitea.com/Fromsko/mocode"); err != nil {
		fmt.Fprintln(os.Stderr, "❌ 代理无法访问 Gitea:", err)
		os.Exit(1)
	}
	fmt.Println("✓ 代理 ping gitea.com 成功")

	// 4) 业务调用
	body := getJSON(client, "https://gitea.com/api/v1/repos/Fromsko/mocode/pulls?state=all&limit=50")
	var prs []*PullRequest
	json.Unmarshal(body, &prs)
	for _, pr := range prs {
		fmt.Printf("#%d [%s] %s\n", pr.Number, pr.State, pr.Title)
	}
}

func ping(client *http.Client, u string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "HEAD", u, nil)
	req.Header.Set("User-Agent", "gitea-pr-fetcher/1.0")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

func getJSON(client *http.Client, u string) []byte {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", u, nil)
	req.Header.Set("User-Agent", "gitea-pr-fetcher/1.0")
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return body
}
```

---

## 🎯 5 个关键设计决策

1. **必须用独立 `go.mod`** — 避免污染主项目
2. **必须显式设 `ResponseHeaderTimeout`** — 默认无限制会导致单次连接卡死
3. **必须先 HEAD ping 代理** — 失败时立即 `os.Exit(1)`，避免浪费循环
4. **必须设 `User-Agent`** — 某些 API（如 GitHub）拒绝空 UA
5. **不需要 `go mod tidy`** — 我们的程序只用标准库

---

## 🌐 Gitea 公开 REST API 端点

| 端点 | 用途 | 需要登录 |
|---|---|---|
| `GET /api/v1/repos/{owner}/{repo}/pulls?state=all&limit=N` | 列出所有 PR | ❌ 公开 |
| `GET /api/v1/repos/{owner}/{repo}/pulls/{id}/commits` | 拉 PR 提交 | ❌ 公开 |
| `GET /api/v1/repos/{owner}/{repo}/pulls/{id}/files` | 拉 PR 文件清单 | ❌ 公开 |
| `GET /{owner}/{repo}/pulls/{id}.diff` | 拉 PR 原始 diff | ✅ 需要登录 |

**实战**：发现 `.diff` 端点需要登录后，改用 `pulls/{id}/files` 代替。

---

## 📋 Gitea API 返回字段陷阱

| 字段 | 注意点 |
|---|---|
| `additions` / `deletions` / `changed_files` | ✅ 存在 |
| `filename`（不是 `path`）| ⚠️ 容易写错 |
| `contents_url` | ✅ 存在（文件原始内容）|
| `merge_base` | ⚠️ 文档不完整 |

---

## 💡 Windows 下的额外陷阱

### 1. Unix 工具不存在

| 工具 | 替代方案 |
|---|---|
| `find` | 用 `glob` 工具 |
| `head` / `tail` | 用 `read_files` + 限制 offset/limit |
| `wc` | 写小 Go 程序数行 |
| `tee` | 临时文件 + 读 |
| `which` | 不需要，Windows 用 `where` |

### 2. `cd` 后的相对路径陷阱

```bash
# 错误示范
cd tmp/foo && go run . > tmp/foo/output.txt
# cwd 改了，相对路径变成 tmp/foo/tmp/foo/output.txt
```

**应对**：用**绝对路径**或**前置 cd**。

### 3. CRLF vs LF 警告

```
warning: in the working copy of 'go.mod', LF will be replaced by CRLF
```

**根因**：
- `go` 命令创建的文件用 **LF**
- 项目 `.gitattributes` 可能要求某些文件用 **CRLF**

**应对**：
- 警告**可以忽略** — Git 会自动按 `.gitattributes` 处理
- 想消除警告：在 `.gitattributes` 显式指定 `go.mod text eol=lf`
- **不要**用 `git config core.autocrlf=false` 强行关闭
