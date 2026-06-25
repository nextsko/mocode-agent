// Package memory holds the pure domain types for the agent memory system:
// the Kind/Memory/Entry value types, the request/response DTOs exposed by the
// memory tools, sentinel errors, and the deterministic ID generator.
//
// These types have no I/O and no upward dependencies, so both the persistence
// layer (internal/store) and the memory service implementation
// (internal/core/knowledge/memory) depend downward on this package without
// creating a layer inversion.
package memory
