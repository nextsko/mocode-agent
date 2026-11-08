package browser

import (
	"charm.land/fantasy"

	"github.com/nextsko/mocode-agent/internal/core/agent/agenttooladapter"
	"github.com/nextsko/mocode-agent/internal/core/tools/contract"
)

// asAgentTool wraps a migrated port with the production adapter so tests can
// keep exercising the fantasy AgentTool surface (Run/Info) they historically
// asserted against.
func asAgentTool(port contract.Tool) fantasy.AgentTool {
	return agenttooladapter.New(port, nil)
}
