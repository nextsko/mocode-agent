// slash.go provides WeChat-specific slash commands that bypass the agent.
package wechat

import (
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	wechatbot "github.com/nextsko/mocode-agent/internal/integration/wechat/sdk"
)

// slashCommand represents a WeChat-specific command.
type slashCommand struct {
	name        string
	description string
	handler     func(ctx context.Context, ch *Channel, msg *wechatbot.IncomingMessage, args string) string
}

// slashRegistry is built in init().
var slashRegistry []slashCommand

func init() {
	slashRegistry = []slashCommand{
		{"/help", "帮助手册", cmdHelp},
		{"/status", "系统状态", cmdStatus},
		{"/list", "会话列表", cmdList},
		{"/models", "可用模型", cmdModels},
		{"/model", "切换模型", cmdModel},
		{"/test", "测试模型连通性", cmdTestModel},
		{"/screenshot", "截取桌面截图", cmdScreenshot},
		{"/send", "发送文件到微信", cmdSendFile},
	}
}

// handleSlashCommand intercepts slash commands. Uses the bot directly for replies.
// Returns true if the message was fully handled.
func (c *Channel) handleSlashCommand(ctx context.Context, msg *wechatbot.IncomingMessage, text string) bool {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "/") {
		return false
	}

	parts := strings.SplitN(text, " ", 2)
	cmdName := strings.ToLower(parts[0])
	args := ""
	if len(parts) > 1 {
		args = strings.TrimSpace(parts[1])
	}

	for _, cmd := range slashRegistry {
		if cmd.name == cmdName {
			slog.Debug("WeChat slash command", "cmd", cmdName, "args", args)
			reply := cmd.handler(ctx, c, msg, args)
			if reply != "" {
				if err := c.replyWithRetry(ctx, msg, reply); err != nil {
					slog.Error("WeChat slash reply failed", "cmd", cmdName, "error", err)
				}
			}
			return true
		}
	}
	return false
}

// ─── /help ───────────────────────────────────────────────────────────────────

func cmdHelp(_ context.Context, _ *Channel, _ *wechatbot.IncomingMessage, _ string) string {
	var b strings.Builder
	b.WriteString("━━━━━━━━━━━━━━━━\n📖 Mocode WeChat 帮助\n━━━━━━━━━━━━━━━━\n\n")
	for _, cmd := range slashRegistry {
		b.WriteString(fmt.Sprintf("  %-14s %s\n", cmd.name, cmd.description))
	}
	b.WriteString("\n  其他消息 → 总管 Agent 处理\n")
	return b.String()
}

// ─── /status ─────────────────────────────────────────────────────────────────

func cmdStatus(_ context.Context, ch *Channel, _ *wechatbot.IncomingMessage, _ string) string {
	var b strings.Builder
	b.WriteString("━━━━━━━━━━━━━━━━\n🟢 系统状态\n━━━━━━━━━━━━━━━━\n\n")

	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	b.WriteString(fmt.Sprintf("  内存: %.1f MB (GC %d 次)\n", float64(mem.Alloc)/1024/1024, mem.NumGC))
	b.WriteString(fmt.Sprintf("  协程: %d\n", runtime.NumGoroutine()))

	if ch.slashCfg.CurrentModel != nil {
		b.WriteString(fmt.Sprintf("  主模型: %s\n", ch.slashCfg.CurrentModel()))
	}
	if ch.slashCfg.SmallModel != nil {
		b.WriteString(fmt.Sprintf("  小模型: %s\n", ch.slashCfg.SmallModel()))
	}

	n := 0
	ch.sessions.Range(func(_, _ any) bool { n++; return true })
	b.WriteString(fmt.Sprintf("  会话: %d\n", n))
	if ch.Credentials != nil {
		b.WriteString(fmt.Sprintf("  账号: %s\n", ch.Credentials.UserID))
	}
	return b.String()
}

// ─── /list ───────────────────────────────────────────────────────────────────

func cmdList(_ context.Context, ch *Channel, _ *wechatbot.IncomingMessage, _ string) string {
	var b strings.Builder
	b.WriteString("━━━━━━━━━━━━━━━━\n📋 会话列表\n━━━━━━━━━━━━━━━━\n\n")

	type e struct{ u, s string }
	var es []e
	ch.sessions.Range(func(k, v any) bool { es = append(es, e{k.(string), v.(string)}); return true })
	if len(es) == 0 {
		b.WriteString("  (无绑定会话)\n")
		return b.String()
	}
	sort.Slice(es, func(i, j int) bool { return es[i].u < es[j].u })
	for i, e := range es {
		displayUser := stripSessionKeyPrefix(e.u)
		b.WriteString(fmt.Sprintf("  #%d 👤 %s → %s\n", i+1, displayUser, shortSessionID(e.s)))
	}
	return b.String()
}

// ─── /models ─────────────────────────────────────────────────────────────────

func cmdModels(_ context.Context, ch *Channel, _ *wechatbot.IncomingMessage, _ string) string {
	var b strings.Builder
	b.WriteString("━━━━━━━━━━━━━━━━\n🤖 可用模型\n━━━━━━━━━━━━━━━━\n\n")
	if ch.slashCfg.ListModels == nil {
		b.WriteString("  (配置未注入)\n")
		return b.String()
	}
	models := ch.slashCfg.ListModels()
	current := ""
	if ch.slashCfg.CurrentModel != nil {
		current = ch.slashCfg.CurrentModel()
	}
	for _, m := range models {
		marker := "  "
		if m == current {
			marker = "→ "
		}
		b.WriteString(fmt.Sprintf("  %s%s\n", marker, m))
	}
	b.WriteString("\n  → 当前选中\n")
	return b.String()
}

// ─── /model ──────────────────────────────────────────────────────────────────

func cmdModel(_ context.Context, ch *Channel, _ *wechatbot.IncomingMessage, args string) string {
	if args == "" || ch.slashCfg.SwitchModel == nil {
		return "用法: /model <provider/model>"
	}
	parts := strings.SplitN(args, "/", 2)
	if len(parts) != 2 {
		return "❌ 格式错误: provider/model"
	}
	if err := ch.slashCfg.SwitchModel(parts[0], parts[1]); err != nil {
		return fmt.Sprintf("❌ %v", err)
	}
	return fmt.Sprintf("✅ 已切换: %s/%s", parts[0], parts[1])
}

// ─── /test model ─────────────────────────────────────────────────────────────

func cmdTestModel(_ context.Context, ch *Channel, _ *wechatbot.IncomingMessage, args string) string {
	if !strings.HasPrefix(args, "model ") || ch.slashCfg.TestModel == nil {
		return "用法: /test model <provider/model>"
	}
	modelPath := strings.TrimSpace(strings.TrimPrefix(args, "model"))
	parts := strings.SplitN(modelPath, "/", 2)
	if len(parts) != 2 {
		return "❌ 格式错误"
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("━━━━━━━━━━━━━━━━\n🔍 测试: %s\n━━━━━━━━━━━━━━━━\n\n", modelPath))
	start := time.Now()
	err := ch.slashCfg.TestModel(parts[0], parts[1])
	elapsed := time.Since(start)
	if err != nil {
		b.WriteString(fmt.Sprintf("  ❌ %v (%dms)\n", err, elapsed.Milliseconds()))
	} else {
		b.WriteString(fmt.Sprintf("  ✅ 可用 (%dms)\n", elapsed.Milliseconds()))
	}
	return b.String()
}

// ─── /screenshot ─────────────────────────────────────────────────────────────

func cmdScreenshot(_ context.Context, ch *Channel, msg *wechatbot.IncomingMessage, _ string) string {
	path, err := takeScreenshot()
	if err != nil {
		return fmt.Sprintf("❌ %v", err)
	}
	defer os.Remove(path)
	if msg == nil || msg.UserID == "" {
		return "❌ 无法确定收件人"
	}
	if err := ch.SendFile(context.Background(), msg.UserID, path); err != nil {
		return fmt.Sprintf("❌ 发送失败: %v", err)
	}
	return "📸 截图已发送"
}

// ─── /send ───────────────────────────────────────────────────────────────────

func cmdSendFile(_ context.Context, ch *Channel, msg *wechatbot.IncomingMessage, args string) string {
	if args == "" {
		return "用法: /send <文件路径>"
	}
	path := strings.TrimSpace(args)
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return fmt.Sprintf("❌ 文件不存在: %s", path)
	}
	if info.Size() > 5*1024*1024 {
		compressed, err := gzipFile(path)
		if err != nil {
			return fmt.Sprintf("❌ 压缩失败: %v", err)
		}
		defer os.Remove(compressed)
		path = compressed
	}
	if msg == nil || msg.UserID == "" {
		return "❌ 无法确定收件人"
	}
	if err := ch.SendFile(context.Background(), msg.UserID, path); err != nil {
		return fmt.Sprintf("❌ 发送失败: %v", err)
	}
	return fmt.Sprintf("📎 已发送: %s", filepath.Base(path))
}

// ─── Shared Helpers ──────────────────────────────────────────────────────────

func shortSessionID(id string) string {
	if len(id) > 8 {
		return id[:8] + "..."
	}
	return id
}

func takeScreenshot() (string, error) {
	tmpPath := filepath.Join(os.TempDir(), fmt.Sprintf("mocode-ss-%d.png", time.Now().UnixMilli()))
	switch runtime.GOOS {
	case "windows":
		ps := fmt.Sprintf(`Add-Type -AssemblyName System.Windows.Forms;`+
			`$b=[System.Windows.Forms.SystemInformation]::PrimaryMonitorSize;`+
			`$bm=New-Object System.Drawing.Bitmap($b.Width,$b.Height);`+
			`$g=[System.Drawing.Graphics]::FromImage($bm);`+
			`$g.CopyFromScreen(0,0,0,0,$b);`+
			`$bm.Save('%s',[System.Drawing.Imaging.ImageFormat]::Png);`+
			`$g.Dispose();$bm.Dispose()`, strings.ReplaceAll(tmpPath, "'", "''"))
		out, err := exec.Command("powershell", "-NoProfile", "-Command", ps).CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("ps: %s %w", string(out), err)
		}
	case "darwin":
		if err := exec.Command("screencapture", "-x", tmpPath).Run(); err != nil {
			return "", fmt.Errorf("screencapture: %w", err)
		}
	case "linux":
		if exec.Command("scrot", tmpPath).Run() != nil {
			if exec.Command("import", "-window", "root", tmpPath).Run() != nil {
				return "", fmt.Errorf("no screenshot tool")
			}
		}
	default:
		return "", fmt.Errorf("unsupported: %s", runtime.GOOS)
	}
	return tmpPath, nil
}

func gzipFile(path string) (string, error) {
	in, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer in.Close()
	outPath := path + ".gz"
	out, err := os.Create(outPath)
	if err != nil {
		return "", err
	}
	defer out.Close()
	w := gzip.NewWriter(out)
	if _, err := io.Copy(w, in); err != nil {
		w.Close()
		return "", err
	}
	return outPath, w.Close()
}
