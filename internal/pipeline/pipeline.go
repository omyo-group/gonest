package pipeline

import (
	"log/slog"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
	"github.com/omyo/gonest/internal/core"
)

// Options configures a Mounter.
type Options struct {
	// Engine receives the routes. Required.
	Engine *gin.Engine
	// Logger is the framework logger; nil means slog.Default().
	Logger *slog.Logger
	// Routes receives every mounted route. Required.
	Routes *core.RouteTable
	// GlobalPrefix is prepended to every path, e.g. "/api".
	GlobalPrefix string
	// Validator overrides the shared validator instance; nil means a default one.
	Validator *validator.Validate
}

// Globals are the application-wide pipeline components.
type Globals struct {
	Guards       []core.Guard
	Interceptors []core.Interceptor
	Pipes        []core.Pipe
	Filters      []core.ExceptionFilter
}

// MiddlewareFor returns the gin middleware to run before the pipeline of the
// route with the given method and full path. May be nil.
type MiddlewareFor func(method, fullPath string) []gin.HandlerFunc

// Mounter mounts controllers. Wave 0 stub.
type Mounter struct{ opts Options }

// New creates a Mounter.
func New(opts Options) *Mounter { return &Mounter{opts: opts} }

// SetGlobals installs global components. It may be called before or after
// Mount but must be called before the first request is served.
func (m *Mounter) SetGlobals(g Globals) {}

// Mount calls ctrl.Routes, compiles each route and registers it on the engine
// and in the route table. Duplicate method+path returns an error instead of
// panicking.
func (m *Mounter) Mount(ctrl core.Controller, module string, mw MiddlewareFor) ([]*core.RouteInfo, error) {
	return nil, core.ErrNotImplemented
}

// InstallFallbacks makes unknown routes and methods answer with the standard
// JSON error body (404 / 405).
func (m *Mounter) InstallFallbacks() {}
