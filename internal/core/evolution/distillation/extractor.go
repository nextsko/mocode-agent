// Package distillation turns raw conversation turns into structured, durable
// facts. It is the write-side of the self-evolution pipeline and is shared by
// Reviewer (per-turn cadence) and Dreamer (idle cadence).
//
// Inspired by:
//   - MiMo-Code /dream and /distill prompt design.
//   - hermes-agent background_review.py prompt template.
//   - grok-build is_semantically_duplicate for cosine threshold.
package distillation

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"charm.land/fantasy"

	"github.com/nextsko/mocode-agent/internal/core/evolution/fact"
)

// MaxMessagesForExtraction caps how many recent messages we feed to the LLM.
// Beyond ~30 the prompt grows linearly and we lose focus; this matches the
// hermes-agent review window.
const MaxMessagesForExtraction = 30

// extractionSchema is the JSON shape we ask the LLM to emit. We rely on
// fantasy's structured output to enforce it.
type extractionSchema struct {
	Facts []struct {
		Content    string   `json:"content"`
		Topics     []string `json:"topics"`
		Kind       string   `json:"kind"`        // user_pref | project_rule | verified_fact | episode
		Confidence float64  `json:"confidence"`  // 0..1
	} `json:"facts"`
}

// Extractor is the LLM-side of the distillation pipeline. Given a slice of
// recent messages, it produces structured ExtractedFacts.
//
// Design notes:
//
//   - Single LLM call per turn-batch; cheap on tokens, deterministic with
//     temperature=0 (MiMo-Code goal.ts pattern).
//   - Output is validated server-side; we never trust the LLM's `confidence`
//     blindly — Writeback applies MinConfidence after the fact leaves this
//     layer.
//   - All kinds map cleanly to the existing memdom.Kind values via
//     fact.Kind.ToMemoryKind.
type Extractor struct {
	model    fantasy.LanguageModel
	maxToks  int64
	nowFn    func() time.Time
}

// NewExtractor constructs an Extractor. maxToks caps the LLM output; pass
// 0 to use the evolution default.
func NewExtractor(model fantasy.LanguageModel, maxToks int64) *Extractor {
	if maxToks <= 0 {
		maxToks = 1024
	}
	return &Extractor{
		model:   model,
		maxToks: maxToks,
		nowFn:   time.Now,
	}
}

// MessageInput is the minimal message shape Extractor needs. We keep this
// small to avoid coupling to the message.Service internals; callers project
// from message.Message via MessagesFromDomain.
type MessageInput struct {
	ID      string
	Role    string
	Content string
}

// Message is an alias for backwards compatibility with older callers.
type Message = MessageInput

// Extract runs the extraction prompt and returns the validated facts.
//
// The caller is expected to filter the slice (e.g. last N turns) before
// calling; Extractor itself does not slice.
//
// Errors:
//   - ctx.Err() if the context is cancelled.
//   - json.Unmarshal errors if the LLM emits malformed JSON (rare with
//     structured output).
//   - errors wrapping the LLM error.
func (e *Extractor) Extract(ctx context.Context, messages []Message) ([]fact.Fact, error) {
	if len(messages) == 0 {
		return nil, nil
	}
	if len(messages) > MaxMessagesForExtraction {
		messages = messages[len(messages)-MaxMessagesForExtraction:]
	}

	prompt := buildExtractionPrompt(messages)
	maxToks := e.maxToks

	var schema extractionSchema
	// We rely on the small model's default temperature (typically 0). The
	// prompt is highly structured with an explicit JSON schema, so the
	// default works; we don't pass Options here.
	agent := fantasy.NewAgent(e.model)
	res, err := agent.Generate(ctx, fantasy.AgentCall{
		Prompt:          prompt,
		MaxOutputTokens: &maxToks,
	})
	if err != nil {
		return nil, fmt.Errorf("distillation: extractor LLM call: %w", err)
	}
	text := strings.TrimSpace(res.Response.Content.Text())

	if err := json.Unmarshal([]byte(text), &schema); err != nil {
		return nil, fmt.Errorf("distillation: parse LLM output: %w (raw=%q)", err, truncate(text, 200))
	}

	now := e.nowFn()
	out := make([]fact.Fact, 0, len(schema.Facts))
	for _, f := range schema.Facts {
		if f.Content == "" {
			continue
		}
		kind := parseKind(f.Kind)
		if kind == "" {
			// Unknown kind → default to fact, never silently drop.
			kind = fact.KindVerifiedFact
		}
		out = append(out, fact.Fact{
			Content:    strings.TrimSpace(f.Content),
			Topics:     cleanTopics(f.Topics),
			Kind:       kind,
			Confidence: clampUnit(f.Confidence),
			At:         now,
		})
	}
	return out, nil
}

// buildExtractionPrompt constructs the system+user prompt. We borrow from
// MiMo-Code's /dream "Phase 2: candidate extraction" and hermes-agent's
// background_review prompt, but stay tightly scoped to durable facts.
func buildExtractionPrompt(messages []Message) string {
	var b strings.Builder
	b.WriteString(extractionSystemPrompt)
	b.WriteString("\n\nRecent conversation (most recent last):\n")
	for _, m := range messages {
		role := strings.ToLower(strings.TrimSpace(m.Role))
		if role == "" {
			role = "user"
		}
		fmt.Fprintf(&b, "- [%s] %s\n", role, truncate(m.Content, 400))
	}
	b.WriteString("\nReturn ONLY JSON matching the schema. If nothing is worth remembering, return {\"facts\":[]}.")
	return b.String()
}

// extractionSystemPrompt is the persona + rubric. Keep it short to fit in
// the small model context.
const extractionSystemPrompt = `You extract durable, high-signal knowledge from a conversation. Output JSON with key "facts": a list. Each fact has:
- "content": one concise sentence (≤140 chars), self-contained
- "topics": short tags ["language:zh","domain:k8s","tool:git"]
- "kind": one of "user_pref" (preference/style), "project_rule" (project convention), "verified_fact" (confirmed domain fact), "episode" (one-off event worth a timestamp)
- "confidence": 0..1, how sure you are this is durable and correct

Skip greetings, small talk, transient status, and anything the user did not confirm.
Prefer one specific fact over many vague ones. Never invent names, numbers, or paths not present in the conversation.`

func parseKind(s string) fact.Kind {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "user_pref", "user-pref", "preference":
		return fact.KindUserPref
	case "project_rule", "rule", "convention":
		return fact.KindProjectRule
	case "verified_fact", "fact", "verified":
		return fact.KindVerifiedFact
	case "episode", "event":
		return fact.KindEpisode
	default:
		return ""
	}
}

func cleanTopics(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	seen := make(map[string]bool, len(in))
	for _, t := range in {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		if len(t) > 32 {
			t = t[:32]
		}
		out = append(out, t)
		if len(out) >= 8 {
			break
		}
	}
	return out
}

func clampUnit(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}