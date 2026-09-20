package browser

import (
	"context"
	"fmt"
	"strings"
)

// TabInfo describes one open page target.
type TabInfo struct {
	Index    int    `json:"index"`
	TargetID string `json:"targetId"`
	URL      string `json:"url"`
	Current  bool   `json:"current"`
}

// targetInfo is the subset of CDP's Target.TargetInfo the session cares about.
type targetInfo struct {
	TargetID string `json:"targetId"`
	Type     string `json:"type"`
	URL      string `json:"url"`
}

// pageTargets lists every open page target.
func (s *Session) pageTargets(ctx context.Context) ([]targetInfo, error) {
	var res struct {
		TargetInfos []targetInfo `json:"targetInfos"`
	}
	if err := s.conn.Call(ctx, "", "Target.getTargets", nil, &res); err != nil {
		return nil, fmt.Errorf("listing targets: %w", err)
	}
	pages := make([]targetInfo, 0, len(res.TargetInfos))
	for _, t := range res.TargetInfos {
		if t.Type == "page" {
			pages = append(pages, t)
		}
	}
	return pages, nil
}

// Tabs lists the open pages, marking the one the session acts on.
func (s *Session) Tabs(ctx context.Context) ([]TabInfo, error) {
	pages, err := s.pageTargets(ctx)
	if err != nil {
		return nil, err
	}
	tabs := make([]TabInfo, 0, len(pages))
	for i, p := range pages {
		tabs = append(tabs, TabInfo{
			Index:    i,
			TargetID: p.TargetID,
			URL:      p.URL,
			Current:  p.TargetID == s.targetID,
		})
	}
	return tabs, nil
}

// TabNew opens a page, attaches to it, and makes it the session's current tab.
//
// The tab is created blank and navigated afterwards. Handing the URL to
// createTarget instead means the page can finish loading before the session has
// attached, and the load event would be missed — leaving the caller waiting out
// the timeout for a page that is already there.
func (s *Session) TabNew(ctx context.Context, url string) error {
	var created struct {
		TargetID string `json:"targetId"`
	}
	if err := s.conn.Call(ctx, "", "Target.createTarget", map[string]any{"url": "about:blank"}, &created); err != nil {
		return fmt.Errorf("creating a tab: %w", err)
	}
	if created.TargetID == "" {
		return fmt.Errorf("creating a tab: browser returned no target id")
	}
	if err := s.switchPage(ctx, created.TargetID); err != nil {
		return err
	}
	if u := strings.TrimSpace(url); u != "" && !isBlankURL(u) {
		return s.Navigate(ctx, u)
	}
	return nil
}

// TabSelect makes the page at index the session's current tab.
func (s *Session) TabSelect(ctx context.Context, index int) error {
	tabs, err := s.Tabs(ctx)
	if err != nil {
		return err
	}
	if index < 0 || index >= len(tabs) {
		return fmt.Errorf("tab index %d is out of range: %d tab(s) open", index, len(tabs))
	}
	if tabs[index].Current {
		return nil
	}
	return s.switchPage(ctx, tabs[index].TargetID)
}

// TabClose closes the page at index.
//
// Closing the current tab switches to the next surviving one rather than
// leaving the session pointing at a dead target. Closing the last tab is
// refused: ending the session is what action=close is for.
func (s *Session) TabClose(ctx context.Context, index int) error {
	tabs, err := s.Tabs(ctx)
	if err != nil {
		return err
	}
	if index < 0 || index >= len(tabs) {
		return fmt.Errorf("tab index %d is out of range: %d tab(s) open", index, len(tabs))
	}
	if len(tabs) == 1 {
		return fmt.Errorf("cannot close the only tab; use action=close to end the session")
	}

	closing := tabs[index]
	if err := s.conn.Call(ctx, "", "Target.closeTarget", map[string]any{"targetId": closing.TargetID}, nil); err != nil {
		return fmt.Errorf("closing tab %d: %w", index, err)
	}

	if !closing.Current {
		return nil
	}
	// The current page is gone; fall back to the session's usual picking, which
	// prefers a page holding a real document and only creates one if none is
	// left — exactly the semantics wanted here.
	return s.attachPage(ctx)
}
