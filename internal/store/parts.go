package store

import (
	"encoding/json"
	"fmt"

	"github.com/nextsko/mocode-agent/internal/domain/session/message"
)

// partWrapper wraps a ContentPart with its type discriminator for JSON
// serialization. Mirrors the logic in internal/message/message.go.
type partWrapper struct {
	Type string              `json:"type"`
	Data message.ContentPart `json:"data"`
}

// marshalParts serializes a slice of ContentParts to JSON.
func marshalParts(parts []message.ContentPart) ([]byte, error) {
	wrapped := make([]partWrapper, len(parts))
	for i, part := range parts {
		typ := partType(part)
		if typ == "" {
			return nil, fmt.Errorf("unknown part type: %T", part)
		}
		wrapped[i] = partWrapper{Type: typ, Data: part}
	}
	return json.Marshal(wrapped)
}

// unmarshalParts deserializes a JSON blob back to a slice of ContentParts.
func unmarshalParts(data json.RawMessage) ([]message.ContentPart, error) {
	var wrappers []struct {
		Type string          `json:"type"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(data, &wrappers); err != nil {
		return nil, err
	}

	var parts []message.ContentPart
	for _, w := range wrappers {
		part, err := newPart(w.Type, w.Data)
		if err != nil {
			return nil, err
		}
		parts = append(parts, part)
	}
	return parts, nil
}

func partType(part message.ContentPart) string {
	switch part.(type) {
	case message.ReasoningContent:
		return "reasoning"
	case message.TextContent:
		return "text"
	case message.ImageURLContent:
		return "image_url"
	case message.BinaryContent:
		return "binary"
	case message.ToolCall:
		return "tool_call"
	case message.ToolResult:
		return "tool_result"
	case message.Finish:
		return "finish"
	default:
		return ""
	}
}

func newPart(typ string, data json.RawMessage) (message.ContentPart, error) {
	switch typ {
	case "reasoning":
		var p message.ReasoningContent
		if err := json.Unmarshal(data, &p); err != nil {
			return nil, err
		}
		return p, nil
	case "text":
		var p message.TextContent
		if err := json.Unmarshal(data, &p); err != nil {
			return nil, err
		}
		return p, nil
	case "image_url":
		var p message.ImageURLContent
		if err := json.Unmarshal(data, &p); err != nil {
			return nil, err
		}
		return p, nil
	case "binary":
		var p message.BinaryContent
		if err := json.Unmarshal(data, &p); err != nil {
			return nil, err
		}
		return p, nil
	case "tool_call":
		var p message.ToolCall
		if err := json.Unmarshal(data, &p); err != nil {
			return nil, err
		}
		return p, nil
	case "tool_result":
		var p message.ToolResult
		if err := json.Unmarshal(data, &p); err != nil {
			return nil, err
		}
		return p, nil
	case "finish":
		var p message.Finish
		if err := json.Unmarshal(data, &p); err != nil {
			return nil, err
		}
		return p, nil
	default:
		return nil, fmt.Errorf("unknown part type: %s", typ)
	}
}
