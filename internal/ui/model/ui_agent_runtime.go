package model

import (
	"encoding/json"
	"fmt"
	"strings"

	agentcore "github.com/nextsko/mocode-agent/internal/core/agent"
	"github.com/nextsko/mocode-agent/internal/domain/session/message"
	"github.com/nextsko/mocode-agent/internal/ui/chat"
)

func (m *UI) registerAgentToolChild(parentToolCallID, childToolCallID string) {
	parentToolCallID = strings.TrimSpace(parentToolCallID)
	childToolCallID = strings.TrimSpace(childToolCallID)
	if parentToolCallID == "" || childToolCallID == "" || parentToolCallID == childToolCallID {
		return
	}
	if m.agentToolChildren == nil {
		m.agentToolChildren = make(map[string]string)
	}
	m.agentToolChildren[childToolCallID] = parentToolCallID
}

func (m *UI) registerAgentToolTopology(messageID string, tc message.ToolCall) []string {
	if messageID == "" {
		return nil
	}
	childToolCallIDs := agentToolChildCallIDs(tc)
	for _, childToolCallID := range childToolCallIDs {
		m.registerAgentToolChild(tc.ID, childToolCallID)
	}
	return childToolCallIDs
}

func (m *UI) resolveAgentToolContainerID(toolCallID string) string {
	toolCallID = strings.TrimSpace(toolCallID)
	if toolCallID == "" {
		return ""
	}
	if item := m.chat.MessageItem(toolCallID); item != nil {
		if _, ok := item.(chat.NestedToolContainer); ok {
			return toolCallID
		}
	}
	if m.agentToolChildren != nil {
		if parentToolCallID := strings.TrimSpace(m.agentToolChildren[toolCallID]); parentToolCallID != "" {
			return parentToolCallID
		}
	}
	return ""
}

func (m *UI) findAgentToolItem(containerID string) (chat.NestedToolContainer, chat.ToolMessageItem) {
	if containerID == "" {
		return nil, nil
	}
	item := m.chat.MessageItem(containerID)
	if item == nil {
		return nil, nil
	}
	agentItem, ok := item.(chat.NestedToolContainer)
	if !ok {
		return nil, nil
	}
	toolItem, ok := item.(chat.ToolMessageItem)
	if !ok {
		return nil, nil
	}
	return agentItem, toolItem
}

func agentToolChildCallIDs(tc message.ToolCall) []string {
	if tc.ID == "" {
		return nil
	}
	if tc.Name != agentcore.AgentToolName {
		return []string{tc.ID}
	}

	var params agentcore.AgentParams
	if err := json.Unmarshal([]byte(tc.Input), &params); err != nil || len(params.Tasks) == 0 {
		return []string{tc.ID}
	}

	hasDeps := false
	for _, task := range params.Tasks {
		if len(task.DependsOn) > 0 {
			hasDeps = true
			break
		}
	}
	childIDs := make([]string, 0, len(params.Tasks))
	if !hasDeps {
		for i := range params.Tasks {
			childIDs = append(childIDs, fmt.Sprintf("%s-%d", tc.ID, i+1))
		}
		return childIDs
	}

	for i, task := range params.Tasks {
		taskID := strings.TrimSpace(task.ID)
		if taskID == "" {
			taskID = fmt.Sprintf("task-%d", i+1)
		}
		childIDs = append(childIDs, fmt.Sprintf("%s-%s", tc.ID, taskID))
	}
	return childIDs
}

func parseChildSessionID(sessionID string) (messageID string, toolCallID string, ok bool) {
	parts := strings.Split(sessionID, "$$")
	if len(parts) != 2 {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func firstContentLine(text string) string {
	for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return ""
}
