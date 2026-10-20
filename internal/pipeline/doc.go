// Package pipeline mounts controllers on a gin engine and runs the request
// pipeline: middleware -> guards -> interceptors (before) -> binding and
// validation -> pipes -> handler -> interceptors (after) -> exception filters.
// Binders and chains are compiled at mount time.
//
// Owner: agent A2.
package pipeline
