// Package tools is the framework-agnostic toolkit that hosts every
// LLM-callable tool in the mocode runtime.
//
// # Directory is the architecture (目录即结构)
//
// The layout of this tree IS the architecture — the three-directory contract
// (core / internalx / external) maps one-to-one onto the capability model:
//
//	tools/                  composition + contracts only (no tool impls)
//	  registry.go           THE composition point: plugin blocks in order
//	  registry_filter.go    post-Build filtering
//	  contracts.go          target Tool/ToolContext/ToolResult contracts
//	  runtime.go result.go  runtime dep-set plumbing
//	  tools.go              façade: re-exports keep tools.X paths stable
//	  doc.go                this file — the single structure document
//
//	core/                   CORE tools (runnable with zero externals)
//	  fs/                   file tools: edit multiedit write view read_files
//	                        ls glob grep
//	  shell/                bash + background jobs: bash bash_safe job_input
//	                        job_kill job_output
//	  web/                  web→md tools: fetch crawl web_fetch web_search
//	                        download download_docs sourcegraph
//	                        (+ crawler/ shared crawling foundation)
//
//	internalx/              INTERNAL tools (agent-adjacent capabilities)
//	  lsp/                  LSP tools + manager (+ lsputil, references)
//	  agent/                think todos learn transfer diagnostics
//	                        mocode_info mocode_logs session_export
//	                        session_summary session_search message_export
//	  filter/               composable predicates applied after Build
//	  modes/                (future, P6) moa | team | tree agent modes
//	  domain/               (future, P6) domain tools
//
//	external/               EXTERNAL systems
//	  mcp/                  MCP bridge: sessions, transports, meta tools
//	  connector/            the Connector port (systems plug in here)
//	  nethttp/              THE outbound-HTTP port (interface block)
//	  systems/              ssh gitea gitops wechat browser
//	  plugins/              shared libs per category
//	    netcommon/          fetch/search providers all web tools share
//	    searchcommon/       ripgrep/pure-Go search stack (grep, glob,
//	                        lsp references)
//	    sshcommon/          SSH connection pool + exec helpers
//	    giteacommon/        tea CLI plumbing
//	    gitopscommon/       git commit classification
//
// A tool file exports New<Tool>Tool(deps...) constructors plus one
// <Tool>ToolName constant with its .md description sibling. A plugin block
// in registry.go wires exactly those constructors with the ports it needs.
// The façade (tools.go) re-exports the stable tools.X symbols so consumers
// outside the tree are insulated from the layout — types MUST use
// `type X = pkg.X` (identity preserved for UI type switches).
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
//	domain/...             may not import charm.land/* (LLM framework/SDK types)
//	core/agent/messages    may not import charm.land/* (helpers live in llmconv)
//	tool packages          ported and framework-free. Enforced by layercheck:
//	  tools/internalx/agent, tools/internalx/{lsp,filter}, tools/core/{fs,
//	  web,shell,sandbox,question}, tools/external/systems/{ssh,gitea,gitops,
//	  browser,wechat}, tools/external/common/ssh, shellruntime/screencap and
//	  tools/external/mcp (the dynamic bridge maps image/media payloads to
//	  ToolResult.Attachments).
//
// nethttp stays pure (net/http + time only) so any future runtime can sit
// behind it; netcommon may not grow LLM-runtime coupling.
//
// # Honest boundary status
//
// The framework-agnostic ambition is INCOMPLETE and tracked here instead of
// being papered over:
//
//   - Lived today: the contract types (Tool/ToolContext/ToolResult/Schema)
//     moved to the leaf package core/tools/contract and are re-exported here
//     via type aliases, so implementations can import them without a cycle.
//     core/agent/agent_tool_context.go implements the ToolContext port, and
//     core/agent/agenttooladapter converts a tools.Tool into fantasy.AgentTool.
//   - Closed: every tool in internalx/agent (12/12) is implemented as a
//     tools.Tool and wrapped by the adapter; the package no longer imports
//     charm.land/fantasy, layercheck enforces that, and the legacy fantasy
//     constructors were removed. toolutil's fantasy-backed helpers are still
//     used by other (not-yet-ported) tool packages.
//   - Remaining: the other 40+ builtin constructors still return
//     fantasy.AgentTool directly, and this package still imports fantasy,
//     core/config, and core/agent/toolutil. Migrate tools one at a time to
//     tools.Tool and drop the legacy constructors; only then can layercheck
//     ban fantasy imports here.
//   - Adapter wiring: the coordinator injects a context factory, so every
//     Execute call gets a fresh ToolContext carrying the session id (from the
//     run context), the working directory, and a permission checker that
//     prompts through permission.Service (deny-by-default). MCP handles and
//     telemetry callbacks are still TODO and degrade to no-ops.
//   - Coordinator-owned tools (agent, agentic_fetch, transfer_to_agent) are
//     built outside the registry because they call back into coordinator
//     state; their descriptors are mirrored in coordinatorToolNames().
//
// # Adding a new tool
//
//  1. Place <tool>.go in the matching category directory (core/internalx/
//     external by the affinity table), export New<Tool>Tool(...) and
//     <Tool>ToolName, embed <tool>.md for the description.
//  2. Add the descriptor + wiring to the matching plugin block in
//     registry.go (that file is the single composition point).
//  3. If the tool is network-bound, take *nethttp.Factory (or a client
//     derived from it) — never build your own transport.
//  4. Add the tool name to BOTH config.allToolNames() (which drives every
//     agent's AllowedTools, so a name missing there is filtered out and the
//     model never sees the tool) and knownAllToolNames in registry_test.go.
//     TestAllToolNames_MatchesConfigList asserts the two lists are equal, but
//     it only compares against knownAllToolNames, so it cannot catch drift
//     between knownAllToolNames and config on its own — update both by hand.
//  5. If the tool writes to disk, gate it through permission.Service.Request
//     the way write/learn do, so the write is an explicit, auditable decision.
//
// # Module
//
// This package lives in the same Go module as the agent runtime;
// the import path below is the only supported entry point.
package tools // import "github.com/nextsko/mocode-agent/internal/core/tools"
