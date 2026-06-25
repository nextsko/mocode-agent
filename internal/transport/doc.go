// Package transport holds entry surfaces: the Cobra CLI (cmd), the local
// admin HTTP settings UI, and the frontend workspace facade. These are the
// top-level ways the outside world reaches the core; they depend downward
// on core, store, domain, and util.
package transport
