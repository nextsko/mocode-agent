package netcommon

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDuckDuckGoInstantAnswerSearch(t *testing.T) {
	payload := `{
		"AbstractText": "Go is a statically typed, compiled programming language. It is C-like.",
		"AbstractURL": "https://go.dev",
		"Answer": "",
		"Definition": "",
		"RelatedTopics": [
			{"text": "Go language - an open source language", "FirstURL": "https://go.dev/ref/spec"},
			{"text": "Goroutines - lightweight threads", "FirstURL": "https://go.dev/tour/concurrency"}
		]
	}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, payload)
	}))
	defer srv.Close()

	p := DuckDuckGoInstantAnswerProvider{BaseURL: srv.URL}
	results, err := p.Search(context.Background(), srv.Client(), "Go programming language", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least one result")
	}
	if results[0].Snippet == "" {
		t.Fatal("expected abstract snippet on first result")
	}
	if results[0].Link != "https://go.dev" {
		t.Fatalf("expected abstract URL, got %q", results[0].Link)
	}
}

func TestInstantAnswerEmptyReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{}`)
	}))
	defer srv.Close()

	p := DuckDuckGoInstantAnswerProvider{BaseURL: srv.URL}
	_, err := p.Search(context.Background(), srv.Client(), "nonexistent", 5)
	if err == nil {
		t.Fatal("expected error for empty instant-answer response")
	}
}

type stubProvider struct {
	name    string
	results []SearchResult
	err     error
}

func (s stubProvider) Name() string { return s.name }

func (s stubProvider) Search(_ context.Context, _ *http.Client, _ string, _ int) ([]SearchResult, error) {
	return s.results, s.err
}

func TestMultiProviderFirstWins(t *testing.T) {
	want := []SearchResult{{Title: "primary hit", Link: "https://a.example"}}
	m := MultiProvider{
		Providers: []Provider{
			stubProvider{name: "p1", results: want},
			stubProvider{name: "p2"},
		},
	}
	got, err := m.Search(context.Background(), nil, "q", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Title != "primary hit" {
		t.Fatalf("expected primary hit, got %v", got)
	}
}

func TestMultiProviderFallsBackOnError(t *testing.T) {
	want := []SearchResult{{Title: "fallback hit", Link: "https://b.example"}}
	m := MultiProvider{
		Providers: []Provider{
			stubProvider{name: "p1", err: fmt.Errorf("rate limited")},
			stubProvider{name: "p2", results: want},
		},
	}
	got, err := m.Search(context.Background(), nil, "q", 5)
	if err != nil {
		t.Fatalf("expected fallback success, got error: %v", err)
	}
	if len(got) != 1 || got[0].Title != "fallback hit" {
		t.Fatalf("expected fallback hit, got %v", got)
	}
}

func TestMultiProviderFallsBackOnEmpty(t *testing.T) {
	want := []SearchResult{{Title: "fallback hit"}}
	m := MultiProvider{
		Providers: []Provider{
			stubProvider{name: "p1", results: nil},
			stubProvider{name: "p2", results: want},
		},
	}
	got, err := m.Search(context.Background(), nil, "q", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("expected fallback hit, got %v", got)
	}
}

func TestMultiProviderAllFailReturnsError(t *testing.T) {
	m := MultiProvider{
		Providers: []Provider{
			stubProvider{name: "p1", err: fmt.Errorf("e1")},
			stubProvider{name: "p2", err: fmt.Errorf("e2")},
		},
	}
	_, err := m.Search(context.Background(), nil, "q", 5)
	if err == nil {
		t.Fatal("expected error when all providers fail")
	}
}

func TestFirstSentence(t *testing.T) {
	if got := firstSentence("Go is a language. More text."); got != "Go is a language" {
		t.Fatalf("expected leading sentence, got %q", got)
	}
	if got := firstSentence("short"); got != "short" {
		t.Fatalf("expected unchanged short string, got %q", got)
	}
	long := strings.Repeat("word ", 40)
	got := firstSentence(long)
	if !strings.HasSuffix(got, "...") {
		t.Fatalf("expected truncation suffix, got %q", got)
	}
	if len(got) > len(long) {
		t.Fatalf("truncated output must not exceed input length")
	}
}

func TestDefaultSearchProviderHasFallback(t *testing.T) {
	p := DefaultSearchProvider()
	mp, ok := p.(MultiProvider)
	if !ok {
		t.Fatalf("expected MultiProvider, got %T", p)
	}
	if len(mp.Providers) < 2 {
		t.Fatalf("expected at least 2 providers in default chain, got %d", len(mp.Providers))
	}
}
