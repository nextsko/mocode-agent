package contract

import "errors"

// Text returns a successful text result.
func Text(content string) ToolResult { return ToolResult{Content: content} }

// Error returns a tool-level error result. The agent runtime surfaces it to the
// model as text; it is not a Go error.
func Error(text string) ToolResult { return ToolResult{Error: errors.New(text)} }

// WithMetadata attaches structured metadata under the "result" key. The agent
// adapter serialises it onto the runtime response.
func WithMetadata(res ToolResult, meta any) ToolResult {
	if res.Metadata == nil {
		res.Metadata = map[string]any{}
	}
	res.Metadata["result"] = meta
	return res
}
