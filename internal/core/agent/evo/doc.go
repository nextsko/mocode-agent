// Package evo implements the self-evolution (/evo) mode runtime.
//
// The /evo mode is a special session in which an agent iterates on itself:
// it observes its own runs, reconstructs its context toward an "optimal
// theory", captures emergent skills, and on exit fixes (固化) the resulting
// agent into a local directory under ~/local/mocode/evo/agent-<name>/ as a
// revisioned snapshot. Later sessions load a fixed agent like a mode, with its
// optimal context already baked in.
//
// This package holds the mode-agnostic core: the agent manifest, the
// fixation store (a revisioned, active-pointer snapshot store modeled on
// trpc-agent-go's evolution CandidateStore/ActivePointer), and the mode
// state machine. The TUI wiring (theme swap, /evo command, second-confirm
// exit) lives in internal/ui.
package evo
