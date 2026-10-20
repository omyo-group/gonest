# Modules

A module groups providers and controllers, exactly like a NestJS
`@Module`. It is a plain struct value:

```text
gonest.Module{
    Name        string             // required, unique in the application
    Imports     []*gonest.Module   // modules whose exports this module can inject
    Providers   []gonest.Provider  // registered in this module
    Controllers []any              // constructors of values implementing gonest.Controller
    Exports     []gonest.Token     // which of this module's providers importers may inject
    ReExports   []*gonest.Module   // imported modules passed on to importers
    Global      bool               // exports visible everywhere without importing
    Configure   any                // func(*gonest.MiddlewareConsumer, deps...) binding gin middleware
}
```

Modules are identified by pointer: importing the same `*Module` from several
places yields one instance and one set of singletons.

## A feature module

<!-- snippet: examples/saas/internal/projects/module.go#projects-module -->
```go
// Module wires the projects feature. Repository is bound to the in-memory
// implementation (NestJS useClass); tenants and auth provide the exported
// TenantService and guards.
var Module = &gonest.Module{
	Name:    "projects",
	Imports: []*gonest.Module{auth.Module, tenants.Module},
	Providers: []gonest.Provider{
		gonest.Bind[Repository](NewMemoryRepository),
		gonest.Provide(NewService),
	},
	Controllers: []any{NewProjectsController},
}
```

`projects` imports `auth` and `tenants`, so its providers and controller can
inject what those modules export: the guards and `*tenants.Service`.
`Repository` is an interface, bound to the in-memory implementation.

## Exports

A provider is private to its module unless the module lists its token in
`Exports`. Tokens are built with `gonest.TokenOf[T]()` (or
`gonest.NamedTokenOf[T](name)` for named providers).

<!-- snippet: examples/saas/internal/auth/module.go#auth-module -->
```go
// Module provides token handling and the guards. JWTGuard and RolesGuard are
// exported so feature modules that import auth.Module can inject them into
// their controllers and pass them to gonest.UseGuards.
var Module = &gonest.Module{
	Name: "auth",
	Providers: []gonest.Provider{
		gonest.Value[Clock](time.Now),
		gonest.Provide(NewTokenService),
		gonest.Provide(NewUserStore),
		gonest.Provide(NewJWTGuard),
		gonest.Provide(NewRolesGuard),
	},
	Controllers: []any{NewAuthController},
	Exports: []gonest.Token{
		gonest.TokenOf[*JWTGuard](),
		gonest.TokenOf[*RolesGuard](),
		gonest.TokenOf[*TokenService](),
	},
}
```

Injecting a provider that exists but is not exported fails at startup:

<!-- snippet: examples/guide/modules_test.go#visibility-error -->
```go
type Secret struct{}
private := &gonest.Module{
	Name:      "private",
	Providers: []gonest.Provider{gonest.Provide(func() *Secret { return &Secret{} })},
	// no Exports
}
type User struct{}
consumer := &gonest.Module{
	Name:      "consumer",
	Imports:   []*gonest.Module{private},
	Providers: []gonest.Provider{gonest.Provide(func(*Secret) *User { return &User{} })},
}
_, err := gonest.New(&gonest.Module{Name: "app", Imports: []*gonest.Module{consumer}}, quiet()...)
fmt.Println(err)
```

```text
gonest: cannot resolve *guide_test.User -> *guide_test.Secret in module consumer: not visible from module consumer (hint: *guide_test.Secret is provided by module private; add it to the Exports of private and import private in module consumer)
```

A module may also export a token it only sees through its imports (it then
re-exports that provider). Every exported token must be provided by the
module or visible from its imports, otherwise `gonest.New` fails.

## Re-exporting a module

<!-- snippet: examples/guide/modules_test.go#reexports -->
```go
// Repo is provided by DatabaseModule.
type Repo struct{}

// DatabaseModule exports *Repo.
var DatabaseModule = &gonest.Module{
	Name:      "database",
	Providers: []gonest.Provider{gonest.Provide(func() *Repo { return &Repo{} })},
	Exports:   []gonest.Token{gonest.TokenOf[*Repo]()},
}

// CoreModule re-exports DatabaseModule (NestJS: exports: [DatabaseModule]),
// so whoever imports CoreModule can inject *Repo too.
var CoreModule = &gonest.Module{
	Name:      "core",
	Imports:   []*gonest.Module{DatabaseModule},
	ReExports: []*gonest.Module{DatabaseModule},
}
```

## Global modules

<!-- snippet: examples/guide/modules_test.go#global-module -->
```go
// Clocks is provided by a global module.
type Clocks struct{}

// ClockModule is global: its exports are injectable everywhere once the
// module is reachable from the root (imported once, usually by the root).
var ClockModule = &gonest.Module{
	Name:      "clock",
	Global:    true,
	Providers: []gonest.Provider{gonest.Provide(func() *Clocks { return &Clocks{} })},
	Exports:   []gonest.Token{gonest.TokenOf[*Clocks]()},
}
```

`config.ForRoot` and `logger.ForRoot` return global modules: after the root
imports them, `*settings.Config` and `*slog.Logger` can be injected anywhere
in the SaaS example. A global module is discovered by walking the imports
from the root, so it must be imported somewhere; global modules are
initialised before the modules that use them.

## Dynamic modules

NestJS `forRoot()`/`register()` become functions returning a `*Module`:

<!-- snippet: examples/guide/modules_test.go#dynamic-module -->
```go
// CacheOptions configure CacheModule.
type CacheOptions struct{ TTL time.Duration }

// Cache is the provider exported by CacheModule.
type Cache struct{ TTL time.Duration }

// CacheModule is a dynamic module (NestJS CacheModule.register()): a
// function returning a *gonest.Module. Call it once and share the pointer:
// two calls produce two distinct modules, which must then have different
// names.
func CacheModule(opts CacheOptions) *gonest.Module {
	return &gonest.Module{
		Name:      "cache",
		Providers: []gonest.Provider{gonest.Provide(func() *Cache { return &Cache{TTL: opts.TTL} })},
		Exports:   []gonest.Token{gonest.TokenOf[*Cache]()},
	}
}
```

Wiring them together:

<!-- snippet: examples/guide/modules_test.go#modules-wiring -->
```go
featureModule := &gonest.Module{
	Name:    "feature",
	Imports: []*gonest.Module{CoreModule, CacheModule(CacheOptions{TTL: time.Minute})},
	Providers: []gonest.Provider{gonest.Provide(func(r *Repo, c *Cache, k *Clocks) *Feature {
		return &Feature{Repo: r, Cache: c, Clocks: k}
	})},
	Exports: []gonest.Token{gonest.TokenOf[*Feature]()},
}
root := &gonest.Module{Name: "app", Imports: []*gonest.Module{ClockModule, featureModule}}
```

The SaaS root module is built the same way, because it contains dynamic
modules configured by `main` and by the tests:

<!-- snippet: examples/saas/internal/app/app.go#root-module -->
```go
// NewModule returns the root module. Dynamic modules (config.ForRoot,
// logger.ForRoot, openapi.Module) are functions returning *gonest.Module, so
// the root is built by a function too.
func NewModule(o Options) *gonest.Module {
	return &gonest.Module{
		Name: "app",
		Imports: []*gonest.Module{
			// Global modules: *settings.Config and *slog.Logger are injectable everywhere.
			config.ForRoot[settings.Config](config.Options{
				Prefix:   settings.EnvPrefix,
				Lookup:   o.Lookup,
				EnvFiles: o.EnvFiles,
			}),
			logger.ForRoot(logger.Options{Level: o.LogLevel, Format: o.LogFormat, Output: o.LogOutput}),

			platform.Module,
			auth.Module,
			tenants.Module,
			projects.Module,

			openapi.Module(openapi.Options{
				Info: openapi.Info{
					Title:       "Gonest SaaS example",
					Version:     "1.0.0",
					Description: "Multi-tenant projects API. Log in with POST /auth/login, then send the token and X-Tenant-ID.",
				},
				SecuritySchemes: map[string]openapi.SecurityScheme{"bearer": openapi.BearerJWT()},
			}),
		},
		// Request logging for every route except the health probe.
		Configure: func(m *gonest.MiddlewareConsumer, l *slog.Logger) {
			m.Apply(logger.RequestLogger(l)).
				Exclude(gonest.ForPath("/health")).
				ForAllRoutes()
		},
	}
}
```

## Middleware: `Configure`

`Configure` is the NestJS `configure(consumer)` method. It is a function
whose first parameter is `*gonest.MiddlewareConsumer`; its other parameters
are injected from the module. It runs once, at startup, and binds ordinary
`gin.HandlerFunc` middleware to routes:

<!-- snippet: examples/saas/internal/tenants/tenants.go#tenants-module -->
```go
// Module provides and exports Service, and applies the tenant middleware to
// the tenant-scoped routes of the whole application (NestJS configure()).
// RouteSpec paths are relative to the global prefix; a trailing * matches the
// path itself and everything below it.
var Module = &gonest.Module{
	Name:        "tenants",
	Imports:     []*gonest.Module{auth.Module},
	Providers:   []gonest.Provider{gonest.Provide(NewService)},
	Controllers: []any{NewTenantsController},
	Exports:     []gonest.Token{gonest.TokenOf[*Service]()},
	Configure: func(m *gonest.MiddlewareConsumer, svc *Service) {
		m.Apply(svc.Middleware()).ForRoutes(
			gonest.ForPath("/projects/*"),
			gonest.ForPath("/tenants/*"),
		)
	},
}
```

The middleware itself is plain Gin:

<!-- snippet: examples/saas/internal/tenants/tenants.go#tenant-middleware -->
```go
// Middleware is plain gin middleware (NestMiddleware). It resolves the tenant
// from the X-Tenant-ID header and stores it in the request values; missing
// or unknown tenants are rejected before guards run.
func (s *Service) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader(HeaderName)
		if id == "" {
			c.AbortWithStatusJSON(http.StatusBadRequest, gonest.BadRequest("missing "+HeaderName+" header").Body())
			return
		}
		t, ok := s.Find(id)
		if !ok {
			c.AbortWithStatusJSON(http.StatusNotFound, gonest.NotFound("unknown tenant").Body())
			return
		}
		c.Set(tenantKey, &t)
		c.Next()
	}
}
```

Rules:

- `m.Apply(h...)` starts a rule; finish it with `ForRoutes(specs...)` or
  `ForAllRoutes()`. `Exclude(specs...)` removes routes (the root module of
  the example excludes `/health` from request logging).
- `gonest.ForPath("/projects/*")` selects every method on a path; a trailing
  `*` matches the path itself and everything below it.
  `gonest.ForRoute("GET", "/projects")` selects one method.
- A rule applies to the routes of the whole application, not only to the
  module's own controllers. Paths are relative to `WithGlobalPrefix`.
- Matching happens at startup: each route gets its own precomputed handler
  chain. Middleware runs before guards (see [the pipeline](pipeline.md)).
- Middleware only runs on matched routes; the JSON 404/405 fallbacks bypass it.

## Startup errors

`gonest.New` validates the whole graph before serving. Besides missing or
invisible providers, it reports import cycles, duplicate or empty module
names and invalid exports:

```text
gonest: import cycle between modules: a -> b -> a
```
