package logger

import (
	"bytes"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/omyo/gonest"
	"github.com/omyo/gonest/internal/pipeline"
)

// ContextKey must stay equal to the key the pipeline reads, otherwise the
// framework's own logs lose request_id.
func TestContextKeyMatchesPipeline(t *testing.T) {
	if ContextKey != pipeline.LoggerKey {
		t.Fatalf("ContextKey %q != pipeline.LoggerKey %q", ContextKey, pipeline.LoggerKey)
	}
}

type failController struct{}

func (failController) Routes(r gonest.Router) {
	r.GET("/fail", gonest.Handle(func(ctx *gonest.Ctx, _ gonest.Empty) (string, error) {
		if FromCtx(ctx) != ctx.Logger() {
			return "", gonest.BadRequest("ctx.Logger() is not the request logger")
		}
		return "", errors.New("database unavailable")
	}))
}

// The framework's error log for a request carries request_id without any
// extra interceptor.
func TestFrameworkErrorLogCarriesRequestID(t *testing.T) {
	var buf bytes.Buffer
	root := &gonest.Module{
		Name:        "app",
		Imports:     []*gonest.Module{ForRoot(Options{Format: "json", Output: &buf})},
		Controllers: []any{func() failController { return failController{} }},
		Configure: func(m *gonest.MiddlewareConsumer, l *slog.Logger) {
			m.Apply(RequestLogger(l)).ForAllRoutes()
		},
	}
	app, err := gonest.New(root, gonest.WithoutStartupLog())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Shutdown(t.Context()) })
	req := httptest.NewRequest(http.MethodGet, "/fail", nil)
	req.Header.Set(RequestIDHeader, "rid-7")
	rec := httptest.NewRecorder()
	app.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var found bool
	for _, rec := range parseLines(t, &buf) {
		if rec["msg"] == "gonest: unhandled error" {
			found = true
			if rec["request_id"] != "rid-7" {
				t.Fatalf("framework error log without request_id: %v", rec)
			}
		}
	}
	if !found {
		t.Fatalf("no framework error log in %q", strings.TrimSpace(buf.String()))
	}
}
