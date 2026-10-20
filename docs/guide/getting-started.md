# Getting started

## Requirements and installation

Gonest needs Go 1.24 or later.

```sh
go get github.com/omyo/gonest
```

Gonest depends only on Gin, go-playground/validator (and cobra, for the CLI).

## A first application

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
go run ./examples/hello
curl 'localhost:8080/hello?name=Ada'   # {"message":"Hello, Ada!"}
curl 'localhost:8080/hello'            # 400, see below
```

```json
{"statusCode":400,"message":"validation failed","error":"Bad Request",
 "details":[{"field":"name","source":"query","rule":"required","message":"is required"}]}
```

What happened at `gonest.New`:

1. The module graph was walked from `AppModule`.
2. `NewGreeter` and `NewGreetController` were analysed; the controller's
   `*Greeter` parameter was resolved by type and every singleton was
   built. A missing or invisible dependency fails here, with the full
   dependency path, not on the first request.
3. `Routes` was called once and the input binder for `GreetInput` was
   compiled. A tag mistake (for example a `path:"id"` field on a route
   without `:id`) also fails here.
4. Lifecycle hooks ran, and the route table was logged:

```text
level=INFO msg="gonest: modules loaded" count=1 modules=app
level=INFO msg="gonest: mapped route" route="GET /hello -> GreetController.Greet"
level=INFO msg="gonest: application started" routes=1 duration=...
```

The name in the log is the handler's function name, which is why handlers
are passed as method values (`gonest.Handle(c.Greet)`) rather than closures.

`app.Listen(addr)` serves until SIGINT or SIGTERM, then stops accepting
connections, waits for in-flight requests (10 seconds by default, see
`gonest.WithShutdownTimeout`) and runs the shutdown hooks.

## The SaaS example

`examples/saas` is a complete multi-tenant API (auth, tenants, projects,
OpenAPI). Its `main`:

<!-- snippet: examples/saas/cmd/server/main.go#main -->
```go
func main() {
	debug := flag.Bool("debug", false, "log at debug level")
	flag.Parse()
	if err := run(*debug); err != nil {
		log.Fatal(err)
	}
}

func run(debug bool) error {
	level := slog.LevelInfo
	if debug {
		level = slog.LevelDebug
	}
	// New validates the whole graph (configuration included), mounts every
	// route and runs OnModuleInit / OnApplicationBootstrap. Errors are
	// returned, never panicked.
	application, err := gonest.New(
		app.NewModule(app.Options{LogLevel: level}),
		gonest.WithShutdownTimeout(15*time.Second),
	)
	if err != nil {
		return err
	}
	cfg, err := gonest.Get[*settings.Config](application)
	if err != nil {
		return err
	}
	logger := gonest.MustGet[*slog.Logger](application)
	logger.Info("configuration", "config", config.Redacted(cfg))

	// Listen blocks until SIGINT/SIGTERM, drains in-flight requests (up to the
	// shutdown timeout), then runs OnModuleDestroy,
	// BeforeApplicationShutdown and OnApplicationShutdown hooks.
	if err := application.Listen(fmt.Sprintf(":%d", cfg.Port)); err != nil {
		return err
	}
	logger.Info("bye")
	return nil
}
```

```sh
go run ./examples/saas/cmd/server            # SAAS_PORT, SAAS_JWT_SECRET, SAAS_TOKEN_TTL
TOKEN=$(curl -s -X POST localhost:8080/auth/login \
  -d '{"email":"alice@acme.test","password":"demo-password"}' | jq -r .accessToken)
curl -H "Authorization: Bearer $TOKEN" -H 'X-Tenant-ID: acme' localhost:8080/projects
curl -i -X DELETE -H "Authorization: Bearer $TOKEN" -H 'X-Tenant-ID: acme' localhost:8080/projects/1   # 204
open http://localhost:8080/docs
```

Demo accounts (password `demo-password`): `alice@acme.test` (admin, tenant
`acme`), `bob@acme.test` (member, `acme`), `carol@globex.test` (admin,
`globex`).

## Starting a new project with the CLI

```sh
go install github.com/omyo/gonest/cmd/gonest@latest
gonest new shop --module example.com/shop
cd shop && go mod tidy && go run .
gonest g module orders && gonest g service orders && gonest g controller orders
```

See [CLI](cli.md).

## Next

Read [Modules](modules.md) and [Providers](providers.md), then
[Controllers](controllers.md) and [the request pipeline](pipeline.md).
