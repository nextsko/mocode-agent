package evolution

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Producer turns session-log observations into evolution patches. It is the
// "write side" of the patch loop that was missing: it reads the sessionlog data
// (now populated by the observability layer) and emits rule patches for
// recurring errors, closing the loop that injectEvolutionContext consumes.
//
// The first producer is rule-based (no LLM): it scans bug logs for repeated
// tool errors and promotes them into a KindRule patch. This is deliberately
// conservative — it only patches what recurs — and mirrors how ErrorLearner
// promotes after a threshold, but produces a persistent prompt patch rather
// than an in-memory correction.
type Producer struct {
	store        *PatchStore
	sessionsDir  string // .mocode/sessions/
	minRepeats   int    // errors of the same tool+signature before a patch is produced
	signatureLen int    // chars of error content used as the dedup signature
}

// ProducerOption configures a Producer.
type ProducerOption func(*Producer)

// WithMinRepeats sets how many occurrences of the same error signature are
// required before a rule patch is produced. Default 3.
func WithMinRepeats(n int) ProducerOption {
	return func(p *Producer) {
		if n > 0 {
			p.minRepeats = n
		}
	}
}

// NewProducer builds a Producer reading session logs from sessionsDir and
// writing patches via store.
func NewProducer(store *PatchStore, sessionsDir string, opts ...ProducerOption) (*Producer, error) {
	if store == nil {
		return nil, fmt.Errorf("evolution producer: store is required")
	}
	p := &Producer{store: store, sessionsDir: sessionsDir, minRepeats: 3, signatureLen: 120}
	for _, opt := range opts {
		opt(p)
	}
	return p, nil
}

// errorSignature is one observed recurring error cluster.
type errorSignature struct {
	ToolName string
	Fragment string // truncated error content used as the signature
	Count    int
	Sessions []string
	Examples []string
}

// Produce scans all session bug logs for recurring tool errors and creates a
// single KindRule patch per cluster that meets the minRepeats threshold. It
// returns the IDs of patches created. Re-running on the same data is safe:
// existing patches with the same title are not duplicated.
func (p *Producer) Produce() ([]string, error) {
	clusters, err := p.scanErrors()
	if err != nil {
		return nil, err
	}
	// Merge in errors from the errcoll JSONL logs (.mocode/errors/*.jsonl).
	// This unifies the two parallel error sources so patches reflect both
	// sessionlog-recorded and errcoll-recorded failures.
	errcollClusters, err := p.scanErrcollErrors()
	if err != nil {
		return nil, err
	}
	clusters = mergeClusters(clusters, errcollClusters)

	existing, err := p.existingTitles()
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool, len(existing))
	for _, t := range existing {
		seen[t] = true
	}

	var created []string
	for _, c := range clusters {
		if c.Count < p.minRepeats {
			continue
		}
		title := patchTitle(c.ToolName, c.Fragment)
		if seen[title] {
			continue
		}
		id, err := p.emitRulePatch(c, title)
		if err != nil {
			return created, err
		}
		created = append(created, id)
		seen[title] = true
	}
	return created, nil
}

// scanErrors reads every session's bug.md, parses the JSON-in-markdown entries,
// and clusters tool errors by (tool name, error fragment).
func (p *Producer) scanErrors() ([]errorSignature, error) {
	type key struct {
		tool, frag string
	}
	clusterMap := make(map[key]*errorSignature)

	entries, err := os.ReadDir(p.sessionsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read sessions dir: %w", err)
	}
	for _, sess := range entries {
		if !sess.IsDir() {
			continue
		}
		bugPath := filepath.Join(p.sessionsDir, sess.Name(), "bug.md")
		observations := parseBugEntries(bugPath)
		for _, obs := range observations {
			k := key{tool: obs.toolName, frag: obs.fragment}
			c, ok := clusterMap[k]
			if !ok {
				c = &errorSignature{ToolName: obs.toolName, Fragment: obs.fragment}
				clusterMap[k] = c
			}
			c.Count++
			c.Sessions = appendUnique(c.Sessions, sess.Name())
			if len(c.Examples) < 3 {
				c.Examples = append(c.Examples, obs.data)
			}
		}
	}

	out := make([]errorSignature, 0, len(clusterMap))
	for _, c := range clusterMap {
		out = append(out, *c)
	}
	return out, nil
}

// bugObservation is a single parsed bug entry.
type bugObservation struct {
	toolName string
	fragment string // truncated error content used as the dedup signature
	data     string // the full "data" field of the entry
}

// parseBugEntries extracts error observations from a session's bug.md. Each
// sessionlog entry is a fenced ```json block; we pull the tool_name and data
// fields and signature-truncate the data.
func parseBugEntries(path string) []bugObservation {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var observations []bugObservation
	for _, block := range splitJSONBlocks(string(raw)) {
		var entry struct {
			Event    string `json:"event"`
			Data     string `json:"data"`
			Meta     struct {
				ToolName  string `json:"tool_name"`
				ErrorType string `json:"error_type"`
			} `json:"meta"`
		}
		if err := json.Unmarshal([]byte(block), &entry); err != nil {
			continue
		}
		if entry.Meta.ToolName == "" {
			continue
		}
		frag := entry.Data
		if len(frag) > 120 {
			frag = frag[:120]
		}
		observations = append(observations, bugObservation{
			toolName: entry.Meta.ToolName,
			fragment: frag,
			data:     entry.Data,
		})
	}
	return observations
}

// splitJSONBlocks extracts the content of each fenced ```json block from the
// markdown log file.
func splitJSONBlocks(md string) []string {
	var blocks []string
	const fence = "```json"
	for {
		start := strings.Index(md, fence)
		if start < 0 {
			break
		}
		md = md[start+len(fence):]
		end := strings.Index(md, "```")
		if end < 0 {
			break
		}
		blocks = append(blocks, strings.TrimSpace(md[:end]))
		md = md[end+3:]
	}
	return blocks
}

// emitRulePatch writes a KindRule patch for an error cluster.
func (p *Producer) emitRulePatch(c errorSignature, title string) (string, error) {
	rule := buildRuleText(c)
	patch := Patch{
		Kind:        KindRule,
		Title:       title,
		Description: fmt.Sprintf("Recurring %s error observed %d time(s) across %d session(s).", c.ToolName, c.Count, len(c.Sessions)),
		Priority:    3,
		Source:      strings.Join(c.Sessions, ","),
		CreatedAt:   time.Now(),
		// Auto-converge: after this many injections the rule is considered
		// learned and drops out of the unapplied set (prevents unbounded
		// context growth). Tuned for repeated exposure without permanence.
		MaxInjects: 10,
	}
	patchDir, err := p.store.CreatePatch(patch)
	if err != nil {
		return "", err
	}
	if err := p.store.WriteFile(patchDir, "rules", "rule.md", rule); err != nil {
		return "", err
	}
	return patch.ID, nil
}

// buildRuleText renders the human-readable rule that gets injected.
func buildRuleText(c errorSignature) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("<!-- evolution: rule for recurring %s error -->\n", c.ToolName))
	sb.WriteString(fmt.Sprintf("**Avoid this recurring error with the `%s` tool** (seen %d times):\n\n", c.ToolName, c.Count))
	sb.WriteString("When using this tool, watch for the following failure mode:\n\n")
	for _, ex := range c.Examples {
		sb.WriteString(fmt.Sprintf("- %s\n", ex))
	}
	sb.WriteString("\nCheck these conditions before invoking the tool to prevent repeating the failure.\n")
	return sb.String()
}

// patchTitle produces a stable, dedup-friendly title for a cluster.
func patchTitle(toolName, fragment string) string {
	short := fragment
	if i := strings.IndexAny(short, "\n:"); i >= 0 {
		short = short[:i]
	}
	if len(short) > 60 {
		short = short[:60]
	}
	return fmt.Sprintf("rule: avoid %s error — %s", toolName, short)
}

// existingTitles returns the titles of all current patches (for dedup).
func (p *Producer) existingTitles() ([]string, error) {
	patches, err := p.store.List()
	if err != nil {
		return nil, err
	}
	titles := make([]string, 0, len(patches))
	for _, pa := range patches {
		titles = append(titles, pa.Title)
	}
	return titles, nil
}

func appendUnique(slice []string, v string) []string {
	for _, s := range slice {
		if s == v {
			return slice
		}
	}
	return append(slice, v)
}

// scanErrcollErrors reads the errcoll JSONL files (sibling `errors/` dir) and
// clusters tool errors by (tool name, error fragment), mirroring scanErrors.
// This unifies the errcoll error source into patch production.
func (p *Producer) scanErrcollErrors() ([]errorSignature, error) {
	errorsDir := filepath.Join(filepath.Dir(p.sessionsDir), "errors")
	entries, err := os.ReadDir(errorsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read errors dir: %w", err)
	}
	type key struct {
		tool, frag string
	}
	clusterMap := make(map[key]*errorSignature)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(errorsDir, e.Name()))
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(raw), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			var rec struct {
				ToolName  string `json:"tool_name"`
				Error     string `json:"error"`
				SessionID string `json:"session_id"`
			}
			if err := json.Unmarshal([]byte(line), &rec); err != nil {
				continue
			}
			if rec.ToolName == "" {
				continue
			}
			frag := rec.Error
			if len(frag) > 120 {
				frag = frag[:120]
			}
			k := key{tool: rec.ToolName, frag: frag}
			c, ok := clusterMap[k]
			if !ok {
				c = &errorSignature{ToolName: rec.ToolName, Fragment: frag}
				clusterMap[k] = c
			}
			c.Count++
			sess := rec.SessionID
			if sess == "" {
				sess = "errcoll:" + e.Name()
			}
			c.Sessions = appendUnique(c.Sessions, sess)
			if len(c.Examples) < 3 {
				c.Examples = append(c.Examples, rec.Error)
			}
		}
	}
	out := make([]errorSignature, 0, len(clusterMap))
	for _, c := range clusterMap {
		out = append(out, *c)
	}
	return out, nil
}

// mergeClusters combines two error cluster slices, summing counts for matching
// (tool, fragment) keys and unioning their sessions/examples.
func mergeClusters(a, b []errorSignature) []errorSignature {
	type key struct {
		tool, frag string
	}
	merged := make(map[key]*errorSignature, len(a)+len(b))
	add := func(c errorSignature) {
		k := key{tool: c.ToolName, frag: c.Fragment}
		if existing, ok := merged[k]; ok {
			existing.Count += c.Count
			for _, s := range c.Sessions {
				existing.Sessions = appendUnique(existing.Sessions, s)
			}
			for _, ex := range c.Examples {
				if len(existing.Examples) < 3 {
					existing.Examples = append(existing.Examples, ex)
				}
			}
		} else {
			cp := c
			merged[k] = &cp
		}
	}
	for _, c := range a {
		add(c)
	}
	for _, c := range b {
		add(c)
	}
	out := make([]errorSignature, 0, len(merged))
	for _, c := range merged {
		out = append(out, *c)
	}
	return out
}
