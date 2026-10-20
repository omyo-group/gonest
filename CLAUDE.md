# Gonest: shared context for every agent

You are one of several agents building Gonest, a Go web framework on top of Gin
(github.com/gin-gonic/gin) that reproduces the NestJS architecture: modules,
providers with dependency injection, controllers, guards, pipes, interceptors,
exception filters, lifecycle hooks and a CLI. Other agents work in parallel on
other packages. Stay strictly inside the paths you own.

## Goals, in priority order
1. Stability: small, explicit public API. Nothing breaks after v1.
2. Efficiency: all discovery work (reflection over types, tag parsing, DI
   resolution, chain composition) happens once at bootstrap. The request path
   is precomputed closures and slices inside a Gin handler: no map lookups by
   type, no tag parsing, no container access per request.
   Target: under 5% overhead versus an equivalent raw Gin handler.
3. Familiarity: a NestJS developer recognises every concept.
4. Idiomatic Go: errors returned, never thrown; context.Context everywhere;
   no global mutable state; no panics for control flow.

## Technical constraints
- Go 1.24 (go.mod says `go 1.24.0`). Module path: github.com/omyo/gonest.
  Root package name: gonest.
- Dependencies are frozen in go.mod: gin v1.11.0, validator/v10 v10.30.0,
  cobra v1.10.2 (CLI only). Tests use the standard library only (no testify).
  Do not add modules. If you believe one is required, write a proposal in
  docs/contract-changes.md and continue without it.
- No runtime code generation. Code generation exists only in CLI scaffolding.

## Build environment (this machine)
- Before any go command run: `source /tmp/claude-0/goenv-agent.sh`
  (modules resolve from GitHub through a local mapper; GOFLAGS=-mod=readonly,
  so go.mod and go.sum must not change).
- `golangci-lint` v2.5.0 is installed; config in .golangci.yml.
- The machine has 2 CPUs shared by parallel agents: run `go test -race` on your
  own packages while iterating, and the whole repo once at the end.

## Architecture
- `internal/core`: FROZEN contracts (types, interfaces, option helpers).
  Read it first; it is short. Do not edit it.
- Root package `gonest`: aliases of core plus `App` (app.go, lifecycle.go).
- `internal/di` (A1), `internal/pipeline` (A2), `modules/config` and
  `modules/logger` (A3), `cmd/gonest` and `internal/codegen` (A4),
  `internal/module` + root app.go/lifecycle.go (B1), `openapi` (B2),
  `gonesttest` (B3), `examples` and `docs/guide` (C1).
- Seams between internal packages are the stub signatures already present in
  each package (for example pipeline.New / Mounter.Mount, di.New). Keep those
  signatures; you may add unexported code and new exported helpers inside
  your own package.
- See docs/contracts.md for the API overview and docs/adr/0001-architecture.md
  for the reasoning.

## NestJS to Gonest mapping
| NestJS | Gonest |
|---|---|
| @Module({imports, providers, controllers, exports}) | `&gonest.Module{Name, Imports, Providers, Controllers, Exports, ReExports, Global, Configure}` |
| @Injectable + constructor injection | constructor `func NewUsersService(r *UsersRepo, l *slog.Logger) *UsersService`, parameters resolved by type |
| useValue / useClass / useFactory / named tokens | `gonest.Value(v)`, `gonest.Bind[Iface](NewImpl)`, `gonest.Factory[T](fn)`, `gonest.Named("x")` + parameter object with `name:"x"` tag |
| @Inject('TOKEN') / @Optional | parameter object: struct embedding `gonest.In`, fields tagged `name:"..."`, `optional:"true"` |
| Scopes DEFAULT / TRANSIENT / REQUEST | `gonest.Singleton` (default), `gonest.Transient`; `RequestScoped` reserved (error in v0.1) |
| APP_GUARD / APP_INTERCEPTOR / APP_PIPE / APP_FILTER | `gonest.AppGuard(ctor)`, `AppInterceptor`, `AppPipe`, `AppFilter` |
| @Controller('users') + @Get(':id') | `Routes(r gonest.Router)`: `r.Prefix("/users")`, `r.GET("/:id", gonest.Handle(c.FindOne))` |
| @Param / @Query / @Body / @Headers | one input struct per handler, tags `path:"id"`, `query:"page"`, `header:"X-Tenant"`, `form:"f"`, `json:"name"`, `validate:"required"` |
| Handler method | `func (c *UsersController) FindOne(ctx *gonest.Ctx, in FindOneInput) (UserDTO, error)` |
| @UseGuards / @UseInterceptors / @UsePipes / @UseFilters | route or controller options `gonest.UseGuards(...)` etc.; controller level via `r.Use(...)` |
| SetMetadata + Reflector | `gonest.SetMetadata(k, v)` read with `ctx.Route().Metadata(k)` |
| HttpException family | `gonest.NotFound(msg)`, `BadRequest`, `Unauthorized`, `Forbidden`, `Conflict`, ... (`*gonest.HTTPError`) |
| NestMiddleware + configure() | `Module.Configure: func(m *gonest.MiddlewareConsumer, deps...)`, `m.Apply(ginMw).Exclude(...).ForRoutes(...)` |
| Lifecycle hooks | `OnModuleInit`, `OnApplicationBootstrap`, `OnModuleDestroy`, `BeforeApplicationShutdown`, `OnApplicationShutdown` (each `(ctx context.Context) error`) |
| ConfigModule.forRoot() | `config.ForRoot[AppConfig](config.Options{...})` returns `*gonest.Module` |
| Test.createTestingModule | `gonesttest.New(t, mod).OverrideProvider(...).Compile()` |
| nest CLI | `gonest new`, `gonest g module|controller|service|guard|...` |

## Request pipeline (same order as NestJS)
gin middleware (Configure rules) -> guards (global, controller, route) ->
interceptors before (global, controller, route) -> binding + validation of In ->
pipes (global, controller, route) -> handler -> interceptors after (reverse) ->
response. Any error goes to exception filters: route, then controller, then
global; the first whose Catch returns true wins, else the default filter.

## Code rules
- Every exported identifier has a doc comment; every package has a doc.go.
- Wrap errors with %w. Framework errors are typed (`*core.ResolveError`,
  `*core.HTTPError`, `core.ValidationErrors`).
- Tests: table-driven, run with -race, at least 85% statement coverage on your
  packages, plus benchmarks for anything on the request path.
- No init() side effects. Keep the public API minimal: when unsure, make it unexported.
- `gofmt`, `go vet` and `golangci-lint run` must be clean.

## Git and handoff
- You work in your own git worktree on branch `agent/<id>`. Commit your work
  there with clear messages (author is already configured). Do not merge,
  rebase or touch other branches.
- When finished, write docs/handoff/<agent-id>.md: what you built, the public
  API, decisions taken, known gaps, how to test. Commit it. Then stop.
- Never edit paths you do not own. If a frozen contract blocks you, write the
  proposal in docs/contract-changes.md (append a section) and continue
  against a local workaround.
