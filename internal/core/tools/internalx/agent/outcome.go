package agenttools

// toolOutcome is the framework-neutral result shared by the tool
// implementations in this package. The port constructors convert it to
// contract.ToolResult, and the agent adapter maps that to the LLM runtime.
type toolOutcome struct {
	Text     string
	Err      bool
	Meta     any
	StopTurn bool
}

// okOutcome returns a successful text outcome.
func okOutcome(text string) toolOutcome { return toolOutcome{Text: text} }

// errOutcome returns a tool-level error outcome.
func errOutcome(text string) toolOutcome { return toolOutcome{Text: text, Err: true} }

// deniedOutcome is the "user denied permission" result: a tool error that also
// ends the turn.
func deniedOutcome() toolOutcome {
	return toolOutcome{Text: "User denied permission", Err: true, StopTurn: true}
}

// withMeta attaches structured metadata to an outcome.
func withMeta(o toolOutcome, meta any) toolOutcome {
	o.Meta = meta
	return o
}
