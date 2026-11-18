package browser

import (
	"context"
	"fmt"
)

// The design canvas is the viewport mocode uses when a task is judged on
// visual fidelity at a fixed artboard size. 2500x1600 CSS pixels is large
// enough to hold a dense application shell without the page having to
// collapse its own layout, and it is a size real design tools emit.
const (
	DesignCanvasWidth  = 2500
	DesignCanvasHeight = 1600
)

// maxViewportDimension bounds an emulated viewport. Chrome itself refuses
// absurd sizes, and an unbounded value turns a typo into a browser that
// allocates a gigabyte of pixels.
const maxViewportDimension = 10000

// Viewport is the emulated viewport of a page session.
//
// It is applied with CDP's Emulation.setDeviceMetricsOverride, which is a
// per-session override: it does not resize the OS window and it survives
// navigations, so a page laid out for the canvas stays laid out for it.
type Viewport struct {
	// Width and Height are CSS pixels.
	Width  int `json:"width"`
	Height int `json:"height"`
	// Scale is the device pixel ratio the page is emulated at.
	Scale float64 `json:"scale,omitempty"`
	// Mobile turns on the mobile viewport and touch emulation.
	Mobile bool `json:"mobile,omitempty"`
}

// DesignCanvas returns the 2500x1600 design canvas viewport.
func DesignCanvas() Viewport {
	return Viewport{Width: DesignCanvasWidth, Height: DesignCanvasHeight, Scale: 1}
}

// Normalized fills in the defaults a caller left out.
func (v Viewport) Normalized() Viewport {
	if v.Scale <= 0 {
		v.Scale = 1
	}
	return v
}

// Validate rejects a viewport Chrome would not honour, with an error the
// caller can show to a model and act on.
func (v Viewport) Validate() error {
	if v.Width <= 0 || v.Height <= 0 {
		return fmt.Errorf("viewport needs a positive width and height (got %dx%d)", v.Width, v.Height)
	}
	if v.Width > maxViewportDimension || v.Height > maxViewportDimension {
		return fmt.Errorf("viewport %dx%d exceeds the %d pixel limit", v.Width, v.Height, maxViewportDimension)
	}
	if v.Scale < 0.1 || v.Scale > 8 {
		return fmt.Errorf("viewport scale %.2f is out of range (0.1-8)", v.Scale)
	}
	return nil
}

// String describes the viewport the way a human would say it.
func (v Viewport) String() string {
	return fmt.Sprintf("%dx%d@%gx", v.Width, v.Height, v.Scale)
}

// SetViewport emulates a viewport size on the current page session.
//
// The override is remembered so it can be re-applied after a tab switch:
// emulation is bound to a page session, not to the browser.
func (s *Session) SetViewport(ctx context.Context, vp Viewport) error {
	vp = vp.Normalized()
	if err := vp.Validate(); err != nil {
		return err
	}
	if err := s.conn.Call(ctx, s.sessionID, "Emulation.setDeviceMetricsOverride", viewportParams(vp), nil); err != nil {
		return fmt.Errorf("setting viewport %s: %w", vp, err)
	}
	s.viewportMu.Lock()
	s.viewport = vp
	s.viewportMu.Unlock()
	return nil
}

// ClearViewport removes the emulation override and returns the page to the
// browser's own window size.
func (s *Session) ClearViewport(ctx context.Context) error {
	if err := s.conn.Call(ctx, s.sessionID, "Emulation.clearDeviceMetricsOverride", nil, nil); err != nil {
		return fmt.Errorf("clearing viewport: %w", err)
	}
	s.viewportMu.Lock()
	s.viewport = Viewport{}
	s.viewportMu.Unlock()
	return nil
}

// CurrentViewport reports the viewport in force, or the zero value when the
// page is running at the browser's own size.
func (s *Session) CurrentViewport() Viewport {
	s.viewportMu.Lock()
	defer s.viewportMu.Unlock()
	return s.viewport
}

// applyViewport re-applies a remembered viewport after the page session
// changed. It is a no-op when no viewport was ever set.
func (s *Session) applyViewport(ctx context.Context) error {
	vp := s.CurrentViewport()
	if vp.Width == 0 && vp.Height == 0 {
		return nil
	}
	if err := s.conn.Call(ctx, s.sessionID, "Emulation.setDeviceMetricsOverride", viewportParams(vp), nil); err != nil {
		return fmt.Errorf("re-applying viewport %s: %w", vp, err)
	}
	return nil
}

// viewportParams builds the CDP payload for a viewport.
//
// screenWidth and screenHeight are set to the same values so that media
// queries and position:fixed elements see a screen that matches the
// viewport instead of the real window.
func viewportParams(vp Viewport) map[string]any {
	return map[string]any{
		"width":             vp.Width,
		"height":            vp.Height,
		"deviceScaleFactor": vp.Scale,
		"mobile":            vp.Mobile,
		"screenWidth":       vp.Width,
		"screenHeight":      vp.Height,
	}
}
