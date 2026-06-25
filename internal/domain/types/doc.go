// Package types contains cross-module shared type definitions.
//
// # Purpose
//
// The types in this package are DTOs (Data Transfer Objects) designed to be
// used across module boundaries without creating tight coupling. They are
// especially useful for:
//
//   - Public APIs (re-exported via pkg/types)
//   - SDK consumers
//   - Plugin authors
//   - Cross-module data exchange where ContentPart or other sealed interfaces
//     are not needed
//
// # Currently Exports
//
//   - ToolResult — unified tool execution result DTO
//
// # When to Add a Type Here
//
// A type belongs in this package if and only if ALL of the following are true:
//
//  1. It is used across at least 2 different module boundaries
//     (e.g., session/message + web + admin)
//  2. It does not need to implement any sealed interface
//     (i.e., it does not need to implement ContentPart or similar interfaces
//     that use unexported marker methods like isPart())
//  3. It has a clear, stable contract that external consumers can rely on
//  4. The cost of keeping it in two places (internal/* and pkg/types) is
//     higher than the cost of adding it here
//
// # When NOT to Add a Type Here
//
// Do NOT add a type to this package if:
//
//   - It only implements ContentPart or another sealed interface — use
//     session/message instead
//   - It has strong coupling to a specific business context (e.g.,
//     agenttools.ToolDescriptor is tightly coupled to agent's tool registry)
//   - It is a private implementation detail of one package
//   - The "shared" usage is just 1-2 callers (not worth the indirection)
//
// # Two-Layer ToolResult Architecture
//
// mocode has three ToolResult types by design:
//
//  1. session/message.ToolResult — RUNTIME, implements ContentPart (~80+ uses)
//  2. types.ToolResult — DTO, public, no ContentPart (this package)
//
// These cannot be unified via type alias due to Go's sealed interface
// pattern. See internal/types/tool.go for full details.
package types
