package uidissect

// CollectorScript measures the visible DOM in the current page. It returns a
// Raw-shaped object via Runtime.evaluate (returnByValue). The browser tool
// injects it; keeping the script here means the measurement contract and the
// analysis types live together.
const CollectorScript = `(() => {
  const MAX_NODES = 1200;
  const MAX_DEPTH = 14;
  const SKIP = new Set(['SCRIPT','STYLE','NOSCRIPT','TEMPLATE','HEAD','META','LINK','BR','HR']);
  const INTERACTIVE = new Set(['A','BUTTON','INPUT','SELECT','TEXTAREA','SUMMARY','DETAILS','LABEL']);
  const px = (v) => { const n = parseFloat(v); return isFinite(n) ? n : 0; };

  const visible = (el, st) => {
    const r = el.getBoundingClientRect();
    if (r.width < 1 || r.height < 1) return false;
    if (st.display === 'none' || st.visibility === 'hidden' || parseFloat(st.opacity) === 0) return false;
    if (r.bottom < -2000 || r.top > (innerHeight + 8000)) return false;
    return true;
  };

  const pathOf = (el) => {
    const parts = [];
    let cur = el;
    let guard = 0;
    while (cur && cur.nodeType === 1 && guard < 6) {
      if (cur.id) { parts.unshift('#' + CSS.escape(cur.id)); break; }
      let sel = cur.tagName.toLowerCase();
      const parent = cur.parentElement;
      if (parent) {
        const same = Array.from(parent.children).filter((c) => c.tagName === cur.tagName);
        if (same.length > 1) sel += ':nth-of-type(' + (same.indexOf(cur) + 1) + ')';
      }
      parts.unshift(sel);
      cur = parent;
      guard++;
    }
    return parts.join(' > ');
  };

  const label = (el) => {
    const text = (el.innerText || '').replace(/\s+/g, ' ').trim();
    if (text) return text.slice(0, 140);
    return String(el.getAttribute('aria-label') || el.getAttribute('placeholder') || el.value || el.getAttribute('title') || '').slice(0, 140);
  };

  const out = { url: location.href, title: document.title, viewport: { w: innerWidth, h: innerHeight, dpr: devicePixelRatio || 1 }, nodes: [] };

  const walk = (el, depth) => {
    if (out.nodes.length >= MAX_NODES || depth > MAX_DEPTH) return;
    if (!el || el.nodeType !== 1) return;
    const tag = el.tagName;
    if (SKIP.has(tag)) return;
    const st = getComputedStyle(el);
    if (!visible(el, st)) return;

    const r = el.getBoundingClientRect();
    const interactive = INTERACTIVE.has(tag) || el.getAttribute('role') === 'button' || el.tabIndex >= 0;
    const leaf = el.childElementCount === 0;

    out.nodes.push({
      path: pathOf(el),
      tag: tag.toLowerCase(),
      id: el.id || '',
      classes: Array.from(el.classList).slice(0, 8),
      role: el.getAttribute('role') || '',
      aria_label: el.getAttribute('aria-label') || '',
      text: leaf ? label(el) : '',
      rect: { x: r.left + scrollX, y: r.top + scrollY, w: r.width, h: r.height },
      styles: {
        font_size: px(st.fontSize),
        font_weight: st.fontWeight || '',
        line_height: st.lineHeight === 'normal' ? 0 : px(st.lineHeight),
        color: st.color || '',
        background: st.backgroundColor && st.backgroundColor !== 'rgba(0, 0, 0, 0)' ? st.backgroundColor : '',
        margin_top: px(st.marginTop), margin_bottom: px(st.marginBottom),
        margin_left: px(st.marginLeft), margin_right: px(st.marginRight),
        padding_top: px(st.paddingTop), padding_bottom: px(st.paddingBottom),
        padding_left: px(st.paddingLeft), padding_right: px(st.paddingRight),
        radius: st.borderRadius && st.borderRadius !== '0px' ? st.borderRadius : '',
        shadow: st.boxShadow && st.boxShadow !== 'none',
        display: st.display || '', position: st.position || '', z_index: st.zIndex || '',
        overflow_x: el.scrollWidth > el.clientWidth + 1,
        overflow_y: el.scrollHeight > el.clientHeight + 1
      },
      depth: depth,
      child_count: el.childElementCount,
      interactive: interactive,
      disabled: el.disabled === true || el.getAttribute('aria-disabled') === 'true',
      img_alt: tag === 'IMG' ? (el.getAttribute('alt') || '') : '',
      href: tag === 'A' ? (el.getAttribute('href') || '') : '',
      value: (tag === 'INPUT' || tag === 'TEXTAREA') ? String(el.value || '').slice(0, 80) : ''
    });

    if (el.shadowRoot) {
      for (const c of el.shadowRoot.children) walk(c, depth + 1);
    }
    for (const c of el.children) walk(c, depth + 1);
  };

  if (document.body) walk(document.body, 0);
  return out;
})()`
