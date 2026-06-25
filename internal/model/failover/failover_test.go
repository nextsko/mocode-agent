package failover

import (
	"context"
	"errors"
	"testing"

	"charm.land/fantasy"
)

// fakeModel is a minimal fantasy.LanguageModel stub for testing failover.
type fakeModel struct {
	provider string
	name     string
	genErr   error // non-nil => Generate returns this error
}

func (f fakeModel) Provider() string { return f.provider }
func (f fakeModel) Model() string    { return f.name }
func (f fakeModel) Generate(_ context.Context, _ fantasy.Call) (*fantasy.Response, error) {
	if f.genErr != nil {
		return nil, f.genErr
	}
	return &fantasy.Response{FinishReason: fantasy.FinishReasonStop}, nil
}
func (f fakeModel) Stream(_ context.Context, _ fantasy.Call) (fantasy.StreamResponse, error) {
	if f.genErr != nil {
		return nil, f.genErr
	}
	return func(yield func(fantasy.StreamPart) bool) {
		yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextDelta, Delta: "hi"})
	}, nil
}
func (f fakeModel) GenerateObject(_ context.Context, _ fantasy.ObjectCall) (*fantasy.ObjectResponse, error) {
	if f.genErr != nil {
		return nil, f.genErr
	}
	return &fantasy.ObjectResponse{}, nil
}
func (f fakeModel) StreamObject(_ context.Context, _ fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error) {
	if f.genErr != nil {
		return nil, f.genErr
	}
	return func(yield func(fantasy.ObjectStreamPart) bool) {}, nil
}

var _ fantasy.LanguageModel = fakeModel{}

func TestNewValidation(t *testing.T) {
	t.Run("missing primary", func(t *testing.T) {
		_, err := New(WithFallback(fakeModel{name: "fb"}))
		if err == nil {
			t.Fatal("expected error for missing primary")
		}
	})
	t.Run("missing fallback", func(t *testing.T) {
		_, err := New(WithPrimary(fakeModel{name: "p"}))
		if err == nil {
			t.Fatal("expected error for missing fallback")
		}
	})
	t.Run("nil fallback", func(t *testing.T) {
		_, err := New(WithPrimary(fakeModel{name: "p"}), WithFallback(nil))
		if err == nil {
			t.Fatal("expected error for nil fallback")
		}
	})
	t.Run("ok", func(t *testing.T) {
		m, err := New(WithPrimary(fakeModel{name: "p"}), WithFallback(fakeModel{name: "fb"}))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if m.Model() != "p" {
			t.Fatalf("Model() = %q, want p", m.Model())
		}
	})
}

func TestGenerateFailover(t *testing.T) {
	primary := fakeModel{provider: "openai", name: "gpt", genErr: errors.New("rate limited")}
	fallback := fakeModel{provider: "anthropic", name: "claude"}
	m, err := New(WithPrimary(primary), WithFallback(fallback))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	resp, err := m.Generate(context.Background(), fantasy.Call{})
	if err != nil {
		t.Fatalf("Generate should have failed over: %v", err)
	}
	if resp == nil || resp.FinishReason != fantasy.FinishReasonStop {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestGenerateAllFail(t *testing.T) {
	m, err := New(
		WithPrimary(fakeModel{name: "p", genErr: errors.New("boom")}),
		WithFallback(fakeModel{name: "f", genErr: errors.New("kaboom")}),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = m.Generate(context.Background(), fantasy.Call{})
	if err == nil {
		t.Fatal("expected error when all candidates fail")
	}
}

func TestGeneratePrimarySucceedsNoFallback(t *testing.T) {
	m, err := New(
		WithPrimary(fakeModel{name: "p"}),
		WithFallback(fakeModel{name: "f", genErr: errors.New("should not be called")}),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := m.Generate(context.Background(), fantasy.Call{}); err != nil {
		t.Fatalf("primary should succeed without touching fallback: %v", err)
	}
}

func TestStreamFailoverOnErrorPart(t *testing.T) {
	// Primary emits an error part first; failover should switch to fallback.
	primary := fakeStreamModel{firstErr: true}
	fallback := fakeStreamModel{delta: "ok"}
	m, err := New(WithPrimary(primary), WithFallback(fallback))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	seq, err := m.Stream(context.Background(), fantasy.Call{})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	var got []string
	seq(func(p fantasy.StreamPart) bool {
		if p.Type == fantasy.StreamPartTypeTextDelta {
			got = append(got, p.Delta)
		}
		return true
	})
	if len(got) == 0 || got[0] != "ok" {
		t.Fatalf("expected fallback delta 'ok', got %v", got)
	}
}

// fakeStreamModel lets a test emit an error-first stream part.
type fakeStreamModel struct {
	firstErr bool
	delta    string
	name     string
}

func (f fakeStreamModel) Provider() string { return "test" }
func (f fakeStreamModel) Model() string    { return f.name }
func (f fakeStreamModel) Generate(context.Context, fantasy.Call) (*fantasy.Response, error) {
	return &fantasy.Response{}, nil
}
func (f fakeStreamModel) Stream(_ context.Context, _ fantasy.Call) (fantasy.StreamResponse, error) {
	return func(yield func(fantasy.StreamPart) bool) {
		if f.firstErr {
			yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeError, Error: errors.New("primary stream error")})
			return
		}
		yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextDelta, Delta: f.delta})
	}, nil
}
func (f fakeStreamModel) GenerateObject(context.Context, fantasy.ObjectCall) (*fantasy.ObjectResponse, error) {
	return &fantasy.ObjectResponse{}, nil
}
func (f fakeStreamModel) StreamObject(context.Context, fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error) {
	return func(yield func(fantasy.ObjectStreamPart) bool) {}, nil
}

var _ fantasy.LanguageModel = fakeStreamModel{}
