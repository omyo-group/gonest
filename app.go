package gonest

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/omyo/gonest/internal/core"
)

// App is a bootstrapped Gonest application.
//
//	app, err := gonest.New(AppModule)
//	if err != nil { log.Fatal(err) }
//	log.Fatal(app.Listen(":8080"))
type App struct {
	cfg AppConfig
	// Implementation fields are added by agent B1 (internal/module).
}

// AppConfig is the result of applying Options. Exposed for internal packages.
type AppConfig struct {
	Engine          *gin.Engine
	Logger          *slog.Logger
	ShutdownTimeout time.Duration
	Guards          []Guard
	Interceptors    []Interceptor
	Pipes           []Pipe
	Filters         []ExceptionFilter
	// GlobalPrefix is prepended to every route path, e.g. "/api".
	GlobalPrefix string
	// DisableStartupLog silences the route table logged at boot.
	DisableStartupLog bool
}

// Option configures New.
type Option func(*AppConfig)

// WithEngine uses an existing gin engine instead of gin.New().
func WithEngine(e *gin.Engine) Option { return func(c *AppConfig) { c.Engine = e } }

// WithLogger sets the framework logger (default slog.Default()). If the module
// graph provides a *slog.Logger (for example logger.ForRoot), that one wins.
func WithLogger(l *slog.Logger) Option { return func(c *AppConfig) { c.Logger = l } }

// WithShutdownTimeout bounds graceful shutdown (default 10s).
func WithShutdownTimeout(d time.Duration) Option {
	return func(c *AppConfig) { c.ShutdownTimeout = d }
}

// WithGlobalGuards adds guards that run before controller and route guards.
func WithGlobalGuards(g ...Guard) Option {
	return func(c *AppConfig) { c.Guards = append(c.Guards, g...) }
}

// WithGlobalInterceptors adds outermost interceptors.
func WithGlobalInterceptors(i ...Interceptor) Option {
	return func(c *AppConfig) { c.Interceptors = append(c.Interceptors, i...) }
}

// WithGlobalPipes adds pipes that run before controller and route pipes.
func WithGlobalPipes(p ...Pipe) Option {
	return func(c *AppConfig) { c.Pipes = append(c.Pipes, p...) }
}

// WithGlobalFilters adds filters tried after route and controller filters.
func WithGlobalFilters(f ...ExceptionFilter) Option {
	return func(c *AppConfig) { c.Filters = append(c.Filters, f...) }
}

// WithGlobalPrefix prefixes every route, e.g. "/api/v1".
func WithGlobalPrefix(p string) Option { return func(c *AppConfig) { c.GlobalPrefix = p } }

// WithoutStartupLog disables the route table logged at boot.
func WithoutStartupLog() Option { return func(c *AppConfig) { c.DisableStartupLog = true } }

// New builds the module graph rooted at root, registers and validates every
// provider, instantiates controllers, mounts routes and runs OnModuleInit and
// OnApplicationBootstrap hooks. Any failure aborts startup with a descriptive error.
func New(root *Module, opts ...Option) (*App, error) {
	_ = root
	_ = opts
	return nil, core.ErrNotImplemented
}

// Listen serves HTTP on addr until SIGINT or SIGTERM, then shuts down gracefully
// and runs shutdown hooks. It returns nil after a clean shutdown.
func (a *App) Listen(addr string) error { return core.ErrNotImplemented }

// Serve serves on an existing listener-backed server; useful for tests and
// custom TLS. It blocks until ctx is cancelled, then shuts down gracefully.
func (a *App) Serve(ctx context.Context, srv *http.Server) error {
	return core.ErrNotImplemented
}

// Shutdown runs OnModuleDestroy, BeforeApplicationShutdown and
// OnApplicationShutdown hooks in reverse dependency order. Safe to call once;
// later calls return nil.
func (a *App) Shutdown(ctx context.Context) error { return core.ErrNotImplemented }

// Engine returns the gin engine, for advanced customisation.
func (a *App) Engine() *gin.Engine { return a.cfg.Engine }

// Handler returns the application as an http.Handler (for httptest or custom servers).
func (a *App) Handler() http.Handler { return a.cfg.Engine }

// Routes returns every mounted route.
func (a *App) Routes() []*RouteInfo { return nil }

// Get resolves a provider of type T from the application, as seen from the root module.
func Get[T any](a *App) (T, error) {
	var zero T
	return zero, core.ErrNotImplemented
}

// GetNamed resolves a named provider of type T.
func GetNamed[T any](a *App, name string) (T, error) {
	var zero T
	return zero, core.ErrNotImplemented
}

// MustGet is Get that panics on error; for main functions and tests.
func MustGet[T any](a *App) T {
	v, err := Get[T](a)
	if err != nil {
		panic(err)
	}
	return v
}
