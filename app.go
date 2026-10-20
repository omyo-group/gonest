package gonest

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/omyo/gonest/internal/module"
	"github.com/omyo/gonest/internal/pipeline"
)

// App is a bootstrapped Gonest application.
//
//	app, err := gonest.New(AppModule)
//	if err != nil { log.Fatal(err) }
//	log.Fatal(app.Listen(":8080"))
type App struct {
	cfg AppConfig
	rt  *module.Runtime

	shutdownOnce sync.Once
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
	// BodyLimit caps JSON request bodies in bytes (default 1 MiB, see WithBodyLimit).
	BodyLimit int64
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

// DefaultBodyLimit is the JSON request body limit applied unless WithBodyLimit
// says otherwise: 1 MiB.
const DefaultBodyLimit int64 = 1 << 20

// WithBodyLimit caps JSON request bodies at n bytes; larger requests get
// 413 Request Entity Too Large. n <= 0 disables the limit. Default: DefaultBodyLimit.
func WithBodyLimit(n int64) Option {
	return func(c *AppConfig) { c.BodyLimit = n }
}

// WithGlobalPrefix prefixes every route, e.g. "/api/v1".
func WithGlobalPrefix(p string) Option { return func(c *AppConfig) { c.GlobalPrefix = p } }

// WithoutStartupLog disables the route table logged at boot.
func WithoutStartupLog() Option { return func(c *AppConfig) { c.DisableStartupLog = true } }

// New builds the module graph rooted at root, registers and validates every
// provider, instantiates controllers, mounts routes and runs OnModuleInit and
// OnApplicationBootstrap hooks. Any failure aborts startup with a descriptive error.
func New(root *Module, opts ...Option) (*App, error) {
	cfg := AppConfig{ShutdownTimeout: defaultShutdownTimeout, BodyLimit: DefaultBodyLimit}
	for _, o := range opts {
		if o != nil {
			o(&cfg)
		}
	}
	if cfg.ShutdownTimeout <= 0 {
		cfg.ShutdownTimeout = defaultShutdownTimeout
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.Engine == nil {
		cfg.Engine = newEngine()
	}
	rt, err := module.Bootstrap(context.Background(), root, module.Options{
		Engine:       cfg.Engine,
		Logger:       cfg.Logger,
		GlobalPrefix: cfg.GlobalPrefix,
		BodyLimit:    cfg.BodyLimit,
		Globals: pipeline.Globals{
			Guards:       cfg.Guards,
			Interceptors: cfg.Interceptors,
			Pipes:        cfg.Pipes,
			Filters:      cfg.Filters,
		},
		StartupLog: !cfg.DisableStartupLog,
	})
	if err != nil {
		return nil, err
	}
	cfg.Logger = rt.Logger
	return &App{cfg: cfg, rt: rt}, nil
}

// Engine returns the gin engine, for advanced customisation.
func (a *App) Engine() *gin.Engine { return a.cfg.Engine }

// Handler returns the application as an http.Handler (for httptest or custom servers).
func (a *App) Handler() http.Handler { return a.cfg.Engine }

// Routes returns every mounted route.
func (a *App) Routes() []*RouteInfo { return a.rt.Routes.All() }

// Get resolves a provider of type T from the application, as seen from the
// root module: the root's own providers, providers exported to it by its
// imports, global modules' exports and framework providers. Providers private
// to another module are not reachable.
func Get[T any](a *App) (T, error) {
	return resolve[T](a, TokenOf[T]())
}

// GetNamed resolves a named provider of type T, with the same visibility as Get.
func GetNamed[T any](a *App, name string) (T, error) {
	return resolve[T](a, NamedTokenOf[T](name))
}

// MustGet is Get that panics on error; for main functions and tests.
func MustGet[T any](a *App) T {
	v, err := Get[T](a)
	if err != nil {
		panic(err)
	}
	return v
}
