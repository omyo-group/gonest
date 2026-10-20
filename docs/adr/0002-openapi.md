# ADR 0002: OpenAPI generation without a dependency

Status: accepted (agent B2)

## Context

Gonest documents its routes in OpenAPI 3.1 and serves Swagger UI. Go libraries
exist (kin-openapi, huma, swaggest), but go.mod is frozen and they bring
OpenAPI 3.0 models, router coupling or reflection helpers we do not need.

## Decision

- **No OpenAPI dependency.** `openapi/types.go` has the small subset of the
  3.1 object model that Gonest emits (Document, PathItem, Operation,
  Parameter, RequestBody, Response, MediaType, Schema, Components,
  SecurityScheme). Types are plain structs with json tags and `omitempty`;
  maps give deterministic output because `encoding/json` sorts keys.
- **Public API only.** The package imports the root package `gonest`
  (`RouteInfo`, `RouteTable`, `Handler`, `ErrorBody`...) and the standard
  library. It never touches `internal/*`.
- **Pure generator.** `Generate(routes, info, opts...)` depends only on its
  arguments, so golden tests need no application. `Module` is a thin wrapper
  that calls it once in `OnApplicationBootstrap`, when `*RouteTable` is
  complete, caches the encoded bytes and serves them: nothing is computed per
  request.
- **Schemas come from Go types by reflection, once, at bootstrap.** Struct
  types become `components/schemas` entries named `pkg.Type` (sanitised;
  generic arguments folded into the name) and are referenced with `$ref`;
  recursion is handled by reserving the component name before building it.
  The JSON-body view of a handler input is a separate component
  (`pkg.TypeBody`) holding only the `json`-tagged fields, since the same struct
  also carries path/query/header fields.
- **3.1 idioms:** nullable pointers are `type: [T, "null"]` (or
  `oneOf: [$ref, null]` for references), `exclusiveMinimum` is numeric.
- **Swagger UI is embedded, not loaded from a CDN.** `ui/` holds
  swagger-ui-dist 5.x `swagger-ui-bundle.js`, `swagger-ui.css` and the Apache-2.0
  LICENSE (about 1.9 MB), served from `embed.FS`. The HTML page uses only
  relative URLs, so it works under a global prefix and offline.

## Mapping rules (summary)

- `validate`: `required` -> object `required` (unless json `omitempty`);
  `min/max/len/gte/lte/gt/lt` -> minLength/maxLength (strings),
  minimum/maximum (numbers), minItems/maxItems (slices); `oneof` -> enum;
  `email`, `url`/`uri`, `uuid*` -> format; rules after `dive` apply to elements.
- `doc` -> description, `example` and `default` -> converted to the field type.
- `time.Duration` -> integer/int64 (nanoseconds, as encoding/json writes it)
  in JSON schemas; string in parameters and form fields, which the binder
  parses with `time.ParseDuration` (amended by C2).
- Parameters from `path`/`query`/`header` (path always required, others when
  `validate` has `required`); body from `json` fields on POST/PUT/PATCH;
  `form` fields -> urlencoded, or multipart when a file field is present.
- Responses: success status and `Out` schema (none for `NoContent`, raw
  handlers, 1xx/204/205/304), documented `Responds`, then 400 (input has
  tagged fields), 401/403 (route or controller guards) and 500 as `ErrorBody`.
  Global guards are not visible in `RouteInfo`, so they add no 401/403.
- `Security(names...)` lists alternatives (OR). A name without a scheme makes
  `Generate` (and so application bootstrap) fail.

## Consequences

- We maintain the model ourselves; unsupported constructs (callbacks, links,
  webhooks, oauth2 flows, discriminators) are simply absent.
- Fields without `validate` are never `required`, even if always emitted
  (documented simplification).
