// Package tools is the framework-agnostic toolkit that hosts every
// LLM-callable tool in the mocode runtime.
//
// # Directory is the architecture (目录即结构)
//
// The layout of this tree IS the architecture. Like a Next.js app where
// app/<route>/page.tsx is the route, here the directory tells you what a
// file is for — no registration file to hunt down:
//
//	tools/                  one builtin tool per file (fetch.go → fetch tool)
//	  registry.go           the composition point: plugin blocks in build order
//	  contracts.go          the target Tool/ToolContext/ToolResult contracts
//	  filter/               composable predicates applied after Build
//	  nethttp/              THE outbound-HTTP port (interface block)
//	  net/                  web tools: fetch, crawl, web_fetch, web_search,
//	                        download, download_docs, sourcegraph
//	  ssh/                  ssh_exec, ssh_upload, ssh_download, ssh_list_hosts
//	  gitea/                gitea_issues, gitea_pulls, gitea_notifications
//	  gitops/               git_plan_commits, git_execute_commits
//	  wechat/               send_wechat_file/image, screenshot_to_wechat
//	                        (built by the coordinator, not the registry)
//	  mcp/                  MCP bridge: sessions, transports, meta tools
//	                        (list/read_mcp_resources, mcp-tools adapter)
//	  lsp/                  LSP manager and its tools
//	  plugins/<x>common/    shared library for one tool category
//	    netcommon/          fetch/search helpers all web tools share
//	    sshcommon/          SSH connection pool + exec helpers
//	    giteacommon/        tea CLI plumbing
//
// Root-level files are the fs/shell/session core that every runtime needs
// (edit, view, bash, job_*, todos, ...). This is deliberate: the file tools
// share one dependency set (permissions + filetracker + history) wired by a
// single plugin block, so splitting them would cut a cohesive unit — they
// stay until the contracts adapter lands. Everything with an external-system
// affinity lives in its category directory; the façade (tools.go) re-exports
// the stable tools.X symbols so consumers outside the tree are insulated
// from the layout.
//
// A file in tools/ root exports New<Tool>Tool(deps...) fantasy.AgentTool
// constructors and one <Tool>ToolName constant. A plugin block in
// registry.go wires exactly those constructors with the ports it needs.
//
// # SysML view: blocks, ports, constraints
//
//   - Block: every ToolPlugin (execPlugin, networkPlugin, sshPlugin, ...) is
//     a block — encapsulated state plus behaviour, composed only through the
//     registry.
//   - Port: ToolDeps fields are the block's ports. HTTP is the canonical
//     example: plugins must derive clients from deps.HTTP (*nethttp.Factory)
//     and never construct transports themselves, so proxy configuration and
//     the connection pool have exactly one owner. sshPlugin owns its
//     connection pool privately and exposes it only through its four tools.
//   - Constraint: scripts/layercheck is the constraint verifier. Besides the
//     layer rule (util ← domain ← store ← core ← transport/ui) it enforces
//     per-package import bans declared below; violations fail the build.
//   - Lifecycle: plugins implementing Startable are started/stopped by
//     Registry.StartAll/StopAll. The coordinator holds ONE registry for its
//     lifetime and drains it via Close on app shutdown.
//
// # Enforced package constraints (see scripts/layercheck)
//
//	tools/nethttp          may not import fantasy, catwalk, core/agent, core/config
//	tools/plugins/netcommon may not import fantasy, catwalk, core/agent
//
// nethttp stays pure (net/http + time only) so any future runtime can sit
// behind it; netcommon may not grow LLM-runtime coupling.
//
// # Honest boundary status
//
// The framework-agnostic ambition is INCOMPLETE and tracked here instead of
// being papered over:
//
//   - Lived today: tools.ToolContext / ToolResult / MCPHandles are real —
//     core/agent/agent_tool_context.go implements the ToolContext port for
//     in-process tool execution.
//   - Not yet: no production tool implements tools.Tool; the 40+ builtin
//     constructors return fantasy.AgentTool directly and this package imports
//     charm.land/fantasy, core/config, and core/agent/toolutil. The planned
//     consolidation point is an agenttool_adapter.go at the agent boundary
//     that converts tools.Tool ⇄ fantasy.AgentTool. Until that adapter lands,
//     layercheck deliberately does NOT ban fantasy imports here; banning them
//     would fail the build ~40 times and teach people to ignore the check.
//   - Coordinator-owned tools (agent, agentic_fetch, transfer_to_agent) are
//     built outside the registry because they call back into coordinator
//     state; their descriptors are mirrored in coordinatorToolNames().
//
// # Adding a new tool
//
//  1. Create <tool>.go next to its siblings, export New<Tool>Tool(...) and
//     <Tool>ToolName, embed <tool>.md for the description.
//  2. Add the descriptor + wiring to the matching plugin block in
//     registry.go (that file is the single composition point).
//  3. If the tool is network-bound, take *nethttp.Factory (or a client
//     derived from it) — never build your own transport.
//  4. Add the tool name to knownAllToolNames in registry_test.go so the
//     config list cross-check stays authoritative.
//
// # Module
//
// This package lives in the same Go module as the agent runtime;
// the import path below is the only supported entry point.
package tools // import "github.com/nextsko/mocode-agent/internal/core/tools"
