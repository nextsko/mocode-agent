package kngs

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Watcher monitors directories for file changes and syncs them to the store.
type Watcher struct {
	store    *Store
	watcher  *fsnotify.Watcher
	paths    []string
	mu       sync.Mutex
	debounce map[string]*time.Timer
	done     chan struct{}
}

// NewWatcher creates a new file watcher backed by the given store.
func NewWatcher(store *Store) (*Watcher, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	return &Watcher{
		store:    store,
		watcher:  w,
		debounce: make(map[string]*time.Timer),
		done:     make(chan struct{}),
	}, nil
}

// Watch starts watching the given directories. It performs an initial sync
// and then processes file system events.
func (w *Watcher) Watch(ctx context.Context, paths []string) error {
	w.mu.Lock()
	w.paths = paths
	w.mu.Unlock()

	// Initial sync of all paths.
	if err := w.store.Sync(ctx, paths); err != nil {
		slog.Warn("Kngs initial sync failed", "error", err)
	}

	// Add directories to the watcher.
	for _, path := range paths {
		if err := w.watcher.Add(path); err != nil {
			if !strings.Contains(err.Error(), "no such file") {
				slog.Warn("Kngs failed to watch directory", "path", path, "error", err)
			}
		}
	}

	// Start event loop.
	go w.eventLoop(ctx)

	return nil
}

// eventLoop processes file system events.
func (w *Watcher) eventLoop(ctx context.Context) {
	defer close(w.done)

	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-w.watcher.Events:
			if !ok {
				return
			}
			w.handleEvent(ctx, event)
		case err, ok := <-w.watcher.Errors:
			if !ok {
				return
			}
			slog.Warn("Kngs watcher error", "error", err)
		}
	}
}

// handleEvent processes a single file system event with debouncing.
func (w *Watcher) handleEvent(ctx context.Context, event fsnotify.Event) {
	// Only care about files, not directories.
	if event.Has(fsnotify.Create) || event.Has(fsnotify.Write) || event.Has(fsnotify.Remove) {
		// Check if this is a file (not a directory).
		// For Remove events, we process immediately.
		if event.Has(fsnotify.Remove) {
			w.handleChange(ctx, event.Name, true)
			return
		}

		// Debounce writes to avoid processing intermediate save states.
		w.debounceEvent(ctx, event.Name)
	}
}

func (w *Watcher) debounceEvent(ctx context.Context, path string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if timer, ok := w.debounce[path]; ok {
		timer.Stop()
	}

	w.debounce[path] = time.AfterFunc(500*time.Millisecond, func() {
		w.handleChange(ctx, path, false)

		w.mu.Lock()
		delete(w.debounce, path)
		w.mu.Unlock()
	})
}

func (w *Watcher) handleChange(ctx context.Context, path string, isRemove bool) {
	// Ignore hidden files.
	if strings.HasPrefix(filepath.Base(path), ".") {
		return
	}

	if isRemove {
		slog.Debug("Kngs file removed", "path", path)
		if err := w.store.Remove(ctx, path); err != nil {
			slog.Warn("Kngs failed to remove entry", "path", path, "error", err)
		}
	} else {
		slog.Debug("Kngs file changed", "path", path)
		if err := w.store.AddOrUpdate(ctx, path); err != nil {
			slog.Warn("Kngs failed to update entry", "path", path, "error", err)
		}
	}
}

// Close stops the watcher.
func (w *Watcher) Close() error {
	return w.watcher.Close()
}

// Done returns a channel that's closed when the event loop exits.
func (w *Watcher) Done() <-chan struct{} {
	return w.done
}
