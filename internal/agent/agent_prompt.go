package agent

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"strings"

	"charm.land/fantasy"
	"github.com/package-register/mocode/internal/session"
	"github.com/package-register/mocode/internal/session/message"
)

func (a *sessionAgent) createUserMessage(ctx context.Context, call SessionAgentCall) (message.Message, error) {
	parts := []message.ContentPart{message.TextContent{Text: call.Prompt}}
	var attachmentParts []message.ContentPart
	for _, attachment := range call.Attachments {
		attachmentParts = append(attachmentParts, message.BinaryContent{Path: attachment.FilePath, MIMEType: attachment.MimeType, Data: attachment.Content})
	}
	parts = append(parts, attachmentParts...)
	msg, err := a.messages.Create(ctx, call.SessionID, message.CreateMessageParams{
		Role:  message.User,
		Parts: parts,
	})
	if err != nil {
		return message.Message{}, fmt.Errorf("failed to create user message: %w", err)
	}
	return msg, nil
}

func (a *sessionAgent) preparePrompt(msgs []message.Message, supportsImages bool, attachments ...message.Attachment) ([]fantasy.Message, []fantasy.FilePart) {
	var history []fantasy.Message
	if !a.isSubAgent {
		history = append(history, fantasy.NewUserMessage(
			fmt.Sprintf("<system_reminder>%s</system_reminder>",
				`This is a reminder that your todo list is currently empty. DO NOT mention this to the user explicitly because they are already aware.
If you are working on tasks that would benefit from a todo list please use the "todos" tool to create one.
If not, please feel free to ignore. Again do not mention this message to the user.`,
			),
		))
	}
	// Collect all tool call IDs present in assistant messages and all tool
	// result IDs present in tool messages. This lets us detect both orphaned
	// tool results (result without a call) and orphaned tool calls (call
	// without a result).
	knownToolCallIDs := make(map[string]struct{})
	knownToolResultIDs := make(map[string]struct{})
	for _, m := range msgs {
		switch m.Role {
		case message.Assistant:
			for _, tc := range m.ToolCalls() {
				knownToolCallIDs[tc.ID] = struct{}{}
			}
		case message.Tool:
			for _, tr := range m.ToolResults() {
				knownToolResultIDs[tr.ToolCallID] = struct{}{}
			}
		}
	}

	totalMsgs := len(msgs)
	for i, m := range msgs {
		if len(m.Parts) == 0 {
			continue
		}
		// Assistant message without content or tool calls (cancelled before it returned anything).
		if m.Role == message.Assistant && len(m.ToolCalls()) == 0 && m.Content().Text == "" && m.ReasoningContent().String() == "" {
			continue
		}
		distanceFromEnd := totalMsgs - i - 1
		if m.Role == message.Tool {
			if msg, ok := filterOrphanedToolResults(m, knownToolCallIDs); ok {
				// Build toolCallID → toolName map from the original message
				// parts so the compressor can apply tool-specific rules.
				toolNames := make(map[string]string, len(m.ToolResults()))
				for _, tr := range m.ToolResults() {
					toolNames[tr.ToolCallID] = tr.Name
				}
				// Apply multi-level compression to tool results.
				msg = a.compressToolMessage(msg, distanceFromEnd, toolNames)
				history = append(history, msg)
			}
			continue
		}
		aiMsgs := m.ToAIMessage()
		if !supportsImages {
			aiMsgs = filterImagePartsFromMessages(aiMsgs)
		}
		history = append(history, aiMsgs...)

		if m.Role == message.Assistant {
			if msg, ok := syntheticToolResultsForOrphanedCalls(m, knownToolResultIDs); ok {
				history = append(history, msg)
			}
		}
	}

	// Final safety filter: drop any messages with empty content arrays.
	// Empty content (e.g. {"role": "assistant", "content": []}) causes
	// "messages.content.type is invalid" errors on most LLM APIs.
	filteredHistory := history[:0]
	for _, msg := range history {
		if len(msg.Content) == 0 {
			slog.Warn("Dropping message with empty content before sending to LLM API",
				"role", msg.Role,
			)
			continue
		}
		filteredHistory = append(filteredHistory, msg)
	}

	var files []fantasy.FilePart
	for _, attachment := range attachments {
		if attachment.IsText() {
			continue
		}
		if !supportsImages {
			continue
		}
		files = append(files, fantasy.FilePart{
			Filename:  attachment.FileName,
			Data:      attachment.Content,
			MediaType: attachment.MimeType,
		})
	}

	return filteredHistory, files
}

func (a *sessionAgent) getSessionMessages(ctx context.Context, session session.Session) ([]message.Message, error) {
	msgs, err := a.messages.List(ctx, session.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to list messages: %w", err)
	}

	if session.SummaryMessageID != "" {
		summaryMsgIndex := -1
		for i, msg := range msgs {
			if msg.ID == session.SummaryMessageID {
				summaryMsgIndex = i
				break
			}
		}
		if summaryMsgIndex != -1 {
			msgs = msgs[summaryMsgIndex:]
			msgs[0].Role = message.User
		}
	}
	return msgs, nil
}

func (a *sessionAgent) generateTitle(ctx context.Context, sessionID string, userPrompt string) {
	if userPrompt == "" {
		return
	}

	smallModel := a.smallModel.Get()
	largeModel := a.largeModel.Get()
	systemPromptPrefix := a.systemPromptPrefix.Get()

	var maxOutputTokens int64 = 40
	if smallModel.CatwalkCfg.CanReason {
		maxOutputTokens = smallModel.CatwalkCfg.DefaultMaxTokens
	}

	newAgent := func(m fantasy.LanguageModel, p []byte, tok int64) fantasy.Agent {
		return fantasy.NewAgent(m,
			fantasy.WithSystemPrompt(string(p)+"\n /no_think"),
			fantasy.WithMaxOutputTokens(tok),
			fantasy.WithUserAgent(userAgent),
		)
	}

	streamCall := fantasy.AgentStreamCall{
		Prompt: fmt.Sprintf("Generate a concise title for the following content:\n\n%s\n <think>\n\n</think>", userPrompt),
		PrepareStep: func(callCtx context.Context, opts fantasy.PrepareStepFunctionOptions) (_ context.Context, prepared fantasy.PrepareStepResult, err error) {
			prepared.Messages = opts.Messages
			if systemPromptPrefix != "" {
				prepared.Messages = append([]fantasy.Message{
					fantasy.NewSystemMessage(systemPromptPrefix),
				}, prepared.Messages...)
			}
			return callCtx, prepared, nil
		},
	}

	// Use the small model to generate the title.
	model := smallModel
	agent := newAgent(model.Model, titlePrompt, maxOutputTokens)
	resp, err := agent.Stream(ctx, streamCall)
	if err == nil {
		// We successfully generated a title with the small model.
		slog.Debug("Generated title with small model")
	} else {
		// It didn't work. Let's try with the big model.
		slog.Error("Error generating title with small model; trying big model", "err", err)
		model = largeModel
		agent = newAgent(model.Model, titlePrompt, maxOutputTokens)
		resp, err = agent.Stream(ctx, streamCall)
		if err == nil {
			slog.Debug("Generated title with large model")
		} else {
			// Welp, the large model didn't work either. Use the default
			// session name and return.
			slog.Error("Error generating title with large model", "err", err)
			saveErr := a.sessions.Rename(ctx, sessionID, DefaultSessionName)
			if saveErr != nil {
				slog.Error("Failed to save session title", "error", saveErr)
			}
			return
		}
	}

	if resp == nil {
		// Actually, we didn't get a response so we can't. Use the default
		// session name and return.
		slog.Error("Response is nil; can't generate title")
		saveErr := a.sessions.Rename(ctx, sessionID, DefaultSessionName)
		if saveErr != nil {
			slog.Error("Failed to save session title", "error", saveErr)
		}
		return
	}

	// Clean up title.
	var title string
	title = strings.ReplaceAll(resp.Response.Content.Text(), "\n", " ")

	// Remove thinking tags if present.
	title = thinkTagRegex.ReplaceAllString(title, "")
	title = orphanThinkTagRegex.ReplaceAllString(title, "")

	title = strings.TrimSpace(title)
	title = cmp.Or(title, DefaultSessionName)

	// Calculate usage and cost.
	var openrouterCost *float64
	for _, step := range resp.Steps {
		stepCost := a.openrouterCost(step.ProviderMetadata)
		if stepCost != nil {
			newCost := *stepCost
			if openrouterCost != nil {
				newCost += *openrouterCost
			}
			openrouterCost = &newCost
		}
	}

	modelConfig := model.CatwalkCfg
	cost := modelConfig.CostPer1MInCached/1e6*float64(resp.TotalUsage.CacheCreationTokens) +
		modelConfig.CostPer1MOutCached/1e6*float64(resp.TotalUsage.CacheReadTokens) +
		modelConfig.CostPer1MIn/1e6*float64(resp.TotalUsage.InputTokens) +
		modelConfig.CostPer1MOut/1e6*float64(resp.TotalUsage.OutputTokens)

	// Use override cost if available (e.g., from OpenRouter).
	if openrouterCost != nil {
		cost = *openrouterCost
	}

	promptTokens := resp.TotalUsage.InputTokens + resp.TotalUsage.CacheCreationTokens
	completionTokens := resp.TotalUsage.OutputTokens
	cacheReadTokens := resp.TotalUsage.CacheReadTokens
	cacheCreationTokens := resp.TotalUsage.CacheCreationTokens

	// Atomically update only title and usage fields to avoid overriding other
	// concurrent session updates.
	saveErr := a.sessions.UpdateTitleAndUsage(ctx, sessionID, title, promptTokens, completionTokens, cacheReadTokens, cacheCreationTokens, cost)
	if saveErr != nil {
		slog.Error("Failed to save session title and usage", "error", saveErr)
		return
	}
}
