package agenttools

// MessageExportToolName is the registered name of the message_export tool.
const MessageExportToolName = "message_export"

// MessageExportParams holds input for message_export.
type MessageExportParams struct {
	MessageIDs []string `json:"message_ids,omitempty" description:"Specific message IDs to export. If empty, exports the last assistant reply."`
	Format     string   `json:"format,omitempty" description:"Export format: markdown, md, or obsidian-md. Defaults to markdown"`
}
