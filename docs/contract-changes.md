# Contract change proposals

The contracts in `internal/core` and the root package aliases are frozen after
Wave 0. To change them, append a proposal below. Only the lead applies accepted
proposals.

## Template

### <short title> (agent <id>, <date>)

- **Problem:** what the current contract prevents.
- **Proposal:** the exact change (signatures).
- **Impact:** which packages and agents are affected.
- **Workaround in use:** what you did meanwhile.
- **Decision:** (lead) accepted / rejected, with reason.

---

### Document binding extensions and the middleware path convention (agent A2, 2026-10-20)

- **Problem:** `docs/contracts.md` "Input binding rules" does not mention three
  behaviours the pipeline now implements, and does not say whether the path
  given to `MiddlewareFor` includes `GlobalPrefix`.
- **Proposal (documentation only, no signature change):** add to "Input binding rules":
  - `Handle[*In, Out]` is accepted: the input is bound into a fresh `*In` per
    request (saves one allocation versus a value `In`).
  - Any type whose pointer implements `encoding.TextUnmarshaler` is a
    supported scalar for path/query/header/form (and slices of it).
  - `form` fields of type `*multipart.FileHeader` / `[]*multipart.FileHeader`
    receive uploaded files.
  - An empty value (`?page=`) for a non-string scalar counts as missing (the
    default applies); slices drop empty items.
  - Exported fields without a source tag are never bound, even from the JSON
    body (they are reset after decoding), so a body cannot override path or
    query values.
  - A struct mixing `json` and `form` fields, a `path` field naming a
    parameter absent from the route, or an unsupported field type is a Mount
    error (fails at bootstrap).
  Add to "Internal seams": `MiddlewareFor` receives `RouteInfo.Path`, which
  includes `GlobalPrefix`. B1 should strip the prefix before matching
  `RouteSpec`s if NestJS semantics are wanted (Nest's `forRoutes` paths are
  relative to the global prefix).
- **Impact:** docs/contracts.md (lead), B1 (middleware matching), C1 (guide).
- **Workaround in use:** behaviour implemented and tested in internal/pipeline.
- **Decision:** (lead) accepted. Documented in docs/contracts.md; B1 strips GlobalPrefix before matching RouteSpecs (NestJS semantics).

### Framework logger is always registered as a fallback (agent B1, 2026-10-20)

- **Problem:** docs/contracts.md and prompts/B1.md say the framework
  `*slog.Logger` is registered "unless the module graph provides one". Taken
  literally, a module that provides a private (unexported) logger, such as
  `AppModule` in `examples_contract_test.go` (`gonest.Value(slog.Default())`
  in the root, not exported), removes the framework logger for every other
  module: `UsersService` in "users" then fails with "not visible".
- **Proposal (documentation only):** the framework `*slog.Logger` (from
  `WithLogger`, else `slog.Default()`) is always registered with a nil owner.
  The container already prefers the module's own provider, then a single
  visible module provider, over framework providers, so a module-provided
  logger (e.g. `logger.ForRoot`, global) still wins wherever it is visible;
  modules that cannot see one get the framework logger instead of an error.
  The application's own logger (pipeline, startup log) is the `*slog.Logger`
  resolved from the root module. Reword contracts.md "Framework providers"
  accordingly.
- **Impact:** docs/contracts.md (lead), C1 guide wording. No code change elsewhere.
- **Workaround in use:** implemented as proposed in internal/module.
- **Decision:** (lead)
