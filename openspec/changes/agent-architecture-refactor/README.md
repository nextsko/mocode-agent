# agent-architecture-refactor

Redesign internal/core/agent/tools into a same-module third-party-style package with interface-driven abstractions (AgentTool, ToolProvider, ToolContext) and a static import + init() registration loader. One-shot migration of 40+ existing tools.
