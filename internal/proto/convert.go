package proto

import "github.com/package-register/mocode/internal/session/message"

// ToMessageToolResult converts a proto.ToolResult to a message.ToolResult.
//
// This function preserves ALL 8 fields (ToolCallID, Name, AgentName, Content,
// Data, MIMEType, Metadata, IsError), avoiding the data loss that occurred
// when callers manually copied only 4-5 fields.
//
// The reverse conversion is FromMessageToolResult.
func ToMessageToolResult(p ToolResult) message.ToolResult {
	return message.ToolResult{
		ToolCallID: p.ToolCallID,
		Name:       p.Name,
		AgentName:  p.AgentName,
		Content:    p.Content,
		Data:       p.Data,
		MIMEType:   p.MIMEType,
		Metadata:   p.Metadata,
		IsError:    p.IsError,
	}
}

// FromMessageToolResult converts a message.ToolResult to a proto.ToolResult.
//
// This function preserves ALL 8 fields. The reverse conversion is
// ToMessageToolResult.
func FromMessageToolResult(m message.ToolResult) ToolResult {
	return ToolResult{
		ToolCallID: m.ToolCallID,
		Name:       m.Name,
		AgentName:  m.AgentName,
		Content:    m.Content,
		Data:       m.Data,
		MIMEType:   m.MIMEType,
		Metadata:   m.Metadata,
		IsError:    m.IsError,
	}
}
