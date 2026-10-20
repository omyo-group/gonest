# Gonest

**The NestJS architecture for Go, on top of Gin.** Modules, providers with
dependency injection, controllers, guards, interceptors, pipes, exception
filters, lifecycle hooks, typed configuration, OpenAPI 3.1 and a CLI, without
decorators and without runtime magic: all wiring is plain Go values, checked
once at startup, and serving a request is a precomputed Gin handler chain.

> **Status: v0.1, pre-1.0.** The public API is deliberately small and meant
> to stay, but it may still change before 1.0.

## Why

- **Familiar.** A NestJS developer recognises every concept: `Module`,
  `Imports`/`Exports`, constructor injection, `UseGuards`, `SetMetadata`,
  `OnModuleInit`, `Test.createTestingModule`, `nest g`.
- **Checked at startup.** Missing or invisible providers, import cycles,
  bad input tags, duplicate routes and invalid configuration fail
  `gonest.New` with the full dependency path, not the first request.
- **Typed handlers.** `func(ctx *gonest.Ctx, in In) (Out, error)`: one input
  struct bound from path, query, headers and body and validated; the output
  written as JSON; both used to generate the OpenAPI document.
- **Idiomatic Go.** Errors are returned, `*gonest.Ctx` is a
  `context.Context`, middleware is plain `gin.HandlerFunc`, logging is
  `log/slog`, there are no globals and no `init()` side effects.
- **Few dependencies.** Gin, go-playground/validator, and cobra for the CLI.

## Install

```sh
go get github.com/omyo/gonest            # Go 1.24+
go install github.com/omyo/gonest/cmd/gonest@latest   # optional CLI
```

## Quick start

<!-- snippet: examples/hello/main.go#hello -->
```go
import (
	"log"

	"github.com/omyo/gonest"
)

type Greeter struct{}

func NewGreeter() *Greeter { return &Greeter{} }

func (g *Greeter) Greet(name string) string { return "Hello, " + name + "!" }

type GreetInput struct {
	Name string `query:"name" validate:"required,min=2"`
}

type GreetController struct{ greeter *Greeter }

func NewGreetController(g *Greeter) *GreetController { return &GreetController{greeter: g} }

func (c *GreetController) Routes(r gonest.Router) {
	r.GET("/hello", gonest.Handle(c.Greet))
}

func (c *GreetController) Greet(_ *gonest.Ctx, in GreetInput) (map[string]string, error) {
	return map[string]string{"message": c.greeter.Greet(in.Name)}, nil
}

var AppModule = &gonest.Module{
	Name:        "app",
	Providers:   []gonest.Provider{gonest.Provide(NewGreeter)},
	Controllers: []any{NewGreetController},
}

func main() {
	app, err := gonest.New(AppModule)
	if err != nil {
		log.Fatal(err)
	}
	log.Fatal(app.Listen(":8080"))
}
```

```sh
$ go run ./examples/hello
$ curl 'localhost:8080/hello?name=Ada'
{"message":"Hello, Ada!"}
$ curl 'localhost:8080/hello'
{"statusCode":400,"message":"validation failed","error":"Bad Request","details":[{"field":"name","source":"query","rule":"required","message":"is required"}]}
```

A complete multi-tenant API (JWT auth, roles, tenant middleware, CRUD with
pagination, 404/409/422 errors, OpenAPI at `/docs`) lives in
[`examples/saas`](examples/saas): `go run ./examples/saas/cmd/server`.

## Features

| Feature | Gonest | NestJS equivalent |
|---|---|---|
| Modules | `&gonest.Module{Imports, Providers, Controllers, Exports, ReExports, Global}` | `@Module`, `@Global` |
| Dynamic modules | functions returning `*gonest.Module` | `forRoot()`, `register()` |
| Dependency injection | constructors resolved by type; `Value`, `Bind[I]`, `Factory[T]`, `Named`, optional, `Transient` | `@Injectable`, `useValue/useClass/useFactory`, `@Inject`, `@Optional` |
| Controllers | `Routes(r gonest.Router)`, `r.GET("/:id", gonest.Handle(c.FindOne))` | `@Controller`, `@Get` |
| Input binding + validation | struct tags `path`, `query`, `header`, `form`, `json`, `default`, `validate` | `@Param`, `@Query`, `@Body`, `ValidationPipe` |
| Guards, interceptors, pipes, filters | route, controller (`r.Use`) and global (`AppGuard`...) levels, NestJS order | `@UseGuards`..., `APP_GUARD`... |
| Route metadata | `gonest.SetMetadata`, `ctx.Route().Metadata` | `SetMetadata`, `Reflector` |
| HTTP errors | `gonest.NotFound(...)`, `Conflict`, ... rendered as `{statusCode, message, error, details}` | `HttpException` family |
| Middleware | Gin middleware bound in `Module.Configure` with `Apply/Exclude/ForRoutes` | `NestMiddleware`, `configure()` |
| Lifecycle hooks | `OnModuleInit` ... `OnApplicationShutdown`, graceful shutdown on SIGINT/SIGTERM | same |
| Configuration | `config.ForRoot[T]`: defaults, `.env`, env vars, validation, secrets redacted | `@nestjs/config` |
| Logging | `logger.ForRoot`, request logger with request ids, `log/slog` | `Logger` |
| OpenAPI | `openapi.Module`: OpenAPI 3.1 from handler types + embedded Swagger UI | `@nestjs/swagger` |
| Testing | `gonesttest.New(t, mod).OverrideProvider(...).Compile()`, request helpers, `NewCtx` for unit tests | `Test.createTestingModule` |
| CLI | `gonest new`, `gonest g module/controller/service/guard/...` | `nest new`, `nest g` |

Not in v0.1: request-scoped providers, WebSockets/microservices, YAML
configuration.

## Documentation

The [guide](docs/guide/index.md):
[getting started](docs/guide/getting-started.md) ·
[modules](docs/guide/modules.md) ·
[providers](docs/guide/providers.md) ·
[controllers](docs/guide/controllers.md) ·
[request pipeline](docs/guide/pipeline.md) ·
[configuration](docs/guide/configuration.md) ·
[logging](docs/guide/logging.md) ·
[testing](docs/guide/testing.md) ·
[OpenAPI](docs/guide/openapi.md) ·
[CLI](docs/guide/cli.md) ·
[coming from NestJS](docs/guide/coming-from-nestjs.md) ·
[performance](docs/guide/performance.md)

Design notes: [contracts](docs/contracts.md) and the
[architecture decision records](docs/adr/).

## Performance

Per request, Gonest adds one 32-byte allocation and about 0.1 µs over a raw
Gin handler for the pipeline itself, plus about 0.2 µs for reflective input
binding and about 0.3 µs when `validate` tags are present. These are
constants, independent of the handler; see
[performance](docs/guide/performance.md) for the benchmark and its caveats.

## Development

```sh
make test     # go test -race ./...
make lint     # golangci-lint run ./...
go test ./examples/docscheck/   # every Go snippet in the docs matches compiled code
```
