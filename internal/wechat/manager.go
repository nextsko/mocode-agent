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
	"sync"
	"time"

	wechatbot "github.com/package-register/mocode/internal/wechat/sdk"
)

// AccountStatus represents the connection state of a WeChat account.
type AccountStatus string

const (
	AccountStatusOnline       AccountStatus = "online"
	AccountStatusOffline      AccountStatus = "offline"
	AccountStatusReconnecting AccountStatus = "reconnecting"
	AccountStatusExpired      AccountStatus = "expired"
	AccountStatusError        AccountStatus = "error"
)

// AccountInfo holds summary information about a WeChat account for UI display.
type AccountInfo struct {
	ID       string        `json:"id"`
	UserID   string        `json:"user_id"`
	Name     string        `json:"name"`
	BaseURL  string        `json:"base_url"`
	Status   AccountStatus `json:"status"`
	IsActive bool          `json:"is_active"`
}

// AccountManager manages multiple WeChat channels with account switching.
type AccountManager struct {
	mu        sync.RWMutex
	accounts  map[string]*Channel
	defaultID string
	storeDir  string

	runners  map[string]context.CancelFunc // per-account Run() cancellation
	runnerWG sync.WaitGroup
}

var (
	globalManager     *AccountManager
	globalManagerOnce sync.Once
)

// GetManager returns the global AccountManager, creating it if needed.
func GetManager() *AccountManager {
	globalManagerOnce.Do(func() {
		globalManager = NewAccountManager("")
	})
	return globalManager
}

// NewAccountManager creates a new AccountManager.
func NewAccountManager(storeDir string) *AccountManager {
	if storeDir == "" {
		home, _ := os.UserHomeDir()
		storeDir = filepath.Join(home, ".mocode", "wechat")
	}
	m := &AccountManager{
		accounts: make(map[string]*Channel),
		storeDir: storeDir,
		runners:  make(map[string]context.CancelFunc),
	}
	// Try to load existing accounts.
	if err := m.loadRegistry(); err != nil {
		slog.Debug("no existing WeChat accounts", "error", err)
	}
	return m
}

// Login starts QR login for a new WeChat account. The account is added to the
// manager on success.
func (m *AccountManager) Login(ctx context.Context, force bool, callbacks LoginCallbacks, client *http.Client) (*AccountInfo, error) {
	ch := New()
	if err := ch.LoginWithCallbacks(ctx, force, callbacks, client); err != nil {
		return nil, fmt.Errorf("wechat login: %w", err)
	}

	creds := ch.Credentials
	if creds == nil {
		return nil, fmt.Errorf("wechat login: no credentials returned")
	}

	id := accountID(creds.BaseURL, creds.UserID)
	ch.accountID = id
	ch.storePath = filepath.Join(m.storeDir, id, "sessions.json")
	if err := ch.loadSessions(); err != nil {
		slog.Warn("failed to load WeChat sessions for new account", "account", id, "error", err)
	}

	m.mu.Lock()
	m.accounts[id] = ch
	if m.defaultID == "" {
		m.defaultID = id
	}
	m.mu.Unlock()

	if err := m.saveRegistry(); err != nil {
		slog.Warn("failed to save account registry", "error", err)
	}

	// Persist credentials to per-account file so loadRegistry can restore them.
	if err := m.saveCredentials(id, creds); err != nil {
		slog.Warn("failed to save account credentials", "account", id, "error", err)
	}

	return &AccountInfo{
		ID:       id,
		UserID:   creds.UserID,
		Name:     creds.UserID,
		BaseURL:  creds.BaseURL,
		Status:   AccountStatusOnline,
		IsActive: id == m.defaultID,
	}, nil
}

// Get returns the channel for a given account ID.
func (m *AccountManager) Get(id string) *Channel {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.accounts[id]
}

// GetActive returns the currently active (default) channel.
func (m *AccountManager) GetActive() *Channel {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.accounts[m.defaultID]
}

// Switch changes the active (default) account.
func (m *AccountManager) Switch(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.accounts[id]; !ok {
		return fmt.Errorf("account %q not found", id)
	}
	m.defaultID = id
	return m.saveRegistry()
}

// Start begins the long-poll message loop for a specific account in a background goroutine.
// The account must already be registered (via Login or loaded from disk).
// Only one Run() per account is allowed at a time; subsequent calls are no-ops.
func (m *AccountManager) Start(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	ch, ok := m.accounts[id]
	if !ok {
		return fmt.Errorf("account %q not found", id)
	}
	if _, running := m.runners[id]; running {
		return nil // already running
	}

	if !ch.IsLoggedIn() {
		return fmt.Errorf("account %q is not logged in", id)
	}

	ctx, cancel := context.WithCancel(ctx)
	m.runners[id] = cancel

	m.runnerWG.Add(1)
	go func() {
		defer m.runnerWG.Done()
		defer func() {
			m.mu.Lock()
			delete(m.runners, id)
			m.mu.Unlock()
		}()
		if err := ch.Run(ctx); err != nil && ctx.Err() == nil {
			slog.Error("WeChat account Run() exited with error", "account", id, "error", err)
		}
	}()

	return nil
}

// Stop cancels the long-poll loop for a specific account.
func (m *AccountManager) Stop(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if cancel, ok := m.runners[id]; ok {
		cancel()
		delete(m.runners, id)
	}
}

// Reconnect performs a smart reconnect for the given account:
//   - If logged in and poll loop is running → restart the poll loop (useful for stuck states).
//   - If logged in but poll loop is not running → start the poll loop.
//   - If not logged in → caller should re-trigger QR login via Login().
//
// Returns nil when the poll loop is started/restarted, or an error if the
// account is missing or the credentials are gone.
func (m *AccountManager) Reconnect(ctx context.Context, id string) error {
	m.mu.Lock()
	ch, ok := m.accounts[id]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("account %q not found", id)
	}
	if !ch.IsLoggedIn() {
		m.mu.Unlock()
		return fmt.Errorf("account %q is not logged in, please re-scan QR", id)
	}

	// Cancel any existing runner so we can start a fresh one.
	if cancel, ok := m.runners[id]; ok {
		cancel()
		delete(m.runners, id)
	}
	m.mu.Unlock()

	// Brief wait so the previous goroutine fully exits before we spawn a new one.
	time.Sleep(50 * time.Millisecond)
	return m.Start(ctx, id)
}

// Delete removes the account entirely: stops the poll loop, deletes
// credentials and per-account directory, and removes it from the registry.
// If the deleted account was the default, the first remaining account
// (if any) becomes the new default.
func (m *AccountManager) Delete(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	ch, ok := m.accounts[id]
	if !ok {
		return fmt.Errorf("account %q not found", id)
	}

	// Stop the poll loop first.
	if cancel, ok := m.runners[id]; ok {
		cancel()
		delete(m.runners, id)
	}

	// Stop the underlying bot if running.
	if ch != nil {
		ch.Stop()
	}

	// Remove from in-memory map.
	delete(m.accounts, id)

	// Update default if needed.
	if m.defaultID == id {
		m.defaultID = ""
		for newDefault := range m.accounts {
			m.defaultID = newDefault
			break
		}
	}

	// Remove on-disk account dir (sessions.json, credentials.json, etc.).
	dir := filepath.Join(m.storeDir, id)
	if err := os.RemoveAll(dir); err != nil {
		slog.Warn("failed to remove account dir", "account", id, "error", err)
	}

	// Persist updated registry.
	if err := m.saveRegistry(); err != nil {
		return fmt.Errorf("save registry: %w", err)
	}
	return nil
}

// StopAll cancels all running account long-poll loops and waits for them to finish.
func (m *AccountManager) StopAll() {
	m.mu.Lock()
	for id, cancel := range m.runners {
		cancel()
		delete(m.runners, id)
	}
	m.mu.Unlock()
	m.runnerWG.Wait()
}

// IsRunning reports whether the given account's poll loop is active.
func (m *AccountManager) IsRunning(id string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, running := m.runners[id]
	return running
}

// List returns all registered accounts.
func (m *AccountManager) List() []AccountInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var result []AccountInfo
	for id, ch := range m.accounts {
		status := AccountStatusOnline
		if !ch.IsLoggedIn() {
			status = AccountStatusOffline
		}
		info := AccountInfo{
			ID:       id,
			Status:   status,
			IsActive: id == m.defaultID,
		}
		if ch.Credentials != nil {
			info.UserID = ch.Credentials.UserID
			info.Name = ch.Credentials.UserID
			info.BaseURL = ch.Credentials.BaseURL
		}
		result = append(result, info)
	}
	return result
}

func (m *AccountManager) loadRegistry() error {
	path := filepath.Join(m.storeDir, "registry.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	var registry struct {
		Default  string   `json:"default"`
		Accounts []string `json:"accounts"`
	}
	if err := json.Unmarshal(data, &registry); err != nil {
		return err
	}

	m.mu.Lock()
	m.defaultID = registry.Default
	m.mu.Unlock()

	for _, id := range registry.Accounts {
		ch := New()
		ch.accountID = id
		ch.storePath = filepath.Join(m.storeDir, id, "sessions.json")
		credPath := filepath.Join(m.storeDir, id, "credentials.json")
		credData, err := os.ReadFile(credPath)
		if err != nil {
			slog.Debug("no stored credentials for account", "account", id, "error", err)
			continue
		}
		var stored Credentials
		if err := json.Unmarshal(credData, &stored); err != nil {
			slog.Warn("failed to parse credentials for account", "account", id, "error", err)
			continue
		}
		ch.Credentials = &stored
		// Re-initialize the bot with stored credentials so
		// IsLoggedIn() returns true and the channel is usable.
		bot := wechatbot.New(wechatbot.Options{})
		if !bot.SetCredentials(&stored) {
			slog.Warn("incomplete credentials for stored account", "account", id)
			continue
		}
		ch.bot = bot

		if err := ch.loadSessions(); err != nil {
			slog.Warn("failed to load sessions for restored account", "account", id, "error", err)
		}

		m.mu.Lock()
		m.accounts[id] = ch
		m.mu.Unlock()
	}

	return nil
}

func (m *AccountManager) saveRegistry() error {
	path := filepath.Join(m.storeDir, "registry.json")
	var ids []string
	for id := range m.accounts {
		ids = append(ids, id)
	}
	registry := struct {
		Default  string   `json:"default"`
		Accounts []string `json:"accounts"`
	}{Default: m.defaultID, Accounts: ids}

	data, err := json.MarshalIndent(registry, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(m.storeDir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// accountID derives a stable identifier from baseURL and userID.
func accountID(baseURL, userID string) string {
	key := baseURL + "|" + userID
	h := sha256.Sum256([]byte(key))
	return hex.EncodeToString(h[:])[:16]
}

// saveCredentials persists account credentials to a per-account JSON file.
func (m *AccountManager) saveCredentials(id string, creds *Credentials) error {
	dir := filepath.Join(m.storeDir, id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "credentials.json"), append(data, '\n'), 0o600)
}
