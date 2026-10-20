# Gonest guide

Gonest brings the NestJS architecture to Go, on top of Gin: modules,
providers with dependency injection, controllers, guards, interceptors,
pipes, exception filters, lifecycle hooks, typed configuration, OpenAPI and a
CLI. Everything is wired once at startup; serving a request is a plain Gin
handler call.

Status: **v0.1, pre-1.0**. The API is small and meant to stay, but it may
still change before 1.0.

## Pages

| Page | What it covers |
|---|---|
| [Getting started](getting-started.md) | Install, a first application, the SaaS example, the CLI |
| [Modules](modules.md) | `gonest.Module`, imports and exports, global and dynamic modules, middleware |
| [Providers](providers.md) | Constructor injection, `Value`, `Bind`, `Factory`, `Named`, parameter objects, scopes, lifecycle hooks |
| [Controllers](controllers.md) | Routes, input binding and validation, responses, errors |
| [Request pipeline](pipeline.md) | Middleware, guards, interceptors, pipes, filters and their order |
| [Configuration](configuration.md) | `modules/config`: typed config from defaults, `.env` and the environment |
| [Logging](logging.md) | `modules/logger`: `*slog.Logger` provider and request logging |
| [Testing](testing.md) | `gonesttest`: testing modules, overrides, request helpers, unit tests of enhancers |
| [OpenAPI](openapi.md) | OpenAPI 3.1 document and Swagger UI |
| [CLI](cli.md) | `gonest new`, `gonest generate`, `gonest info` |
| [Coming from NestJS](coming-from-nestjs.md) | Decorators side by side with their Gonest equivalent |
| [Performance](performance.md) | Benchmarks against raw Gin and what the overhead is made of |

## The example application

Most snippets come from `examples/saas`, a small multi-tenant API that uses
every feature:

| Package | Shows |
|---|---|
| `examples/saas/internal/settings` | typed configuration struct |
| `examples/saas/internal/auth` | HS256 JWT built on `crypto/hmac`, `JWTGuard`, `CurrentUser`, `Roles` metadata + `RolesGuard` |
| `examples/saas/internal/tenants` | tenant resolved from `X-Tenant-ID` by middleware bound in `Module.Configure`, exported `Service` |
| `examples/saas/internal/projects` | CRUD, validation, pagination defaults, 404/409, 204 delete, a pipe, a filter mapping a domain error to 422, `OnModuleInit` seeding |
| `examples/saas/internal/platform` | global timing interceptor (`gonest.AppInterceptor`), health check |
| `examples/saas/internal/app` | root module: config, logger, OpenAPI at `/docs` and `/openapi.json`; the tests |
| `examples/saas/cmd/server` | `main`, graceful shutdown |

Run it with `go run ./examples/saas/cmd/server` and open
<http://localhost:8080/docs>.

Snippets that the example does not cover come from `examples/guide`, a
package of runnable `Example` functions whose printed output is checked by
`go test`. A test in `examples/docscheck` fails when a snippet in this guide
differs from its source, so the guide always shows code that compiles.
