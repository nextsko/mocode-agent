package tools

func ErrorResult(err error) (ToolResult, error) {
	return ToolResult{Error: err}, nil
}

func ErrorResultWithMetadata(err error, metadata map[string]any) (ToolResult, error) {
	return ToolResult{Error: err, Metadata: metadata}, nil
}
