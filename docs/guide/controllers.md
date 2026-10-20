# Controllers

A controller is any value with a `Routes(r gonest.Router)` method. It is
listed in `Module.Controllers` as a constructor, so its dependencies are
injected like a provider's. `Routes` is called once, at startup.

<!-- snippet: examples/guide/controllers_test.go#controller -->
```go
// ArticlesController shows Prefix, Use, Group and route options.
type ArticlesController struct{}

// NewArticlesController is listed in Module.Controllers.
func NewArticlesController() *ArticlesController { return &ArticlesController{} }

// Routes declares the routes (the decorators of a NestJS controller).
func (c *ArticlesController) Routes(r gonest.Router) {
	r.Prefix("/orgs/:org/articles")
	r.Use(gonest.Tags("articles")) // applies to every route declared below

	r.GET("", gonest.Handle(c.Search), gonest.Summary("Search articles"))
	r.POST("", gonest.Handle(c.Create))                                         // 201 by default
	r.PUT("/:id", gonest.Handle(c.Replace), gonest.Status(http.StatusAccepted)) // 202
	r.DELETE("/:id", gonest.Handle(c.Delete))                                   // NoContent => 204

	r.Group("/export", func(g gonest.Router) {
		g.GET("/csv", gonest.HandleRaw(c.ExportCSV))
	}, gonest.Deprecated())
}
```

Router methods:

| Method | Meaning |
|---|---|
| `Prefix(p)` | path prefix of every route of the controller (`@Controller('p')`) |
| `Use(opts...)` | options for every route declared after it (controller-level decorators) |
| `Group(prefix, fn, opts...)` | sub-router with an extra prefix and options |
| `GET`, `POST`, `PUT`, `PATCH`, `DELETE`, `OPTIONS`, `HEAD`, `Handle(method, ...)` | declare a route |

Paths use Gin syntax (`:id`, `*file`). Declaring the same method and path
twice, or a path that Gin rejects, makes `gonest.New` fail.

## Handlers

`gonest.Handle(fn)` takes a typed handler
`func(ctx *gonest.Ctx, in In) (Out, error)`:

- `In` is a struct bound from the request and validated before the handler
  runs. Use `gonest.Empty` when there is no input. `*In` also works and
  saves one allocation per request.
- `Out` is written as JSON. Use `gonest.NoContent` for a 204 without body.
- A non-nil error goes to the exception filters (see
  [the pipeline](pipeline.md)).

`gonest.HandleRaw(func(ctx *gonest.Ctx) error)` is for handlers that write
their own response (files, streams, SSE): guards, interceptors and filters
still apply; there is no binding and no pipe.

<!-- snippet: examples/guide/controllers_test.go#handlers -->
```go
// Search is a typed handler: input bound and validated, output written as JSON.
func (c *ArticlesController) Search(_ *gonest.Ctx, in SearchInput) (map[string]any, error) {
	return map[string]any{
		"org": in.Org, "tags": in.Tags, "limit": in.Limit,
		"since": in.Since.String(), "verbose": in.Verbose != nil && *in.Verbose,
	}, nil
}

// ArticleInput is a JSON body plus a path parameter. Fields without a source
// tag are never bound, so a body cannot override path values.
type ArticleInput struct {
	Org   string `path:"org"`
	Title string `json:"title" validate:"required,min=3"`
}

// Create returns 201 Created because the route is a POST.
func (c *ArticlesController) Create(_ *gonest.Ctx, in ArticleInput) (ArticleInput, error) {
	return in, nil
}

// ReplaceInput identifies an article.
type ReplaceInput struct {
	ID    int    `path:"id" validate:"min=1"`
	Title string `json:"title"`
}

// Replace overrides the status for this request only with SetStatus.
func (c *ArticlesController) Replace(ctx *gonest.Ctx, in ReplaceInput) (ReplaceInput, error) {
	if in.Title == "" {
		ctx.SetStatus(http.StatusOK)
	}
	return in, nil
}

// IDInput identifies an article.
type IDInput struct {
	ID int `path:"id"`
}

// Delete answers 204 No Content.
func (c *ArticlesController) Delete(_ *gonest.Ctx, in IDInput) (gonest.NoContent, error) {
	if in.ID == 404 {
		return gonest.NoContent{}, gonest.NotFound(fmt.Sprintf("article %d not found", in.ID))
	}
	return gonest.NoContent{}, nil
}

// ExportCSV writes its own response; guards, interceptors and filters still
// apply, binding and pipes do not.
func (c *ArticlesController) ExportCSV(ctx *gonest.Ctx) error {
	ctx.Gin().Header("Content-Type", "text/csv")
	_, err := ctx.Writer().WriteString("id,title\n1,hello\n")
	return err
}
```

The SaaS projects controller:

<!-- snippet: examples/saas/internal/projects/controller.go#routes -->
```go
// Routes implements gonest.Controller. Options passed to r.Use apply to every
// route below it (controller-level decorators in NestJS).
func (c *ProjectsController) Routes(r gonest.Router) {
	r.Prefix("/projects")
	r.Use(
		gonest.Tags("projects"),
		gonest.Security("bearer"),
		// Guards run in order: authenticate, check tenant membership, check roles.
		gonest.UseGuards(c.jwt, tenants.MemberGuard, c.roles),
		gonest.UseFilters(DomainErrorFilter),
	)

	r.GET("", gonest.Handle(c.List), gonest.Summary("List projects"))
	r.POST("", gonest.Handle(c.Create),
		gonest.Summary("Create a project"),
		gonest.UsePipes(TrimPipe),
		gonest.Responds(http.StatusConflict, "Name already used", gonest.ErrorBody{}),
		gonest.Responds(http.StatusUnprocessableEntity, "Plan quota exceeded", gonest.ErrorBody{}))
	r.GET("/:id", gonest.Handle(c.FindOne),
		gonest.Summary("Get a project"),
		gonest.Responds(http.StatusNotFound, "Not found", gonest.ErrorBody{}))
	r.PATCH("/:id", gonest.Handle(c.Update),
		gonest.Summary("Update a project"),
		gonest.Responds(http.StatusNotFound, "Not found", gonest.ErrorBody{}),
		gonest.Responds(http.StatusConflict, "Name already used", gonest.ErrorBody{}),
		gonest.Responds(http.StatusUnprocessableEntity, "Archived project", gonest.ErrorBody{}))
	r.DELETE("/:id", gonest.Handle(c.Delete),
		gonest.Summary("Delete a project (admins only)"),
		auth.Roles(auth.RoleAdmin),
		gonest.Responds(http.StatusNotFound, "Not found", gonest.ErrorBody{}))
}
```

<!-- snippet: examples/saas/internal/projects/controller.go#handlers -->
```go
// List returns a page of the current tenant's projects.
func (c *ProjectsController) List(ctx *gonest.Ctx, in ListInput) (Page[Project], error) {
	t, err := tenantOf(ctx)
	if err != nil {
		return Page[Project]{}, err
	}
	return c.svc.List(ctx, t, in), nil
}

// FindOne returns one project.
func (c *ProjectsController) FindOne(ctx *gonest.Ctx, in IDInput) (Project, error) {
	t, err := tenantOf(ctx)
	if err != nil {
		return Project{}, err
	}
	return c.svc.Get(ctx, t, in.ID)
}

// Create adds a project (201 Created, the default for POST).
func (c *ProjectsController) Create(ctx *gonest.Ctx, in CreateInput) (Project, error) {
	t, err := tenantOf(ctx)
	if err != nil {
		return Project{}, err
	}
	return c.svc.Create(ctx, t, in.Name, in.Description)
}

// Update changes a project.
func (c *ProjectsController) Update(ctx *gonest.Ctx, in UpdateInput) (Project, error) {
	t, err := tenantOf(ctx)
	if err != nil {
		return Project{}, err
	}
	return c.svc.Update(ctx, t, in)
}

// Delete removes a project and answers 204 No Content.
func (c *ProjectsController) Delete(ctx *gonest.Ctx, in IDInput) (gonest.NoContent, error) {
	t, err := tenantOf(ctx)
	if err != nil {
		return gonest.NoContent{}, err
	}
	return gonest.NoContent{}, c.svc.Delete(ctx, t, in.ID)
}
```

## Input binding

One struct per handler replaces `@Param`, `@Query`, `@Headers` and `@Body`.
Each exported field names its source with one tag:

| Tag | Source |
|---|---|
| `path:"id"` | path parameter (must exist in the route, checked at startup) |
| `query:"page"` | query string |
| `header:"X-Tenant"` | request header |
| `form:"f"` | urlencoded or multipart form; `*multipart.FileHeader` / `[]*multipart.FileHeader` fields receive uploads |
| `json:"name"` | JSON body (decoded when the struct has a json field and the body is not empty) |

Other tags:

| Tag | Meaning |
|---|---|
| `default:"20"` | value used when a query, header or form value is missing or empty |
| `validate:"..."` | go-playground/validator rules |
| `doc:"..."`, `example:"..."` | OpenAPI description and example |

Supported field types for path, query, header and form: strings, bools, all
integer and float kinds, `time.Time` (RFC 3339), `time.Duration`, types whose
pointer implements `encoding.TextUnmarshaler`, pointers to these (nil when
absent) and, except for path, slices of these (repeated keys or comma
separated). Embedded structs are flattened, which is how the SaaS example
shares its tenant header between inputs.

<!-- snippet: examples/guide/controllers_test.go#input-tags -->
```go
// SearchInput shows every binding source. One struct per handler replaces
// @Param, @Query, @Headers and @Body.
type SearchInput struct {
	Org     string        `path:"org"`
	Tags    []string      `query:"tag"`                                  // ?tag=a&tag=b or ?tag=a,b
	Limit   int           `query:"limit" default:"10" validate:"max=50"` // default when missing or empty
	Since   time.Duration `query:"since" default:"24h"`
	Tenant  string        `header:"X-Tenant" validate:"required"`
	Verbose *bool         `query:"verbose"` // nil when absent
}
```

Exported fields without a source tag are never bound, not even from the JSON
body, so a client cannot override a path value with a body field. A struct
mixing `json` and `form` fields, or with an unsupported field type, fails at
startup.

The pagination DTO of the SaaS example:

<!-- snippet: examples/saas/internal/projects/model.go#inputs -->
```go
// ListInput is the pagination query DTO. Missing or empty values take the
// default; out of range values fail validation with 400.
type ListInput struct {
	tenants.TenantHeader
	Page   int    `query:"page" default:"1" validate:"min=1" doc:"1-based page number"`
	Limit  int    `query:"limit" default:"20" validate:"min=1,max=100" doc:"page size"`
	Search string `query:"q" validate:"omitempty,max=64" doc:"case-insensitive name filter"`
	Status string `query:"status" validate:"omitempty,oneof=active archived"`
}

// IDInput selects one project.
type IDInput struct {
	tenants.TenantHeader
	ID int `path:"id" validate:"min=1"`
}

// CreateInput is the body of POST /projects.
type CreateInput struct {
	tenants.TenantHeader
	Name        string `json:"name" validate:"required,min=3,max=64" example:"Website relaunch"`
	Description string `json:"description" validate:"max=500"`
}

// UpdateInput is the body of PATCH /projects/:id. Nil pointers mean
// "leave unchanged".
type UpdateInput struct {
	tenants.TenantHeader
	ID          int     `path:"id" validate:"min=1"`
	Name        *string `json:"name" validate:"omitempty,min=3,max=64"`
	Description *string `json:"description" validate:"omitempty,max=500"`
	Status      *string `json:"status" validate:"omitempty,oneof=active archived"`
}
```

## Responses and errors

What the controller above returns, request by request:

<!-- snippet: examples/guide/controllers_test.go#controllers-output -->
```go
fmt.Println(call(app, "GET", "/orgs/acme/articles?tag=go,web&verbose=true", "", "X-Tenant", "t1"))
fmt.Println(call(app, "GET", "/orgs/acme/articles?limit=99", "", "X-Tenant", "t1"))
fmt.Println(call(app, "GET", "/orgs/acme/articles?limit=abc", "", "X-Tenant", "t1"))
fmt.Println(call(app, "POST", "/orgs/acme/articles", `{"title":"Hi"}`))
fmt.Println(call(app, "POST", "/orgs/acme/articles", `{"title":"Hello","Org":"evil"}`))
fmt.Println(call(app, "PUT", "/orgs/acme/articles/7", `{"title":"New"}`))
fmt.Println(call(app, "DELETE", "/orgs/acme/articles/7", ""))
fmt.Println(call(app, "DELETE", "/orgs/acme/articles/404", ""))
fmt.Println(call(app, "GET", "/orgs/acme/articles/export/csv", ""))
fmt.Println(call(app, "PATCH", "/orgs/acme/articles", ""))
```

```text
200 {"limit":10,"org":"acme","since":"24h0m0s","tags":["go","web"],"verbose":true}
400 {"statusCode":400,"message":"validation failed","error":"Bad Request","details":[{"field":"limit","source":"query","rule":"max","param":"50","message":"must be at most 50"}]}
400 {"statusCode":400,"message":"validation failed","error":"Bad Request","details":[{"field":"limit","source":"query","rule":"type","param":"int","message":"must be an integer"}]}
400 {"statusCode":400,"message":"validation failed","error":"Bad Request","details":[{"field":"title","source":"body","rule":"min","param":"3","message":"must be at least 3 characters long"}]}
201 {"Org":"acme","title":"Hello"}
202 {"ID":7,"title":"New"}
204
404 {"statusCode":404,"message":"article 404 not found","error":"Not Found"}
200 id,title
1,hello
405 {"statusCode":405,"message":"Cannot PATCH /orgs/acme/articles","error":"Method Not Allowed"}
```

(The output is checked by `go test ./examples/guide/`.) Notes:

- The success status is 201 for POST and 200 otherwise, unless the route or
  controller sets `gonest.Status(code)`, the handler calls
  `ctx.SetStatus(code)` for one request, or `Out` is `gonest.NoContent`
  (always 204).
- Validation failures are 400 with one `FieldError` per problem; field names
  are the client-facing names (`limit`, `title`, `X-Tenant`) and `source`
  says where they came from. Unparseable values fail with rule `type`.
- The JSON field `Org` in the fifth request was ignored: `Org` comes from the
  path.
- Unknown paths answer 404 `Cannot GET /x`; a known path with another method
  answers 405, both as `gonest.ErrorBody`.

Return `*gonest.HTTPError` values to choose the status:
`gonest.BadRequest`, `Unauthorized`, `Forbidden`, `NotFound`, `Conflict`,
`UnprocessableEntity`, `TooManyRequests`, `Internal`, or
`gonest.NewHTTPError(status, msg, details)`. They render as

```json
{"statusCode": 404, "message": "project 99 not found", "error": "Not Found", "details": ...}
```

Attach a cause with `.Wrap(err)`: it is logged for 5xx responses and never
sent to the client. Any other error becomes 500 `Internal Server Error` and
is logged by the application logger; panics are recovered the same way.

## The request context

`*gonest.Ctx` wraps `*gin.Context` and implements `context.Context`
(cancelled when the client goes away), so pass it to anything that takes a
context. Useful methods: `Param`, `Query`, `Header`, `Set`/`Get` (request
values shared with Gin middleware), `gonest.CtxValue[T](ctx, key)`,
`Route()` (the matched route and its metadata), `Logger()`, `SetStatus`,
`Gin()`, `Request()`, `Writer()`.

Request values are how the SaaS example passes the authenticated user from
a guard to handlers (the NestJS `@CurrentUser()` custom decorator):

<!-- snippet: examples/saas/internal/auth/guards.go#current-user -->
```go
// CurrentUser returns the user authenticated by JWTGuard for this request
// (the NestJS @CurrentUser() custom decorator).
func CurrentUser(ctx *gonest.Ctx) (*User, bool) {
	return gonest.CtxValue[*User](ctx, UserKey)
}
```

## Global prefix

`gonest.WithGlobalPrefix("/api/v1")` prefixes every route, including the
OpenAPI routes. Middleware `RouteSpec` paths stay relative to the prefix.
