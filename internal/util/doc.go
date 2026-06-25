// Package util is a container for cross-cutting infrastructure primitives
// with no upward domain dependencies. Sub-packages are independent leaf
// libraries (string/path helpers, concurrency, fs, logging, event bus, etc.)
// depended on by every higher layer. See internal/README.md for the layering.
package util
