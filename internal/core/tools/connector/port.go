// Package connector defines the unified port every external-system tool
// bundle plugs into. In SysML terms this is the interface block between the
// tool tree (blocks like ssh/, gitea/, gitops/) and the registry: a connector
// owns its own client state (connection pools, CLI plumbing) and exposes
// nothing but tools plus an explicit lifecycle.
//
// Today ssh and gitea are wired directly as plugin blocks in registry.go;
// this port exists so they — and future bundles (git host B, mail, calendar)
// — can migrate behind one shape without touching the composition point
// again. Migration rule stays the same as the whole tree: connectors may not
// import core/agent, charm.land/fantasy is allowed only in implementations
// (not in this file — keep the port pure).
package connector

import (
	"context"

	"charm.land/fantasy"
)

// Connector is a self-contained external-system tool bundle.
//
// Lifecycle contract: Build is called once per registry build; Start is
// optional (return nil when nothing to warm up); Close is always called from
// Registry.StopAll exactly once, after which the connector must not be reused.
type Connector interface {
	// Name is the connector's stable identifier (used in logs and status).
	Name() string

	// Tools returns the agent tools this connector contributes.
	Tools(ctx context.Context) []fantasy.AgentTool
}

// Startable is implemented by connectors that own resources needing warm-up
// (connection pools, CLI discovery). Mirrors tools.Startable at the plugin
// level so the registry can drive both through one loop.
type Startable interface {
	Start(ctx context.Context) error
}

// Closer is implemented by connectors that own resources needing release
// (SSH pools, subprocess handles). Close must be idempotent.
type Closer interface {
	Close(ctx context.Context) error
}

// The registry-side adapter contract: a connector plugin wraps a Connector
// and adapts it to the ToolPlugin shape without the connector knowing about
// ToolDeps. This keeps the composition point the ONLY place that sees deps.
type Adapter interface {
	Connector() Connector
}
