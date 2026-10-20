# v0.1 integration review (agent C2)

Scope: the merged main branch (`bbba021`) reviewed as a maintainer preparing
v0.1, following `prompts/C2.md`. Small fixes were applied on branch
`agent/c2` (one commit each, listed below). Larger items are written up with a
suggested fix. Line numbers refer to `agent/c2` HEAD.

## Summary

- **No blockers.** The architecture matches ADR 0001 and docs/contracts.md:
  packages are layered as specified (only `gonesttest` and some tests import
  `internal/core`, as documented). Discovery work happens at bootstrap, and
  the request path does no container access, tag parsing or chain
  composition.
- **Three majors were found and fixed:** silent empty 200 when the response
  cannot be encoded, framework error logs without `request_id`, and OpenAPI
  documenting `time.Duration` with the wrong type.
- **Two majors remain open, both design decisions for the lead:** no default
  JSON body size limit, and global guards are invisible to OpenAPI.
- **Performance.** Every benchmarked scenario allocates less than before.
  The no-input and JSON-body routes are now level with raw Gin. Routes with
  validated input stay about 30 to 36% slower than hand-written Gin, and
  almost all of that is go-playground/validator.
- **Recommendation: GO for v0.1**, provided the open majors are listed as
  known limitations in the release notes (the guide already lists them).

## Findings

### Blocker

None.

### Major

| # | Status | Where | Finding | Evidence | Fix |
|---|---|---|---|---|---|
| M1 | **fixed** (`5716a40`, `1dccd04`) | `internal/pipeline/exec.go:200` (`write`) | An `Out` value that `encoding/json` rejects (NaN, a channel, a failing `MarshalJSON`) produced **200 with an empty body and no log**. `gin.Context.JSON` sets the status before it marshals and swallows the error into `c.Errors`. | Probe test: `GET /nan` returned `status=200 body=""`, with an empty log. | The response is now marshalled first with gin's codec (`codec/json.API`, so jsoniter/sonic build tags still apply). An error goes through the filters and the default filter logs it and answers 500. The bytes are written directly with a shared Content-Type slice, keeping gin's no-body rule for 1xx/204/304. Tests: `TestResponseEncodingFailure`, `TestWriteStatusWithoutBody`. |
| M2 | **fixed** (`bb8d2f2`) | `internal/pipeline/exec.go:77` (`logger`) | `logger.RequestLogger` stored its child logger under a gin key, but `Ctx.Logger()` stayed the application logger. Framework error logs (unhandled errors, 5xx, panics, guard errors) therefore lacked `request_id`. The guide's interceptor workaround could not cover guards. | `docs/handoff/c1.md` item 1; reproduced. | The pipeline owns the convention key `pipeline.LoggerKey = "gonest.logger"` and uses a `*slog.Logger` found there when it builds the `Ctx`. The key equals `logger.ContextKey`, and `modules/logger/framework_test.go` pins that equality. No core or root signature changed. The guide workaround was removed, and contracts.md now documents the key. Cost: one `gc.Get` per request, within benchmark noise. |
| M3 | **fixed** (`1f56d85`) | `openapi/schema.go` (`base`, `paramSchema`) | `time.Duration` was documented as `string`, but `encoding/json` writes it as int64 nanoseconds. | `docs/handoff/b2.md` | JSON schemas (bodies, outputs) now use `integer`/`int64` with a description, and examples such as `"5s"` become nanoseconds. Path, query, header and form durations **stay strings**, because the binder parses them with `time.ParseDuration`. Fixing only the reported case would have broken parameters. Golden file, guide and ADR 0002 were updated. |
| M4 | open | `internal/pipeline/binder.go:421` (`decodeJSON`) | **No JSON body size limit.** `json.Decoder` buffers the whole value, so one large body can exhaust memory. NestJS (body-parser) defaults to 100 kB. Form bodies are bounded by `net/http` (10 MB) and Gin's `MaxMultipartMemory`. | Code reading. | Applied now: a body cut by a user-installed `http.MaxBytesReader` returns **413** instead of 400 "invalid JSON body" (`224f72b`), and the guide explains how to set a limit. **Proposed:** a root option `gonest.WithBodyLimit(n int64)` (default 1 MiB, `<= 0` disables), passed to `pipeline.Options.BodyLimit` and applied with `http.MaxBytesReader` in `decodeJSON`. This is new root API, so it needs the lead's decision. |
| M5 | open | `openapi/generate.go` (responses/security); `docs/guide/openapi.md` Limitations | Global guards (`AppGuard`, `WithGlobalGuards`) are not part of `RouteInfo`. Routes protected only by them get no 401/403 and no security requirement, so generated clients and the docs under-describe the API. | `docs/handoff/b2.md`, `c1.md` | **Proposed (openapi-only, additive):** add `openapi.Options.GlobalSecurity []string`, emitted as document-level `security` plus 401/403 on every operation. Add `openapi.Public() gonest.RouteOption`, a metadata key the generator reads to emit `security: []` (OpenAPI's per-operation opt-out). The guide's `Public()` pattern can reuse that key. Not applied, because the shape of the API is a product decision. Applied meanwhile (`4c94073`): `openapi.Options.RouteOptions`, so the docs routes themselves can carry `Public` metadata or guards. Before, a global "public escape hatch" guard blocked `/openapi.json` and `/docs` with no way out. |

### Minor

| # | Status | Where | Finding / suggestion |
|---|---|---|---|
| m1 | **fixed** (`89e5cb9`) | `lifecycle.go:38` (`Listen`) | `signal.NotifyContext` kept intercepting SIGINT/SIGTERM during the graceful drain, so a second Ctrl+C was swallowed. `stop()` now runs on the first signal (the `os/signal` idiom). |
| m2 | **fixed** (`1612327`) | `docs/guide/pipeline.md` | It was undocumented, and verified by a probe, that `app.Engine().Use(...)` after `gonest.New` has no effect: gin binds middleware at registration. The guide now points to `Configure(...).ForAllRoutes()` or `WithEngine` with `Use` called beforehand, and notes that only engine middleware sees the 404/405 fallbacks (CORS preflight). |
| m3 | **fixed** (`b7d4b70`) | `cmd/gonest/main_test.go` | The end-to-end CLI test only compiled `--minimal`, because config/logger did not exist when it was written. It now builds, vets and tests the **full** template as well. Both pass. |
| m4 | not a bug | `internal/codegen/templates/gen/module.go.tmpl:11` | The reported "duplicate `Exports`" does not exist. The template emits one empty `Exports: []gonest.Token{}` placeholder, and registering services and controllers does not duplicate it (checked by scaffolding). This mirrors `nest g module`'s `@Module({})`. Keep it. |
| m5 | open | `openapi/generate.go:256`, `internal/core/funcname.go:9` | operationIds and startup-log names are `Type.Method` without the package. `auth.Controller.Login` and `users.Controller.Login` do not collide in the document (the second gets `_post` or `_2`, and components get `Name2`), but which one gets the suffix depends on mount order, so ids are unstable for client generators. Suggest: on collision, qualify with the package (`users.Controller.Login`) instead of suffixing, in openapi only. Core is frozen and the log name is cosmetic. |
| m6 | open | `internal/pipeline/binder.go:393` (`bindFile`) | A malformed multipart body (or a non-multipart request to a route with file fields) is silently ignored, and the file field stays nil instead of producing a 400. Suggest returning 400 "invalid multipart body" when `Content-Type` is multipart and parsing fails. |
| m7 | open | `internal/pipeline/binder.go:437` | A JSON type mismatch reports `Param: te.Type.String()`, which for struct fields exposes Go type names (`pkg.Address`) to clients. Suggest using the JSON kind (`object`, `string`, `number`...). |
| m8 | open | `openapi/ui.go:103` | Unknown asset paths answer with `http.NotFound` (text/plain) instead of `ErrorBody`. Cosmetic inconsistency. |
| m9 | open (document) | `lifecycle.go:85` (`Serve`) | When the drain times out, `srv.Close()` closes the connections but cannot stop running handlers, which may still run while `OnModuleDestroy` closes their dependencies. This matches net/http and NestJS. Document it in providers.md "Lifecycle hooks". |
| m10 | open (document) | `internal/module/bootstrap.go:95,108` | If `New` fails after `Validate` (middleware or mount error), singletons were already built and no destroy hook runs. With `WithEngine`, routes of controllers mounted before the failure stay registered on the user's engine. Known (`docs/handoff/b1.md`). Fine for v0.1 because the error is fatal anyway. |
| m11 | open (API surface) | `internal/core/ctx.go:72`, `route.go:182`, `handler.go:68`, root `app.go:28` | Exported but not in contracts.md: `Ctx.StatusOverride`, `RouteTable.Add` (lets user code add fake routes to the OpenAPI table), `Handler.Call`, and `AppConfig` ("exposed for internal packages", but public through `Option`). Decide before v1 whether they are API: document them or hide them behind internal accessors. |
| m12 | open (document) | `internal/pipeline/exec.go:106,135` | The interceptor chain memoises the bound input per request. An interceptor that calls `next()` **concurrently** from several goroutines races on that memo. Calling it again sequentially (retry) is supported. Document "next must not be called concurrently". |
| m13 | open (verify) | `internal/module/lifecycle.go` | Hook order inside a module is singletons, then controllers. NestJS (`callModuleInitHook`) visits controllers before providers. This only matters when a controller's hook depends on a provider's hook having run, which Gonest's order makes safer. Document the order rather than change it. |
| m14 | info | `internal/core` | No in-package tests (0%). Through the rest of the repo (`-coverpkg=./internal/core`) it is covered at 90.4%. |

## Checks performed (C2.md)

1. **Contract fidelity.** Every root contract in docs/contracts.md is
   implemented (`go doc` of the root matches the table, apart from m11). The
   import graph is as specified: the root uses core, module and pipeline;
   openapi, config and logger use only the root; gonesttest uses the root
   plus `core.NewCtx`.
2. **NestJS parity.** Pipeline order and filter precedence are covered by
   `TestPipelineOrder` and `TestExceptionFilters` (route → controller →
   global → default, with panicking filters) and by the guide's executed
   trace (`examples/guide/pipeline_test.go`). Module visibility and lifecycle
   order are covered by B1's tests. No gap required new tests, apart from
   m13 (documentation).
3. **Performance.** See below. Reading the request path confirms that
   `route.serve` uses only precomputed slices, a cached binder, an atomic
   load of the globals and one `gc.Get` for the request logger. No DI
   lookups, tag parsing or chain composition happen per request.
   `pprof -top` on the guarded route: validator.Struct is about 24% of
   binding time, `GetQueryArray` about 50% (shared with raw Gin's
   `c.Query`), and `reflect.New` plus boxing about 12%.
4. **Robustness.**
   - `go test -race -count=3 ./...` passes.
   - The new fuzz target `FuzzBind` (`internal/pipeline/fuzz_test.go`) ran
     for 2 minutes: 140k execs, 264 corpus entries, no 5xx and no missing
     error body. Earlier runs seemed to stall at 0 execs/s, but that was
     the fuzzer's 60 s minimisation phase, not a hang (replayed: the input
     runs in 0 ms).
   - Graceful shutdown under load: `TestServeGracefulShutdownUnderLoad`
     (16 clients, 50 ms handler, a real listener). Every accepted request
     returns 200, no handler is still running when `OnModuleDestroy`
     starts, and `Serve` returns nil. It passes 3 times with `-race`.
   - No goroutine leak in `Serve`: the result channel is buffered and always
     drained.
5. **Ergonomics.** I wrote a fresh `todos` module (`Bind[Store]`, service,
   controller with path and JSON inputs, `NoContent`, `AppGuard` with
   `Public()` metadata, a gonesttest test) using only `docs/guide`. It
   compiled and passed on the first attempt. Places where the guide left me
   unsure:
   - whether `Bind[I]` accepts a constructor returning an unexported
     concrete type (it does);
   - whether `ExpectErrorMessage` matches a substring (it does, and it
     searches details too);
   - where application-wide gin middleware goes (now documented, m2);
   - how to keep `/docs` reachable behind a global guard (fixed with
     `RouteOptions`, M5).
6. **Consistency.** gofmt, `go vet` and golangci-lint report 0 issues.
   examples/docscheck passes after every guide change.

## Benchmarks (`examples/bench`)

Shared 2-vCPU Xeon, `GOMAXPROCS=1`, Gin release mode, `-benchtime 200000x`.
"Before" is merged main, 6 runs. "After" is `agent/c2`, 12 interleaved
rounds of a prebuilt test binary. The table gives min ns/op (median), plus
allocs/op.

| Scenario | Gin | Gonest before | Gonest after | allocs Gin / before / after |
|---|---|---|---|---|
| NoInput | 1271 (1340) | 1374 (1426), **+13%** min | 1276 (1416), **+0%** min | 11 / 12 / **11** |
| PathQuery (validated) | 1633 (1922) | 2326 (2617) | 2124 (2497), +30% | 14 / 17 / **15** |
| PathQueryPtr (validated) | 1633 | 2282 | 2168, +33% | 14 / 16 / **15** |
| PathQueryNoValidatePtr | 1633 | 1873 | 1901, +16% | 14 / 16 / **15** |
| GuardInterceptor | 1743 (2019) | 2396 (2715) | 2379 (2730), +36% | 14 / 19 / **17** |
| JSONBody | 2755 (3083) | 2827 (3004) | 2761 (3280), **+0%** min | 21 / 23 / **21** |

"Before" percentages use the Gin minimums from the same run (for example
NoInput Gin was 1214 there). Run-to-run noise on this machine is about
±15%, so read the allocation counts and the min-to-min pairs.

What changed, and what was rejected:

- **Applied:** value inputs are bound into a per-binder `sync.Pool` scratch
  struct, which is zeroed after the copy-out (`0605822`, −1 alloc).
  `TestScratchValuesAreNotShared` checks that retained inputs keep their
  own slices, maps and pointers across sequential and concurrent requests,
  and a mutation test showed that removing the zeroing makes it fail.
- **Applied:** the response is encoded with gin's codec and written
  directly, skipping the boxed `render.JSON` (−1 alloc, part of the M1 fix).
- **Rejected:** pooling `Ctx`, because it may escape into goroutines (A2's
  decision, upheld).
- **Rejected:** sharing the pooled struct's memory with the interface via
  `unsafe`, because the gain was not worth the unsafe code.
- **Rejected:** a single depth-counter `next` closure for interceptors. It
  saves nothing with one interceptor and breaks interceptors that call
  `next` from another goroutine.
- **Rejected:** per-field `validator.Var` instead of `Struct`. It would lose
  cross-field and struct-level rules.
- **Remaining gap:** on validated routes the gap is go-playground/validator,
  about 250 ns for two rules. The raw Gin baseline only does `id < 1`, so
  the comparison overstates the framework cost: the JSON-body pair, where
  both sides validate, is level. The <5% target is met where both sides do
  equivalent work and missed by a constant ~0.25 µs where Gonest
  reflectively binds query and path values. docs/guide/performance.md and
  the README have been updated with these numbers.

## Coverage (`go test -cover ./...`)

| Package | Coverage |
|---|---|
| `gonest` (root) | 97.7% |
| `cmd/gonest` | 91.8% |
| `gonesttest` | 97.8% |
| `internal/codegen` | 90.5% |
| `internal/core` (via `-coverpkg` across the repo) | 90.4% |
| `internal/di` | 99.4% |
| `internal/module` | 99.2% |
| `internal/pipeline` | 97.4% |
| `modules/config` | 94.2% |
| `modules/logger` | 98.0% |
| `openapi` | 96.9% |
| `examples/saas/...` (`-coverpkg`) | 89.2% |

## Fixes applied (branch `agent/c2`)

| Commit | Change |
|---|---|
| `0605822` | pipeline: recycle the binding scratch of value inputs (−1 alloc, isolation test) |
| `bb8d2f2` | pipeline: middleware request logger becomes `Ctx.Logger()` (M2); guide, example and contracts updated |
| `1f56d85` | openapi: `time.Duration` is integer nanoseconds in JSON and a string in parameters (M3) |
| `b7d4b70` | cmd/gonest: end-to-end test compiles the full template too (m3) |
| `224f72b` | pipeline: 413 for bodies over a `MaxBytesReader` limit; guide note (M4, partial) |
| `89e5cb9` | Listen: a second signal is no longer swallowed (m1) |
| `5716a40` | pipeline: encode before writing, logged 500 on failure (M1); `FuzzBind` |
| `1612327` | docs: where application-wide gin middleware must go (m2) |
| `8e9e24e` | test: graceful shutdown under concurrent load |
| `4c94073` | openapi: `Options.RouteOptions` for the docs routes (M5, partial) |
| `1dccd04` | pipeline: write the encoded response without gin's renderer (no extra alloc) |
| `b75d46f` | docs: performance numbers refreshed |

No exported signature of `internal/core` or the root package changed. The
new exported identifiers are `pipeline.LoggerKey` (internal package) and
`openapi.Options.RouteOptions` (additive field). go.mod and go.sum are
untouched.

## v0.1 recommendation: GO

Release v0.1 with M4 (body size limit) and M5 (global guards in OpenAPI) in
the release notes' known limitations. Schedule `WithBodyLimit` and
`openapi.GlobalSecurity`/`Public()` for v0.2, and settle m11 (exported
surface of core types) before any v1 freeze.
