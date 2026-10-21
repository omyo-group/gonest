package logger

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/omyo-group/gonest"
	"github.com/omyo-group/gonest/internal/core"
)

func parseLines(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, ln := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if ln == "" {
			continue
		}
		m := map[string]any{}
		if err := json.Unmarshal([]byte(ln), &m); err != nil {
			t.Fatalf("bad json %q: %v", ln, err)
		}
		out = append(out, m)
	}
	return out
}

func engine(l *slog.Logger) *gin.Engine {
	gin.SetMode(gin.TestMode)
	e := gin.New()
	e.Use(RequestLogger(l))
	e.GET("/ok", func(c *gin.Context) { c.String(200, "hello") })
	e.GET("/bad", func(c *gin.Context) { c.Status(404) })
	e.GET("/boom", func(c *gin.Context) { c.Status(500) })
	e.GET("/child", func(c *gin.Context) {
		v, _ := c.Get(ContextKey)
		v.(*slog.Logger).Info("inside")
		c.Status(204)
	})
	return e
}

func TestRequestLogger(t *testing.T) {
	tests := []struct {
		name, path, reqID, level string
		status, bytes            int
	}{
		{"ok", "/ok", "", "INFO", 200, 5},
		{"warn", "/bad", "abc-123", "WARN", 404, 0},
		{"error", "/boom", "", "ERROR", 500, 0},
		{"invalid id replaced", "/ok", "bad id", "INFO", 200, 5},
		{"too long id replaced", "/ok", strings.Repeat("a", 200), "INFO", 200, 5},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			e := engine(New(Options{Format: "json", Output: &buf}))
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			if tc.reqID != "" {
				req.Header.Set(RequestIDHeader, tc.reqID)
			}
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)
			lines := parseLines(t, &buf)
			if len(lines) != 1 {
				t.Fatalf("lines: %v", lines)
			}
			m := lines[0]
			echoed := rec.Header().Get(RequestIDHeader)
			if echoed == "" || m["request_id"] != echoed {
				t.Errorf("id echo %q vs %v", echoed, m["request_id"])
			}
			if tc.name == "warn" && echoed != "abc-123" {
				t.Errorf("id not propagated: %q", echoed)
			}
			if strings.HasPrefix(tc.name, "invalid") || strings.HasPrefix(tc.name, "too") {
				if echoed == tc.reqID || len(echoed) != 32 {
					t.Errorf("expected generated id, got %q", echoed)
				}
			}
			if m["level"] != tc.level || m["method"] != "GET" || m["path"] != tc.path ||
				int(m["status"].(float64)) != tc.status || int(m["bytes"].(float64)) != tc.bytes {
				t.Errorf("record: %v", m)
			}
			if _, ok := m["latency"]; !ok {
				t.Error("no latency")
			}
			if _, ok := m["client_ip"]; !ok {
				t.Error("no client_ip")
			}
		})
	}
}

func TestChildLoggerInContext(t *testing.T) {
	var buf bytes.Buffer
	e := engine(New(Options{Format: "json", Output: &buf}))
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/child", nil))
	lines := parseLines(t, &buf)
	if len(lines) != 2 || lines[0]["msg"] != "inside" || lines[0]["request_id"] != rec.Header().Get(RequestIDHeader) {
		t.Fatalf("%v", lines)
	}
}

func TestNilLoggerUsesDefault(t *testing.T) {
	if RequestLogger(nil) == nil {
		t.Fatal("nil")
	}
}

func TestNewFormats(t *testing.T) {
	attr := []slog.Attr{slog.String("svc", "x")}
	tests := []struct {
		name string
		opts Options
		env  string
		json bool
	}{
		{"explicit json", Options{Format: "json"}, "", true},
		{"explicit text", Options{Format: "text"}, "release", false},
		{"release default json", Options{}, "release", true},
		{"debug default text", Options{}, "debug", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("GIN_MODE", tc.env)
			var buf bytes.Buffer
			tc.opts.Output = &buf
			tc.opts.Attrs = attr
			tc.opts.AddSource = true
			l := New(tc.opts)
			l.Debug("hidden")
			l.Info("shown")
			if strings.Contains(buf.String(), "hidden") {
				t.Error("level not applied")
			}
			out := buf.String()
			if tc.json != strings.HasPrefix(out, "{") {
				t.Errorf("format: %s", out)
			}
			if !strings.Contains(out, "svc") || !strings.Contains(out, "logger_test.go") {
				t.Errorf("attrs/source missing: %s", out)
			}
		})
	}
	var buf bytes.Buffer
	l := New(Options{Level: slog.LevelDebug, Output: &buf})
	l.Debug("d")
	if !strings.Contains(buf.String(), "d") {
		t.Error("debug level")
	}
	_ = New(Options{}) // default stdout
}

func TestForRoot(t *testing.T) {
	m := ForRoot(Options{Output: &bytes.Buffer{}})
	if m.Name != "logger" || !m.Global || len(m.Providers) != 1 || len(m.Exports) != 1 {
		t.Fatalf("%+v", m)
	}
	if m.Exports[0] != gonest.TokenOf[*slog.Logger]() || m.Providers[0].Token != gonest.TokenOf[*slog.Logger]() {
		t.Error("token")
	}
	l := m.Providers[0].Ctor.(func() *slog.Logger)()
	if l == nil {
		t.Fatal("nil logger")
	}
}

func TestFromCtx(t *testing.T) {
	gin.SetMode(gin.TestMode)
	fallback := slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil))
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	ctx := core.NewCtx(c, nil, fallback)
	if FromCtx(ctx) != fallback {
		t.Error("expected fallback")
	}
	child := slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil))
	c.Set(ContextKey, child)
	if FromCtx(ctx) != child {
		t.Error("expected child")
	}
}

func TestRequestIDHelpers(t *testing.T) {
	if validRequestID("") || validRequestID("a\nb") || validRequestID("é") || !validRequestID("ok-1_2") {
		t.Error("validRequestID")
	}
	a, b := newRequestID(), newRequestID()
	if a == b || len(a) != 32 {
		t.Errorf("ids %q %q", a, b)
	}
}

func BenchmarkRequestLogger(b *testing.B) {
	e := engine(New(Options{Format: "json", Output: discard{}}))
	req := httptest.NewRequest(http.MethodGet, "/ok", nil)
	rec := httptest.NewRecorder()
	b.ReportAllocs()
	for b.Loop() {
		e.ServeHTTP(rec, req)
	}
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }
