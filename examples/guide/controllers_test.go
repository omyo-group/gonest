package guide_test

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/omyo/gonest"
)

// docs:input-tags

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

// docs:end

// docs:controller

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

// docs:end

// docs:handlers

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

// docs:end

func Example_controllers() {
	app, err := gonest.New(&gonest.Module{Name: "app", Controllers: []any{NewArticlesController}}, quiet()...)
	if err != nil {
		fmt.Println(err)
		return
	}
	// docs:controllers-output
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
	// docs:end

	// Output:
	// 200 {"limit":10,"org":"acme","since":"24h0m0s","tags":["go","web"],"verbose":true}
	// 400 {"statusCode":400,"message":"validation failed","error":"Bad Request","details":[{"field":"limit","source":"query","rule":"max","param":"50","message":"must be at most 50"}]}
	// 400 {"statusCode":400,"message":"validation failed","error":"Bad Request","details":[{"field":"limit","source":"query","rule":"type","param":"int","message":"must be an integer"}]}
	// 400 {"statusCode":400,"message":"validation failed","error":"Bad Request","details":[{"field":"title","source":"body","rule":"min","param":"3","message":"must be at least 3 characters long"}]}
	// 201 {"Org":"acme","title":"Hello"}
	// 202 {"ID":7,"title":"New"}
	// 204
	// 404 {"statusCode":404,"message":"article 404 not found","error":"Not Found"}
	// 200 id,title
	// 1,hello
	// 405 {"statusCode":405,"message":"Cannot PATCH /orgs/acme/articles","error":"Method Not Allowed"}
}

func Example_globalPrefix() {
	app, err := gonest.New(&gonest.Module{Name: "app", Controllers: []any{NewArticlesController}},
		append(quiet(), gonest.WithGlobalPrefix("/api/v1"))...)
	if err != nil {
		fmt.Println(err)
		return
	}
	for _, r := range app.Routes() {
		if strings.HasSuffix(r.Path, "csv") {
			fmt.Println(r.Method, r.Path, r.Handler.Name)
		}
	}
	// Output: GET /api/v1/orgs/:org/articles/export/csv ArticlesController.ExportCSV
}
