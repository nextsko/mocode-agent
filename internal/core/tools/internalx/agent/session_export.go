package agenttools

// SessionExportToolName is the registered name of the session_export tool.
const SessionExportToolName = "session_export"

// SessionExportParams holds input for session_export.
type SessionExportParams struct {
	Format string `json:"format,omitempty" description:"Export format: markdown, md, obsidian-md, or html. Defaults to markdown"`
	Scope  string `json:"scope,omitempty" description:"Export scope: all or recent10. Defaults to all"`
}
