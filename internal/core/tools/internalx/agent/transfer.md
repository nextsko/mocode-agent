Transfer control to another agent mode. The target agent takes over the conversation with its own system prompt, tools, and model configuration.

<when_to_use>
Use this tool when:
- The current task is better handled by a specialized agent
- You need capabilities only available in another agent mode
- The user's request falls outside your current mode's expertise
- You want to delegate a sub-task to a more appropriate agent

DO NOT use this tool when:
- You can handle the task with your current tools and capabilities
- The target agent has the same capabilities as the current one
- You've already transferred recently (avoid transfer loops)
</when_to_use>

<usage>
- Provide the target agent name (must be in the sub_agents list)
- Optionally provide a message with context for the target agent
</usage>

<features>
- Seamless handoff between agent modes
- Context preservation across transfers
- Anti-loop protection (max 5 transfers per conversation)
- Target agent inherits the full conversation history
</features>

<limitations>
- Can only transfer to agents listed in the current mode's sub_agents
- Maximum 5 transfers per conversation to prevent loops
- Cannot transfer back to the same agent immediately
</limitations>
