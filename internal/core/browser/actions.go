package browser

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

// Click activates an element addressed by ref or CSS selector.
//
// Ref-based clicks dispatch real mouse events at the element's centre rather
// than calling element.click(): a synthetic DOM click skips the pointer event
// sequence that many apps (and all drag/drop or canvas UIs) depend on, which
// makes the tool lie about what a user would experience.
func (s *Session) Click(ctx context.Context, target string) error {
	expr, err := resolveTarget(target)
	if err != nil {
		return err
	}

	box, err := s.elementCentre(ctx, expr)
	if err != nil {
		return err
	}

	for _, ev := range []map[string]any{
		{"type": "mouseMoved", "x": box.X, "y": box.Y},
		{"type": "mousePressed", "x": box.X, "y": box.Y, "button": "left", "clickCount": 1},
		{"type": "mouseReleased", "x": box.X, "y": box.Y, "button": "left", "clickCount": 1},
	} {
		if err := s.conn.Call(ctx, s.sessionID, "Input.dispatchMouseEvent", ev, nil); err != nil {
			return fmt.Errorf("clicking %s: %w", target, err)
		}
	}
	return nil
}

// Fill sets the value of an input or textarea.
//
// The value is assigned through the prototype's native setter and followed by
// input/change events. A plain `el.value = x` is invisible to React, Vue and
// friends, which track the value on their own side and would submit the old
// one — so filling that way looks like it worked and silently does not.
func (s *Session) Fill(ctx context.Context, target, text string, submit bool) error {
	expr, err := resolveTarget(target)
	if err != nil {
		return err
	}

	script := fmt.Sprintf(`(() => {
  const el = %s;
  if (!el) return 'not found';
  el.focus();
  const proto = el.tagName === 'TEXTAREA' ? HTMLTextAreaElement.prototype : HTMLInputElement.prototype;
  const desc = Object.getOwnPropertyDescriptor(proto, 'value');
  if (desc && desc.set) { desc.set.call(el, %s); } else { el.value = %s; }
  el.dispatchEvent(new Event('input', { bubbles: true }));
  el.dispatchEvent(new Event('change', { bubbles: true }));
  return 'ok';
})()`, expr, jsString(text), jsString(text))

	var res string
	if err := s.Evaluate(ctx, script, &res); err != nil {
		return fmt.Errorf("filling %s: %w", target, err)
	}
	if res != "ok" {
		return fmt.Errorf("filling %s: element not found (the page may have re-rendered; take a new snapshot)", target)
	}

	if submit {
		return s.Press(ctx, "Enter")
	}
	return nil
}

// Press sends a key to the page's focused element.
func (s *Session) Press(ctx context.Context, key string) error {
	code, keyCode, text := keyDescriptor(key)
	if code == "" {
		return fmt.Errorf("unsupported key %q", key)
	}

	for _, ev := range []map[string]any{
		{"type": "keyDown", "key": key, "code": code, "windowsVirtualKeyCode": keyCode, "text": text},
		{"type": "keyUp", "key": key, "code": code, "windowsVirtualKeyCode": keyCode},
	} {
		if err := s.conn.Call(ctx, s.sessionID, "Input.dispatchKeyEvent", ev, nil); err != nil {
			return fmt.Errorf("pressing %s: %w", key, err)
		}
	}
	return nil
}

// Screenshot captures the page as PNG bytes.
func (s *Session) Screenshot(ctx context.Context, fullPage bool) ([]byte, error) {
	params := map[string]any{"format": "png"}
	if fullPage {
		params["captureBeyondViewport"] = true
	}

	var res struct {
		Data string `json:"data"`
	}
	if err := s.conn.Call(ctx, s.sessionID, "Page.captureScreenshot", params, &res); err != nil {
		return nil, fmt.Errorf("capturing screenshot: %w", err)
	}
	if res.Data == "" {
		return nil, fmt.Errorf("capturing screenshot: browser returned no image data")
	}

	data, err := base64.StdEncoding.DecodeString(res.Data)
	if err != nil {
		return nil, fmt.Errorf("decoding screenshot: %w", err)
	}
	return data, nil
}

// navigateHistory is CDP's Page.getNavigationHistory shape. Real Chrome sends
// entry ids as numbers — decoding them as strings fails the whole call.
type navigateHistory struct {
	CurrentIndex int `json:"currentIndex"`
	Entries      []struct {
		ID int64 `json:"id"`
	} `json:"entries"`
}

// NavigateBack goes back one entry in the session history.
func (s *Session) NavigateBack(ctx context.Context) error {
	var entries navigateHistory
	if err := s.conn.Call(ctx, s.sessionID, "Page.getNavigationHistory", nil, &entries); err != nil {
		return fmt.Errorf("reading navigation history: %w", err)
	}
	if entries.CurrentIndex <= 0 || entries.CurrentIndex > len(entries.Entries) {
		return fmt.Errorf("no previous page in history")
	}
	prev := entries.Entries[entries.CurrentIndex-1]
	return s.conn.Call(ctx, s.sessionID, "Page.navigateToHistoryEntry",
		map[string]any{"entryId": prev.ID}, nil)
}

// NavigateForward goes forward one entry in the session history.
func (s *Session) NavigateForward(ctx context.Context) error {
	var entries navigateHistory
	if err := s.conn.Call(ctx, s.sessionID, "Page.getNavigationHistory", nil, &entries); err != nil {
		return fmt.Errorf("reading navigation history: %w", err)
	}
	if entries.CurrentIndex < 0 || entries.CurrentIndex+1 >= len(entries.Entries) {
		return fmt.Errorf("no next page in history")
	}
	next := entries.Entries[entries.CurrentIndex+1]
	return s.conn.Call(ctx, s.sessionID, "Page.navigateToHistoryEntry",
		map[string]any{"entryId": next.ID}, nil)
}

// Reload reloads the current page and waits for it to load.
func (s *Session) Reload(ctx context.Context) error {
	if err := s.conn.Call(ctx, s.sessionID, "Page.reload", nil, nil); err != nil {
		return fmt.Errorf("reloading: %w", err)
	}
	return s.waitLoad(ctx)
}

// Hover moves the pointer over an element.
//
// It dispatches a real mouseMoved at the element's centre, which is what
// CSS :hover and the hover handlers of charts and menus listen for.
func (s *Session) Hover(ctx context.Context, target string) error {
	expr, err := resolveTarget(target)
	if err != nil {
		return err
	}
	box, err := s.elementCentre(ctx, expr)
	if err != nil {
		return err
	}
	if err := s.conn.Call(ctx, s.sessionID, "Input.dispatchMouseEvent", map[string]any{
		"type": "mouseMoved", "x": box.X, "y": box.Y,
	}, nil); err != nil {
		return fmt.Errorf("hovering %s: %w", target, err)
	}
	return nil
}

// DblClick double-clicks an element with real pointer events.
func (s *Session) DblClick(ctx context.Context, target string) error {
	expr, err := resolveTarget(target)
	if err != nil {
		return err
	}
	box, err := s.elementCentre(ctx, expr)
	if err != nil {
		return err
	}
	for _, ev := range []map[string]any{
		{"type": "mouseMoved", "x": box.X, "y": box.Y},
		{"type": "mousePressed", "x": box.X, "y": box.Y, "button": "left", "clickCount": 2},
		{"type": "mouseReleased", "x": box.X, "y": box.Y, "button": "left", "clickCount": 2},
	} {
		if err := s.conn.Call(ctx, s.sessionID, "Input.dispatchMouseEvent", ev, nil); err != nil {
			return fmt.Errorf("double-clicking %s: %w", target, err)
		}
	}
	return nil
}

// checkedState is what the probe reads off a checkbox or radio.
type checkedState struct {
	Tag      string `json:"tag"`
	Type     string `json:"type"`
	Checked  bool   `json:"checked"`
	Disabled bool   `json:"disabled"`
}

// Check ticks a checkbox or radio; Uncheck clears it. want carries which.
//
// The state is verified after clicking: a click that the page swallowed (a
// disabled control, a preventDefault handler) must not be reported as success.
func (s *Session) setChecked(ctx context.Context, target string, want bool) error {
	expr, err := resolveTarget(target)
	if err != nil {
		return err
	}

	probe := fmt.Sprintf(`(() => {
  const el = %s;
  if (!el) return null;
  return { tag: el.tagName, type: (el.getAttribute('type') || '').toLowerCase(), checked: !!el.checked, disabled: !!el.disabled };
})()`, expr)
	var state *checkedState
	if err := s.Evaluate(ctx, probe, &state); err != nil {
		return err
	}
	if state == nil {
		return fmt.Errorf("element not found (the page may have re-rendered; take a new snapshot)")
	}

	if state.Tag != "INPUT" || (state.Type != "checkbox" && state.Type != "radio") {
		return fmt.Errorf("%s is not a checkbox or radio (tag %s, type %q)", target, state.Tag, state.Type)
	}
	if state.Disabled {
		return fmt.Errorf("%s is disabled", target)
	}
	if state.Checked == want {
		return nil // already in the wanted state
	}
	if err := s.Click(ctx, target); err != nil {
		return err
	}

	if err := s.Evaluate(ctx, probe, &state); err != nil {
		return err
	}
	if state == nil || state.Checked != want {
		verb := "checking"
		if !want {
			verb = "unchecking"
		}
		return fmt.Errorf("%s %s: the click did not change the state", verb, target)
	}
	return nil
}

// Check ticks a checkbox or selects a radio.
func (s *Session) Check(ctx context.Context, target string) error {
	return s.setChecked(ctx, target, true)
}

// Uncheck clears a checkbox.
func (s *Session) Uncheck(ctx context.Context, target string) error {
	return s.setChecked(ctx, target, false)
}

// SelectOption picks an option of a <select> by value or visible label.
func (s *Session) SelectOption(ctx context.Context, target, value string) error {
	expr, err := resolveTarget(target)
	if err != nil {
		return err
	}

	script := fmt.Sprintf(`(() => {
  const el = %s;
  if (!el) return 'not found';
  if (el.tagName !== 'SELECT') return 'not a select';
  const wanted = %s;
  const byValue = [...el.options].find(o => o.value === wanted);
  const byLabel = [...el.options].find(o => (o.label || o.text).trim() === wanted.trim());
  const opt = byValue || byLabel;
  if (!opt) return 'no option ' + wanted;
  el.value = opt.value;
  el.dispatchEvent(new Event('input', { bubbles: true }));
  el.dispatchEvent(new Event('change', { bubbles: true }));
  return 'ok:' + opt.value;
})()`, expr, jsString(value))

	var res string
	if err := s.Evaluate(ctx, script, &res); err != nil {
		return fmt.Errorf("selecting %q in %s: %w", value, target, err)
	}
	switch {
	case res == "not found":
		return fmt.Errorf("element not found (the page may have re-rendered; take a new snapshot)")
	case res == "not a select":
		return fmt.Errorf("%s is not a <select>", target)
	case strings.HasPrefix(res, "no option"):
		return fmt.Errorf("%s in %s", res, target)
	}
	return nil
}

// PDF renders the page to PDF bytes. Only headless Chrome supports this; a
// headed session fails with the browser's own error, which is the honest
// message.
func (s *Session) PDF(ctx context.Context) ([]byte, error) {
	var res struct {
		Data string `json:"data"`
	}
	if err := s.conn.Call(ctx, s.sessionID, "Page.printToPDF", map[string]any{
		"printBackground":   true,
		"preferCSSPageSize": true,
	}, &res); err != nil {
		return nil, fmt.Errorf("printing to PDF: %w", err)
	}
	if res.Data == "" {
		return nil, fmt.Errorf("printing to PDF: browser returned no data")
	}
	data, err := base64.StdEncoding.DecodeString(res.Data)
	if err != nil {
		return nil, fmt.Errorf("decoding PDF: %w", err)
	}
	return data, nil
}

// box is an element's centre in CSS pixels.
type box struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// elementCentre scrolls the element into view and returns its centre point.
//
// Scrolling first is required for correctness, not comfort: mouse events carry
// viewport coordinates, so an element below the fold would otherwise be clicked
// at the wrong place (or clamped to the edge).
func (s *Session) elementCentre(ctx context.Context, expr string) (box, error) {
	script := fmt.Sprintf(`(() => {
  const el = %s;
  if (!el) return null;
  el.scrollIntoView({ block: 'center', inline: 'center' });
  const r = el.getBoundingClientRect();
  if (r.width === 0 && r.height === 0) return null;
  return { x: r.left + r.width / 2, y: r.top + r.height / 2 };
})()`, expr)

	var out *box
	if err := s.Evaluate(ctx, script, &out); err != nil {
		return box{}, err
	}
	if out == nil {
		return box{}, fmt.Errorf("element is not present or not visible; take a new snapshot")
	}
	return *out, nil
}

// resolveTarget turns a ref or a CSS selector into a JS expression.
func resolveTarget(target string) (string, error) {
	trimmed := strings.TrimSpace(target)
	if trimmed == "" {
		return "", fmt.Errorf("ref or selector is required")
	}
	if isValidRef(trimmed) {
		return refExpression(trimmed)
	}
	if strings.HasPrefix(trimmed, "e") && len(trimmed) <= 6 {
		// Shaped like a ref but not one — most likely a typo, and saying so is
		// more useful than silently treating it as a selector.
		return "", fmt.Errorf("invalid ref %q: expected the form e0, e12 (take it from the snapshot output)", trimmed)
	}
	// Otherwise treat it as a CSS selector, embedded as JSON so quoting cannot
	// break out of the expression.
	return "document.querySelector(" + jsString(trimmed) + ")", nil
}

// jsString encodes s as a JavaScript string literal. Encoding via JSON is the
// safe way to embed model-supplied text in a script: it escapes quotes,
// backslashes and newlines exactly as JS expects.
func jsString(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		// json.Marshal on a string cannot fail; be explicit rather than panic.
		return `""`
	}
	return string(b)
}

// keyDescriptor maps a key name to the CDP key fields for the common keys.
// Anything unmapped falls back to a single-character keystroke.
func keyDescriptor(key string) (code string, keyCode int, text string) {
	switch key {
	case "Enter":
		return "Enter", 13, "\r"
	case "Tab":
		return "Tab", 9, "\t"
	case "Escape", "Esc":
		return "Escape", 27, ""
	case "Backspace":
		return "Backspace", 8, ""
	case "Delete":
		return "Delete", 46, ""
	case "ArrowUp":
		return "ArrowUp", 38, ""
	case "ArrowDown":
		return "ArrowDown", 40, ""
	case "ArrowLeft":
		return "ArrowLeft", 37, ""
	case "ArrowRight":
		return "ArrowRight", 39, ""
	case "PageUp":
		return "PageUp", 33, ""
	case "PageDown":
		return "PageDown", 34, ""
	case "Home":
		return "Home", 36, ""
	case "End":
		return "End", 35, ""
	case " ":
		return "Space", 32, " "
	}
	if len([]rune(key)) == 1 {
		return "Key" + strings.ToUpper(key), int([]rune(key)[0]), key
	}
	return "", 0, ""
}
