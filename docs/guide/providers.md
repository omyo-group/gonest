# Providers and dependency injection

A provider is a constructor. Its parameter types are its dependencies, its
first result type is the token under which it is registered. Constructors
return `T` or `(T, error)`; an error aborts startup.

<!-- snippet: examples/saas/internal/projects/service.go#service-ctor -->
```go
// NewService is the provider constructor. Repository is an interface bound
// in the module; *tenants.Service is exported by the imported tenants
// module; *slog.Logger comes from the global logger module.
func NewService(repo Repository, ts *tenants.Service, log *slog.Logger) *Service {
	return &Service{repo: repo, tenants: ts, log: log.With("module", "projects")}
}
```

Registered with `gonest.Provide(NewService)`, the service is built once
(singleton, the default) when the application starts, after `Repository`,
`*tenants.Service` and `*slog.Logger` were resolved.

All resolution happens in `gonest.New`. The container is a bootstrap tool:
handlers never look anything up at request time.

## Provider kinds

| NestJS | Gonest |
|---|---|
| `@Injectable()` class | `gonest.Provide(NewX)` |
| `{ provide: X, useValue: v }` | `gonest.Value(v)` or `gonest.Value[I](v)` |
| `{ provide: I, useClass: Impl }` | `gonest.Bind[I](NewImpl)` |
| `{ provide: X, useFactory, inject }` | `gonest.Factory[X](func(deps...) X)` |
| string token `'PRIMARY_DB'` | `gonest.Named("primary")` option |
| `@Inject('PRIMARY_DB')`, `@Optional()` | parameter object fields tagged `name:"..."`, `optional:"true"` |
| `scope: Scope.TRANSIENT` | `gonest.Transient` option |
| `APP_GUARD`, `APP_INTERCEPTOR`, `APP_PIPE`, `APP_FILTER` | `gonest.AppGuard(ctor)`, `AppInterceptor`, `AppPipe`, `AppFilter` |

<!-- snippet: examples/guide/providers_test.go#providers-kinds -->
```go
// Clock is an interface token.
type Clock interface{ Now() string }

type fixedClock struct{}

func (fixedClock) Now() string { return "2026-01-01" }

// NewFixedClock returns a concrete type; Bind registers it under Clock.
func NewFixedClock() *fixedClock { return &fixedClock{} }

// DB stands for a database handle.
type DB struct{ DSN string }

// Settings is registered with Value.
type Settings struct{ PrimaryDSN, ReplicaDSN string }

// ReportDeps is a parameter object: Named providers and optional
// dependencies are only available through one.
type ReportDeps struct {
	gonest.In
	Primary *DB          `name:"primary"`
	Replica *DB          `name:"replica"`
	Log     *slog.Logger `optional:"true"`
	Clock   Clock
}

// ReportService depends on two DBs of the same type.
type ReportService struct{ d ReportDeps }

// NewReportService is a constructor with a parameter object.
func NewReportService(d ReportDeps) *ReportService { return &ReportService{d: d} }

// ReportsModule shows every provider kind.
var ReportsModule = &gonest.Module{
	Name: "reports",
	Providers: []gonest.Provider{
		gonest.Value(&Settings{PrimaryDSN: "db1", ReplicaDSN: "db2"}), // useValue
		gonest.Bind[Clock](NewFixedClock),                             // useClass
		gonest.Factory[*DB](func(s *Settings) *DB { // useFactory
			return &DB{DSN: s.PrimaryDSN}
		}, gonest.Named("primary")),
		gonest.Factory[*DB](func(s *Settings) (*DB, error) {
			if s.ReplicaDSN == "" {
				return nil, errors.New("no replica configured")
			}
			return &DB{DSN: s.ReplicaDSN}, nil
		}, gonest.Named("replica")),
		gonest.Provide(NewReportService), // @Injectable()
	},
	Exports: []gonest.Token{gonest.TokenOf[*ReportService]()},
}
```

## Parameter objects

A constructor parameter whose type is a struct embedding `gonest.In` is a
parameter object: every exported field is injected by type. It is the only
way to request a named provider (`name:"..."`) or an optional one
(`optional:"true"`, left at its zero value when no provider is visible).
The SaaS projects controller uses one simply to name its dependencies:

<!-- snippet: examples/saas/internal/projects/controller.go#controller-deps -->
```go
// ControllerDeps is a parameter object (struct embedding gonest.In): each
// field is injected by type, like constructor parameters in NestJS.
type ControllerDeps struct {
	gonest.In
	Service *Service
	JWT     *auth.JWTGuard
	Roles   *auth.RolesGuard
}

// ProjectsController serves /projects.
type ProjectsController struct {
	svc   *Service
	jwt   *auth.JWTGuard
	roles *auth.RolesGuard
}

// NewProjectsController is the controller constructor.
func NewProjectsController(d ControllerDeps) *ProjectsController {
	return &ProjectsController{svc: d.Service, jwt: d.JWT, roles: d.Roles}
}
```

The auth module makes its clock optional, so `TokenService` also works where
no `Clock` is provided:

<!-- snippet: examples/saas/internal/auth/jwt.go#token-deps -->
```go
// Clock returns the current time. The auth module provides time.Now; tests
// override it with gonesttest.OverrideProvider.
type Clock func() time.Time

// TokenDeps is the parameter object of NewTokenService: Clock is optional,
// so the service also works in a module that does not provide one.
type TokenDeps struct {
	gonest.In
	Config *settings.Config
	Clock  Clock `optional:"true"`
}
```

## Scopes

- `gonest.Singleton` (default): one instance per application.
- `gonest.Transient`: a new instance for every injection.
- `gonest.RequestScoped` is reserved; registering one is an error in v0.1
  (request-scoped providers would need a container lookup per request). Use
  request values (`ctx.Set`, `gonest.CtxValue`) for per-request data, as the
  SaaS example does with the current user and tenant.

<!-- snippet: examples/guide/providers_test.go#transient -->
```go
// Counter counts how many instances were built.
type Counter struct{ ID int }

// Consumers receive their own Counter because it is Transient.
type Consumers struct {
	gonest.In
	A *ConsumerA
	B *ConsumerB
}

// ConsumerA depends on a transient Counter.
type ConsumerA struct{ C *Counter }

// ConsumerB depends on a transient Counter.
type ConsumerB struct{ C *Counter }

func Example_transient() {
	built := 0
	mod := &gonest.Module{
		Name: "app",
		Providers: []gonest.Provider{
			gonest.Provide(func() *Counter { built++; return &Counter{ID: built} }, gonest.Transient),
			gonest.Provide(func(c *Counter) *ConsumerA { return &ConsumerA{C: c} }),
			gonest.Provide(func(c *Counter) *ConsumerB { return &ConsumerB{C: c} }),
		},
	}
	app, err := gonest.New(mod, quiet()...)
	if err != nil {
		fmt.Println(err)
		return
	}
	a := gonest.MustGet[*ConsumerA](app)
	b := gonest.MustGet[*ConsumerB](app)
	fmt.Println(a.C.ID != b.C.ID)
	// Output: true
}
```

## Resolution errors

Errors are `*gonest.ResolveError` values carrying the dependency path and a
hint:

<!-- snippet: examples/guide/providers_test.go#resolve-error -->
```go
type Mailer struct{}
type SignupService struct{ m *Mailer }
mod := &gonest.Module{
	Name: "signup",
	Providers: []gonest.Provider{
		gonest.Provide(func(m *Mailer) *SignupService { return &SignupService{m: m} }),
	},
}
_, err := gonest.New(mod)
fmt.Println(err)
```

```text
gonest: cannot resolve *guide_test.SignupService -> *guide_test.Mailer in module signup: no provider (hint: register a provider for *guide_test.Mailer in this module or import a module that exports it)
```

## Getting a provider outside the graph

`gonest.Get[T](app)`, `gonest.GetNamed[T](app, name)` and
`gonest.MustGet[T](app)` resolve a provider as seen from the root module
(its own providers, its imports' exports, global modules). They are meant
for `main` functions and tests:

<!-- snippet: examples/guide/providers_test.go#get -->
```go
svc, err := gonest.Get[*ReportService](app) // for main functions and tests
```

## Lifecycle hooks

Any provider or controller implementing one of these interfaces is called
by the application. Every hook has the signature
`func(ctx context.Context) error` and receives a context with a 30 second
timeout.

| Hook | When |
|---|---|
| `OnModuleInit` | in `gonest.New`, after every singleton is built; modules in dependency order (imports first) |
| `OnApplicationBootstrap` | in `gonest.New`, after every `OnModuleInit`; all routes are mounted |
| `OnModuleDestroy` | on shutdown, after the HTTP server stopped; reverse order |
| `BeforeApplicationShutdown` | after every `OnModuleDestroy` |
| `OnApplicationShutdown` | last; close connections here |

<!-- snippet: examples/saas/internal/projects/service.go#lifecycle -->
```go
// OnModuleInit seeds demo data once the module's providers are built
// (NestJS OnModuleInit).
func (s *Service) OnModuleInit(ctx context.Context) error {
	seed := map[string][]string{
		"acme":   {"Website relaunch", "Mobile app", "Data warehouse"},
		"globex": {"Moonshot"},
	}
	for _, id := range s.tenants.IDs() {
		for _, name := range seed[id] {
			if _, err := s.Create(ctx, id, name, ""); err != nil {
				return fmt.Errorf("seed %s/%s: %w", id, name, err)
			}
		}
	}
	s.log.InfoContext(ctx, "seeded projects", "tenants", len(seed))
	return nil
}

// OnApplicationShutdown runs last during graceful shutdown; a real service
// would flush buffers or close connections here.
func (s *Service) OnApplicationShutdown(ctx context.Context) error {
	s.log.InfoContext(ctx, "projects service stopped")
	return nil
}
```

If a startup hook fails, `gonest.New` returns the error and the instances
whose `OnModuleInit` already ran receive the three shutdown hooks. Shutdown
hooks run on `app.Listen`/`app.Serve` exit, or when you call
`app.Shutdown(ctx)` yourself (only the first call does anything).
