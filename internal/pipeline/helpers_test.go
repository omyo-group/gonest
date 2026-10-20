package pipeline

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/omyo/gonest/internal/core"
)

func TestMain(m *testing.M) {
	gin.SetMode(gin.ReleaseMode)
	os.Exit(m.Run())
}

// ctrlFunc adapts a function to core.Controller.
type ctrlFunc func(r core.Router)

func (f ctrlFunc) Routes(r core.Router) { f(r) }

// syncBuffer is a goroutine-safe log sink.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

type harness struct {
	t      *testing.T
	m      *Mounter
	engine *gin.Engine
	routes *core.RouteTable
	logs   *syncBuffer
}

func newHarness(t *testing.T, prefix string) *harness {
	t.Helper()
	logs := &syncBuffer{}
	e := gin.New()
	rt := &core.RouteTable{}
	m := New(Options{
		Engine:       e,
		Routes:       rt,
		GlobalPrefix: prefix,
		Logger:       slog.New(slog.NewTextHandler(logs, nil)),
	})
	return &harness{t: t, m: m, engine: e, routes: rt, logs: logs}
}

func (h *harness) mount(fn func(r core.Router)) []*core.RouteInfo {
	h.t.Helper()
	infos, err := h.m.Mount(ctrlFunc(fn), "test", nil)
	if err != nil {
		h.t.Fatalf("Mount: %v", err)
	}
	return infos
}

type reqOpt func(*http.Request)

func withHeader(k, v string) reqOpt { return func(r *http.Request) { r.Header.Add(k, v) } }

func (h *harness) do(method, target, body string, opts ...reqOpt) *httptest.ResponseRecorder {
	h.t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, target, rd)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for _, o := range opts {
		o(req)
	}
	rec := httptest.NewRecorder()
	h.engine.ServeHTTP(rec, req)
	return rec
}

// errorBody is core.ErrorBody with typed details.
type errorBody struct {
	StatusCode int               `json:"statusCode"`
	Message    string            `json:"message"`
	Error      string            `json:"error"`
	Details    []core.FieldError `json:"details"`
}

func decodeError(t *testing.T, rec *httptest.ResponseRecorder) errorBody {
	t.Helper()
	var eb errorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &eb); err != nil {
		t.Fatalf("decode error body %q: %v", rec.Body.String(), err)
	}
	return eb
}

func decodeJSONBody[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode body %q: %v", rec.Body.String(), err)
	}
	return v
}

// echo returns a handler that answers with its bound input.
func echo[T any]() *core.Handler {
	return core.Handle(func(_ *core.Ctx, in T) (T, error) { return in, nil })
}
