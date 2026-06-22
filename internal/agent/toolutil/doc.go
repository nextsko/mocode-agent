// Package toolutil provides low-level utility helpers, context propagation,
// capability discovery, callback decorators, retry policies, and LSP helpers
// shared across all agent tool implementations.
//
// This package must not import any package from the agent/tools tree to avoid
// import cycles.
package toolutil
