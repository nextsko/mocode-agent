package browser

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	corebrowser "github.com/nextsko/mocode-agent/internal/core/browser"
	"github.com/nextsko/mocode-agent/internal/core/tools/contract"
)

// viewportAction changes the emulated viewport of the live session.
//
// The viewport is a CDP device-metrics override: the page lays out at the
// requested size without the OS window changing, and it survives navigations
// and tab switches.
func viewportAction(ctx context.Context, session *corebrowser.Session, p BrowserParams) (contract.ToolResult, error) {
	switch strings.ToLower(strings.TrimSpace(p.Op)) {
	case "", "set":
		if p.Width <= 0 && p.Height <= 0 {
			return softError("action=viewport op=set needs width and height, or use op=design for the 2500x1600 canvas"), nil
		}
		vp := corebrowser.Viewport{Width: p.Width, Height: p.Height, Scale: p.Scale, Mobile: p.Mobile}
		if err := session.SetViewport(ctx, vp); err != nil {
			return softError(err.Error()), nil
		}
		return contract.WithMetadata(
			contract.Text(fmt.Sprintf("Viewport set to %s. The page lays out at that size now; screenshots and recordings use it.", vp)),
			BrowserResponseMetadata{Action: actionViewport},
		), nil
	case "design", "design-canvas", "canvas":
		vp := corebrowser.DesignCanvas()
		if err := session.SetViewport(ctx, vp); err != nil {
			return softError(err.Error()), nil
		}
		return contract.WithMetadata(
			contract.Text(fmt.Sprintf("Viewport set to the design canvas (%s).", vp)),
			BrowserResponseMetadata{Action: actionViewport},
		), nil
	case "reset", subActionClear:
		if err := session.ClearViewport(ctx); err != nil {
			return softError(err.Error()), nil
		}
		return contract.WithMetadata(
			contract.Text("Viewport cleared. The page is back at the browser's own window size."),
			BrowserResponseMetadata{Action: actionViewport},
		), nil
	case "get":
		vp := session.CurrentViewport()
		if vp.Width == 0 && vp.Height == 0 {
			return contract.WithMetadata(
				contract.Text("The page is running at the browser's own window size; no viewport override is set."),
				BrowserResponseMetadata{Action: actionViewport},
			), nil
		}
		return contract.WithMetadata(
			contract.Text(fmt.Sprintf("Viewport: %s", vp)),
			BrowserResponseMetadata{Action: actionViewport},
		), nil
	default:
		return softError("op for action=viewport must be set, design, reset or get"), nil
	}
}

// recordAction captures the page as a video.
//
// The recording is a real video file: Chrome's screencast hands us JPEG
// frames and they go into an MJPEG AVI verbatim, so no codec and no
// external tool is involved.
func recordAction(ctx context.Context, session *corebrowser.Session, p BrowserParams) (contract.ToolResult, error) {
	switch strings.ToLower(strings.TrimSpace(p.Op)) {
	case "start":
		opts := corebrowser.RecordOptions{
			FPS:       p.FPS,
			Quality:   p.Quality,
			MaxWidth:  p.MaxWidth,
			MaxHeight: p.MaxHeight,
		}
		rec, err := session.StartRecording(ctx, opts)
		if err != nil {
			return softError(err.Error()), nil
		}
		settings := rec.Options()
		return contract.WithMetadata(
			contract.Text(fmt.Sprintf("Recording started (%d fps, quality %d). Do what you want to capture, then call action=record op=stop to write the video.", settings.FPS, settings.Quality)),
			BrowserResponseMetadata{Action: actionRecord},
		), nil
	case "stop":
		rec := session.Recorder()
		if rec == nil {
			return softError("no recording is in progress; start one with action=record op=start"), nil
		}
		frames, err := rec.Stop(ctx)
		if err != nil {
			return softError(err.Error()), nil
		}
		if len(frames) == 0 {
			return softError("the recording captured no frames"), nil
		}
		path := recordingOutputPath(p.Path)
		if err := corebrowser.WriteMJPEGAVI(path, frames, rec.Options().FPS); err != nil {
			return softError(fmt.Sprintf("writing the recording: %v", err)), nil
		}
		size := 0
		if info, err := os.Stat(path); err == nil {
			size = int(info.Size())
		}
		message := fmt.Sprintf("Recording written to %s: %d frames, %d bytes. It is an MJPEG AVI; `ffmpeg -i %s out.mp4` converts it to MP4.", path, len(frames), size, path)
		return contract.WithMetadata(
			contract.Text(message),
			BrowserResponseMetadata{Action: actionRecord, Path: path, Count: len(frames), Bytes: size},
		), nil
	case "status":
		rec := session.Recorder()
		if rec == nil {
			return contract.WithMetadata(
				contract.Text("No recording is in progress."),
				BrowserResponseMetadata{Action: actionRecord},
			), nil
		}
		return contract.WithMetadata(
			contract.Text(fmt.Sprintf("Recording in progress: %d frames captured.", rec.FrameCount())),
			BrowserResponseMetadata{Action: actionRecord, Count: rec.FrameCount()},
		), nil
	default:
		return softError("op for action=record must be start, stop or status"), nil
	}
}

// recordingOutputPath resolves where a recording is written.
func recordingOutputPath(path string) string {
	if trimmed := strings.TrimSpace(path); trimmed != "" {
		return trimmed
	}
	dir, err := os.Getwd()
	if err != nil {
		dir = "."
	}
	return filepath.Join(dir, fmt.Sprintf("mocode-recording-%s.avi", time.Now().Format("20060102-150405")))
}
