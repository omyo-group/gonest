// Package module builds the module graph, enforces provider visibility and
// drives bootstrap and shutdown for the root package's App.
//
//   - Build walks Imports and ReExports from the root, dedupes modules by
//     pointer, rejects empty or duplicate names and import cycles, and
//     computes the dependency order (imports before importers, global
//     modules as early as possible).
//   - NewVisibility precomputes which module may inject which provider and
//     validates Exports.
//   - CollectMiddleware invokes every Module.Configure and Middleware.For
//     matches the rules against route paths relative to the global prefix.
//   - Lifecycle runs the startup and shutdown hooks in dependency order.
//   - Bootstrap ties it together in the order documented in docs/contracts.md.
//
// Owner: agent B1.
package module
