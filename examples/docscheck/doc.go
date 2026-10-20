// Package docscheck verifies that every Go snippet in docs/guide and README.md
// is a verbatim copy of a `// docs:<name>` region of compiled code under
// examples/. In Markdown, a snippet is written as
//
//	<!-- snippet: examples/saas/internal/auth/guards.go#jwt-guard -->
//	```go
//	...
//	```
//
// Run `go test ./examples/docscheck/` after editing either side.
package docscheck
