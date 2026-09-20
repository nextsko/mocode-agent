// Package agenttools hosts agent-adjacent tools: reasoning scratchpads, todo
// bookkeeping, session exports, skill learning and session memory.
package agenttools

import (
	"context"
	_ "embed"
	"fmt"
	"strings"
	"time"

	"charm.land/fantasy"

	"github.com/nextsko/mocode-agent/internal/core/agent/toolutil"
	"github.com/nextsko/mocode-agent/internal/core/memory"
)

//go:embed memory.md
var memoryDescription []byte

// MemoryToolName is the name of the session memory tool.
const MemoryToolName = "memory"

// Memory actions.
const (
	memActionWrite   = "write"
	memActionList    = "list"
	memActionSearch  = "search"
	memActionExtract = "extract"
	memActionForget  = "forget"
)

// MemoryDeps carries the memory store.
type MemoryDeps struct {
	// Store is the file-backed memory tree.
	Store *memory.Store
	// Indexer maintains the global index. Nil uses the file implementation.
	Indexer memory.Indexer
	// Now overrides the clock in tests.
	Now func() time.Time
}

// MemoryParams holds the parameters for a memory tool call.
//
// One tool with an action rather than the six separate `memory_*` tools the
// config once advertised: those names were never implemented, and the split
// forced a caller to know which of six nearly-identical names matched its
// intent. The whole surface fits in one schema.
type MemoryParams struct {
	Action string `json:"action" enum:"write,list,search,extract,forget" description:"write stores an entry; list and search browse them; extract promotes a session memory into the durable global tier; forget archives one."`
	//nolint:lll // descriptions are read by the model, not by humans
	Title string `json:"title,omitempty" description:"For action=write: a one-line title. Make it specific — it is what the index shows and what a future session searches. Required for write unless path is given."`
	//nolint:lll
	Body string `json:"body,omitempty" description:"For action=write: the markdown content. Include the concrete detail that would otherwise be lost: the error text, the command that worked, why the obvious approach failed."`
	//nolint:lll
	Scope string `json:"scope,omitempty" enum:"session,global" description:"For action=write: session (default) keeps it with this session; global makes it durable and cross-project. Prefer extract over writing straight to global — extraction preserves where the knowledge came from."`
	//nolint:lll
	Kind string `json:"kind,omitempty" enum:"note,lesson,decision,fact" description:"For action=write or extract: what sort of memory this is. Defaults to note."`
	//nolint:lll
	Tags []string `json:"tags,omitempty" description:"Free-form labels to make the entry findable later, e.g. [\"go\",\"testing\"]."`
	//nolint:lll
	Path string `json:"path,omitempty" description:"Root-relative path of an entry, e.g. sessions/1a2b3c4d/20260914T120000-1a2b3c4d-flaky-test.md. Required for extract and forget."`
	//nolint:lll
	Query string `json:"query,omitempty" description:"For action=search: the text to look for across titles, tags and bodies."`
	//nolint:lll
	Session string `json:"session,omitempty" description:"For action=list, search or extract: restrict to one session (short id or full id). Defaults to all sessions for list/search, and to the current session for extract."`
	//nolint:lll
	Limit int `json:"limit,omitempty" description:"Maximum entries to return. Defaults to 20 for list and search."`
}

// MemoryResponseMetadata describes what a memory call did.
type MemoryResponseMetadata struct {
	Action       string `json:"action"`
	Scope        string `json:"scope,omitempty"`
	Path         string `json:"path,omitempty"`
	Total        int    `json:"total,omitempty"`
	Results      int    `json:"results,omitempty"`
	Created      bool   `json:"created,omitempty"`
	PromotedTo   string `json:"promoted_to,omitempty"`
	ArchivedTo   string `json:"archived_to,omitempty"`
	IndexBytes   int    `json:"index_bytes,omitempty"`
	IndexRebuilt bool   `json:"index_rebuilt,omitempty"`
}

// NewMemoryTool creates the session memory tool.
func NewMemoryTool(deps MemoryDeps) fantasy.AgentTool {
	if deps.Indexer == nil && deps.Store != nil {
		deps.Indexer = memory.NewFileIndexer(deps.Store)
	}
	if deps.Now == nil {
		deps.Now = func() time.Time { return time.Now().UTC() }
	}

	return fantasy.NewAgentTool(
		MemoryToolName,
		toolutil.FirstLineDescription(memoryDescription),
		func(ctx context.Context, params MemoryParams, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
			return runMemory(ctx, deps, params)
		},
	)
}

func runMemory(ctx context.Context, deps MemoryDeps, p MemoryParams) (fantasy.ToolResponse, error) {
	if deps.Store == nil {
		return memError("memory is not configured for this session"), nil
	}

	switch action := strings.ToLower(strings.TrimSpace(p.Action)); action {
	case memActionWrite:
		return memoryWrite(ctx, deps, p)
	case memActionList:
		return memoryList(ctx, deps, p)
	case memActionSearch:
		return memorySearch(ctx, deps, p)
	case memActionExtract:
		return memoryExtract(ctx, deps, p)
	case memActionForget:
		return memoryForget(ctx, deps, p)
	case "":
		return memError("action is required: one of write, list, search, extract, forget"), nil
	default:
		return memError(fmt.Sprintf("unknown action %q: expected one of write, list, search, extract, forget", action)), nil
	}
}

// memoryWrite stores one entry and refreshes the index.
func memoryWrite(ctx context.Context, deps MemoryDeps, p MemoryParams) (fantasy.ToolResponse, error) {
	scope := memory.Scope(strings.ToLower(strings.TrimSpace(p.Scope)))
	if scope == "" {
		// Session is the default because it is the safe direction: promoting
		// later is easy, un-promoting is not.
		scope = memory.ScopeSession
	}
	if !scope.Valid() {
		return memError(fmt.Sprintf("invalid scope %q: expected one of %s", p.Scope, scopeNames())), nil
	}

	kind := memory.Kind(strings.ToLower(strings.TrimSpace(p.Kind)))
	if kind == "" {
		kind = memory.KindNote
	}
	if !kind.Valid() {
		return memError(fmt.Sprintf("invalid kind %q: expected one of note, lesson, decision, fact", p.Kind)), nil
	}

	sessionID := memorySessionID(ctx, p.Session)

	e := &memory.Entry{
		Title:   strings.TrimSpace(p.Title),
		Body:    strings.TrimSpace(p.Body),
		Scope:   scope,
		Kind:    kind,
		Session: sessionID,
		Tags:    p.Tags,
		Date:    deps.Now().UTC(),
	}
	if err := e.Validate(); err != nil {
		return memError(err.Error()), nil
	}

	res, err := deps.Store.Write(ctx, e)
	if err != nil {
		return memError(fmt.Sprintf("writing memory: %v", err)), nil
	}

	idx, idxErr := memory.UpdateIndex(ctx, deps.Store, deps.Indexer)

	var b strings.Builder
	fmt.Fprintf(&b, "Stored %s memory %q.\nPath: %s\n", scope, e.Title, res.RelPath)
	if res.Overwrote {
		b.WriteString("\n(An entry with the same timestamp and title already existed and was replaced.)\n")
	}
	if idxErr != nil {
		// The entry is on disk; only the index lagged. Say so rather than
		// reporting a failure that would make the agent write it again.
		fmt.Fprintf(&b, "\nThe entry was written, but refreshing the index failed: %v\n", idxErr)
	}
	if scope == memory.ScopeSession {
		b.WriteString("\nThis is session memory. Promote it with action=extract once it is worth keeping beyond this session.")
	}

	meta := MemoryResponseMetadata{
		Action: memActionWrite, Scope: string(scope), Path: res.RelPath, Created: !res.Overwrote,
	}
	if idx != nil {
		meta.Total = idx.Total()
	}
	return fantasy.WithResponseMetadata(fantasy.NewTextResponse(b.String()), meta), nil
}

// memoryList lists entries, newest first.
func memoryList(ctx context.Context, deps MemoryDeps, p MemoryParams) (fantasy.ToolResponse, error) {
	scope, errResp := scopeFromParam(p, false)
	if errResp != nil {
		return *errResp, nil
	}

	limit := p.Limit
	if limit <= 0 {
		limit = 20
	}

	entries, err := deps.Store.List(ctx, memory.ListOptions{
		Scope:   scope,
		Session: strings.TrimSpace(p.Session),
		Limit:   limit,
	})
	if err != nil {
		return memError(fmt.Sprintf("listing memory: %v", err)), nil
	}

	idx, idxErr := deps.Indexer.Rebuild(ctx)
	total := 0
	if idxErr == nil {
		total = idx.Total()
	} else {
		total = len(entries)
	}

	var b strings.Builder
	if len(entries) == 0 {
		fmt.Fprintf(&b, "No memory entries%s.\n", scopeSuffix(scope))
		if idxErr == nil && idx.Total() > 0 {
			fmt.Fprintf(&b, "\n%d entries exist in the other tier(s); drop `scope` to see them.\n", idx.Total())
		}
		b.WriteString("\nStore memory with action=write.")
	} else {
		fmt.Fprintf(&b, "%d of %d entries%s, newest first:\n\n", len(entries), total, scopeSuffix(scope))
		for _, e := range entries {
			fmt.Fprintf(&b, "%s\n", renderEntryLine(e))
		}
	}

	meta := MemoryResponseMetadata{Action: memActionList, Total: total, Results: len(entries)}
	if scope != "" {
		meta.Scope = string(scope)
	}
	return fantasy.WithResponseMetadata(fantasy.NewTextResponse(b.String()), meta), nil
}

// memorySearch finds entries by text.
func memorySearch(ctx context.Context, deps MemoryDeps, p MemoryParams) (fantasy.ToolResponse, error) {
	query := strings.TrimSpace(p.Query)
	if query == "" {
		return memError("query is required for action=search"), nil
	}

	scope, errResp := scopeFromParam(p, false)
	if errResp != nil {
		return *errResp, nil
	}

	limit := p.Limit
	if limit <= 0 {
		limit = 20
	}

	matches, err := deps.Store.Search(ctx, query, memory.ListOptions{
		Scope:   scope,
		Session: strings.TrimSpace(p.Session),
		Limit:   limit,
	})
	if err != nil {
		return memError(fmt.Sprintf("searching memory: %v", err)), nil
	}

	var b strings.Builder
	if len(matches) == 0 {
		fmt.Fprintf(&b, "No memory matched %q.\n", query)
		b.WriteString("\nIf this is the first time you have needed it, consider recording it with action=write.")
	} else {
		fmt.Fprintf(&b, "%d match(es) for %q:\n\n", len(matches), query)
		for _, m := range matches {
			fmt.Fprintf(&b, "%s  (matched %s)\n", renderEntryLine(m.Entry), m.Where)
			if m.Snippet != "" {
				fmt.Fprintf(&b, "      %s\n", m.Snippet)
			}
		}
	}

	meta := MemoryResponseMetadata{Action: memActionSearch, Results: len(matches)}
	return fantasy.WithResponseMetadata(fantasy.NewTextResponse(b.String()), meta), nil
}

// memoryExtract promotes a session entry into the global tier.
func memoryExtract(ctx context.Context, deps MemoryDeps, p MemoryParams) (fantasy.ToolResponse, error) {
	kind := memory.Kind(strings.ToLower(strings.TrimSpace(p.Kind)))
	if kind != "" && !kind.Valid() {
		return memError(fmt.Sprintf("invalid kind %q: expected one of note, lesson, decision, fact", p.Kind)), nil
	}

	session := memorySessionID(ctx, p.Session)
	target := strings.TrimSpace(p.Path)
	fromOtherSession := ""

	// Without an explicit path, promote the most recent session entry. The
	// common case is "that last thing was worth keeping".
	//
	// When the current session has none, fall back to the most recent entry from
	// *any* session: a session that can see another session's memory must be able
	// to promote it, and requiring the exact path would make the cross-session
	// flow unusable. The response names the source session so the choice is never
	// silent.
	if target == "" {
		entries, err := deps.Store.List(ctx, memory.ListOptions{Scope: memory.ScopeSession, Session: session, Limit: 1})
		if err != nil {
			return memError(fmt.Sprintf("finding the latest session memory: %v", err)), nil
		}
		if len(entries) == 0 {
			anyEntries, aerr := deps.Store.List(ctx, memory.ListOptions{Scope: memory.ScopeSession, Limit: 1})
			if aerr != nil {
				return memError(fmt.Sprintf("finding session memory: %v", aerr)), nil
			}
			if len(anyEntries) == 0 {
				return memError("no session memory to extract. Write one first with action=write, " +
					"or pass path to promote a specific entry (action=list shows the available paths)."), nil
			}
			entries = anyEntries
			fromOtherSession = memory.SessionShort(entries[0].Session)
		}
		target = entries[0].RelPath
	}

	res, err := memory.Extract(ctx, deps.Store, target, memory.ExtractOptions{
		Title: strings.TrimSpace(p.Title),
		Body:  strings.TrimSpace(p.Body),
		Kind:  kind,
		Tags:  p.Tags,
	})
	if err != nil {
		return memError(fmt.Sprintf("extracting memory: %v", err)), nil
	}

	if _, err := memory.UpdateIndex(ctx, deps.Store, deps.Indexer); err != nil {
		return memError(fmt.Sprintf("the memory was promoted but refreshing the index failed: %v", err)), nil
	}

	var b strings.Builder
	if res.Created {
		fmt.Fprintf(&b, "Promoted %q into global memory.\n", res.Entry.Title)
	} else {
		fmt.Fprintf(&b, "%q is already in global memory; nothing was duplicated.\n", res.Entry.Title)
	}
	fmt.Fprintf(&b, "From: %s\nTo:   %s\n", res.SourceRelPath, res.RelPath)
	if fromOtherSession != "" {
		fmt.Fprintf(&b, "\n(This session had no memory of its own, so the most recent entry was taken from session %s.)\n",
			fromOtherSession)
	}
	b.WriteString("\nThe session entry is untouched — extraction copies, so the session keeps its own record.")

	meta := MemoryResponseMetadata{
		Action: memActionExtract, Scope: string(memory.ScopeGlobal),
		Path: res.RelPath, PromotedTo: res.RelPath, Created: res.Created,
	}
	return fantasy.WithResponseMetadata(fantasy.NewTextResponse(b.String()), meta), nil
}

// memoryForget archives an entry.
func memoryForget(ctx context.Context, deps MemoryDeps, p MemoryParams) (fantasy.ToolResponse, error) {
	target := strings.TrimSpace(p.Path)
	if target == "" {
		return memError("path is required for action=forget (list or search first to find it)"), nil
	}

	archived, err := deps.Store.Forget(ctx, target)
	if err != nil {
		return memError(fmt.Sprintf("forgetting memory: %v", err)), nil
	}
	if _, err := memory.UpdateIndex(ctx, deps.Store, deps.Indexer); err != nil {
		return memError(fmt.Sprintf("the entry was archived but refreshing the index failed: %v", err)), nil
	}

	text := fmt.Sprintf("Archived %s.\nMoved to: %s\n\nNothing is deleted — move the file back if this was a mistake.",
		target, archived)

	meta := MemoryResponseMetadata{Action: memActionForget, Path: target, ArchivedTo: archived}
	return fantasy.WithResponseMetadata(fantasy.NewTextResponse(text), meta), nil
}

// ─── helpers ─────────────────────────────────────────────────────────────────

// memorySessionID resolves the session an entry belongs to, preferring an
// explicit parameter and falling back to the session the tool is running in.
func memorySessionID(ctx context.Context, explicit string) string {
	if s := strings.TrimSpace(explicit); s != "" {
		return s
	}
	return toolutil.GetSessionFromContext(ctx)
}

// scopeFromParam reads the scope parameter, reporting a bad value.
func scopeFromParam(p MemoryParams, allowEmpty bool) (memory.Scope, *fantasy.ToolResponse) {
	raw := strings.ToLower(strings.TrimSpace(p.Scope))
	if raw == "" {
		if allowEmpty {
			return "", nil
		}
		return "", nil
	}
	scope := memory.Scope(raw)
	if !scope.Valid() {
		resp := memError(fmt.Sprintf("invalid scope %q: expected one of %s", p.Scope, scopeNames()))
		return "", &resp
	}
	return scope, nil
}

func scopeNames() string {
	scopes := memory.ValidScopes()
	names := make([]string, 0, len(scopes))
	for _, s := range scopes {
		names = append(names, string(s))
	}
	return strings.Join(names, ", ")
}

func scopeSuffix(scope memory.Scope) string {
	if scope == "" {
		return " in either tier"
	}
	return fmt.Sprintf(" in the %s tier", scope)
}

// renderEntryLine formats one entry for a listing.
func renderEntryLine(e *memory.Entry) string {
	date := "-"
	if !e.Date.IsZero() {
		date = e.Date.UTC().Format("2006-01-02 15:04")
	}
	line := fmt.Sprintf("  [%s] %s (%s", date, e.Title, e.Kind)
	if len(e.Tags) > 0 {
		line += ": " + strings.Join(e.Tags, ",")
	}
	line += ")"
	if e.PromotedFrom != "" {
		line += " ↑promoted"
	}
	line += "\n      " + e.RelPath
	return line
}

func memError(msg string) fantasy.ToolResponse {
	return fantasy.NewTextErrorResponse(msg)
}
