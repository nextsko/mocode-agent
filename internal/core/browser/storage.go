package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// Cookie mirrors CDP's Network.Cookie for the operations we expose.
type Cookie struct {
	Name     string  `json:"name"`
	Value    string  `json:"value"`
	Domain   string  `json:"domain"`
	Path     string  `json:"path"`
	Expires  float64 `json:"expires"`
	HTTPOnly bool    `json:"httpOnly"`
	Secure   bool    `json:"secure"`
	SameSite string  `json:"sameSite,omitempty"`
}

// String renders a cookie the way the storage action prints it.
func (c Cookie) String() string {
	exp := ""
	if c.Expires > 0 {
		exp = fmt.Sprintf(" expires=%.0f", c.Expires)
	}
	flags := ""
	if c.HTTPOnly {
		flags += " httpOnly"
	}
	if c.Secure {
		flags += " secure"
	}
	return fmt.Sprintf("%s=%s (domain=%s path=%s%s%s)", c.Name, c.Value, c.Domain, c.Path, exp, flags)
}

// Cookies returns every cookie the browser holds.
//
// The cookie commands run on the page session, not the browser connection:
// current Chrome only exposes Network.setCookie & co per-session, and answers
// "'Network.setCookie' wasn't found" at the browser level.
func (s *Session) Cookies(ctx context.Context) ([]Cookie, error) {
	var res struct {
		Cookies []Cookie `json:"cookies"`
	}
	// getAllCookies rather than getCookies: the debugging question is usually
	// "what is the browser carrying", not "what applies to this URL".
	if err := s.conn.Call(ctx, s.sessionID, "Network.getAllCookies", nil, &res); err != nil {
		return nil, fmt.Errorf("reading cookies: %w", err)
	}
	return res.Cookies, nil
}

// CookieSetter is the input side of SetCookie: Name and Value are required,
// everything else is optional and defaults to what the browser applies for the
// current page.
type CookieSetter struct {
	Name     string
	Value    string
	Domain   string
	Path     string
	Expires  float64
	HTTPOnly bool
	Secure   bool
}

// SetCookie writes one cookie.
func (s *Session) SetCookie(ctx context.Context, c CookieSetter) error {
	if strings.TrimSpace(c.Name) == "" {
		return fmt.Errorf("cookie name is required")
	}
	params := map[string]any{
		"name":     c.Name,
		"value":    c.Value,
		"httpOnly": c.HTTPOnly,
		"secure":   c.Secure,
	}
	// CDP requires a url, or a domain (optionally with a path). Defaulting to
	// the current page's URL means "set it for the site I am on" needs no
	// explicit domain.
	switch {
	case c.Domain != "":
		params["domain"] = c.Domain
		if c.Path != "" {
			params["path"] = c.Path
		}
	default:
		var pageURL string
		if err := s.Evaluate(ctx, "location.href", &pageURL); err != nil {
			return fmt.Errorf("deriving the cookie domain: %w", err)
		}
		if pageURL == "" || isBlankURL(pageURL) {
			return fmt.Errorf("cookie needs a domain, or a page that is not blank, to know where it belongs")
		}
		params["url"] = pageURL
		if c.Path != "" {
			params["path"] = c.Path
		}
	}
	if c.Expires > 0 {
		params["expires"] = c.Expires
	}

	var res struct {
		Success bool `json:"success"`
	}
	if err := s.conn.Call(ctx, s.sessionID, "Network.setCookie", params, &res); err != nil {
		return fmt.Errorf("setting cookie %s: %w", c.Name, err)
	}
	if !res.Success {
		return fmt.Errorf("setting cookie %s: the browser rejected it", c.Name)
	}
	return nil
}

// DeleteCookies removes cookies by name, scoped to domain when given.
func (s *Session) DeleteCookies(ctx context.Context, name, domain string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("cookie name is required")
	}
	params := map[string]any{"name": name}
	if domain != "" {
		params["domain"] = domain
	}
	if err := s.conn.Call(ctx, s.sessionID, "Network.deleteCookies", params, nil); err != nil {
		return fmt.Errorf("deleting cookie %s: %w", name, err)
	}
	return nil
}

// ClearCookies wipes every cookie the browser holds.
func (s *Session) ClearCookies(ctx context.Context) error {
	if err := s.conn.Call(ctx, s.sessionID, "Network.clearBrowserCookies", nil, nil); err != nil {
		return fmt.Errorf("clearing cookies: %w", err)
	}
	return nil
}

// StorageKind normalises a storage kind to the window property exposing it.
func StorageKind(kind string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "local", "localstorage":
		return "localStorage", nil
	case "session", "sessionstorage":
		return "sessionStorage", nil
	}
	return "", fmt.Errorf("unknown storage kind %q: expected local or session", kind)
}

// storageName maps a storage kind to the window property exposing it.
func storageName(kind string) (string, error) {
	return StorageKind(kind)
}

// StorageList returns every key/value pair of the page's local or session
// storage.
func (s *Session) StorageList(ctx context.Context, kind string) (map[string]string, error) {
	store, err := storageName(kind)
	if err != nil {
		return nil, err
	}
	var out map[string]string
	expr := fmt.Sprintf(`(() => {
  const out = {};
  for (let i = 0; i < %s.length; i++) { const k = %s.key(i); out[k] = %s.getItem(k); }
  return out;
})()`, store, store, store)
	if err := s.Evaluate(ctx, expr, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// StorageGet reads one key.
func (s *Session) StorageGet(ctx context.Context, kind, key string) (string, error) {
	store, err := storageName(kind)
	if err != nil {
		return "", err
	}
	var out any
	expr := fmt.Sprintf(`%s.getItem(%s)`, store, jsString(key))
	if err := s.Evaluate(ctx, expr, &out); err != nil {
		return "", err
	}
	if out == nil {
		return "", fmt.Errorf("no %s entry for key %q", store, key)
	}
	return fmt.Sprintf("%v", out), nil
}

// StorageSet writes one key.
func (s *Session) StorageSet(ctx context.Context, kind, key, value string) error {
	store, err := storageName(kind)
	if err != nil {
		return err
	}
	expr := fmt.Sprintf(`(() => { %s.setItem(%s, %s); return 'ok'; })()`, store, jsString(key), jsString(value))
	var res string
	if err := s.Evaluate(ctx, expr, &res); err != nil {
		return err
	}
	if res != "ok" {
		return fmt.Errorf("writing %s key %q failed", store, key)
	}
	return nil
}

// StorageRemove deletes one key.
func (s *Session) StorageRemove(ctx context.Context, kind, key string) error {
	store, err := storageName(kind)
	if err != nil {
		return err
	}
	expr := fmt.Sprintf(`(() => { %s.removeItem(%s); return 'ok'; })()`, store, jsString(key))
	var res string
	if err := s.Evaluate(ctx, expr, &res); err != nil {
		return err
	}
	if res != "ok" {
		return fmt.Errorf("removing %s key %q failed", store, key)
	}
	return nil
}

// StorageClear empties the page's local or session storage.
func (s *Session) StorageClear(ctx context.Context, kind string) error {
	store, err := storageName(kind)
	if err != nil {
		return err
	}
	expr := fmt.Sprintf(`(() => { %s.clear(); return 'ok'; })()`, store)
	var res string
	if err := s.Evaluate(ctx, expr, &res); err != nil {
		return err
	}
	if res != "ok" {
		return fmt.Errorf("clearing %s failed", store)
	}
	return nil
}

// OriginState is the storage of one origin.
type OriginState struct {
	Origin         string            `json:"origin"`
	LocalStorage   map[string]string `json:"localStorage,omitempty"`
	SessionStorage map[string]string `json:"sessionStorage,omitempty"`
}

// BrowserState is the serialisable snapshot SaveState writes.
//
// Cookies are browser-wide; storage is only readable for the current page's
// origin (reading another origin's storage would require navigating there), so
// a saved state carries exactly that origin's entries.
type BrowserState struct {
	Cookies []Cookie      `json:"cookies"`
	Origins []OriginState `json:"origins"`
}

// SaveState captures cookies plus the current page's storage.
func (s *Session) SaveState(ctx context.Context) (*BrowserState, error) {
	cookies, err := s.Cookies(ctx)
	if err != nil {
		return nil, err
	}

	var origin string
	if err := s.Evaluate(ctx, "location.origin", &origin); err != nil {
		return nil, err
	}

	st := &BrowserState{Cookies: cookies}
	if !isBlankURL(origin) {
		local, err1 := s.StorageList(ctx, "local")
		session, err2 := s.StorageList(ctx, "session")
		if err1 != nil || err2 != nil {
			return nil, fmt.Errorf("reading page storage: %v / %v", err1, err2)
		}
		st.Origins = []OriginState{{Origin: origin, LocalStorage: local, SessionStorage: session}}
	}
	return st, nil
}

// LoadStateReport says what applying a state did, so an origin that could not
// be restored is visible rather than silently dropped.
type LoadStateReport struct {
	CookiesApplied int
	StorageApplied int
	SkippedOrigins []string
}

// LoadState restores a saved state: cookies always, storage only for origins
// the current page is actually on.
func (s *Session) LoadState(ctx context.Context, st *BrowserState) (*LoadStateReport, error) {
	report := &LoadStateReport{}

	for _, c := range st.Cookies {
		if err := s.SetCookie(ctx, CookieSetter{
			Name:     c.Name,
			Value:    c.Value,
			Domain:   c.Domain,
			Path:     c.Path,
			Expires:  c.Expires,
			HTTPOnly: c.HTTPOnly,
			Secure:   c.Secure,
		}); err != nil {
			return report, err
		}
		report.CookiesApplied++
	}

	var origin string
	if err := s.Evaluate(ctx, "location.origin", &origin); err != nil {
		return report, err
	}

	for _, o := range st.Origins {
		if o.Origin != origin {
			report.SkippedOrigins = append(report.SkippedOrigins, o.Origin)
			continue
		}
		for k, v := range o.LocalStorage {
			if err := s.StorageSet(ctx, "local", k, v); err != nil {
				return report, err
			}
			report.StorageApplied++
		}
		for k, v := range o.SessionStorage {
			if err := s.StorageSet(ctx, "session", k, v); err != nil {
				return report, err
			}
			report.StorageApplied++
		}
	}
	return report, nil
}

// WriteStateFile serialises a state to disk.
func WriteStateFile(path string, st *BrowserState) error {
	raw, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o644)
}

// ReadStateFile reads a state previously written by WriteStateFile.
func ReadStateFile(path string) (*BrowserState, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var st BrowserState
	if err := json.Unmarshal(raw, &st); err != nil {
		return nil, fmt.Errorf("parsing state file %s: %w", path, err)
	}
	return &st, nil
}
