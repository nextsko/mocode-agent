// Package wechat provides WeChat iLink Bot integration for oMo code.
package wechat

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	wechatbot "github.com/nextsko/mocode-agent/internal/integration/wechat/sdk"
	"github.com/nextsko/mocode-agent/internal/util/infra"
)

// Credentials aliases the SDK's credentials type.
type Credentials = wechatbot.Credentials

var (
	defaultChannel *Channel
	defaultMu      sync.Mutex
)

// Default returns the global WeChat channel.
func Default() *Channel {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultChannel == nil {
		defaultChannel = New()
	}
	return defaultChannel
}

// Message aliases the SDK's incoming message type.
type Message = wechatbot.IncomingMessage

// MessageHandler is called for each incoming WeChat message.
type MessageHandler func(msg *Message) string

// MediaInfo describes a downloaded media file attached to an incoming message.
type MediaInfo struct {
	Type     string // "image", "file", "video", "voice"
	Path     string // local file path after download
	FileName string
	Format   string // e.g. "silk" for voice
}

// IncomingMessage is an enriched message with optional media attachments.
type IncomingMessage struct {
	*Message
	MediaPaths []MediaInfo // downloaded media files
	QuotedText string      // text from quoted/referenced message
}

// AgentHandler handles a WeChat message by routing to the AI agent.
type AgentHandler func(ctx context.Context, userID, text string, msg *IncomingMessage) (reply string, err error)

type LoginCallbacks struct {
	OnQRURL    func(string)
	OnScanned  func()
	OnExpired  func()
	OnLoggedIn func(userID string)
}

// VoiceCapabilities declares ASR/TTS support for the WeChat channel.
type VoiceCapabilities struct {
	ASR bool // Automatic Speech Recognition (voice → text)
	TTS bool // Text-to-Speech (text → voice)
}

// VoiceCap returns the voice capabilities of the WeChat channel.
func (c *Channel) VoiceCap() VoiceCapabilities {
	return VoiceCapabilities{ASR: true, TTS: false}
}

// Channel wraps the WeChat bot.
type Channel struct {
	bot     *wechatbot.Bot
	handler MessageHandler
	agentFn AgentHandler
	mu      sync.Mutex
	running bool
	cancel  context.CancelFunc

	sessions  sync.Map // WeChat userID → mocode sessionID
	storePath string
	accountID string
	mediaDir  string // directory for downloaded media files

	recentMsgs    map[string]time.Time // SHA-256 hash → first seen time
	recentMsgsMu  sync.Mutex
	recentMsgPath string // path to persist recent message hashes

	slashCfg SlashConfig // injected config for slash commands

	activeSession string // userID of the currently active session (for butler routing)

	butler   *llmButlerHandler // butler routing handler (initialized via InitButler)
	butlerMu sync.Mutex

	userLocks sync.Map // WeChat userID -> *sync.Mutex

	Credentials *Credentials
}

// SlashConfig provides the configuration needed by slash commands.
// Injected by the gateway/admin layer to avoid circular imports.
type SlashConfig struct {
	// CurrentModel returns "provider/model" of the active large model.
	CurrentModel func() string
	// SmallModel returns "provider/model" of the active small model.
	SmallModel func() string
	// ListModels returns all available "provider/model" entries.
	ListModels func() []string
	// SwitchModel sets the active large model by "provider/model".
	SwitchModel func(provider, model string) error
	// TestModel tests a model's connectivity. Returns "" on success.
	TestModel func(provider, model string) error
}

// New creates a new WeChat channel.
func New() *Channel {
	return &Channel{}
}

// Login starts the QR login flow.
func (c *Channel) Login(ctx context.Context, force bool, onQRURL func(string), clients ...*http.Client) error {
	return c.LoginWithCallbacks(ctx, force, LoginCallbacks{OnQRURL: onQRURL}, clients...)
}

func (c *Channel) LoginWithCallbacks(ctx context.Context, force bool, callbacks LoginCallbacks, clients ...*http.Client) error {
	var httpClient *http.Client
	if len(clients) > 0 {
		httpClient = clients[0]
	}
	bot := wechatbot.New(wechatbot.Options{
		OnQRURL:    callbacks.OnQRURL,
		HTTPClient: httpClient,
		OnScanned: func() {
			slog.Debug("WeChat QR scanned, waiting for confirmation")
			if callbacks.OnScanned != nil {
				callbacks.OnScanned()
			}
		},
		OnExpired: func() {
			slog.Debug("WeChat QR expired")
			if callbacks.OnExpired != nil {
				callbacks.OnExpired()
			}
		},
		OnError: func(err error) {
			slog.Error("WeChat bot error", "error", err)
		},
	})

	creds, err := bot.Login(ctx, force)
	if err != nil {
		return fmt.Errorf("wechat login: %w", err)
	}

	// Set state dir for cursor/context token persistence.
	stateDir := filepath.Join(os.TempDir(), "mocode-wechat", "state", creds.UserID)
	bot.SetStateDir(stateDir)

	c.mu.Lock()
	c.bot = bot
	c.Credentials = creds
	c.recentMsgPath = filepath.Join(os.TempDir(), "mocode-wechat", "recent-msgs", creds.UserID+".json")
	c.recentMsgs = nil // will be lazy-loaded on first message
	c.mu.Unlock()

	slog.Debug("WeChat logged in", "userID", creds.UserID)
	if callbacks.OnLoggedIn != nil {
		callbacks.OnLoggedIn(creds.UserID)
	}
	return nil
}

// IsLoggedIn returns whether the bot is authenticated.
func (c *Channel) IsLoggedIn() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.bot != nil && c.Credentials != nil
}

// OnMessage registers a simple message handler.
func (c *Channel) OnMessage(handler MessageHandler) {
	c.handler = handler
}

// SetAgentHandler registers the AI agent handler.
func (c *Channel) SetAgentHandler(fn AgentHandler) {
	c.agentFn = fn
}

// InitButler initializes the butler routing system with workspace integration.
// Workspace is the real mocode workspace. After calling this, non-slash messages
// will be routed through the butler instead of the legacy agentFn.
func (c *Channel) InitButler(ws ButlerWorkspace) {
	c.butlerMu.Lock()
	defer c.butlerMu.Unlock()
	butlerCtx := &ButlerContext{
		Channel:   c,
		Workspace: ws,
	}
	c.butler = newButlerHandler(butlerCtx)
}

// SetSlashConfig injects model-related config for slash commands.
func (c *Channel) SetSlashConfig(cfg SlashConfig) {
	c.mu.Lock()
	c.slashCfg = cfg
	c.mu.Unlock()
}

func (c *Channel) SetSessionStore(path string) {
	c.mu.Lock()
	c.storePath = path
	c.mu.Unlock()
	if err := c.loadSessions(); err != nil {
		slog.Warn("failed to load WeChat session bindings", "error", err)
	}
}

// SendTyping shows the typing indicator to a user.
func (c *Channel) SendTyping(ctx context.Context, userID string) error {
	c.mu.Lock()
	bot := c.bot
	c.mu.Unlock()
	if bot == nil {
		return nil
	}
	return bot.SendTyping(ctx, userID)
}

// StartTyping shows typing with keepalive. Returns a stop function.
func (c *Channel) StartTyping(ctx context.Context, userID string) func() {
	typingCtx, cancel := context.WithCancel(ctx)
	var once sync.Once
	stop := func() {
		once.Do(cancel)
	}

	const keepAlive = 5 * time.Second
	ticker := time.NewTicker(keepAlive)

	go func() {
		defer ticker.Stop()
		// Initial typing send.
		_ = c.SendTyping(typingCtx, userID)
		for {
			select {
			case <-typingCtx.Done():
				return
			case <-ticker.C:
				if err := c.SendTyping(typingCtx, userID); err != nil {
					return
				}
			}
		}
	}()

	return stop
}

// SendImage sends an image to a user. Requires a prior message exchange
// to have established a context token.
func (c *Channel) SendImage(ctx context.Context, userID string, data []byte) error {
	c.mu.Lock()
	bot := c.bot
	c.mu.Unlock()
	if bot == nil {
		return fmt.Errorf("not logged in")
	}
	// Use SendMedia which auto-resolves context tokens.
	return bot.SendMedia(ctx, userID, wechatbot.SendImage(data))
}

// SendFile sends a local file as a WeChat attachment.
func (c *Channel) SendFile(ctx context.Context, userID, path string) error {
	c.mu.Lock()
	bot := c.bot
	c.mu.Unlock()
	if bot == nil {
		return fmt.Errorf("not logged in")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}
	return bot.SendMedia(ctx, userID, wechatbot.SendFile(data, filepath.Base(path)))
}

// GetSession returns the stored session ID for a WeChat user.
func (c *Channel) GetSession(userID string) (string, bool) {
	key := SessionKey(userID)
	if v, ok := c.sessions.Load(key); ok {
		return v.(string), true
	}
	// Backward compat for legacy bindings stored under raw userID.
	if v, ok := c.sessions.Load(userID); ok {
		return v.(string), true
	}
	return "", false
}

// SetSession stores the session ID for a WeChat user.
func (c *Channel) SetSession(userID, sessionID string) {
	key := SessionKey(userID)
	c.sessions.Store(key, sessionID)
	c.sessions.Delete(userID)
	if err := c.saveSessions(); err != nil {
		slog.Warn("Failed to save WeChat session bindings", "error", err)
	}
}

// DelSession removes the stored session ID for a WeChat user.
func (c *Channel) DelSession(userID string) {
	c.sessions.Delete(SessionKey(userID))
	c.sessions.Delete(userID)
	if err := c.saveSessions(); err != nil {
		slog.Warn("Failed to save WeChat session bindings after delete", "error", err)
	}
}

func (c *Channel) userLock(userID string) *sync.Mutex {
	v, _ := c.userLocks.LoadOrStore(userID, &sync.Mutex{})
	return v.(*sync.Mutex)
}

const (
	replyRetryAttempts = 3
	replyRetryDelay    = 500 * time.Millisecond
)

func (c *Channel) replyWithRetry(ctx context.Context, msg *wechatbot.IncomingMessage, text string) error {
	var lastErr error
	for attempt := 0; attempt < replyRetryAttempts; attempt++ {
		if err := c.bot.Reply(ctx, msg, text); err != nil {
			lastErr = err
			if attempt < replyRetryAttempts-1 {
				time.Sleep(replyRetryDelay)
			}
			continue
		}
		return nil
	}
	return lastErr
}

func (c *Channel) loadSessions() error {
	c.mu.Lock()
	path := c.storePath
	c.mu.Unlock()
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var sessions map[string]string
	if err := json.Unmarshal(data, &sessions); err != nil {
		return err
	}
	for userID, sessionID := range sessions {
		if userID != "" && sessionID != "" {
			c.sessions.Store(userID, sessionID)
		}
	}
	return nil
}

func (c *Channel) saveSessions() error {
	c.mu.Lock()
	path := c.storePath
	c.mu.Unlock()
	if path == "" {
		return nil
	}
	sessions := map[string]string{}
	c.sessions.Range(func(key, value any) bool {
		userID, ok := key.(string)
		if !ok {
			return true
		}
		sessionID, ok := value.(string)
		if !ok {
			return true
		}
		sessions[userID] = sessionID
		return true
	})
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(sessions, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Run starts the long-poll message loop with reconnection backoff.
func (c *Channel) Run(ctx context.Context) error {
	c.mu.Lock()
	if c.bot == nil {
		c.mu.Unlock()
		return fmt.Errorf("not logged in")
	}
	bot := c.bot
	c.mu.Unlock()

	pollCtx, cancel := context.WithCancel(ctx)
	c.mu.Lock()
	c.cancel = cancel
	c.running = true
	c.mu.Unlock()

	bot.OnMessage(func(msg *wechatbot.IncomingMessage) {
		go c.handleMessage(pollCtx, msg)
	})

	const (
		maxBackoff  = 30 * time.Second
		baseBackoff = 2 * time.Second
		expirePause = 1 * time.Hour
	)
	var consecutiveFailures int

	for {
		slog.Debug("WeChat channel polling started")
		err := bot.Run(pollCtx)

		if err == nil || ctx.Err() != nil {
			c.mu.Lock()
			c.running = false
			c.mu.Unlock()
			return err
		}

		// Check for session expiry.
		if strings.Contains(err.Error(), "-14") || strings.Contains(err.Error(), "errcode -14") {
			slog.Warn("WeChat session expired, pausing for 1 hour", "error", err)
			select {
			case <-time.After(expirePause):
			case <-ctx.Done():
				c.mu.Lock()
				c.running = false
				c.mu.Unlock()
				return ctx.Err()
			}
			consecutiveFailures = 0
			continue
		}

		consecutiveFailures++
		delay := min(time.Duration(consecutiveFailures)*baseBackoff, maxBackoff)
		slog.Warn("WeChat channel error, reconnecting", "error", err, "delay", delay)
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			c.mu.Lock()
			c.running = false
			c.mu.Unlock()
			return ctx.Err()
		}
	}
}

// Stop gracefully stops the channel.
func (c *Channel) Stop() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.bot != nil {
		c.bot.Stop()
	}
	if c.cancel != nil {
		c.cancel()
	}
}

// SendText sends a text message to a user.
func (c *Channel) SendText(ctx context.Context, userID, text string) error {
	c.mu.Lock()
	bot := c.bot
	c.mu.Unlock()
	if bot == nil {
		return fmt.Errorf("not logged in")
	}
	return bot.Send(ctx, userID, text)
}

// SetMediaDir sets the directory for downloaded media files.
func (c *Channel) SetMediaDir(dir string) {
	c.mu.Lock()
	c.mediaDir = dir
	c.mu.Unlock()
	os.MkdirAll(dir, 0o755)
}

func (c *Channel) handleMessage(ctx context.Context, msg *wechatbot.IncomingMessage) {
	mu := c.userLock(msg.UserID)
	mu.Lock()
	defer mu.Unlock()

	startTime := time.Now()
	slog.Debug("WeChat msg", "userID", msg.UserID, "type", msg.Type, "textLen", len(msg.Text))

	// Layer 1: Slash command intercept (bypasses agent).
	if msg.Type == wechatbot.ContentText && msg.Text != "" {
		if c.handleSlashCommand(ctx, msg, msg.Text) {
			return
		}
	}

	// Layer 2: Dedup.
	if c.isDuplicate(msg) {
		slog.Debug("WeChat msg deduped", "userID", msg.UserID)
		return
	}

	// Layer 3: Download media attachments if present.
	incoming := &IncomingMessage{Message: msg}
	if len(msg.Images) > 0 || len(msg.Files) > 0 || len(msg.Videos) > 0 || len(msg.Voices) > 0 {
		c.downloadAllMedia(ctx, msg, incoming)
	}

	// Extract quoted message text.
	if msg.QuotedMessage != nil && msg.QuotedMessage.Text != "" {
		incoming.QuotedText = msg.QuotedMessage.Text
	}

	// Build effective text.
	effectiveText := incoming.Text
	for _, m := range incoming.MediaPaths {
		effectiveText += fmt.Sprintf("\n[media_file: %s] [media_type: %s] [media_path: %s]",
			m.FileName, m.Type, m.Path)
	}
	if incoming.QuotedText != "" {
		effectiveText += fmt.Sprintf("\n[quoted: %s]", incoming.QuotedText)
	}
	effectiveText = strings.TrimSpace(effectiveText)

	if effectiveText == "" {
		return
	}

	// Layer 4: Route through butler if initialized, otherwise fallback to agentFn.
	var reply string
	c.butlerMu.Lock()
	butler := c.butler
	c.butlerMu.Unlock()

	if butler != nil {
		reply = butler.Handle(ctx, msg.UserID, effectiveText)
	} else if c.agentFn != nil {
		r, err := c.agentFn(ctx, msg.UserID, effectiveText, incoming)
		if err != nil {
			slog.Error("Agent handler failed", "error", err)
			reply = "抱歉，处理出错了，请稍后重试。"
		} else {
			reply = r
		}
	} else if c.handler != nil {
		reply = c.handler(msg)
	}

	elapsed := time.Since(startTime)
	slog.Debug("WeChat msg done", "userID", msg.UserID,
		"elapsed", elapsed.Truncate(time.Millisecond), "replyLen", len(reply))

	if reply != "" {
		if err := c.replyWithRetry(ctx, msg, reply); err != nil {
			slog.Error("WeChat reply failed", "userID", msg.UserID, "error", err)
			fallback := "消息已处理，但发送失败，请稍后重试。"
			if retryErr := c.replyWithRetry(ctx, msg, fallback); retryErr != nil {
				slog.Error("WeChat fallback reply failed", "userID", msg.UserID, "error", retryErr)
			}
		}
	}
}

// downloadAllMedia downloads all media attachments from a message.
func (c *Channel) downloadAllMedia(ctx context.Context, msg *wechatbot.IncomingMessage, incoming *IncomingMessage) {
	c.mu.Lock()
	mediaDir := c.mediaDir
	c.mu.Unlock()
	if mediaDir == "" {
		mediaDir = infra.WeChatMediaDir()
	}

	download := func(media *wechatbot.CDNMedia, aesKey, mediaType, fileName string) *MediaInfo {
		if media == nil {
			return nil
		}
		data, err := c.bot.DownloadRaw(ctx, media, aesKey)
		if err != nil {
			slog.Error("WeChat media download failed", "type", mediaType, "error", err)
			return nil
		}
		if len(data) == 0 {
			return nil
		}

		// Build path: mediaDir/{account}/{chatID}/{type}_{hash}.{ext}
		chatID := msg.UserID
		hash := sha256.Sum256(data)
		hashHex := hex.EncodeToString(hash[:8])
		ext := filepath.Ext(fileName)
		if ext == "" {
			ext = guessExtension(mediaType)
		}
		dir := filepath.Join(mediaDir, c.accountID, chatID)
		os.MkdirAll(dir, 0o755)
		savePath := filepath.Join(dir, fmt.Sprintf("%s_%s%s", mediaType, hashHex, ext))

		if err := os.WriteFile(savePath, data, 0o644); err != nil {
			slog.Error("WeChat media save failed", "path", savePath, "error", err)
			return nil
		}

		if fileName == "" {
			fileName = filepath.Base(savePath)
		}
		slog.Debug("WeChat media downloaded", "type", mediaType, "size", len(data), "path", savePath)
		return &MediaInfo{Type: mediaType, Path: savePath, FileName: fileName}
	}

	// Priority: image > video > file > voice
	for _, img := range msg.Images {
		if m := download(img.Media, img.AESKey, "image", ""); m != nil {
			incoming.MediaPaths = append(incoming.MediaPaths, *m)
		}
	}
	for _, vid := range msg.Videos {
		if m := download(vid.Media, "", "video", ""); m != nil {
			incoming.MediaPaths = append(incoming.MediaPaths, *m)
		}
	}
	for _, f := range msg.Files {
		if m := download(f.Media, "", "file", f.FileName); m != nil {
			incoming.MediaPaths = append(incoming.MediaPaths, *m)
		}
	}
	for _, v := range msg.Voices {
		if m := download(v.Media, "", "voice", ""); m != nil {
			m.Format = "silk"
			incoming.MediaPaths = append(incoming.MediaPaths, *m)
		}
	}
}

func guessExtension(mediaType string) string {
	switch mediaType {
	case "image":
		return ".jpg"
	case "video":
		return ".mp4"
	case "voice":
		return ".silk"
	default:
		return ".bin"
	}
}

// isDuplicate returns true if the message has already been processed.
// Uses SHA-256 hash of client_id (preferred) or content, with a 24h window.
func (c *Channel) isDuplicate(msg *wechatbot.IncomingMessage) bool {
	// Build hash key: prefer client_id, fallback to content hash.
	var hashInput string
	if msg.Raw != nil && msg.Raw.ClientID != "" {
		hashInput = "cid:" + msg.Raw.ClientID
	} else {
		hashInput = fmt.Sprintf("%s:%s:%d", msg.UserID, msg.Text, msg.Timestamp.UnixMilli())
	}
	h := sha256.Sum256([]byte(hashInput))
	hashKey := hex.EncodeToString(h[:])

	c.recentMsgsMu.Lock()
	defer c.recentMsgsMu.Unlock()

	// Lazy init.
	if c.recentMsgs == nil {
		c.recentMsgs = make(map[string]time.Time)
		c.loadRecentMsgs()
	}

	now := time.Now()
	if ts, ok := c.recentMsgs[hashKey]; ok {
		if now.Sub(ts) < 24*time.Hour {
			return true
		}
	}

	// Record and prune.
	c.recentMsgs[hashKey] = now
	c.pruneRecentMsgs(now)
	c.saveRecentMsgs()
	return false
}

const maxRecentMsgs = 1000

func (c *Channel) pruneRecentMsgs(now time.Time) {
	if len(c.recentMsgs) <= maxRecentMsgs {
		return
	}
	for k, ts := range c.recentMsgs {
		if now.Sub(ts) >= 24*time.Hour {
			delete(c.recentMsgs, k)
		}
	}
}

func (c *Channel) loadRecentMsgs() {
	path := c.recentMsgPath
	if path == "" {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var entries map[string]string // hash → RFC3339 timestamp
	if json.Unmarshal(data, &entries) != nil {
		return
	}
	now := time.Now()
	for k, tsStr := range entries {
		ts, err := time.Parse(time.RFC3339, tsStr)
		if err != nil {
			continue
		}
		if now.Sub(ts) < 24*time.Hour {
			c.recentMsgs[k] = ts
		}
	}
}

func (c *Channel) saveRecentMsgs() {
	path := c.recentMsgPath
	if path == "" {
		return
	}
	entries := make(map[string]string, len(c.recentMsgs))
	for k, ts := range c.recentMsgs {
		entries[k] = ts.Format(time.RFC3339)
	}
	data, _ := json.MarshalIndent(entries, "", "  ")
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, data, 0o644)
}
