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
- **Decision:**
