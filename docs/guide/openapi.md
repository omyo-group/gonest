# OpenAPI

`github.com/omyo/gonest/openapi` generates an OpenAPI 3.1 document from the
mounted routes and serves it with an embedded Swagger UI (no CDN, works
offline). There are no annotations to write: request and response schemas
come from the handler's `In` and `Out` types, and the rest from route
options.

## Serving the document

Import `openapi.Module(opts)`; it serves the JSON at `/openapi.json` and
Swagger UI at `/docs` (`JSONPath`, `UIPath` and `DisableUI` change that).
From the SaaS root module:

<!-- snippet: examples/saas/internal/app/app.go#root-module -->
```go
// NewModule returns the root module. Dynamic modules (config.ForRoot,
// logger.ForRoot, openapi.Module) are functions returning *gonest.Module, so
// the root is built by a function too.
func NewModule(o Options) *gonest.Module {
	imports := []*gonest.Module{
		// Global modules: *settings.Config and *slog.Logger are injectable everywhere.
		config.ForRoot[settings.Config](config.Options{
			Prefix:   settings.EnvPrefix,
			Lookup:   o.Lookup,
			EnvFiles: o.EnvFiles,
		}),
		logger.ForRoot(logger.Options{Level: o.LogLevel, Format: o.LogFormat, Output: o.LogOutput}),

		platform.Module,
		auth.Module,
		tenants.Module,
		projects.Module,
	}
	if o.Docs {
		imports = append(imports, openapi.Module(openapi.Options{
			Info: openapi.Info{
				Title:       "Gonest SaaS example",
				Version:     "1.0.0",
				Description: "Multi-tenant projects API. Log in with POST /auth/login, then send the token and X-Tenant-ID.",
			},
			SecuritySchemes: map[string]openapi.SecurityScheme{"bearer": openapi.BearerJWT()},
			// Enabled explicitly for local exploration: acknowledge that
			// the documentation is unguarded.
			Public: true,
		}))
	}
	return &gonest.Module{
		Name:      "app",
		Imports:   imports,
		Configure: configure,
	}
}
```

The document is
built once, during `OnApplicationBootstrap`, when every route is known; each
request for it returns the cached bytes. The module's own routes are hidden
from the document and follow `gonest.WithGlobalPrefix`. If a route requires
a security scheme that is not defined, `gonest.New` fails.

Global guards apply to the documentation routes too. `RouteOptions` adds
route options to them: `gonest.SetMetadata(...)` to let a global guard with
a "public" escape hatch (see [Pipeline](pipeline.md#global-guard-with-a-public-escape-hatch))
through, or `gonest.UseGuards(...)` to protect the documentation.

The document maps every route of the API, so in release mode (the Gonest
default) `gonest.New` logs a warning when `RouteOptions` carry no guard.
Set `Public: true` to silence it when that is intended: the API is public,
or a global guard already covers the documentation. The SaaS example goes
one step further and only imports the module when the server runs with
`-docs`.

## What ends up in the document

| Source | OpenAPI |
|---|---|
| `path`, `query`, `header` fields of `In` | parameters (path always required, others when `validate` has `required`) |
| `json` fields of `In` on POST/PUT/PATCH | request body schema (`pkg.TypeBody` component) |
| `form` fields | urlencoded body, or multipart when a file field is present |
| `Out` | success response schema; nothing for `gonest.NoContent` and raw handlers |
| `validate` rules | `required`, `minLength`/`maxLength`, `minimum`/`maximum`, `minItems`/`maxItems`, `enum` (`oneof`), `format` (`email`, `uri`, `uuid`) |
| `doc`, `example`, `default` tags | description, example, default |
| `time.Duration` | integer (int64 nanoseconds, as `encoding/json` writes it) in JSON bodies and outputs; string such as `1h30m` in parameters and form fields |
| `gonest.Summary`, `Description`, `Tags`, `Deprecated` | operation fields (tags default to the first path segment) |
| `gonest.Security("bearer")` | security requirement; several names are alternatives |
| `gonest.Responds(status, desc, body)` | extra documented responses |
| route or controller guards | 401 and 403 responses |
| always | 400 (when `In` has tagged fields) and 500 as `gonest.ErrorBody` |
| `gonest.Hidden()` | route omitted |

The operation id is the handler name (`ProjectsController.Create`). Go types
become `components/schemas` entries named `package.Type`; generic types fold
their arguments into the name (`projects.Page_projects.Project`).

The SaaS document lists every endpoint with these responses:

```text
POST   /auth/login        200 400 401 500
GET    /auth/me           200 401 403 500
GET    /health            200 500
GET    /projects          200 400 401 403 500
POST   /projects          201 400 401 403 409 422 500
GET    /projects/{id}     200 400 401 403 404 500
PATCH  /projects/{id}     200 400 401 403 404 409 422 500
DELETE /projects/{id}     204 400 401 403 404 500
GET    /tenants/current   200 400 401 403 500
```

The `X-Tenant-ID` header appears as a required header parameter on every
tenant route because the inputs embed `tenants.TenantHeader`, and the list
parameters carry their defaults and bounds (`limit`: integer, default 20,
minimum 1, maximum 100).

## Generating the document without a server

`openapi.Generate(routes, info, opts...)` is a pure function of the route
table, handy in CI or to generate clients:

<!-- snippet: examples/guide/openapi_test.go#openapi-generate -->
```go
doc, err := openapi.Generate(app.Routes(), openapi.Info{Title: "Articles", Version: "1.0.0"},
	openapi.WithServers(openapi.Server{URL: "https://api.example.com"}))
if err != nil {
	fmt.Println(err)
	return
}
out := filepath.Join(os.TempDir(), "openapi.json")
if err := openapi.WriteFile(doc, out); err != nil { // e.g. in CI, for client generation
	fmt.Println(err)
	return
}
```

For the articles controller of [Controllers](controllers.md) it yields:

```text
GET /orgs/{org}/articles ArticlesController.Search [200 400 500] false
POST /orgs/{org}/articles ArticlesController.Create [201 400 500] false
GET /orgs/{org}/articles/export/csv ArticlesController.ExportCSV [200 500] true
PUT /orgs/{org}/articles/{id} ArticlesController.Replace [202 400 500] false
DELETE /orgs/{org}/articles/{id} ArticlesController.Delete [204 400 500] false
```

(method, path, operation id, response codes, deprecated).

## Limitations

- Global guards (`AppGuard`, `WithGlobalGuards`) are not part of the route
  metadata, so they add no 401/403 responses or security requirements.
- No callbacks, links, webhooks, discriminators or OAuth2 flows; no cookie
  parameters.
