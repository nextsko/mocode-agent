package evalset

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestEvalSetJSONRoundTrip(t *testing.T) {
	src := `{
		"id": "s1",
		"name": "Sample",
		"cases": [
			{"id": "c1", "user_content": "hi", "expected_text": "hello", "expected_tools": [{"name": "view", "input": "README"}]},
			{"id": "c2", "user_content": "yo", "rubrics": ["be concise"]}
		]
	}`
	es, err := LoadSet(strings.NewReader(src))
	if err != nil {
		t.Fatalf("LoadSet: %v", err)
	}
	if es.ID != "s1" || len(es.Cases) != 2 {
		t.Fatalf("unexpected set: %+v", es)
	}
	c1 := es.Cases[0]
	if c1.ExpectedText != "hello" || len(c1.ExpectedTools) != 1 || c1.ExpectedTools[0].Name != "view" || c1.ExpectedTools[0].Input != "README" {
		t.Fatalf("case c1 fields wrong: %+v", c1)
	}
	if !c1.HasExpectedText() || !c1.HasExpectedTools() {
		t.Fatalf("c1 assertion flags wrong")
	}

	// Re-marshal and reload to confirm the round-trip preserves the data.
	raw, err := json.Marshal(es)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	es2, err := LoadSet(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if es2.Cases[0].ExpectedText != "hello" || len(es2.Cases) != 2 {
		t.Fatalf("round-trip lost data: %+v", es2)
	}
}
