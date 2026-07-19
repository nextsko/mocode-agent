Search through your conversation history across all sessions.

Three modes (inferred from parameters):
1. **Discovery**: Pass `query` to perform full-text search across all session messages. Returns matching sessions with context snippets. Zero LLM cost.
2. **Scroll**: Pass `session_id` + `around_message_id` to view a window of messages centered on the anchor. Re-anchor on the first/last message ID to scroll forward/backward.
3. **Browse**: Pass no arguments to see recent sessions with titles, message counts, and timestamps.
