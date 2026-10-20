# Gonest contracts (frozen in Wave 0)

The source of truth is `internal/core` (re-exported by the root package
`gonest`). This page is the overview. The complete, compiling example is
`examples_contract_test.go` at the repository root.

## Public API at a glance

| Area | API |
|---|---|
| Modules | `gonest.Module{Name, Imports, Providers, Controllers, Exports, ReExports, Global, Configure}` |
| Providers | `Provide(ctor, opts...)`, `Value[T](v)`, `Bind[I](ctor)`, `Factory[T](fn)`, `Named(name)`, scopes `Singleton`, `Transient` |
| Global enhancers | `AppGuard(ctor)`, `AppInterceptor(ctor)`, `AppPipe(ctor)`, `AppFilter(ctor)` |
| Tokens | `TokenOf[T]()`, `NamedTokenOf[T](name)` for `Module.Exports` |
| Parameter objects | struct embedding `gonest.In`; field tags `name:"..."`, `optional:"true"` |
| Controllers | `Routes(r gonest.Router)`; `Router.Prefix`, `Use`, `Group`, `GET`...`HEAD`, `Handle` |
| Handlers | `Handle(func(*Ctx, In) (Out, error))`, `HandleRaw(func(*Ctx) error)`; `Empty` (no input), `NoContent` (204) |
| Input tags | `path`, `query`, `header`, `form`, `json`, `validate`, `default`, `doc`, `example` |
| Route options | `UseGuards`, `UseInterceptors`, `UsePipes`, `UseFilters`, `Status`, `Summary`, `Description`, `Tags`, `Deprecated`, `Hidden`, `Security`, `Responds`, `SetMetadata` |
| Pipeline | `Guard`, `Interceptor` + `Next`, `Pipe` + `ArgMeta`, `ExceptionFilter`, and `...Func` adapters |
| Errors | `*HTTPError` (`NotFound`, `BadRequest`, ...), `ErrorBody`, `ValidationErrors`, `FieldError`, `*ResolveError` |
| Context | `*Ctx`: `Gin`, `Request`, `Writer`, `Route`, `Logger`, `SetLogger`, `Param`, `Query`, `Header`, `Get`, `Set`, `SetStatus`; implements `context.Context`; `CtxValue[T]` |
| Middleware | `Module.Configure: func(*MiddlewareConsumer, deps...)`; `Apply(...).Exclude(...).ForRoutes(...)`/`ForAllRoutes()`; `ForPath`, `ForRoute` |
| Lifecycle | `OnModuleInit`, `OnApplicationBootstrap`, `OnModuleDestroy`, `BeforeApplicationShutdown`, `OnApplicationShutdown` |
| App | `New(root, opts...)`, `Listen`, `Serve`, `Shutdown`, `Engine`, `Handler`, `Routes`; `Get[T]`, `GetNamed[T]`, `MustGet[T]`; options `WithEngine`, `WithLogger`, `WithShutdownTimeout`, `WithGlobalGuards/Interceptors/Pipes/Filters`, `WithGlobalPrefix`, `WithoutStartupLog` |

## Input binding rules (implemented by A2)

- One struct per handler. Exported fields are bound from the tag that names
  their source; a field may have only one source tag.
- `json` fields come from a JSON body (only decoded when the struct has at
  least one json-tagged field and the body is non-empty).
- Supported scalar kinds for path/query/header/form: string, bool, all ints and
  uints, floats, `time.Time` (RFC 3339), `time.Duration`, pointers to these,
  and slices of these for query/form/header (repeated keys or comma lists).
- `default:"..."` fills a missing query/header/form value.
- `validate:"..."` uses go-playground/validator/v10; failures produce 400 with
  `ValidationErrors` details, field names taken from the source tag.
- Conversion failures (e.g. `?page=abc` for an int) produce 400 with a
  FieldError whose Rule is `type`.
- `Handle[*In, Out]` is accepted (binds into a fresh `*In`, one allocation fewer).
- Types whose pointer implements `encoding.TextUnmarshaler` are supported
  scalars; `form` fields of type `*multipart.FileHeader` or
  `[]*multipart.FileHeader` receive uploads.
- An empty value (`?page=`) for a non-string scalar counts as missing, so the
  default applies; slices drop empty items.
- Exported fields without a source tag are never bound, not even from the JSON
  body, so a body cannot override path or query values.
- Mixing `json` and `form` fields, a `path` field naming a parameter absent from
  the route, or an unsupported field type fails at Mount (bootstrap).

## Response rules

- `Out` is written as JSON with `RouteInfo.SuccessStatus()` unless the handler
  called `ctx.SetStatus`. `NoContent` writes 204 with no body.
- Errors go through filters; the default filter writes `ErrorBody`. Non-HTTP
  errors become 500 "Internal Server Error" and are logged with the request
  logger. Panics are recovered and treated as 500.
- A guard returning `false, nil` produces 403 "Forbidden resource".

## Internal seams

| Package | Seam (signatures are frozen) | Consumer |
|---|---|---|
| `internal/di` | `di.New() *Container` implementing `core.Container` | B1 |
| `internal/pipeline` | `pipeline.New(Options) *Mounter`; `Mounter.SetGlobals(Globals)`, `Mounter.Mount(ctrl, module, MiddlewareFor) ([]*core.RouteInfo, error)`, `Mounter.InstallFallbacks()` | B1, B3 |
| `internal/module` | designed by B1 | root `App` |

`MiddlewareFor` receives the full path including `GlobalPrefix`; `RouteSpec`
paths in `Configure` are relative to the global prefix (NestJS semantics), so
the module package strips the prefix before matching.

Framework providers registered by `App` with a nil owner (visible everywhere):
`*core.RouteTable` (as `*gonest.RouteTable`) and `*slog.Logger` (unless the
module graph provides one, e.g. via `logger.ForRoot`).

## Bootstrap order (implemented by B1)

1. Walk the module graph from the root (dedupe by pointer, detect import cycles).
2. Register every provider with its owner module; register framework providers.
3. Install visibility rules; `Container.Validate()` instantiates all singletons.
4. Collect middleware rules by invoking each module's `Configure`.
5. Instantiate controllers (`Container.Invoke`), mount them with the matching middleware.
6. Collect global enhancers (`AppGuard`... providers plus `WithGlobal...` options) and `Mounter.SetGlobals`.
7. `OnModuleInit` for every instance, modules in dependency order (imports first).
8. `OnApplicationBootstrap` likewise.
9. Shutdown: HTTP server drains, then `OnModuleDestroy`, `BeforeApplicationShutdown`, `OnApplicationShutdown` in reverse order.
