package distillation

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"charm.land/fantasy"

	memcore "github.com/nextsko/mocode-agent/internal/core/knowledge/memory"
	memdom "github.com/nextsko/mocode-agent/internal/domain/memory"
	"github.com/nextsko/mocode-agent/internal/core/evolution/fact"
)

// fakeMemory is an in-memory memory.Service stub for testing. We
// implement only the methods the dedupe + writeback path uses.
type fakeMemory struct {
	mu      sync.Mutex
	entries []*memdom.Entry
	addErr  error
}

func newFakeMemory() *fakeMemory { return &fakeMemory{} }

func (f *fakeMemory) AddMemory(_ context.Context, appName, userID, memory string,
	topics []string, kind memdom.Kind, _ *time.Time, _ []string, _ string,
) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.addErr != nil {
		return f.addErr
	}
	id := memdom.GenerateMemoryID(&memdom.Memory{Memory: memory, Topics: topics, Kind: kind}, appName, userID)
	f.entries = append(f.entries, &memdom.Entry{
		ID:      id,
		AppName: appName,
		UserID:  userID,
		Memory:  &memdom.Memory{Memory: memory, Topics: topics, Kind: kind},
	})
	return nil
}

func (f *fakeMemory) SearchMemories(_ context.Context, appName, userID, query string, limit int) ([]*memdom.Entry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	q := strings.ToLower(query)
	var out []*memdom.Entry
	for _, e := range f.entries {
		if e.AppName != appName || e.UserID != userID {
			continue
		}
		if e.Memory == nil {
			continue
		}
		if strings.Contains(strings.ToLower(e.Memory.Memory), q) {
			out = append(out, e)
		}
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (f *fakeMemory) size() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.entries)
}

// Unused methods panic on call to surface accidental misuse.
func (f *fakeMemory) UpdateMemory(context.Context, string, string, string, string, []string, memdom.Kind, *time.Time, []string, string) error {
	panic("fakeMemory.UpdateMemory: not used")
}
func (f *fakeMemory) DeleteMemory(context.Context, string, string, string) error {
	panic("fakeMemory.DeleteMemory: not used")
}
func (f *fakeMemory) ClearMemories(context.Context, string, string) error {
	panic("fakeMemory.ClearMemories: not used")
}
func (f *fakeMemory) ReadMemories(context.Context, string, string, int) ([]*memdom.Entry, error) {
	return nil, nil
}
func (f *fakeMemory) Tools() []fantasy.AgentTool { return nil }
func (f *fakeMemory) Close() error                   { return nil }

// Compile-time interface check.
var _ memcore.Service = (*fakeMemory)(nil)

// --- Tests ---------------------------------------------------------------

func TestJaccardIdentical(t *testing.T) {
	a := tokenize("hello world")
	b := tokenize("hello world")
	if got := jaccard(a, b); got != 1.0 {
		t.Fatalf("jaccard(hello world, hello world) = %v, want 1.0", got)
	}
}

func TestJaccardDisjoint(t *testing.T) {
	a := tokenize("hello world")
	b := tokenize("foo bar")
	if got := jaccard(a, b); got != 0 {
		t.Fatalf("jaccard(disjoint) = %v, want 0", got)
	}
}

func TestJaccardPartial(t *testing.T) {
	a := tokenize("the quick brown fox")
	b := tokenize("the lazy brown dog")
	got := jaccard(a, b)
	if got < 0.32 || got > 0.34 {
		t.Fatalf("jaccard(partial) = %v, want ~0.33", got)
	}
}

func TestJaccardEmpty(t *testing.T) {
	if jaccard(map[string]struct{}{}, map[string]struct{}{}) != 1.0 {
		t.Fatal("empty/empty should be 1.0")
	}
	if jaccard(map[string]struct{}{}, tokenize("x")) != 0 {
		t.Fatal("empty/nonempty should be 0")
	}
}

func TestTokenizeCJK(t *testing.T) {
	tokens := tokenize("我喜欢用中文")
	for _, want := range []string{"我", "喜", "欢", "用", "中", "文"} {
		if _, ok := tokens[want]; !ok {
			t.Fatalf("tokenize(CJK) missing %q in %v", want, tokens)
		}
	}
}

func TestBestJaccardSimilarityFindsBest(t *testing.T) {
	existing := []*memdom.Entry{
		{ID: "low", Memory: &memdom.Memory{Memory: "totally unrelated stuff"}},
		{ID: "high", Memory: &memdom.Memory{Memory: "I prefer Chinese replies"}},
		{ID: "mid", Memory: &memdom.Memory{Memory: "Chinese food is great"}},
	}
	got := bestJaccardSimilarity("I prefer Chinese", existing)
	if got.id != "high" {
		t.Fatalf("bestJaccardSimilarity: got %s, want high", got.id)
	}
}

func TestDeduperFiltersDuplicates(t *testing.T) {
	mem := newFakeMemory()
	if err := mem.AddMemory(context.Background(), "app", "user", "I prefer dark mode", nil, memdom.KindFact, nil, nil, ""); err != nil {
		t.Fatal(err)
	}

	ded := NewDeduper(mem, DeduperConfig{AppName: "app", UserID: "user", JaccardThreshold: 0.85})
	res, err := ded.Dedupe(context.Background(), []fact.Fact{
		{Content: "I prefer dark mode", Confidence: 0.9},
		{Content: "Project uses Postgres", Confidence: 0.9},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Dropped) != 1 || res.Dropped[0].MatchedID == "" {
		t.Fatalf("expected 1 dropped with matched id, got %+v", res.Dropped)
	}
	if len(res.Surviving) != 1 || res.Surviving[0].Content != "Project uses Postgres" {
		t.Fatalf("expected 1 surviving (postgres), got %+v", res.Surviving)
	}
}

func TestDeduperFailOpen(t *testing.T) {
	ded := NewDeduper(explodingMemory{}, DeduperConfig{AppName: "app", UserID: "user"})
	res, err := ded.Dedupe(context.Background(), []fact.Fact{
		{Content: "Anything", Confidence: 0.9},
	})
	if err == nil {
		t.Fatal("expected error from explodingMemory")
	}
	if len(res.Surviving) != 1 {
		t.Fatalf("fail-open should keep all facts; got %+v", res.Surviving)
	}
}

type explodingMemory struct{}

func (explodingMemory) AddMemory(context.Context, string, string, string, []string, memdom.Kind, *time.Time, []string, string) error {
	return errors.New("boom")
}
func (explodingMemory) SearchMemories(context.Context, string, string, string, int) ([]*memdom.Entry, error) {
	return nil, errors.New("search failed")
}
func (explodingMemory) UpdateMemory(context.Context, string, string, string, string, []string, memdom.Kind, *time.Time, []string, string) error {
	panic("unused")
}
func (explodingMemory) DeleteMemory(context.Context, string, string, string) error { panic("unused") }
func (explodingMemory) ClearMemories(context.Context, string, string) error       { panic("unused") }
func (explodingMemory) ReadMemories(context.Context, string, string, int) ([]*memdom.Entry, error) {
	return nil, nil
}
func (explodingMemory) Tools() []fantasy.AgentTool { return nil }
func (explodingMemory) Close() error                   { return nil }

var _ memcore.Service = explodingMemory{}
