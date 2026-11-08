// Package acpfs registers a per-session client filesystem for ACP sessions.
//
// ACP clients may expose fs/read_text_file and fs/write_text_file for files
// that only the editor can see (unsaved buffers, virtual workspaces). Tools
// consult this registry by session id: when a session has a client fs, reads
// and writes go through the client; otherwise they hit the local disk.
package acpfs

import (
	"context"
	"fmt"
	"sync"
)

// Client is the subset of ACP file-system methods the agent uses.
type Client interface {
	ReadTextFile(ctx context.Context, sessionID, path string) (string, error)
	WriteTextFile(ctx context.Context, sessionID, path, content string) error
}

var (
	mu      sync.RWMutex
	clients = map[string]Client{}
)

// Register binds a client filesystem to one session.
func Register(sessionID string, c Client) {
	if sessionID == "" || c == nil {
		return
	}
	mu.Lock()
	clients[sessionID] = c
	mu.Unlock()
}

// Unregister drops the binding when a session closes.
func Unregister(sessionID string) {
	mu.Lock()
	delete(clients, sessionID)
	mu.Unlock()
}

// Has reports whether a client filesystem is registered for the session.
func Has(sessionID string) bool {
	mu.RLock()
	_, ok := clients[sessionID]
	mu.RUnlock()
	return ok
}

// Read reads a path through the client filesystem. ok is false when the
// session has no client fs, in which case the caller should use local disk.
func Read(ctx context.Context, sessionID, path string) (content string, ok bool, err error) {
	mu.RLock()
	c := clients[sessionID]
	mu.RUnlock()
	if c == nil {
		return "", false, nil
	}
	content, err = c.ReadTextFile(ctx, sessionID, path)
	if err != nil {
		return "", true, fmt.Errorf("client fs read %s: %w", path, err)
	}
	return content, true, nil
}

// Write writes a path through the client filesystem. ok is false when the
// session has no client fs.
func Write(ctx context.Context, sessionID, path, content string) (ok bool, err error) {
	mu.RLock()
	c := clients[sessionID]
	mu.RUnlock()
	if c == nil {
		return false, nil
	}
	if err := c.WriteTextFile(ctx, sessionID, path, content); err != nil {
		return true, fmt.Errorf("client fs write %s: %w", path, err)
	}
	return true, nil
}
