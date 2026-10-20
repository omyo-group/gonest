// Package core holds the frozen contracts of Gonest: the types and interfaces
// every other package builds on. The public root package (github.com/omyo/gonest)
// re-exports them as aliases, so users never import core directly.
//
// Core sits at the bottom of the import graph: it imports only the standard
// library, gin and validator. Implementations live elsewhere:
//
//	internal/di        dependency injection container   (implements Container)
//	internal/pipeline  HTTP routing and request pipeline (mounts Controllers)
//	internal/module    module graph, visibility, bootstrap
//
// Changes to this package after Wave 0 go through docs/contract-changes.md.
package core
