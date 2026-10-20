# ADR 0001: Architecture of Gonest

Status: accepted (Wave 0)

## Context

Gonest brings the NestJS programming model (modules, dependency injection,
controllers, guards, pipes, interceptors, filters, lifecycle hooks) to Go on
top of Gin. Go has no decorators, no runtime parameter metadata and a strong
preference for explicit code, so the NestJS concepts must be expressed with
plain values and types.

## Decisions

1. **No decorators: configuration is values.** A module is a `*gonest.Module`
   struct literal; routes are declared in `Routes(r gonest.Router)`; decorators
   become `RouteOption` values (`gonest.UseGuards(...)`, `gonest.Summary(...)`).
   Everything is visible to the compiler and to "go to definition".

2. **Constructor injection by type.** Providers are constructor functions.
   Their parameter types are the dependencies; their first result type is the
   token. Named and optional dependencies use a parameter object embedding
   `gonest.In` (the approach proven by uber/dig and fx). Interfaces are bound
   explicitly with `gonest.Bind[I](ctor)`.

3. **Reflection only at bootstrap.** The container analyses constructors,
   builds and validates the whole graph, and instantiates singletons before the
   server starts. Input binders are compiled per route at mount time. Request
   handling never touches the container or parses tags. A broken graph fails at
   startup with the full dependency path.

4. **Typed handlers.** `gonest.Handle(func(*Ctx, In) (Out, error))` captures In
   and Out with generics. One input struct per handler replaces @Param/@Query/
   @Body/@Headers with struct tags, gives validation for free and lets the
   OpenAPI generator read request and response schemas without annotations.

5. **NestJS pipeline order.** Middleware, guards, interceptors, binding,
   pipes, handler, interceptors (after), filters. Filters are tried route,
   controller, global, default.

6. **Modules by pointer identity.** Importing the same `*Module` twice is one
   instance. Dynamic modules are functions returning `*Module` (for example
   `config.ForRoot[T](...)`). Visibility follows NestJS: own providers,
   exports of imported modules, re-exports, and global modules.

7. **Layering to avoid import cycles.** Contracts live in `internal/core`,
   which depends only on gin and the standard library. The public root package
   re-exports them as aliases and adds `App`. Implementations (`internal/di`,
   `internal/pipeline`, `internal/module`) depend on core, and the root package
   depends on them. Optional modules (`modules/config`, `modules/logger`,
   `openapi`, `gonesttest`) depend only on the public root package, which proves
   third-party modules can be built the same way.

8. **Minimal dependencies.** gin, validator/v10 and, for the CLI only, cobra.
   Tests use the standard library. Fewer dependencies mean fewer breaking
   upgrades for users.

9. **Request-scoped providers deferred.** They require a child container per
   request, which conflicts with goal 2. The scope constant is reserved and
   returns an error in v0.1.

## Consequences

- Slightly more typing than NestJS for routes (one line per route), in exchange
  for compile-time checking and zero hidden behaviour.
- The DI container is a bootstrap tool, not a service locator; `gonest.Get[T]`
  exists for main functions and tests only.
