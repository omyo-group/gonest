package pipeline

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/omyo/gonest/internal/core"
)

type benchOut struct {
	ID   int    `json:"id"`
	Page int    `json:"page"`
	Name string `json:"name"`
}

type benchIn struct {
	ID   int `path:"id"`
	Page int `query:"page" default:"1"`
}

type benchInValidated struct {
	ID   int `path:"id" validate:"gt=0"`
	Page int `query:"page" default:"1" validate:"min=1"`
}

type benchBody struct {
	ID   int    `path:"id"`
	Name string `json:"name" validate:"required"`
}

func runBench(b *testing.B, h http.Handler, method, target, body string) {
	b.Helper()
	req := httptest.NewRequest(method, target, nil)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if body != "" {
			req.Body = io.NopCloser(strings.NewReader(body))
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code >= 300 {
			b.Fatalf("status %d: %s", rec.Code, rec.Body)
		}
	}
}

func benchMounter(b *testing.B) (*gin.Engine, *Mounter) {
	b.Helper()
	gin.SetMode(gin.ReleaseMode)
	e := gin.New()
	m := New(Options{Engine: e, Routes: &core.RouteTable{}, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	return e, m
}

func mustMount(b *testing.B, m *Mounter, fn func(r core.Router)) {
	b.Helper()
	if _, err := m.Mount(ctrlFunc(fn), "bench", nil); err != nil {
		b.Fatal(err)
	}
}

// BenchmarkRawGin is the baseline: a hand-written gin handler doing the same
// work as BenchmarkGonestHandle (path param, query param with default, JSON).
func BenchmarkRawGin(b *testing.B) {
	gin.SetMode(gin.ReleaseMode)
	e := gin.New()
	e.GET("/users/:id", func(c *gin.Context) {
		id, err := strconv.Atoi(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": "bad id"})
			return
		}
		page := 1
		if p := c.Query("page"); p != "" {
			if page, err = strconv.Atoi(p); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"message": "bad page"})
				return
			}
		}
		c.JSON(http.StatusOK, benchOut{ID: id, Page: page, Name: "alice"})
	})
	runBench(b, e, http.MethodGet, "/users/42?page=3", "")
}

// BenchmarkGonestHandle is the same endpoint through the Gonest pipeline.
func BenchmarkGonestHandle(b *testing.B) {
	e, m := benchMounter(b)
	mustMount(b, m, func(r core.Router) {
		r.GET("/users/:id", core.Handle(func(_ *core.Ctx, in benchIn) (benchOut, error) {
			return benchOut{ID: in.ID, Page: in.Page, Name: "alice"}, nil
		}))
	})
	runBench(b, e, http.MethodGet, "/users/42?page=3", "")
}

// BenchmarkGonestHandleValidated adds validator rules to the input.
func BenchmarkGonestHandleValidated(b *testing.B) {
	e, m := benchMounter(b)
	mustMount(b, m, func(r core.Router) {
		r.GET("/users/:id", core.Handle(func(_ *core.Ctx, in benchInValidated) (benchOut, error) {
			return benchOut{ID: in.ID, Page: in.Page, Name: "alice"}, nil
		}))
	})
	runBench(b, e, http.MethodGet, "/users/42?page=3", "")
}

// BenchmarkGonestHandleEnhancers adds a global guard, a controller
// interceptor and a route pipe.
func BenchmarkGonestHandleEnhancers(b *testing.B) {
	e, m := benchMounter(b)
	mustMount(b, m, func(r core.Router) {
		r.Use(core.UseInterceptors(core.InterceptorFunc(func(_ *core.Ctx, next core.Next) (any, error) { return next() })))
		r.GET("/users/:id", core.Handle(func(_ *core.Ctx, in benchIn) (benchOut, error) {
			return benchOut{ID: in.ID, Page: in.Page, Name: "alice"}, nil
		}), core.UsePipes(core.PipeFunc(func(_ *core.Ctx, v any, _ core.ArgMeta) (any, error) { return v, nil })))
	})
	m.SetGlobals(Globals{Guards: []core.Guard{core.GuardFunc(func(*core.Ctx) (bool, error) { return true, nil })}})
	runBench(b, e, http.MethodGet, "/users/42?page=3", "")
}

// BenchmarkRawGinJSONBody is the baseline for a JSON body endpoint.
func BenchmarkRawGinJSONBody(b *testing.B) {
	gin.SetMode(gin.ReleaseMode)
	e := gin.New()
	e.POST("/users/:id", func(c *gin.Context) {
		id, err := strconv.Atoi(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": "bad id"})
			return
		}
		var in struct {
			Name string `json:"name" binding:"required"`
		}
		if err := c.ShouldBindJSON(&in); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
			return
		}
		c.JSON(http.StatusCreated, benchOut{ID: id, Name: in.Name})
	})
	runBench(b, e, http.MethodPost, "/users/42", `{"name":"alice"}`)
}

// BenchmarkGonestJSONBody is the same JSON body endpoint through Gonest.
func BenchmarkGonestJSONBody(b *testing.B) {
	e, m := benchMounter(b)
	mustMount(b, m, func(r core.Router) {
		r.POST("/users/:id", core.Handle(func(_ *core.Ctx, in benchBody) (benchOut, error) {
			return benchOut{ID: in.ID, Name: in.Name}, nil
		}))
	})
	runBench(b, e, http.MethodPost, "/users/42", `{"name":"alice"}`)
}

// BenchmarkRawGinNoInput is the baseline for a route without input binding.
func BenchmarkRawGinNoInput(b *testing.B) {
	gin.SetMode(gin.ReleaseMode)
	e := gin.New()
	e.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, benchOut{ID: 1, Name: "alice"})
	})
	runBench(b, e, http.MethodGet, "/health", "")
}

// BenchmarkGonestNoInput measures the pipeline alone (core.Empty input).
func BenchmarkGonestNoInput(b *testing.B) {
	e, m := benchMounter(b)
	mustMount(b, m, func(r core.Router) {
		r.GET("/health", core.Handle(func(*core.Ctx, core.Empty) (benchOut, error) {
			return benchOut{ID: 1, Name: "alice"}, nil
		}))
	})
	runBench(b, e, http.MethodGet, "/health", "")
}
