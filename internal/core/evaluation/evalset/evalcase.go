// Package evalset defines evaluation cases and sets for the mocode evaluation system.
package evalset

// EvalCase is a single evaluation case: a user input plus expected agent behavior.
// It is the atomic unit an evaluation run executes and scores.
type EvalCase struct {
	// ID uniquely identifies this case within its EvalSet.
	ID string
	// UserContent is the input prompt fed to the agent under test.
	UserContent string
	// SystemPrompt optionally overrides the agent system prompt for this case.
	// Empty means the agent keeps its configured prompt.
	SystemPrompt string
	// ExpectedTools lists tool calls the agent is expected to make, in order.
	// An empty slice means the tool trajectory is not asserted.
	ExpectedTools []ExpectedToolCall
	// ExpectedText is the expected final text response.
	// Compared according to the bound FinalResponse criterion mode.
	ExpectedText string
	// Rubrics are judge-readable scoring criteria consumed by an LLMJudge criterion.
	Rubrics []string
}

// ExpectedToolCall describes a tool call an agent is expected to make.
type ExpectedToolCall struct {
	// Name is the required tool name.
	Name string
	// Input is an optional substring that the tool call input must contain.
	// Empty means input content is not asserted.
	Input string
}

// HasExpectedTools reports whether this case asserts a tool trajectory.
func (c *EvalCase) HasExpectedTools() bool {
	return len(c.ExpectedTools) > 0
}

// HasExpectedText reports whether this case asserts a final text response.
func (c *EvalCase) HasExpectedText() bool {
	return c.ExpectedText != ""
}
