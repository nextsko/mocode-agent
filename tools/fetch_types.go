package tools

// AgenticFetchToolName is the name of the agentic fetch tool.
const AgenticFetchToolName = "agentic_fetch"

// AgenticFetchParams defines the parameters for the agentic fetch tool.
type AgenticFetchParams struct {
	URL    string `json:"url,omitempty" description:"The URL to fetch content from (optional - if not provided, the agent will search the web)"`
	Prompt string `json:"prompt" description:"The prompt describing what information to find or extract"`
}

// AgenticFetchPermissionsParams defines the permission parameters for the agentic fetch tool.
type AgenticFetchPermissionsParams struct {
	URL    string `json:"url,omitempty"`
	Prompt string `json:"prompt"`
}
