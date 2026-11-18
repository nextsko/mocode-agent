package browser

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Recording defaults and limits.
const (
	defaultRecordFPS     = 10
	defaultRecordQuality = 60
	maxRecordQuality     = 100
	// maxRecordFrames bounds a single take. A screencast at 10 fps runs for a
	// minute in 600 frames; beyond that the buffer is a memory leak with a
	// video file attached.
	maxRecordFrames = 600
)

// RecordOptions configures a screencast.
type RecordOptions struct {
	// FPS is the frame rate the recording is played back at.
	FPS int
	// Quality is the JPEG quality of each frame (1-100).
	Quality int
	// MaxWidth and MaxHeight scale the captured frames down. Zero keeps the
	// page's own size, which at the 2500x1600 design canvas is a lot of
	// pixels per frame.
	MaxWidth  int
	MaxHeight int
	// EveryNthFrame drops frames to lower the rate the browser produces.
	EveryNthFrame int
}

// normalized fills in defaults and clamps values the browser would reject.
func (o RecordOptions) normalized() RecordOptions {
	if o.FPS <= 0 {
		o.FPS = defaultRecordFPS
	}
	if o.Quality <= 0 {
		o.Quality = defaultRecordQuality
	}
	if o.Quality > maxRecordQuality {
		o.Quality = maxRecordQuality
	}
	if o.EveryNthFrame <= 0 {
		o.EveryNthFrame = 1
	}
	return o
}

// Recorder captures a page as a stream of JPEG frames.
//
// It is driven by CDP's screencast, which pushes a frame only after the
// client acknowledges the previous one. The acknowledgement therefore has
// to travel from inside the event handler, and it cannot wait for a reply —
// the reply would be delivered by the very read loop that is running the
// handler.
type Recorder struct {
	sess *Session
	opts RecordOptions

	mu        sync.Mutex
	frames    []VideoFrame
	recording bool
	dropped   int

	unsubscribe func()
	started     time.Time
}

// StartRecording starts a screencast on the current page session.
//
// The event handler is registered before the browser is asked to start, so
// no frame is lost between the two.
func (s *Session) StartRecording(ctx context.Context, opts RecordOptions) (*Recorder, error) {
	opts = opts.normalized()
	if existing := s.Recorder(); existing != nil {
		if _, err := existing.Stop(ctx); err != nil {
			return nil, fmt.Errorf("stopping the previous recording: %w", err)
		}
	}

	r := &Recorder{sess: s, opts: opts, started: time.Now()}
	r.unsubscribe = s.conn.On("Page.screencastFrame", r.onFrame)

	params := map[string]any{
		"format":        "jpeg",
		"quality":       opts.Quality,
		"everyNthFrame": opts.EveryNthFrame,
	}
	if opts.MaxWidth > 0 {
		params["maxWidth"] = opts.MaxWidth
	}
	if opts.MaxHeight > 0 {
		params["maxHeight"] = opts.MaxHeight
	}
	if err := s.conn.Call(ctx, s.sessionID, "Page.startScreencast", params, nil); err != nil {
		r.unsubscribe()
		return nil, fmt.Errorf("starting screencast: %w", err)
	}
	r.recording = true
	s.setRecorder(r)
	return r, nil
}

// onFrame stores one screencast frame and acknowledges it.
func (r *Recorder) onFrame(_ string, params json.RawMessage) {
	var ev struct {
		Data     string `json:"data"`
		Metadata struct {
			Timestamp float64 `json:"timestamp"`
			SessionID int64   `json:"sessionId"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(params, &ev); err != nil || ev.Data == "" {
		return
	}
	data, err := base64.StdEncoding.DecodeString(ev.Data)
	if err != nil {
		return
	}

	r.mu.Lock()
	if len(r.frames) >= maxRecordFrames {
		r.dropped++
		r.mu.Unlock()
		r.ack(ev.Metadata.SessionID)
		return
	}
	r.frames = append(r.frames, VideoFrame{
		Data:      data,
		Timestamp: time.Duration(ev.Metadata.Timestamp * float64(time.Second)),
	})
	r.mu.Unlock()
	r.ack(ev.Metadata.SessionID)
}

// ack releases the next frame. It must not wait for a response: the
// response would be delivered by the read loop that is running this
// handler.
func (r *Recorder) ack(sessionID int64) {
	if sessionID == 0 {
		return
	}
	_ = r.sess.conn.Notify(r.sess.sessionID, "Page.screencastFrameAck", map[string]any{"sessionId": sessionID})
}

// Stop ends the screencast and returns the frames captured so far.
func (r *Recorder) Stop(ctx context.Context) ([]VideoFrame, error) {
	r.mu.Lock()
	if !r.recording {
		r.mu.Unlock()
		return nil, errors.New("no recording is in progress")
	}
	r.recording = false
	r.mu.Unlock()

	if r.unsubscribe != nil {
		r.unsubscribe()
		r.unsubscribe = nil
	}
	if err := r.sess.conn.Call(ctx, r.sess.sessionID, "Page.stopScreencast", nil, nil); err != nil {
		return nil, fmt.Errorf("stopping screencast: %w", err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]VideoFrame, len(r.frames))
	copy(out, r.frames)
	return out, nil
}

// FrameCount reports how many frames were captured.
func (r *Recorder) FrameCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.frames)
}

// Recording reports whether the recorder is still capturing.
func (r *Recorder) Recording() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.recording
}

// Dropped reports how many frames were discarded because the take hit its
// frame cap.
func (r *Recorder) Dropped() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.dropped
}

// Options reports the settings the recording was started with.
func (r *Recorder) Options() RecordOptions { return r.opts }

// Duration reports how long the take ran.
func (r *Recorder) Duration() time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.frames) < 2 {
		return 0
	}
	first := r.frames[0].Timestamp
	last := r.frames[len(r.frames)-1].Timestamp
	if last <= first {
		return 0
	}
	return last - first
}
