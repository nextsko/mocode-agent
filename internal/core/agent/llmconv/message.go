// Package llmconv converts domain messages into the fantasy message types the
// agent runtime sends to providers.
//
// It exists so internal/domain stays free of the LLM framework: the domain
// owns provider-neutral content types, and the mapping to fantasy (including
// per-provider reasoning metadata) lives here, at the core/agent boundary.
package llmconv

import (
	"errors"
	"strings"

	"charm.land/fantasy"
	"charm.land/fantasy/providers/anthropic"
	"charm.land/fantasy/providers/google"
	"charm.land/fantasy/providers/openai"

	"github.com/nextsko/mocode-agent/internal/domain/session/message"
	"github.com/nextsko/mocode-agent/internal/util/ext"
)

// mediaLoadFailedPlaceholder marks a media tool result whose payload could not
// be decoded during session replay.
const mediaLoadFailedPlaceholder = "[Image data could not be loaded]"

// ToAIMessage converts one domain message into zero or more fantasy messages.
// Empty messages are dropped: sending {"content": []} makes most provider APIs
// reject the whole request.
func ToAIMessage(m message.Message) []fantasy.Message {
	var messages []fantasy.Message
	switch m.Role {
	case message.User:
		var parts []fantasy.MessagePart
		text := strings.TrimSpace(m.Content().Text)
		var textAttachments []message.Attachment
		for _, content := range m.BinaryContent() {
			if !strings.HasPrefix(content.MIMEType, "text/") {
				continue
			}
			textAttachments = append(textAttachments, message.Attachment{
				FilePath: content.Path,
				MimeType: content.MIMEType,
				Content:  content.Data,
			})
		}
		text = message.PromptWithTextAttachments(text, textAttachments)
		if text != "" {
			parts = append(parts, fantasy.TextPart{Text: text})
		}
		for _, content := range m.BinaryContent() {
			// skip text attachments
			if strings.HasPrefix(content.MIMEType, "text/") {
				continue
			}
			parts = append(parts, fantasy.FilePart{
				Filename:  content.Path,
				Data:      content.Data,
				MediaType: content.MIMEType,
			})
		}
		if len(parts) == 0 {
			break
		}
		messages = append(messages, fantasy.Message{
			Role:    fantasy.MessageRoleUser,
			Content: parts,
		})
	case message.Assistant:
		var parts []fantasy.MessagePart
		text := strings.TrimSpace(m.Content().Text)
		if text != "" {
			parts = append(parts, fantasy.TextPart{Text: text})
		}
		reasoning := m.ReasoningContent()
		if reasoning.Thinking != "" {
			reasoningPart := fantasy.ReasoningPart{Text: reasoning.Thinking, ProviderOptions: fantasy.ProviderOptions{}}
			if reasoning.Signature != "" {
				reasoningPart.ProviderOptions[anthropic.Name] = &anthropic.ReasoningOptionMetadata{
					Signature: reasoning.Signature,
				}
			}
			if reasoning.ResponsesData != nil {
				reasoningPart.ProviderOptions[openai.Name] = toOpenAIReasoningData(reasoning.ResponsesData)
			}
			if reasoning.ThoughtSignature != "" {
				reasoningPart.ProviderOptions[google.Name] = &google.ReasoningMetadata{
					Signature: reasoning.ThoughtSignature,
					ToolID:    reasoning.ToolID,
				}
			}
			parts = append(parts, reasoningPart)
		}
		for _, call := range m.ToolCalls() {
			parts = append(parts, fantasy.ToolCallPart{
				ToolCallID:       call.ID,
				ToolName:         call.Name,
				Input:            call.Input,
				ProviderExecuted: call.ProviderExecuted,
			})
		}
		if len(parts) == 0 {
			break
		}
		messages = append(messages, fantasy.Message{
			Role:    fantasy.MessageRoleAssistant,
			Content: parts,
		})
	case message.Tool:
		parts := make([]fantasy.MessagePart, 0, len(m.ToolResults()))
		for _, result := range m.ToolResults() {
			var content fantasy.ToolResultOutputContent
			if result.IsError {
				content = fantasy.ToolResultOutputContentError{
					Error: errors.New(result.Content),
				}
			} else if result.Data != "" {
				if ext.IsValidBase64(result.Data) {
					content = fantasy.ToolResultOutputContentMedia{
						Data:      result.Data,
						MediaType: result.MIMEType,
					}
				} else {
					content = fantasy.ToolResultOutputContentText{
						Text: mediaLoadFailedPlaceholder,
					}
				}
			} else {
				content = fantasy.ToolResultOutputContentText{
					Text: result.Content,
				}
			}
			parts = append(parts, fantasy.ToolResultPart{
				ToolCallID: result.ToolCallID,
				Output:     content,
			})
		}
		if len(parts) == 0 {
			break
		}
		messages = append(messages, fantasy.Message{
			Role:    fantasy.MessageRoleTool,
			Content: parts,
		})
	default:
		// System messages are provided by the caller, not derived from the
		// session history, so they produce no fantasy message here.
	}
	return messages
}

// ToAIMessages converts a slice of domain messages.
func ToAIMessages(msgs []message.Message) []fantasy.Message {
	out := make([]fantasy.Message, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, ToAIMessage(m)...)
	}
	return out
}

// ToDomainReasoningData converts OpenAI Responses reasoning metadata into the
// provider-neutral domain form so it can be persisted with the session.
func ToDomainReasoningData(data *openai.ResponsesReasoningMetadata) *message.ResponsesReasoningData {
	if data == nil {
		return nil
	}
	return &message.ResponsesReasoningData{
		ItemID:           data.ItemID,
		EncryptedContent: data.EncryptedContent,
		Summary:          data.Summary,
	}
}

// toOpenAIReasoningData converts the stored domain form back to the framework
// type when a request is sent.
func toOpenAIReasoningData(data *message.ResponsesReasoningData) *openai.ResponsesReasoningMetadata {
	if data == nil {
		return nil
	}
	return &openai.ResponsesReasoningMetadata{
		ItemID:           data.ItemID,
		EncryptedContent: data.EncryptedContent,
		Summary:          data.Summary,
	}
}
