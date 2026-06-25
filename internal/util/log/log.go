package log

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/x/term"
	"gopkg.in/natefinch/lumberjack.v2"
)

var (
	initOnce    sync.Once
	initialized atomic.Bool
	logRotator  *lumberjack.Logger
	logHandler  slog.Handler
)

func Setup(logFile string, debug bool, ws ...io.Writer) {
	initOnce.Do(func() {
		logRotator = &lumberjack.Logger{
			Filename:   logFile,
			MaxSize:    10,    // Max size in MB
			MaxBackups: 0,     // Number of backups
			MaxAge:     30,    // Days
			Compress:   false, // Enable compression
		}

		level := slog.LevelInfo
		if debug {
			level = slog.LevelDebug
		}

		opts := &slog.HandlerOptions{
			Level:     level,
			AddSource: true,
		}

		var handlers []slog.Handler
		handlers = append(handlers, slog.NewJSONHandler(logRotator, opts))

		for _, w := range ws {
			if w == nil {
				continue
			}
			if f, ok := w.(term.File); ok && term.IsTerminal(f.Fd()) {
				handlers = append(handlers, slog.NewTextHandler(w, opts))
			} else {
				handlers = append(handlers, slog.NewJSONHandler(w, opts))
			}
		}

		if len(handlers) == 1 {
			logHandler = handlers[0]
		} else {
			logHandler = slog.NewMultiHandler(handlers...)
		}

		slog.SetDefault(slog.New(logHandler))
		initialized.Store(true)
	})
}

func Initialized() bool {
	return initialized.Load()
}

// Sync ensures all pending log entries are flushed to disk.
// This should be called before any critical operation or shutdown.
// Note: lumberjack.Logger doesn't expose Sync() directly, so we
// trigger an immediate flush by closing and reopening the file.
func Sync() {
	if logRotator != nil {
		// Trigger immediate flush by rotating (which flushes current buffer)
		logRotator.Rotate()
	}
}

// Close ensures all logs are flushed and the file is properly closed.
func Close() error {
	if logRotator != nil {
		return logRotator.Close()
	}
	return nil
}

func RecoverPanic(name string, cleanup func()) {
	if r := recover(); r != nil {
		// 1. 先 sync 日志，确保 panic 前的所有日志都写入磁盘
		Sync()

		// 2. 记录 panic 信息到日志
		slog.Error("PANIC_RECOVER: panic detected",
			"name", name,
			"reason", r,
		)

		// 3. 再 sync 一次，确保 panic 日志也写入
		Sync()

		// 4. 执行清理
		if cleanup != nil {
			cleanup()
		}

		// 5. 创建 panic 专用日志文件（独立于主日志）
		timestamp := time.Now().Format("20060102-150405")
		filename := fmt.Sprintf("mocode-panic-%s-%s.log", name, timestamp)

		file, err := os.Create(filename)
		if err == nil {
			defer file.Close()

			// Write panic information and stack trace
			fmt.Fprintf(file, "Panic in %s: %v\n\n", name, r)
			fmt.Fprintf(file, "Time: %s\n\n", time.Now().Format(time.RFC3339))
			fmt.Fprintf(file, "Stack Trace:\n%s\n", debug.Stack())
		}
	}
}
