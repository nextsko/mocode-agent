package evalset

import (
	"encoding/json"
	"io"
)

// jsonEvalCase mirrors EvalCase with JSON tags for file-based sets.
type jsonEvalCase struct {
	ID            string             `json:"id"`
	UserContent   string             `json:"user_content"`
	SystemPrompt  string             `json:"system_prompt,omitempty"`
	ExpectedText  string             `json:"expected_text,omitempty"`
	ExpectedTools []jsonExpectedTool `json:"expected_tools,omitempty"`
	Rubrics       []string           `json:"rubrics,omitempty"`
}

type jsonExpectedTool struct {
	Name  string `json:"name"`
	Input string `json:"input,omitempty"`
}

// jsonEvalSet mirrors EvalSet with JSON tags for file-based sets.
type jsonEvalSet struct {
	ID          string          `json:"id"`
	Name        string          `json:"name,omitempty"`
	Description string          `json:"description,omitempty"`
	Cases       []jsonEvalCase  `json:"cases"`
}

// MarshalJSON serializes an EvalSet to JSON.
func (e *EvalSet) MarshalJSON() ([]byte, error) {
	out := jsonEvalSet{ID: e.ID, Name: e.Name, Description: e.Description, Cases: make([]jsonEvalCase, len(e.Cases))}
	for i, c := range e.Cases {
		tools := make([]jsonExpectedTool, len(c.ExpectedTools))
		for j, t := range c.ExpectedTools {
			tools[j] = jsonExpectedTool{Name: t.Name, Input: t.Input}
		}
		out.Cases[i] = jsonEvalCase{
			ID: c.ID, UserContent: c.UserContent, SystemPrompt: c.SystemPrompt,
			ExpectedText: c.ExpectedText, ExpectedTools: tools, Rubrics: c.Rubrics,
		}
	}
	return json.Marshal(out)
}

// UnmarshalJSON deserializes an EvalSet from JSON.
func (e *EvalSet) UnmarshalJSON(data []byte) error {
	var in jsonEvalSet
	if err := json.Unmarshal(data, &in); err != nil {
		return err
	}
	e.ID, e.Name, e.Description = in.ID, in.Name, in.Description
	e.Cases = make([]*EvalCase, len(in.Cases))
	for i, c := range in.Cases {
		tools := make([]ExpectedToolCall, len(c.ExpectedTools))
		for j, t := range c.ExpectedTools {
			tools[j] = ExpectedToolCall{Name: t.Name, Input: t.Input}
		}
		e.Cases[i] = &EvalCase{
			ID: c.ID, UserContent: c.UserContent, SystemPrompt: c.SystemPrompt,
			ExpectedText: c.ExpectedText, ExpectedTools: tools, Rubrics: c.Rubrics,
		}
	}
	return nil
}

// LoadSet reads and decodes an EvalSet from r.
func LoadSet(r io.Reader) (*EvalSet, error) {
	var es EvalSet
	dec := json.NewDecoder(r)
	if err := dec.Decode(&es); err != nil {
		return nil, err
	}
	return &es, nil
}
