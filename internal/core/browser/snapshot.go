package browser

import (
	"context"
	"fmt"
	"strings"
)

// Element is one addressable node in a page snapshot.
//
// Refs exist so the model never has to invent a CSS selector: selectors are
// brittle, and a wrong one fails in a way that is hard to diagnose. A ref is a
// short handle we minted ourselves, so we can always resolve it back.
type Element struct {
	Ref      string `json:"ref"`
	Tag      string `json:"tag"`
	Role     string `json:"role"`
	Name     string `json:"name"`
	Value    string `json:"value"`
	Href     string `json:"href"`
	Disabled bool   `json:"disabled"`
}

// Snapshot is a page's addressable structure.
type Snapshot struct {
	URL      string    `json:"url"`
	Title    string    `json:"title"`
	Elements []Element `json:"elements"`
	// Truncated is set when the page had more addressable elements than the cap.
	Truncated bool `json:"truncated"`
}

// maxSnapshotElements bounds a snapshot. A large page can hold tens of
// thousands of nodes and dumping them all would blow the model's context for no
// benefit — the model only ever acts on a handful.
const maxSnapshotElements = 300

// refAttr is the attribute our injected script writes refs into. Acting on an
// element later is then a plain attribute lookup, which survives re-renders far
// better than a recorded CSS path.
const refAttr = "data-mocode-ref"

// snapshotScript walks the DOM, tags addressable elements with a ref, and
// returns the outline.
//
// It deliberately ignores anything not visible: an action on a hidden element
// silently does nothing, so offering one is worse than omitting it.
const snapshotScript = `(() => {
  const MAX = %d;
  const ATTR = '%s';
  const SKIP = new Set(['SCRIPT','STYLE','NOSCRIPT','TEMPLATE','IFRAME','OBJECT','EMBED','HEAD','META','LINK']);
  const INTERACTIVE = new Set(['A','BUTTON','INPUT','SELECT','TEXTAREA','OPTION','LABEL','SUMMARY','DETAILS']);

  document.querySelectorAll('[' + ATTR + ']').forEach(el => el.removeAttribute(ATTR));

  const visible = (el) => {
    const r = el.getBoundingClientRect();
    if (r.width === 0 && r.height === 0) return false;
    const st = window.getComputedStyle(el);
    if (!st || st.visibility === 'hidden' || st.display === 'none' || st.opacity === '0') return false;
    return true;
  };

  const label = (el) => {
    const cand = el.getAttribute('aria-label')
      || (el.innerText || '').trim()
      || el.value
      || el.getAttribute('placeholder')
      || el.getAttribute('title')
      || el.getAttribute('alt')
      || el.getAttribute('name')
      || '';
    return String(cand).replace(/\s+/g, ' ').trim().slice(0, 160);
  };

  const roleOf = (el) => {
    const explicit = el.getAttribute('role');
    if (explicit) return explicit;
    const t = el.tagName;
    if (t === 'A') return 'link';
    if (t === 'BUTTON') return 'button';
    if (t === 'INPUT') return (el.getAttribute('type') || 'text').toLowerCase();
    if (t === 'TEXTAREA') return 'textbox';
    if (t === 'SELECT') return 'combobox';
    return t.toLowerCase();
  };

  const out = [];
  const seen = new Set();
  let truncated = false;

  const add = (el) => {
    if (out.length >= MAX) { truncated = true; return; }
    if (seen.has(el)) return;
    if (!visible(el)) return;
    const name = label(el);
    const tag = el.tagName;
    const interactive = INTERACTIVE.has(tag) || el.hasAttribute('role') || el.hasAttribute('tabindex');
    if (!interactive && name === '') return;

    seen.add(el);
    const ref = 'e' + out.length;
    el.setAttribute(ATTR, ref);
    out.push({
      ref,
      tag: tag.toLowerCase(),
      role: roleOf(el),
      name,
      value: String(el.value === undefined ? '' : el.value).slice(0, 160),
      href: el.getAttribute('href') || '',
      disabled: el.disabled === true || el.getAttribute('aria-disabled') === 'true'
    });
  };

  // Two passes, actionable first. A text-heavy page can easily have more than
  // MAX text nodes, and filling the budget with prose would push the buttons and
  // inputs out of the snapshot — making the tool useless exactly where it is
  // needed most.
  for (const el of document.querySelectorAll('*')) {
    if (SKIP.has(el.tagName)) continue;
    if (INTERACTIVE.has(el.tagName) || el.hasAttribute('role') || el.hasAttribute('tabindex')) add(el);
  }
  for (const el of document.querySelectorAll('*')) {
    if (out.length >= MAX) { truncated = true; break; }
    if (SKIP.has(el.tagName)) continue;
    // A leaf with text: any tag, not a fixed list. Rendering an updated result
    // into a plain <div> is the single most common pattern there is, and
    // restricting this to a tag whitelist made such content invisible.
    if (el.children.length === 0 && (el.textContent || '').trim() !== '') add(el);
  }

  return { url: location.href, title: document.title, elements: out, truncated };
})()`

// Snapshot renders the current page's addressable structure.
func (s *Session) Snapshot(ctx context.Context) (*Snapshot, error) {
	script := fmt.Sprintf(snapshotScript, maxSnapshotElements, refAttr)

	var snap Snapshot
	if err := s.Evaluate(ctx, script, &snap); err != nil {
		return nil, fmt.Errorf("taking a snapshot: %w", err)
	}
	if snap.Elements == nil {
		snap.Elements = []Element{}
	}
	return &snap, nil
}

// Render formats a snapshot for the model: one line per element, ref first so
// the model can copy it straight into the next call.
func (snap *Snapshot) Render() string {
	var b strings.Builder
	fmt.Fprintf(&b, "URL: %s\n", snap.URL)
	if snap.Title != "" {
		fmt.Fprintf(&b, "Title: %s\n", snap.Title)
	}
	if len(snap.Elements) == 0 {
		b.WriteString("\nNo addressable elements found. The page may still be loading, or its content may be inside a frame (frames are not traversed).\n")
		return b.String()
	}

	fmt.Fprintf(&b, "\n%d element(s):\n", len(snap.Elements))
	for _, e := range snap.Elements {
		fmt.Fprintf(&b, "  [%s] %s", e.Ref, e.Role)
		if e.Name != "" {
			fmt.Fprintf(&b, " %q", e.Name)
		}
		if e.Value != "" {
			fmt.Fprintf(&b, " value=%q", e.Value)
		}
		if e.Href != "" {
			fmt.Fprintf(&b, " href=%q", truncate(e.Href, 80))
		}
		if e.Disabled {
			b.WriteString(" (disabled)")
		}
		b.WriteString("\n")
	}
	if snap.Truncated {
		fmt.Fprintf(&b, "\n(truncated at %d elements — narrow the page or use eval to query the rest)\n", maxSnapshotElements)
	}
	return b.String()
}

// Lookup finds an element by ref in a snapshot.
func (snap *Snapshot) Lookup(ref string) (Element, bool) {
	for _, e := range snap.Elements {
		if e.Ref == ref {
			return e, true
		}
	}
	return Element{}, false
}

// refExpression builds a JS expression that evaluates to the element for ref.
//
// Refs are minted by us and are plain "e<digits>", so they are validated before
// interpolation — a ref is the one piece of model-supplied text that reaches a
// script, and it must not be able to smuggle syntax in.
func refExpression(ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", fmt.Errorf("ref is empty")
	}
	if !isValidRef(ref) {
		return "", fmt.Errorf("invalid ref %q: expected the form e0, e12 (take it from the snapshot output)", ref)
	}
	return fmt.Sprintf("document.querySelector('[%s=\"%s\"]')", refAttr, ref), nil
}

// isValidRef reports whether ref has the shape snapshot mints.
//
// The bound is deliberately tight (e0 … e9999). A ref is the one piece of
// model-supplied text that reaches a script, so the narrower the accepted set,
// the less it can carry.
func isValidRef(ref string) bool {
	if len(ref) < 2 || len(ref) > 5 || ref[0] != 'e' {
		return false
	}
	for _, r := range ref[1:] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
