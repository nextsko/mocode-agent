Start or resume a multi-agent roundtable meeting to discuss, plan, and optionally execute a task as a team.

Use this tool when the user wants several specialist agents to collaborate on a decision or plan. The roundtable runs as a separate meeting with a moderator, specialists, and (optionally) executors. Participants discuss the topic, can propose formal motions, vote, and reach consensus. If executors are configured and a plan is approved, the executors may carry out the plan using write tools.

Parameters:
- topic (required): The subject or task for the roundtable.
- participants (optional): List of seats. Each seat has a name, an agent ID from the Mocode config, a role description, and a can_execute flag. Defaults to a moderator, researcher, reviewer, and executor.
- max_turns (optional): Maximum discussion turns before the meeting is halted. Defaults to 20.
- resume_id (optional): Resume a previously saved roundtable by its snapshot ID.
