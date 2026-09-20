Drive a real browser to inspect and debug web pages: open a URL, read its structure, act on elements, watch the console and the network.

This uses a Chromium-family browser already installed on the machine over the Chrome DevTools Protocol. Nothing is downloaded and no Node/Playwright install is required. Use it whenever the answer lives in a running page — a dev server, a rendered UI, a client-side failure — because HTTP fetching only ever shows you the source, never the DOM after JavaScript has run.

## The loop

1. `launch` — start a browser, or attach to one the user already has open.
2. `snapshot` — see the page as a list of refs.
3. `click` / `fill` / `select` / `press` / `hover` — act on a ref.
4. `console` / `network` / `eval` — read what happened.
5. `close` — shut the session down when you are done.

Every action that changes the page returns an updated snapshot automatically, so you rarely need to ask for one explicitly.

## Snapshot first, always

Refs (like `e3`) are handles minted by the snapshot against the live DOM. **Use them.** A hand-written CSS selector is a guess about markup you have not read, and when it is wrong it matches something else and the failure looks like a bug in the page rather than in your selector.

```
[e1] text "Your name"
[e2] button "Go"
[e3] link "Docs" href="/docs"
```

`click`, `fill`, `select` and friends take `ref`; `selector` exists only for the cases a ref cannot cover. A ref goes stale as soon as the page re-renders — if an action reports the element is missing, take a fresh snapshot rather than retrying the same ref.

## Reading a page

`eval` runs JavaScript and returns the value as JSON, so return data instead of logging it:

```
eval: document.querySelectorAll('.row').length
eval: ({ title: document.title, links: [...document.querySelectorAll('a')].map(a => a.href) })
```

`console` returns buffered `console.*` output **and** uncaught page exceptions. An uncaught error never reaches `console.error`, so it has its own path and appears with level `exception` — that is usually the bug you are chasing. Use `clear: true` when you want only what has happened since the last read, which is how you attribute an error to the action that caused it.

`network` returns the request log (method, status, URL). Pending requests show status `…`, blocked or failed ones show why. Requests are recorded from the moment the session starts; `clear: true` reads and resets.

## Navigating

`goto` a URL, or `back` / `forward` / `reload` through history. Each returns the snapshot of the page you land on.

## Acting on elements

- `click` / `dblclick` dispatch real pointer events at the element's centre.
- `fill` types into inputs and textareas, dispatching `input`/`change` so React and Vue see the value.
- `select` picks a `<select>` option by value or visible label.
- `check` / `uncheck` tick or clear a checkbox (radios: use `click`) and verify the state actually changed.
- `hover` moves the pointer over the element — the way to open hover menus.
- `press` sends a key to the focused element.

## Tabs

`tabs` with `op`: `list` (with `->` marking the current tab), `new` (optional `url`, becomes current), `select` / `close` by `index`. The session always acts on the current tab; every action's snapshot reflects it.

## Storage and state

`storage` with `kind`:

- `kind=cookies`, `op=list|set|remove|clear` — `set` takes `name`/`value` and an optional `domain` (defaults to the current page).
- `kind=local` or `kind=session`, `op=list|get|set|remove|clear` — the page's localStorage / sessionStorage, addressed by `name`/`value`.

`state` captures the whole logged-in context to a file — cookies plus the current origin's storage (`op=save`, optional `path`) — and restores it (`op=load`, `path` required). Storage only restores on the origin it was saved from: navigate there first, the response names any skipped origins.

## Network control

`route` with `op`:

- `op=add` + `pattern` **blocks** matching requests (they fail like an ad blocker hit).
- `op=add` + `pattern` + `body` (optional `status`, `content_type`) **answers** them with that body instead.
- `op=list` / `op=clear`.

The pattern is a glob over the URL; `**/api/stats` matches anywhere, `https://ads.example.com/*` only that host. Routes apply to future requests — `reload` after installing one.

## Capturing output

- `screenshot` (PNG, `full_page: true` for the whole scroll, `path` to choose the file).
- `pdf` renders the page to a PDF file (headless sessions only — a headed browser refuses, and the error says so).

## Debugging recipes

- **Client-side failure**: `console` first (with `clear: true` right before the action that breaks), then `eval` around the failing state.
- **Request that mysteriously fails**: `network` to see status and error text; `route` to block an analytics host that is stalling the page; `route` with a `body` to stub an API and test the UI against known data.
- **Element not behaving**: `snapshot` to confirm the ref exists, `eval: getComputedStyle(el).display` to check whether it is actually on the page. Elements that are hidden or zero-sized are deliberately left out of the snapshot, so their absence is itself a finding.
- **Form that will not submit**: `fill` dispatches real `input`/`change` events, so it works with React and Vue. If the value still does not stick, `eval` the framework's own state to find out why.
- **Login-gated page**: save a `state` file once, restore it in later sessions instead of scripting a login.
- **Layout or visual bug**: `screenshot` (add `full_page: true` for the whole scroll), or `pdf` when the artifact itself is the deliverable.

## Boundaries

- **Chromium family only.** Chrome, Chromium, Edge, Brave, Vivaldi. Firefox and WebKit speak different protocols and are not supported — `status` says so explicitly if nothing suitable is installed.
- **The main frame only.** Content inside an `<iframe>` is not traversed, so a widget that renders in one will look empty in a snapshot.
- **A snapshot is capped** at 300 elements, prioritising interactive ones. If it reports truncation, narrow the query with `eval` rather than raising the cap.
- **`launch` needs approval** because it starts a program. The debugging actions that follow do not, so the loop stays fast.
- **Sessions do not survive the process.** Call `close` when finished; a headless browser left running is a leaked process.

## Attaching to the user's browser

When the page needs a logged-in session, do not script a login. Ask the user to start their browser with remote debugging and attach to it:

```
chrome --remote-debugging-port=9222
```

then `launch` with `endpoint: "http://127.0.0.1:9222"`. Attaching closes only the tab it opened when you `close` — it never shuts down the user's browser.
