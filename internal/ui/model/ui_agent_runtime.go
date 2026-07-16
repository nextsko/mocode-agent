package model

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	agentcore "github.com/package-register/mocode/internal/core/agent"
	"github.com/package-register/mocode/internal/core/config"
	"github.com/package-register/mocode/internal/domain/session"
	"github.com/package-register/mocode/internal/domain/session/message"
	"github.com/package-register/mocode/internal/ui/chat"
	"github.com/package-register/mocode/internal/ui/panel"
)

func (m *UI) ensureAgentRuntimeState(sessionID string) *sessionAgentRuntimeState {
	if sessionID == "" {
		return nil
	}
	if m.agentRuntimes == nil {
		m.agentRuntimes = make(map[string]*sessionAgentRuntimeState)
	}
	if state, ok := m.agentRuntimes[sessionID]; ok {
		return state
	}
	state := &sessionAgentRuntimeState{
		order:   make([]string, 0, 4),
		entries: make(map[string]*agentRuntimeEntry),
	}
	m.agentRuntimes[sessionID] = state
	return state
}

func (m *UI) registerAgentToolParent(toolCallID, sessionID string) {
	toolCallID = strings.TrimSpace(toolCallID)
	sessionID = strings.TrimSpace(sessionID)
	if toolCallID == "" || sessionID == "" {
		return
	}
	if m.agentToolParents == nil {
		m.agentToolParents = make(map[string]string)
	}
	m.agentToolParents[toolCallID] = sessionID
}

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

func (m *UI) registerAgentToolTaskID(toolCallID, taskID string) {
	toolCallID = strings.TrimSpace(toolCallID)
	taskID = strings.TrimSpace(taskID)
	if toolCallID == "" || taskID == "" {
		return
	}
	if m.agentToolTaskIDs == nil {
		m.agentToolTaskIDs = make(map[string]string)
	}
	m.agentToolTaskIDs[toolCallID] = taskID
}

func (m *UI) registerAgentToolTaskSummary(parentToolCallID, taskID, summary string) {
	parentToolCallID = strings.TrimSpace(parentToolCallID)
	taskID = strings.TrimSpace(taskID)
	summary = strings.TrimSpace(summary)
	if parentToolCallID == "" || taskID == "" || summary == "" {
		return
	}
	if m.agentToolSummaries == nil {
		m.agentToolSummaries = make(map[string]map[string]string)
	}
	taskSummaries := m.agentToolSummaries[parentToolCallID]
	if taskSummaries == nil {
		taskSummaries = make(map[string]string)
		m.agentToolSummaries[parentToolCallID] = taskSummaries
	}
	taskSummaries[taskID] = summary
}

func (m *UI) registerAgentToolTopology(messageID, sessionID string, tc message.ToolCall) []string {
	if messageID == "" {
		return nil
	}
	childToolCallIDs := agentToolChildCallIDs(tc)
	for _, childToolCallID := range childToolCallIDs {
		if sessionID != "" {
			m.registerAgentToolParent(childToolCallID, sessionID)
		}
		m.registerAgentToolChild(tc.ID, childToolCallID)
	}
	return childToolCallIDs
}

func (m *UI) agentTaskPanelsForRender(parentToolCallID string, params agentcore.AgentParams, nestedTools []chat.ToolMessageItem) []panel.AgentPanelData {
	panels := chat.BuildAgentTaskPanels(parentToolCallID, params, nestedTools, func(toolCallID string) string {
		if m.agentToolTaskIDs == nil {
			return ""
		}
		return strings.TrimSpace(m.agentToolTaskIDs[toolCallID])
	}, func(taskID string) string {
		if m.agentToolSummaries == nil {
			return ""
		}
		return strings.TrimSpace(m.agentToolSummaries[parentToolCallID][taskID])
	})
	if len(panels) == 0 {
		return chat.BuildLegacyAgentPanels(parentToolCallID, params, nestedTools)
	}
	return panels
}

func (m *UI) updateAgentRuntime(sessionID, agentID, displayName string, status agentRuntimeStatus, toolName string, activity time.Time) *agentRuntimeEntry {
	if sessionID == "" {
		return nil
	}
	state := m.ensureAgentRuntimeState(sessionID)
	if state == nil {
		return nil
	}
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		agentID = "main"
	}
	entry, ok := state.entries[agentID]
	if !ok {
		name := strings.TrimSpace(displayName)
		if name == "" {
			name = m.lookupAgentDisplayName(agentID)
		}
		entry = &agentRuntimeEntry{
			ID:             agentID,
			DisplayName:    name,
			FirstSeenOrder: len(state.order),
		}
		state.entries[agentID] = entry
		state.order = append(state.order, agentID)
	}
	if displayName != "" {
		entry.DisplayName = displayName
	}
	if entry.DisplayName == "" {
		entry.DisplayName = m.lookupAgentDisplayName(agentID)
	}
	entry.Status = status
	entry.ToolName = strings.TrimSpace(toolName)
	entry.LatestActivity = activity
	switch status {
	case agentRuntimeThinking:
		entry.Summary = "thinking"
	case agentRuntimeExecuting:
		if entry.ToolName != "" {
			entry.Summary = "executing " + entry.ToolName
		} else {
			entry.Summary = "executing"
		}
	case agentRuntimeStopped:
		if entry.Summary == "" {
			entry.Summary = "stopped"
		}
	}
	return entry
}

func (m *UI) parentSessionIDForChild(childSessionID, parentMessageID, toolCallID string) string {
	if toolCallID != "" && m.agentToolParents != nil {
		if sessionID := strings.TrimSpace(m.agentToolParents[toolCallID]); sessionID != "" {
			return sessionID
		}
	}
	_ = childSessionID
	_ = parentMessageID
	return ""
}

func (m *UI) trackChildSessionRuntime(parentSessionID, childSessionID string, parentTool *message.ToolCall, msg message.Message) {
	if parentSessionID == "" || childSessionID == "" {
		return
	}
	parentToolCallID := ""
	if parentTool != nil {
		parentToolCallID = strings.TrimSpace(parentTool.ID)
	}
	taskID := childSessionTaskID(parentTool, childSessionID)

	now := time.Now()
	if msg.UpdatedAt > 0 {
		now = time.Unix(msg.UpdatedAt, 0)
	} else if msg.CreatedAt > 0 {
		now = time.Unix(msg.CreatedAt, 0)
	}
	if len(msg.ToolCalls()) == 0 && len(msg.ToolResults()) == 0 && msg.Role == message.Assistant {
		displayName, fallbackSummary := childSessionDescriptor(parentTool, childSessionID)
		summary := firstContentLine(msg.Content().Text)
		if summary == "" {
			summary = fallbackSummary
		}
		status := agentRuntimeExecuting
		if msg.IsThinking() {
			status = agentRuntimeThinking
			if summary == "" {
				summary = "thinking"
			}
		}
		if msg.IsFinished() {
			status = agentRuntimeStopped
			if summary == "" {
				summary = "completed"
			}
		}
		entry := m.updateAgentRuntime(parentSessionID, childSessionID, displayName, status, "", now)
		if entry != nil && summary != "" {
			entry.Summary = summary
		}
		m.registerAgentToolTaskSummary(parentToolCallID, taskID, summary)
	}
	for _, tc := range msg.ToolCalls() {
		m.registerAgentToolParent(tc.ID, parentSessionID)
		m.registerAgentToolTaskID(tc.ID, taskID)
		displayName, summary := childAgentIdentity(tc)
		entry := m.updateAgentRuntime(parentSessionID, childSessionID, displayName, agentRuntimeExecuting, tc.Name, now)
		if entry == nil {
			continue
		}
		if summary != "" {
			entry.Summary = summary
			m.registerAgentToolTaskSummary(parentToolCallID, taskID, summary)
		}
	}
	for _, tr := range msg.ToolResults() {
		entry := m.updateAgentRuntime(parentSessionID, childSessionID, "", agentRuntimeStopped, tr.Name, now)
		if entry == nil {
			continue
		}
		if text := strings.TrimSpace(tr.Content); text != "" {
			entry.Summary = text
			m.registerAgentToolTaskSummary(parentToolCallID, taskID, text)
		} else if entry.Summary == "" {
			entry.Summary = "stopped"
		}
	}
}

func (m *UI) lookupAgentDisplayName(agentID string) string {
	for _, info := range m.com.Workspace.AvailableAgents() {
		if info.ID == agentID {
			if info.Name != "" {
				return info.Name
			}
			break
		}
	}
	if agentID == "" {
		return "Agent"
	}
	return agentID
}

func childAgentIdentity(tc message.ToolCall) (displayName string, summary string) {
	displayName = strings.TrimSpace(tc.AgentName)
	summary = strings.TrimSpace(tc.Name)

	if tc.Name != "agent" {
		if displayName == "" {
			displayName = "Agent"
		}
		return displayName, summary
	}

	var params struct {
		Prompt string `json:"prompt"`
	}
	if err := json.Unmarshal([]byte(tc.Input), &params); err == nil {
		if prompt := strings.TrimSpace(params.Prompt); prompt != "" {
			summary = prompt
		}
	}

	if displayName == "" {
		displayName = "Agent"
	}
	return displayName, summary
}

func (m *UI) currentSessionAgentEntries() []*agentRuntimeEntry {
	if !m.hasSession() {
		return nil
	}
	state := m.agentRuntimes[m.session.ID]
	if state == nil {
		return nil
	}
	entries := make([]*agentRuntimeEntry, 0, len(state.order))
	for _, id := range state.order {
		if entry := state.entries[id]; entry != nil {
			entries = append(entries, entry)
		}
	}
	return entries
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

func childSessionDescriptor(parentTool *message.ToolCall, childSessionID string) (displayName string, summary string) {
	displayName = "Agent"
	if parentTool == nil || parentTool.Name != agentcore.AgentToolName {
		return displayName, summary
	}

	var params agentcore.AgentParams
	if err := json.Unmarshal([]byte(parentTool.Input), &params); err != nil {
		return displayName, summary
	}
	if len(params.Tasks) == 0 {
		if prompt := strings.TrimSpace(params.Prompt); prompt != "" {
			summary = prompt
		}
		return config.AgentTask, summary
	}

	_, childToolCallID, ok := parseChildSessionID(childSessionID)
	if !ok {
		childToolCallID = childSessionID
	}
	hasDeps := false
	for _, task := range params.Tasks {
		if len(task.DependsOn) > 0 {
			hasDeps = true
			break
		}
	}
	for i, task := range params.Tasks {
		var expectedID string
		if hasDeps {
			taskID := strings.TrimSpace(task.ID)
			if taskID == "" {
				taskID = fmt.Sprintf("task-%d", i+1)
			}
			expectedID = fmt.Sprintf("%s-%s", parentTool.ID, taskID)
		} else {
			expectedID = fmt.Sprintf("%s-%d", parentTool.ID, i+1)
		}
		if expectedID != childToolCallID {
			continue
		}
		if agentName := strings.TrimSpace(task.AgentName); agentName != "" {
			displayName = agentName
		} else {
			displayName = config.AgentTask
		}
		return displayName, strings.TrimSpace(task.Prompt)
	}

	return displayName, summary
}

func childSessionTaskID(parentTool *message.ToolCall, childSessionID string) string {
	if parentTool == nil || parentTool.Name != agentcore.AgentToolName {
		return ""
	}

	var params agentcore.AgentParams
	if err := json.Unmarshal([]byte(parentTool.Input), &params); err != nil || len(params.Tasks) == 0 {
		return ""
	}

	_, childToolCallID, ok := parseChildSessionID(childSessionID)
	if !ok {
		childToolCallID = childSessionID
	}

	hasDeps := false
	for _, task := range params.Tasks {
		if len(task.DependsOn) > 0 {
			hasDeps = true
			break
		}
	}

	for i, task := range params.Tasks {
		taskID := strings.TrimSpace(task.ID)
		if taskID == "" {
			taskID = fmt.Sprintf("task-%d", i+1)
		}

		expectedID := fmt.Sprintf("%s-%d", parentTool.ID, i+1)
		if hasDeps {
			expectedID = fmt.Sprintf("%s-%s", parentTool.ID, taskID)
		}
		if expectedID == childToolCallID {
			return taskID
		}
	}

	return ""
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

func sessionIDOrEmpty(sess *session.Session) string {
	if sess == nil {
		return ""
	}
	return sess.ID
}
