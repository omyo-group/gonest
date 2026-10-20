// Package pipeline mounts controllers on a gin engine and runs the request
// pipeline: middleware -> guards -> interceptors (before) -> binding and
// validation -> pipes -> handler -> interceptors (after) -> exception filters.
// Binders and chains are compiled at mount time.
//
// Mount runs a controller's Routes method against an internal core.Router,
// resolves full paths (global prefix + controller prefix + group prefixes +
// route path), compiles one binder per handler input type (cached by type)
// and registers a single gin handler per route behind the route's middleware.
// Per request, the handler allocates one *core.Ctx, reads the current
// globals from an atomic pointer and walks precomputed slices; there are no
// map lookups by type, no tag parsing and no container access.
//
// Global components may be installed with SetGlobals before or after Mount.
// Every error, including recovered panics, is routed through the exception
// filters (route, controller, global) and finally a default filter that
// writes core.ErrorBody.
//
// Owner: agent A2.
package pipeline
