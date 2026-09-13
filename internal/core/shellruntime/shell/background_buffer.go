package shell

import (
	"bytes"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Bounded-output constants. backgroundOutputCapBytes bounds how much output
// a single background job stream may retain in memory: a stable head plus a
// rolling tail, each half the cap. Bytes that fall out of the middle are
// counted in `omitted` and surfaced as a marker in String() — so a chatty
// job (dev server, watcher) can no longer grow memory without limit.
// Design borrowed from Codex's HeadTailBuffer.
const (
	backgroundOutputCapBytes  = 1 << 20 // 1 MiB per stream
	backgroundOutputHalfBytes = backgroundOutputCapBytes / 2
)

// backgroundOutputKillBytes is the default total-output ceiling per stream
// before a runaway job is cancelled (its in-memory buffer is bounded anyway;
// this stops the process from burning CPU/disk forever). Aligned with Claude
// Code's resource cap idea, sized down for a CLI context.
const backgroundOutputKillBytes int64 = 2 << 30 // 2 GiB

// syncBuffer is a thread-safe bounded head+tail buffer that also tracks when
// the last write happened, so observers can distinguish "producing output"
// from "quiet — maybe waiting for input". The zero value is ready to use.
type syncBuffer struct {
	mu          sync.RWMutex
	head        []byte // stable prefix, capped at backgroundOutputHalfBytes
	tail        []byte // rolling suffix, capped at backgroundOutputHalfBytes
	omitted     int64  // bytes dropped from the middle
	total       int64  // every byte ever observed (head+tail+omitted)
	lastWriteMs atomic.Int64
	// killLimit, when > 0, cancels the job once total output reaches it —
	// the runaway-process guard. onOverflow is invoked asynchronously once.
	killLimit  int64
	onOverflow func()
	overflowed atomic.Bool
}

// OutputCapped reports whether the stream hit its total-output kill limit.
func (sb *syncBuffer) OutputCapped() bool {
	return sb.overflowed.Load()
}

// appendTailLocked adds p to the rolling tail, dropping the oldest tail bytes
// (counted in omitted) once the tail budget is exceeded.
func (sb *syncBuffer) appendTailLocked(p []byte) {
	if len(p) >= backgroundOutputHalfBytes {
		// The chunk alone fills the whole budget: keep only its last bytes.
		sb.omitted += int64(len(p) - backgroundOutputHalfBytes)
		sb.tail = append(sb.tail[:0], p[len(p)-backgroundOutputHalfBytes:]...)
		return
	}
	sb.tail = append(sb.tail, p...)
	if excess := len(sb.tail) - backgroundOutputHalfBytes; excess > 0 {
		sb.omitted += int64(excess)
		copy(sb.tail, sb.tail[excess:])
		sb.tail = sb.tail[:len(sb.tail)-excess]
	}
}

func (sb *syncBuffer) Write(p []byte) (n int, err error) {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	n = len(p)
	sb.total += int64(n)
	if n > 0 {
		sb.lastWriteMs.Store(time.Now().UnixMilli())
	}
	// Runaway guard: fire once, off the write path.
	if sb.killLimit > 0 && sb.total >= sb.killLimit && sb.onOverflow != nil && sb.overflowed.CompareAndSwap(false, true) {
		go sb.onOverflow()
	}
	// Fill the stable head first; everything after it flows into the tail.
	if len(sb.head) < backgroundOutputHalfBytes {
		room := backgroundOutputHalfBytes - len(sb.head)
		if room >= len(p) {
			sb.head = append(sb.head, p...)
			return n, nil
		}
		sb.head = append(sb.head, p[:room]...)
		p = p[room:]
	}
	sb.appendTailLocked(p)
	return n, nil
}

func (sb *syncBuffer) WriteString(s string) (n int, err error) {
	return sb.Write([]byte(s))
}

// String returns head + omission marker (if any) + tail.
func (sb *syncBuffer) String() string {
	sb.mu.RLock()
	defer sb.mu.RUnlock()
	var b strings.Builder
	b.Write(sb.head)
	if sb.omitted > 0 {
		fmt.Fprintf(&b, "\n... [%d bytes omitted] ...\n", sb.omitted)
	}
	b.Write(sb.tail)
	return b.String()
}

// Len returns the total number of bytes ever written to the buffer (including
// omitted bytes), without copying. Callers use it to report progress and to
// decide whether a tail view is truncated.
func (sb *syncBuffer) Len() int {
	sb.mu.RLock()
	defer sb.mu.RUnlock()
	return int(sb.total)
}

// Tail returns at most the last n bytes of the retained output, cut at a line
// boundary when possible so incremental reads stay line-aligned. With no
// omitted bytes head+tail are contiguous, so the tail view spans both; once
// the middle has been dropped the tail view is the rolling tail only.
func (sb *syncBuffer) Tail(n int) string {
	if n <= 0 {
		return ""
	}
	sb.mu.RLock()
	defer sb.mu.RUnlock()

	b := sb.tail
	if sb.omitted == 0 {
		b = make([]byte, 0, len(sb.head)+len(sb.tail))
		b = append(b, sb.head...)
		b = append(b, sb.tail...)
	}
	if len(b) <= n {
		return string(b)
	}
	cut := len(b) - n
	if i := bytes.IndexByte(b[cut:], '\n'); i >= 0 {
		cut += i + 1
	}
	return string(b[cut:])
}

// IdleMs reports milliseconds since the last write (0 when nothing written
// yet and the caller should fall back to started-at).
func (sb *syncBuffer) IdleMs() int64 {
	last := sb.lastWriteMs.Load()
	if last == 0 {
		return 0
	}
	return time.Now().UnixMilli() - last
}
