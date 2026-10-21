package bench

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/omyo-group/gonest"
)

// Item is the response of every benchmarked endpoint.
type Item struct {
	ID      int    `json:"id"`
	Name    string `json:"name"`
	Verbose bool   `json:"verbose"`
}

// lookup is the shared "business logic": both stacks call it.
func lookup(id int, verbose bool) Item { return Item{ID: id, Name: "item", Verbose: verbose} }

// ---- Gonest side ----------------------------------------------------------

type getInput struct {
	ID      int  `path:"id" validate:"min=1"`
	Verbose bool `query:"verbose"`
}

// getInputNoValidate is getInput without validate tags: the validator is
// skipped entirely for such types.
type getInputNoValidate struct {
	ID      int  `path:"id"`
	Verbose bool `query:"verbose"`
}

type createInput struct {
	Name string `json:"name" validate:"required"`
}

type itemsController struct{ auth gonest.Guard }

func (c *itemsController) Routes(r gonest.Router) {
	r.Prefix("/items")
	r.GET("/:id", gonest.Handle(c.Get))
	r.GET("/:id/ptr", gonest.Handle(c.GetPtr))
	r.GET("/:id/novalidate", gonest.Handle(c.GetNoValidate))
	r.GET("/:id/guarded", gonest.Handle(c.Get),
		gonest.UseGuards(c.auth),
		gonest.UseInterceptors(gonest.InterceptorFunc(func(_ *gonest.Ctx, next gonest.Next) (any, error) { return next() })))
	r.GET("", gonest.Handle(c.List))
	r.POST("", gonest.Handle(c.Create))
}

func (c *itemsController) Get(_ *gonest.Ctx, in getInput) (Item, error) {
	return lookup(in.ID, in.Verbose), nil
}

func (c *itemsController) GetPtr(_ *gonest.Ctx, in *getInput) (Item, error) {
	return lookup(in.ID, in.Verbose), nil
}

func (c *itemsController) GetNoValidate(_ *gonest.Ctx, in *getInputNoValidate) (Item, error) {
	return lookup(in.ID, in.Verbose), nil
}

func (c *itemsController) List(_ *gonest.Ctx, _ gonest.Empty) (Item, error) {
	return lookup(1, false), nil
}

func (c *itemsController) Create(_ *gonest.Ctx, in createInput) (Item, error) {
	return Item{ID: 1, Name: in.Name}, nil
}

var bearerGuard = gonest.GuardFunc(func(ctx *gonest.Ctx) (bool, error) {
	if ctx.Header("Authorization") != "Bearer t" {
		return false, gonest.Unauthorized("no")
	}
	return true, nil
})

func gonestEngine(b *testing.B) http.Handler {
	b.Helper()
	gin.SetMode(gin.ReleaseMode)
	mod := &gonest.Module{
		Name:        "bench",
		Controllers: []any{func() *itemsController { return &itemsController{auth: bearerGuard} }},
	}
	app, err := gonest.New(mod, gonest.WithoutStartupLog(),
		gonest.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))))
	if err != nil {
		b.Fatal(err)
	}
	return app.Handler()
}

// ---- raw Gin side: the same endpoints written by hand -----------------------

func ginEngine() http.Handler {
	gin.SetMode(gin.ReleaseMode)
	e := gin.New()
	get := func(c *gin.Context) {
		id, err := strconv.Atoi(c.Param("id"))
		if err != nil || id < 1 {
			c.JSON(http.StatusBadRequest, gin.H{"message": "bad id"})
			return
		}
		verbose, _ := strconv.ParseBool(c.Query("verbose"))
		c.JSON(http.StatusOK, lookup(id, verbose))
	}
	e.GET("/items/:id", get)
	e.GET("/items/:id/guarded", func(c *gin.Context) {
		if c.GetHeader("Authorization") != "Bearer t" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"message": "no"})
			return
		}
		c.Next()
	}, get)
	e.GET("/items", func(c *gin.Context) { c.JSON(http.StatusOK, lookup(1, false)) })
	e.POST("/items", func(c *gin.Context) {
		var in struct {
			Name string `json:"name" binding:"required"`
		}
		if err := c.ShouldBindJSON(&in); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
			return
		}
		c.JSON(http.StatusCreated, Item{ID: 1, Name: in.Name})
	})
	return e
}

// ---- benchmarks ------------------------------------------------------------

type scenario struct {
	name, method, path, body string
	header                   map[string]string
	status                   int
}

var scenarios = []scenario{
	{name: "NoInput", method: "GET", path: "/items", status: 200},
	{name: "PathQuery", method: "GET", path: "/items/42?verbose=true", status: 200},
	{name: "PathQueryPtr", method: "GET", path: "/items/42/ptr?verbose=true", status: 200},
	{name: "PathQueryNoValidatePtr", method: "GET", path: "/items/42/novalidate?verbose=true", status: 200},
	{name: "GuardInterceptor", method: "GET", path: "/items/42/guarded?verbose=true", header: map[string]string{"Authorization": "Bearer t"}, status: 200},
	{name: "JSONBody", method: "POST", path: "/items", body: `{"name":"pen"}`, status: 201},
}

func run(b *testing.B, h http.Handler, s scenario) {
	b.Helper()
	req := httptest.NewRequest(s.method, s.path, nil)
	for k, v := range s.header {
		req.Header.Set(k, v)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if s.body != "" {
			req.Body = io.NopCloser(strings.NewReader(s.body))
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != s.status {
			b.Fatalf("%s: status %d: %s", s.name, w.Code, w.Body)
		}
	}
}

// BenchmarkEndpoints runs every scenario against raw Gin and Gonest. The
// scenarios ending in "Ptr" are Gonest variants of PathQuery (pointer input,
// no validate tags) with no raw Gin twin: compare them with PathQuery/Gin.
func BenchmarkEndpoints(b *testing.B) {
	g := ginEngine()
	n := gonestEngine(b)
	for _, s := range scenarios {
		if !strings.HasSuffix(s.name, "Ptr") {
			b.Run(s.name+"/Gin", func(b *testing.B) { run(b, g, s) })
		}
		b.Run(s.name+"/Gonest", func(b *testing.B) { run(b, n, s) })
	}
}

// TestEndpointsAgree checks that both stacks return the same bodies, so the
// benchmark compares equivalent work.
func TestEndpointsAgree(t *testing.T) {
	g := ginEngine()
	app, err := gonest.New(&gonest.Module{
		Name:        "bench",
		Controllers: []any{func() *itemsController { return &itemsController{auth: bearerGuard} }},
	}, gonest.WithoutStartupLog(), gonest.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))))
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range scenarios {
		t.Run(s.name, func(t *testing.T) {
			p := strings.NewReplacer("/ptr", "", "/novalidate", "").Replace(s.path)
			var bodies [2]string
			for i, h := range []http.Handler{g, app.Handler()} {
				path := p
				if i == 1 {
					path = s.path
				}
				req := httptest.NewRequest(s.method, path, strings.NewReader(s.body))
				for k, v := range s.header {
					req.Header.Set(k, v)
				}
				w := httptest.NewRecorder()
				h.ServeHTTP(w, req)
				if w.Code != s.status {
					t.Fatalf("handler %d: status %d", i, w.Code)
				}
				bodies[i] = w.Body.String()
			}
			if bodies[0] != bodies[1] {
				t.Fatalf("gin %s != gonest %s", bodies[0], bodies[1])
			}
		})
	}
}
