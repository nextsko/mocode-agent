// Package browser exposes mocode's native browser runtime as a single tool.
package browser

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"charm.land/fantasy"

	"github.com/nextsko/mocode-agent/internal/core/agent/toolutil"
	corebrowser "github.com/nextsko/mocode-agent/internal/core/browser"
	"github.com/nextsko/mocode-agent/internal/core/permission"
)

//go:embed browser.md
var browserDescription []byte

// BrowserToolName is the name of the browser tool.
const BrowserToolName = "browser"

// Actions.
const (
	actionStatus     = "status"
	actionLaunch     = "launch"
	actionGoto       = "goto"
	actionBack       = "back"
	actionForward    = "forward"
	actionReload     = "reload"
	actionSnapshot   = "snapshot"
	actionClick      = "click"
	actionDblclick   = "dblclick"
	actionFill       = "fill"
	actionPress      = "press"
	actionHover      = "hover"
	actionCheck      = "check"
	actionUncheck    = "uncheck"
	actionSelect     = "select"
	actionEval       = "eval"
	actionScreenshot = "screenshot"
	actionPDF        = "pdf"
	actionConsole    = "console"
	actionTabs       = "tabs"
	actionStorage    = "storage"
	actionState      = "state"
	actionNetwork    = "network"
	actionRoute      = "route"
	actionClose      = "close"
)

// actionList names every action, for error messages that teach.
const actionList = "status, launch, goto, back, forward, reload, snapshot, click, dblclick, fill, press, hover, check, uncheck, select, eval, screenshot, pdf, console, tabs, storage, state, network, route, close"

// PermissionGate is the slice of permission.Service this tool needs. Kept
// narrow so tests can pass a small fake; nil means pre-approved.
type PermissionGate interface {
	Request(ctx context.Context, opts permission.CreatePermissionRequest) (bool, error)
}

// Deps is what the browser tool needs from the process.
type Deps struct {
	// Permissions gates launching a browser process. Debugging actions
	// (snapshot/click/fill/…) are deliberately not gated: prompting on every
	// click would make the tool unusable, and they act only on a session the
	// user already approved.
	Permissions PermissionGate
	// Manager owns the session across calls.
	Manager *corebrowser.Manager
}

// BrowserParams holds the parameters for a browser tool call.
type BrowserParams struct {
	//nolint:lll // descriptions are read by the model, not by humans
	Action string `json:"action" enum:"status,launch,goto,back,forward,reload,snapshot,click,dblclick,fill,press,hover,check,uncheck,select,eval,screenshot,pdf,console,tabs,storage,state,network,route,close" description:"status reports which browsers are installed; launch starts or attaches to a session; the rest act on the running session. Take a snapshot before using a ref."`
	//nolint:lll
	Op string `json:"op,omitempty" description:"Sub-operation for grouped actions. tabs: list,new,select,close. storage: list,get,set,remove,clear. state: save,load. network: list,clear. route: add,list,clear. Defaults to list (or new/select where a parameter implies it)."`
	//nolint:lll
	URL string `json:"url,omitempty" description:"URL to open, for action=launch, goto, or tabs op=new."`
	//nolint:lll
	Ref string `json:"ref,omitempty" description:"Element handle from a snapshot, e.g. e3. Preferred over selector: it is resolved against the live DOM and cannot silently match the wrong element."`
	//nolint:lll
	Selector string `json:"selector,omitempty" description:"CSS selector, used only when ref is unavailable. Prefer ref."`
	//nolint:lll
	Text string `json:"text,omitempty" description:"Text to type, for action=fill. Dispatches input/change events so framework-bound fields actually update."`
	//nolint:lll
	Key string `json:"key,omitempty" description:"Key for action=press: Enter, Tab, Escape, ArrowUp/Down/Left/Right, PageUp/Down, Home, End, Backspace, Delete, or a single character."`
	//nolint:lll
	Expression string `json:"expression,omitempty" description:"JavaScript expression to evaluate in the page, for action=eval. The value is decoded as JSON, so return a value instead of using console.log."`
	//nolint:lll
	Submit bool `json:"submit,omitempty" description:"For action=fill: press Enter afterwards to submit the form."`
	//nolint:lll
	Headless *bool `json:"headless,omitempty" description:"For action=launch: run without a visible window (default true). Set false to watch the browser work."`
	//nolint:lll
	Browser string `json:"browser,omitempty" description:"For action=launch: pin the browser product (chrome, chromium, msedge, brave, vivaldi). Defaults to the best detected one."`
	//nolint:lll
	Endpoint string `json:"endpoint,omitempty" description:"For action=launch: attach to an existing Chrome over CDP (http://127.0.0.1:9222 or a ws:// URL) instead of starting one. Useful for logged-in sessions."`
	//nolint:lll
	Path string `json:"path,omitempty" description:"Output file for action=screenshot (PNG), action=pdf, or state op=save; input file for state op=load. Defaults to a timestamped file in the working directory."`
	//nolint:lll
	FullPage bool `json:"full_page,omitempty" description:"For action=screenshot: capture the whole scrollable page rather than the viewport."`
	//nolint:lll
	Clear bool `json:"clear,omitempty" description:"For action=console or network: return only entries since the last read, then clear the buffer."`
	//nolint:lll
	Index int `json:"index,omitempty" description:"Tab index (0-based, from tabs op=list) for tabs op=select/close."`
	//nolint:lll
	Kind string `json:"kind,omitempty" description:"For action=storage: cookies, local (localStorage), or session (sessionStorage)."`
	//nolint:lll
	Name string `json:"name,omitempty" description:"Key name for storage op=get/set/remove, or cookie name for kind=cookies op=remove."`
	//nolint:lll
	Value string `json:"value,omitempty" description:"Value for storage op=set, or the option to pick for action=select (matched by value or label)."`
	//nolint:lll
	Domain string `json:"domain,omitempty" description:"Cookie domain for storage kind=cookies op=set/remove. Defaults to the current page."`
	//nolint:lll
	Pattern string `json:"pattern,omitempty" description:"For action=route: URL glob (* and ?), e.g. **/api/stats or https://ads.example.com/*. Without a scheme prefix it matches anywhere in the URL."`
	//nolint:lll
	Body string `json:"body,omitempty" description:"For route op=add: answer matching requests with this body instead of blocking them."`
	//nolint:lll
	ContentType string `json:"content_type,omitempty" description:"For route op=add with a body: the Content-Type to answer with (default text/plain)."`
	//nolint:lll
	Status int `json:"status,omitempty" description:"For route op=add with a body: HTTP status to answer with (default 200)."`
}

// BrowserPermissionParams is the approval payload for launching a browser.
type BrowserPermissionParams struct {
	Action   string `json:"action"`
	Browser  string `json:"browser,omitempty"`
	Path     string `json:"path,omitempty"`
	Endpoint string `json:"endpoint,omitempty"`
	Headless bool   `json:"headless"`
}

// BrowserResponseMetadata describes what a browser call did.
type BrowserResponseMetadata struct {
	Action  string `json:"action"`
	URL     string `json:"url,omitempty"`
	Title   string `json:"title,omitempty"`
	Refs    int    `json:"refs,omitempty"`
	Path    string `json:"path,omitempty"`
	Bytes   int    `json:"bytes,omitempty"`
	Console int    `json:"console,omitempty"`
	Count   int    `json:"count,omitempty"`
}

// NewBrowserTool creates the browser tool.
func NewBrowserTool(deps Deps) fantasy.AgentTool {
	if deps.Manager == nil {
		deps.Manager = corebrowser.NewManager()
	}
	return fantasy.NewAgentTool(
		BrowserToolName,
		toolutil.FirstLineDescription(browserDescription),
		func(ctx context.Context, params BrowserParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			return run(ctx, deps, params, call)
		},
	)
}

func run(ctx context.Context, deps Deps, p BrowserParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
	switch action := strings.ToLower(strings.TrimSpace(p.Action)); action {
	case actionStatus:
		return status(ctx, deps)
	case actionLaunch:
		return launch(ctx, deps, p, call)
	case actionClose:
		return closeSession(ctx, deps)
	case "":
		return softError("action is required: one of " + actionList), nil
	default:
		return sessionAction(ctx, deps, action, p)
	}
}

// status reports the detection result plus the current session, so a failure to
// launch can be diagnosed without guessing.
func status(ctx context.Context, deps Deps) (fantasy.ToolResponse, error) {
	var b strings.Builder
	b.WriteString(corebrowser.Report(ctx, corebrowser.DetectOptions{}))
	b.WriteString("\n")
	b.WriteString(deps.Manager.State())
	b.WriteString("\n")

	return fantasy.WithResponseMetadata(
		fantasy.NewTextResponse(b.String()),
		BrowserResponseMetadata{Action: actionStatus},
	), nil
}

// launch starts or attaches to a session. It is the one action that runs a
// program on the user's machine, and so the one action that is gated.
func launch(ctx context.Context, deps Deps, p BrowserParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
	headless := true
	if p.Headless != nil {
		headless = *p.Headless
	}

	endpoint := strings.TrimSpace(p.Endpoint)
	browserKind := corebrowser.Kind(strings.ToLower(strings.TrimSpace(p.Browser)))

	granted, resp, err := requestLaunch(ctx, deps, call, p, endpoint, headless)
	if err != nil {
		return fantasy.ToolResponse{}, err
	}
	if !granted {
		return resp, nil
	}

	session, err := deps.Manager.Ensure(ctx, corebrowser.EnsureOptions{
		Endpoint: endpoint,
		Headless: headless,
		Kind:     browserKind,
		URL:      strings.TrimSpace(p.URL),
	})
	if err != nil {
		return softError(fmt.Sprintf("could not start a browser session: %v", err)), nil
	}

	var b strings.Builder
	b.WriteString("Browser session ready.\n")
	b.WriteString(deps.Manager.State())
	if url := strings.TrimSpace(p.URL); url != "" {
		fmt.Fprintf(&b, "\n\nOpened: %s", url)
	}
	b.WriteString("\n\nNext: call action=snapshot to see the page, then act on the refs it returns.")

	return fantasy.WithResponseMetadata(
		fantasy.NewTextResponse(b.String()),
		BrowserResponseMetadata{Action: actionLaunch, URL: session.Endpoint()},
	), nil
}

// requestLaunch asks for approval to start (or attach to) a browser.
func requestLaunch(
	ctx context.Context,
	deps Deps,
	call fantasy.ToolCall,
	p BrowserParams,
	endpoint string,
	headless bool,
) (granted bool, resp fantasy.ToolResponse, err error) {
	if deps.Permissions == nil {
		return true, fantasy.ToolResponse{}, nil
	}

	// Attaching to a browser the user already started is a different act from
	// starting one, and the approval prompt must say which it is: "attach to the
	// browser at X" is not something to approve blind.
	target := p.Browser
	verb := "Launch a browser"
	if endpoint != "" {
		verb = "Attach to the browser at"
		target = endpoint
	}
	if strings.TrimSpace(target) == "" {
		target = "best detected browser"
	}

	ok, err := deps.Permissions.Request(ctx, permission.CreatePermissionRequest{
		SessionID:   toolutil.GetSessionFromContext(ctx),
		Path:        target,
		ToolCallID:  call.ID,
		ToolName:    BrowserToolName,
		Action:      actionLaunch,
		Description: fmt.Sprintf("%s %s (headless=%v)", verb, target, headless),
		Params: BrowserPermissionParams{
			Action:   actionLaunch,
			Browser:  p.Browser,
			Path:     p.Browser,
			Endpoint: endpoint,
			Headless: headless,
		},
	})
	if err != nil {
		return false, fantasy.ToolResponse{}, err
	}
	if !ok {
		return false, toolutil.NewPermissionDeniedResponse(), nil
	}
	return true, fantasy.ToolResponse{}, nil
}

func closeSession(ctx context.Context, deps Deps) (fantasy.ToolResponse, error) {
	if deps.Manager.Session() == nil {
		return fantasy.WithResponseMetadata(
			fantasy.NewTextResponse("No browser session is running."),
			BrowserResponseMetadata{Action: actionClose},
		), nil
	}
	if err := deps.Manager.Close(ctx); err != nil {
		return softError(fmt.Sprintf("closing the browser session: %v", err)), nil
	}
	return fantasy.WithResponseMetadata(
		fantasy.NewTextResponse("Browser session closed."),
		BrowserResponseMetadata{Action: actionClose},
	), nil
}

// sessionAction dispatches everything that needs a live session.
func sessionAction(ctx context.Context, deps Deps, action string, p BrowserParams) (fantasy.ToolResponse, error) {
	session, err := deps.Manager.Require()
	if err != nil {
		return softError(err.Error()), nil
	}

	switch action {
	case actionGoto:
		url := strings.TrimSpace(p.URL)
		if url == "" {
			return softError("url is required for action=goto"), nil
		}
		if err := session.Navigate(ctx, url); err != nil {
			return softError(fmt.Sprintf("navigating to %s: %v", url, err)), nil
		}
		return withSnapshot(ctx, session, actionGoto)

	case actionBack:
		if err := session.NavigateBack(ctx); err != nil {
			return softError(fmt.Sprintf("going back: %v", err)), nil
		}
		return withSnapshot(ctx, session, actionBack)

	case actionForward:
		if err := session.NavigateForward(ctx); err != nil {
			return softError(fmt.Sprintf("going forward: %v", err)), nil
		}
		return withSnapshot(ctx, session, actionForward)

	case actionReload:
		if err := session.Reload(ctx); err != nil {
			return softError(fmt.Sprintf("reloading: %v", err)), nil
		}
		return withSnapshot(ctx, session, actionReload)

	case actionSnapshot:
		return withSnapshot(ctx, session, actionSnapshot)

	case actionClick, actionDblclick, actionHover, actionCheck, actionUncheck:
		target, errResp := targetOf(p)
		if errResp != nil {
			return *errResp, nil
		}
		var err error
		verb := action
		switch action {
		case actionClick:
			err = session.Click(ctx, target)
		case actionDblclick:
			err = session.DblClick(ctx, target)
			verb = "double-clicking"
		case actionHover:
			err = session.Hover(ctx, target)
			verb = "hovering"
		case actionCheck:
			err = session.Check(ctx, target)
			verb = "checking"
		default:
			err = session.Uncheck(ctx, target)
			verb = "unchecking"
		}
		if err != nil {
			return softError(fmt.Sprintf("%s %s: %v", verb, target, err)), nil
		}
		return withSnapshot(ctx, session, action)

	case actionSelect:
		target, errResp := targetOf(p)
		if errResp != nil {
			return *errResp, nil
		}
		if strings.TrimSpace(p.Value) == "" {
			return softError("value is required for action=select (the option's value or label)"), nil
		}
		if err := session.SelectOption(ctx, target, p.Value); err != nil {
			return softError(fmt.Sprintf("selecting in %s: %v", target, err)), nil
		}
		return withSnapshot(ctx, session, actionSelect)

	case actionFill:
		target, errResp := targetOf(p)
		if errResp != nil {
			return *errResp, nil
		}
		if err := session.Fill(ctx, target, p.Text, p.Submit); err != nil {
			return softError(fmt.Sprintf("filling %s: %v", target, err)), nil
		}
		return withSnapshot(ctx, session, actionFill)

	case actionPress:
		if strings.TrimSpace(p.Key) == "" {
			return softError("key is required for action=press"), nil
		}
		if err := session.Press(ctx, p.Key); err != nil {
			return softError(fmt.Sprintf("pressing %s: %v", p.Key, err)), nil
		}
		return withSnapshot(ctx, session, actionPress)

	case actionEval:
		expr := strings.TrimSpace(p.Expression)
		if expr == "" {
			return softError("expression is required for action=eval"), nil
		}
		var out any
		if err := session.Evaluate(ctx, expr, &out); err != nil {
			return softError(fmt.Sprintf("evaluating: %v", err)), nil
		}
		text := "Result:\n" + renderValue(out)
		return fantasy.WithResponseMetadata(
			fantasy.NewTextResponse(text),
			BrowserResponseMetadata{Action: actionEval},
		), nil

	case actionScreenshot:
		data, err := session.Screenshot(ctx, p.FullPage)
		if err != nil {
			return softError(fmt.Sprintf("taking a screenshot: %v", err)), nil
		}
		path, werr := writeFile(p.Path, "browser-screenshot-", ".png", data)
		if werr != nil {
			return softError(werr.Error()), nil
		}
		return fantasy.WithResponseMetadata(
			fantasy.NewTextResponse(fmt.Sprintf("Screenshot written to %s (%d bytes).", path, len(data))),
			BrowserResponseMetadata{Action: actionScreenshot, Path: path, Bytes: len(data)},
		), nil

	case actionPDF:
		data, err := session.PDF(ctx)
		if err != nil {
			return softError(fmt.Sprintf("exporting PDF: %v", err)), nil
		}
		path, werr := writeFile(p.Path, "browser-page-", ".pdf", data)
		if werr != nil {
			return softError(werr.Error()), nil
		}
		return fantasy.WithResponseMetadata(
			fantasy.NewTextResponse(fmt.Sprintf("PDF written to %s (%d bytes).", path, len(data))),
			BrowserResponseMetadata{Action: actionPDF, Path: path, Bytes: len(data)},
		), nil

	case actionTabs:
		return tabsAction(ctx, session, p)

	case actionStorage:
		return storageAction(ctx, session, p)

	case actionState:
		return stateAction(ctx, session, p)

	case actionNetwork:
		entries := p.networkEntries(session)
		if len(entries) == 0 {
			return fantasy.WithResponseMetadata(
				fantasy.NewTextResponse("No requests recorded yet."),
				BrowserResponseMetadata{Action: actionNetwork},
			), nil
		}
		var b strings.Builder
		fmt.Fprintf(&b, "%d request(s), newest last:\n", len(entries))
		for _, e := range entries {
			status := fmt.Sprintf("%d", e.Status)
			if e.Status == 0 {
				status = "…"
			}
			if e.Error != "" {
				status = e.Error
			}
			fmt.Fprintf(&b, "  [%s] %s %s\n", status, e.Method, e.URL)
		}
		return fantasy.WithResponseMetadata(
			fantasy.NewTextResponse(b.String()),
			BrowserResponseMetadata{Action: actionNetwork, Count: len(entries)},
		), nil

	case actionRoute:
		return routeAction(ctx, session, p)

	case actionConsole:
		entries := p.consoleEntries(session)
		if len(entries) == 0 {
			note := ""
			if p.Clear {
				note = " since the last read"
			}
			return fantasy.WithResponseMetadata(
				fantasy.NewTextResponse("No console messages"+note+"."),
				BrowserResponseMetadata{Action: actionConsole},
			), nil
		}
		var b strings.Builder
		fmt.Fprintf(&b, "%d console message(s):\n", len(entries))
		for _, e := range entries {
			fmt.Fprintf(&b, "  %s\n", e)
		}
		return fantasy.WithResponseMetadata(
			fantasy.NewTextResponse(b.String()),
			BrowserResponseMetadata{Action: actionConsole, Console: len(entries)},
		), nil

	default:
		return softError(fmt.Sprintf("unknown action %q: expected one of %s", action, actionList)), nil
	}
}

// consoleEntries reads the console buffer, draining only when asked.
func (p BrowserParams) consoleEntries(session *corebrowser.Session) []corebrowser.ConsoleEntry {
	if p.Clear {
		return session.DrainConsole()
	}
	entries, _ := session.Console()
	return entries
}

// networkEntries reads the request log, draining only when asked.
func (p BrowserParams) networkEntries(session *corebrowser.Session) []corebrowser.RequestEntry {
	if p.Clear {
		return session.DrainNetwork()
	}
	entries, _ := session.NetworkLog()
	return entries
}

// opOf normalises the sub-operation, defaulting to a sensible one per action.
func opOf(p BrowserParams, def string) string {
	op := strings.ToLower(strings.TrimSpace(p.Op))
	if op == "" {
		return def
	}
	return op
}

// tabsAction handles action=tabs: list, new, select, close.
func tabsAction(ctx context.Context, session *corebrowser.Session, p BrowserParams) (fantasy.ToolResponse, error) {
	switch op := opOf(p, "list"); op {
	case "list":
		tabs, err := session.Tabs(ctx)
		if err != nil {
			return softError(fmt.Sprintf("listing tabs: %v", err)), nil
		}
		return fantasy.WithResponseMetadata(
			fantasy.NewTextResponse(renderTabs(tabs)),
			BrowserResponseMetadata{Action: actionTabs, Count: len(tabs)},
		), nil

	case "new":
		if err := session.TabNew(ctx, strings.TrimSpace(p.URL)); err != nil {
			return softError(fmt.Sprintf("opening a tab: %v", err)), nil
		}
		return withSnapshot(ctx, session, actionTabs)

	case "select":
		if err := session.TabSelect(ctx, p.Index); err != nil {
			return softError(fmt.Sprintf("selecting the tab: %v", err)), nil
		}
		return withSnapshot(ctx, session, actionTabs)

	case "close":
		if err := session.TabClose(ctx, p.Index); err != nil {
			return softError(fmt.Sprintf("closing the tab: %v", err)), nil
		}
		tabs, err := session.Tabs(ctx)
		if err != nil {
			return softError(fmt.Sprintf("the tab closed, but listing the rest failed: %v", err)), nil
		}
		return fantasy.WithResponseMetadata(
			fantasy.NewTextResponse("Tab closed.\n\n"+renderTabs(tabs)),
			BrowserResponseMetadata{Action: actionTabs, Count: len(tabs)},
		), nil

	default:
		return softError("op for action=tabs must be one of list, new, select, close"), nil
	}
}

// renderTabs lists tabs, marking the current one.
func renderTabs(tabs []corebrowser.TabInfo) string {
	if len(tabs) == 0 {
		return "No tabs open."
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d tab(s):\n", len(tabs))
	for _, t := range tabs {
		marker := "  "
		if t.Current {
			marker = "->"
		}
		url := t.URL
		if url == "" {
			url = "about:blank"
		}
		fmt.Fprintf(&b, "%s [%d] %s\n", marker, t.Index, url)
	}
	return b.String()
}

// storageAction handles action=storage over cookies and web storage.
func storageAction(ctx context.Context, session *corebrowser.Session, p BrowserParams) (fantasy.ToolResponse, error) {
	op := opOf(p, "list")

	if strings.EqualFold(strings.TrimSpace(p.Kind), "cookies") {
		switch op {
		case "list":
			cookies, err := session.Cookies(ctx)
			if err != nil {
				return softError(fmt.Sprintf("reading cookies: %v", err)), nil
			}
			if len(cookies) == 0 {
				return fantasy.WithResponseMetadata(
					fantasy.NewTextResponse("No cookies set."),
					BrowserResponseMetadata{Action: actionStorage, Count: 0},
				), nil
			}
			var b strings.Builder
			fmt.Fprintf(&b, "%d cookie(s):\n", len(cookies))
			for _, c := range cookies {
				fmt.Fprintf(&b, "  %s\n", c)
			}
			return fantasy.WithResponseMetadata(
				fantasy.NewTextResponse(b.String()),
				BrowserResponseMetadata{Action: actionStorage, Count: len(cookies)},
			), nil

		case "set":
			if strings.TrimSpace(p.Name) == "" {
				return softError("name is required to set a cookie"), nil
			}
			if err := session.SetCookie(ctx, corebrowser.CookieSetter{
				Name: p.Name, Value: p.Value, Domain: p.Domain,
			}); err != nil {
				return softError(fmt.Sprintf("setting the cookie: %v", err)), nil
			}
			return fantasy.WithResponseMetadata(
				fantasy.NewTextResponse(fmt.Sprintf("Cookie %s set.", p.Name)),
				BrowserResponseMetadata{Action: actionStorage, Count: 1},
			), nil

		case "remove":
			if err := session.DeleteCookies(ctx, p.Name, p.Domain); err != nil {
				return softError(fmt.Sprintf("removing the cookie: %v", err)), nil
			}
			return fantasy.WithResponseMetadata(
				fantasy.NewTextResponse(fmt.Sprintf("Cookie %s removed.", p.Name)),
				BrowserResponseMetadata{Action: actionStorage},
			), nil

		case "clear":
			if err := session.ClearCookies(ctx); err != nil {
				return softError(fmt.Sprintf("clearing cookies: %v", err)), nil
			}
			return fantasy.WithResponseMetadata(
				fantasy.NewTextResponse("All cookies cleared."),
				BrowserResponseMetadata{Action: actionStorage},
			), nil

		default:
			return softError("op for storage kind=cookies must be one of list, set, remove, clear"), nil
		}
	}

	// Web storage (local / session).
	if _, err := corebrowser.StorageKind(p.Kind); err != nil {
		return softError("kind is required for action=storage: cookies, local, or session"), nil
	}

	switch op {
	case "list":
		items, err := session.StorageList(ctx, p.Kind)
		if err != nil {
			return softError(fmt.Sprintf("listing storage: %v", err)), nil
		}
		if len(items) == 0 {
			return fantasy.WithResponseMetadata(
				fantasy.NewTextResponse(fmt.Sprintf("%s is empty.", strings.Title(p.Kind))), //nolint:staticcheck // Title on a fixed string is fine
				BrowserResponseMetadata{Action: actionStorage, Count: 0},
			), nil
		}
		keys := make([]string, 0, len(items))
		for k := range items {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var b strings.Builder
		fmt.Fprintf(&b, "%d entr%s:\n", len(items), map[bool]string{true: "y", false: "ies"}[len(items) == 1])
		for _, k := range keys {
			fmt.Fprintf(&b, "  %s = %s\n", k, items[k])
		}
		return fantasy.WithResponseMetadata(
			fantasy.NewTextResponse(b.String()),
			BrowserResponseMetadata{Action: actionStorage, Count: len(items)},
		), nil

	case "get":
		if strings.TrimSpace(p.Name) == "" {
			return softError("name (the key) is required for storage op=get"), nil
		}
		value, err := session.StorageGet(ctx, p.Kind, p.Name)
		if err != nil {
			return softError(err.Error()), nil
		}
		return fantasy.WithResponseMetadata(
			fantasy.NewTextResponse(fmt.Sprintf("%s = %s", p.Name, value)),
			BrowserResponseMetadata{Action: actionStorage, Count: 1},
		), nil

	case "set":
		if strings.TrimSpace(p.Name) == "" {
			return softError("name (the key) is required for storage op=set"), nil
		}
		if err := session.StorageSet(ctx, p.Kind, p.Name, p.Value); err != nil {
			return softError(fmt.Sprintf("writing the entry: %v", err)), nil
		}
		return fantasy.WithResponseMetadata(
			fantasy.NewTextResponse(fmt.Sprintf("%s set to %q.", p.Name, p.Value)),
			BrowserResponseMetadata{Action: actionStorage, Count: 1},
		), nil

	case "remove":
		if strings.TrimSpace(p.Name) == "" {
			return softError("name (the key) is required for storage op=remove"), nil
		}
		if err := session.StorageRemove(ctx, p.Kind, p.Name); err != nil {
			return softError(fmt.Sprintf("removing the entry: %v", err)), nil
		}
		return fantasy.WithResponseMetadata(
			fantasy.NewTextResponse(fmt.Sprintf("%s removed.", p.Name)),
			BrowserResponseMetadata{Action: actionStorage},
		), nil

	case "clear":
		if err := session.StorageClear(ctx, p.Kind); err != nil {
			return softError(fmt.Sprintf("clearing storage: %v", err)), nil
		}
		return fantasy.WithResponseMetadata(
			fantasy.NewTextResponse("Storage cleared."),
			BrowserResponseMetadata{Action: actionStorage},
		), nil

	default:
		return softError("op for action=storage must be one of list, get, set, remove, clear"), nil
	}
}

// stateAction handles action=state: save the session's cookies and storage to a
// file, or restore them.
func stateAction(ctx context.Context, session *corebrowser.Session, p BrowserParams) (fantasy.ToolResponse, error) {
	switch op := opOf(p, "save"); op {
	case "save":
		st, err := session.SaveState(ctx)
		if err != nil {
			return softError(fmt.Sprintf("capturing the state: %v", err)), nil
		}
		path, err := statePath(p.Path, "browser-state-", ".json")
		if err != nil {
			return softError(err.Error()), nil
		}
		if err := corebrowser.WriteStateFile(path, st); err != nil {
			return softError(fmt.Sprintf("writing the state file: %v", err)), nil
		}
		return fantasy.WithResponseMetadata(
			fantasy.NewTextResponse(fmt.Sprintf("State written to %s (%d cookie(s), %d origin(s)).", path, len(st.Cookies), len(st.Origins))),
			BrowserResponseMetadata{Action: actionState, Path: path},
		), nil

	case "load":
		path, err := statePath(p.Path, "", ".json")
		if err != nil {
			return softError(err.Error()), nil
		}
		st, err := corebrowser.ReadStateFile(path)
		if err != nil {
			return softError(fmt.Sprintf("reading the state file: %v", err)), nil
		}
		report, err := session.LoadState(ctx, st)
		if err != nil {
			return softError(fmt.Sprintf("restoring the state: %v", err)), nil
		}
		var b strings.Builder
		fmt.Fprintf(&b, "Restored %d cookie(s)", report.CookiesApplied)
		if report.StorageApplied > 0 {
			fmt.Fprintf(&b, " and %d storage entr%s", report.StorageApplied, map[bool]string{true: "y", false: "ies"}[report.StorageApplied == 1])
		}
		b.WriteString(".")
		for _, o := range report.SkippedOrigins {
			fmt.Fprintf(&b, "\nSkipped storage for %s (the page is on a different origin; navigate there and load again).", o)
		}
		return fantasy.WithResponseMetadata(
			fantasy.NewTextResponse(b.String()),
			BrowserResponseMetadata{Action: actionState, Path: path, Count: report.CookiesApplied},
		), nil

	default:
		return softError("op for action=state must be save or load"), nil
	}
}

// routeAction handles action=route: block or mock requests by URL pattern.
func routeAction(ctx context.Context, session *corebrowser.Session, p BrowserParams) (fantasy.ToolResponse, error) {
	switch op := opOf(p, "list"); op {
	case "list":
		routes := session.Routes()
		if len(routes) == 0 {
			return fantasy.WithResponseMetadata(
				fantasy.NewTextResponse("No routes installed. op=add with a pattern blocks matching requests; adding a body answers them with it."),
				BrowserResponseMetadata{Action: actionRoute, Count: 0},
			), nil
		}
		var b strings.Builder
		fmt.Fprintf(&b, "%d route(s):\n", len(routes))
		for _, r := range routes {
			fmt.Fprintf(&b, "  %s\n", r)
		}
		return fantasy.WithResponseMetadata(
			fantasy.NewTextResponse(b.String()),
			BrowserResponseMetadata{Action: actionRoute, Count: len(routes)},
		), nil

	case "add":
		pattern := strings.TrimSpace(p.Pattern)
		if pattern == "" {
			return softError("pattern is required for route op=add, e.g. **/api/stats"), nil
		}
		var mock *corebrowser.MockResponse
		if p.Body != "" {
			status := p.Status
			if status == 0 {
				status = 200
			}
			mock = &corebrowser.MockResponse{Status: status, Body: p.Body, ContentType: p.ContentType}
		}
		if err := session.Route(ctx, pattern, mock); err != nil {
			return softError(fmt.Sprintf("installing the route: %v", err)), nil
		}
		what := "blocked"
		if mock != nil {
			what = "answered with the given body"
		}
		return fantasy.WithResponseMetadata(
			fantasy.NewTextResponse(fmt.Sprintf("Requests matching %s are now %s. Reload the page to see the effect.", pattern, what)),
			BrowserResponseMetadata{Action: actionRoute, Count: 1},
		), nil

	case "clear":
		routes := session.Routes()
		// Clearing has no pattern parameter on purpose: routes are few, and a
		// stale one is removed by re-adding or by clear + re-add.
		for _, line := range extractPatterns(routes) {
			if err := session.Unroute(ctx, line); err != nil {
				return softError(fmt.Sprintf("removing route %s: %v", line, err)), nil
			}
		}
		return fantasy.WithResponseMetadata(
			fantasy.NewTextResponse("All routes removed."),
			BrowserResponseMetadata{Action: actionRoute, Count: 0},
		), nil

	default:
		return softError("op for action=route must be one of add, list, clear"), nil
	}
}

// extractPatterns pulls the raw patterns back out of rendered route lines
// (everything before " -> ").
func extractPatterns(lines []string) []string {
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		if i := strings.Index(l, " -> "); i >= 0 {
			out = append(out, l[:i])
		}
	}
	return out
}

// withSnapshot appends a fresh snapshot to an action's result.
//
// Every action that changes the page re-snapshots, because the state *after* the
// action is what the next decision depends on. Returning it here saves the
// model a round trip it would otherwise always have to make.
func withSnapshot(ctx context.Context, session *corebrowser.Session, action string) (fantasy.ToolResponse, error) {
	snap, err := session.Snapshot(ctx)
	if err != nil {
		return softError(fmt.Sprintf("the action succeeded but taking a snapshot failed: %v", err)), nil
	}
	return fantasy.WithResponseMetadata(
		fantasy.NewTextResponse(snap.Render()),
		BrowserResponseMetadata{Action: action, URL: snap.URL, Title: snap.Title, Refs: len(snap.Elements)},
	), nil
}

// targetOf extracts the element reference, preferring ref over selector.
func targetOf(p BrowserParams) (string, *fantasy.ToolResponse) {
	if ref := strings.TrimSpace(p.Ref); ref != "" {
		return ref, nil
	}
	if sel := strings.TrimSpace(p.Selector); sel != "" {
		return sel, nil
	}
	resp := softError("ref is required (take a snapshot first); selector is accepted as a fallback")
	return "", &resp
}

// writeFile saves tool output (a screenshot, a PDF), defaulting to a
// timestamped file in the working directory.
func writeFile(path, prefix, ext string, data []byte) (string, error) {
	if strings.TrimSpace(path) == "" {
		path = fmt.Sprintf("%s%s%s", prefix, time.Now().Format("20060102-150405"), ext)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolving output path: %w", err)
	}
	if err := os.WriteFile(abs, data, 0o644); err != nil {
		return "", fmt.Errorf("writing %s: %w", abs, err)
	}
	return abs, nil
}

// statePath resolves the path for state save/load. A save gets a default name;
// a load must be told where to read from, because guessing a file the model
// never named would read the wrong session's state.
func statePath(path, prefix, ext string) (string, error) {
	if strings.TrimSpace(path) == "" {
		if prefix == "" {
			return "", fmt.Errorf("path is required for state op=load")
		}
		path = fmt.Sprintf("%s%s%s", prefix, time.Now().Format("20060102-150405"), ext)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolving state path: %w", err)
	}
	return abs, nil
}

// renderValue formats a JS value for the model.
func renderValue(v any) string {
	if v == nil {
		return "undefined"
	}
	switch val := v.(type) {
	case string:
		return val
	case bool:
		return fmt.Sprintf("%v", val)
	case float64:
		// JSON numbers decode to float64; print whole ones without a decimal.
		if val == float64(int64(val)) {
			return fmt.Sprintf("%d", int64(val))
		}
		return fmt.Sprintf("%v", val)
	default:
		raw, err := json.MarshalIndent(val, "", "  ")
		if err != nil {
			return fmt.Sprintf("%v", val)
		}
		return string(raw)
	}
}

func softError(msg string) fantasy.ToolResponse {
	return fantasy.NewTextErrorResponse(msg)
}
