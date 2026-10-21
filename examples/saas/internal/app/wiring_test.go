package app_test

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/omyo-group/gonest"
	"github.com/omyo-group/gonest/examples/saas/internal/app"
	"github.com/omyo-group/gonest/examples/saas/internal/auth"
	"github.com/omyo-group/gonest/examples/saas/internal/platform"
	"github.com/omyo-group/gonest/examples/saas/internal/projects"
	"github.com/omyo-group/gonest/examples/saas/internal/settings"
	"github.com/omyo-group/gonest/gonesttest"
	"github.com/omyo-group/gonest/modules/config"
)

func TestResponseHeaders(t *testing.T) {
	a := newApp(t)
	tok := login(t, a, "bob@acme.test")

	res := a.Request("GET", "/projects").WithBearer(tok).WithHeader("X-Tenant-ID", "acme").
		WithHeader("X-Request-ID", "req-123").Do().ExpectStatus(200)
	if res.Header().Get(platform.TimingHeader) == "" {
		t.Error("timing interceptor did not set " + platform.TimingHeader)
	}
	res.ExpectHeader("X-Request-ID", "req-123") // echoed by logger.RequestLogger

	// The timing interceptor also wraps failing handlers.
	res = a.Request("GET", "/projects/99").WithBearer(tok).WithHeader("X-Tenant-ID", "acme").Do().ExpectStatus(404)
	if res.Header().Get(platform.TimingHeader) == "" {
		t.Error("no timing header on error response")
	}

	// /health is excluded from the request logger middleware.
	res = a.Request("GET", "/health").Do().ExpectStatus(200)
	if res.Header().Get("X-Request-ID") != "" {
		t.Error("request logger ran on /health")
	}
}

func TestOpenAPIDocument(t *testing.T) {
	a := newApp(t)
	var doc struct {
		OpenAPI string                               `json:"openapi"`
		Paths   map[string]map[string]map[string]any `json:"paths"`
		Comps   struct {
			Security map[string]any `json:"securitySchemes"`
		} `json:"components"`
	}
	a.Request("GET", "/openapi.json").Do().ExpectStatus(200).JSON(&doc)
	if doc.OpenAPI != "3.1.0" {
		t.Errorf("openapi = %q", doc.OpenAPI)
	}
	if _, ok := doc.Comps.Security["bearer"]; !ok {
		t.Error("bearer scheme missing")
	}
	for _, r := range a.Routes() {
		if r.Config.Hidden {
			continue
		}
		p := r.Path
		if strings.Contains(p, ":id") {
			p = strings.ReplaceAll(p, ":id", "{id}")
		}
		if _, ok := doc.Paths[p][strings.ToLower(r.Method)]; !ok {
			t.Errorf("%s %s missing from the document", r.Method, p)
		}
	}
	if _, ok := doc.Paths["/docs"]; ok {
		t.Error("hidden docs route listed")
	}
	// The documentation is tenant-independent: no X-Tenant-ID needed.
	a.Request("GET", "/docs").Do().ExpectStatus(200).ExpectHeader("Content-Type", "text/html; charset=utf-8")
	a.Request("GET", "/docs/swagger-ui.css").Do().ExpectStatus(200)
}

func TestDocsDisabled(t *testing.T) {
	o := env(nil)
	o.Docs = false
	a := gonesttest.New(t, app.NewModule(o)).Compile()
	for _, path := range []string{"/openapi.json", "/docs"} {
		a.Request("GET", path).Do().ExpectStatus(404)
	}
}

// docs:testing-override

// TestTokenExpiry replaces the auth module's Clock provider, so the test
// controls time without sleeping.
func TestTokenExpiry(t *testing.T) {
	var mu sync.Mutex
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { mu.Lock(); defer mu.Unlock(); return now }

	a := gonesttest.New(t, app.NewModule(env(map[string]string{"SAAS_TOKEN_TTL": "5m"}))).
		OverrideProvider(gonest.Value[auth.Clock](clock)).
		Compile()

	tok := login(t, a, "alice@acme.test")
	a.Request("GET", "/auth/me").WithBearer(tok).Do().ExpectStatus(http.StatusOK)

	mu.Lock()
	now = now.Add(6 * time.Minute)
	mu.Unlock()
	a.Request("GET", "/auth/me").WithBearer(tok).Do().
		ExpectStatus(http.StatusUnauthorized).
		ExpectErrorMessage("token expired")
}

// docs:end

// TestRepositoryOverride swaps the Repository binding for an instance the
// test owns; OnModuleInit seeds into it.
func TestRepositoryOverride(t *testing.T) {
	repo := projects.NewMemoryRepository()
	a := gonesttest.New(t, app.NewModule(env(nil))).
		OverrideProvider(gonest.Bind[projects.Repository](func() *projects.MemoryRepository { return repo })).
		Compile()
	if got := len(repo.List("acme")); got != 3 {
		t.Fatalf("seeded %d acme projects, want 3", got)
	}
	tok := login(t, a, "carol@globex.test")
	a.Request("DELETE", "/projects/4").WithBearer(tok).WithHeader("X-Tenant-ID", "globex").Do().ExpectStatus(204)
	if len(repo.List("globex")) != 0 {
		t.Fatal("delete did not reach the overridden repository")
	}
}

func TestConfiguration(t *testing.T) {
	a := gonesttest.New(t, app.NewModule(env(map[string]string{
		"SAAS_PORT":       "9090",
		"SAAS_JWT_SECRET": "0123456789abcdef0123456789abcdef-test",
	}))).Compile()
	cfg := gonesttest.Get[*settings.Config](a)
	if cfg.Port != 9090 || cfg.TokenTTL != time.Hour || cfg.JWTSecret != "0123456789abcdef0123456789abcdef-test" {
		t.Fatalf("config = %+v", cfg)
	}
	if s := config.Redacted(cfg); strings.Contains(s, "0123456789") {
		t.Fatalf("secret leaked: %s", s)
	}
}

func TestInvalidConfigurationFailsBootstrap(t *testing.T) {
	cases := map[string]map[string]string{
		"short secret": {"SAAS_JWT_SECRET": "short"},
		"empty secret": {"SAAS_JWT_SECRET": ""},
		"bad port":     {"SAAS_PORT": "http"},
		"port range":   {"SAAS_PORT": "70000"},
	}
	for name, vars := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := gonest.New(app.NewModule(env(vars)), gonest.WithoutStartupLog())
			var cerr *config.Error
			if !errors.As(err, &cerr) {
				t.Fatalf("err = %v, want *config.Error", err)
			}
			if strings.Contains(err.Error(), "short") && name == "short secret" {
				t.Fatalf("secret value leaked in %v", err)
			}
		})
	}
}

func TestLifecycleLogs(t *testing.T) {
	var buf safeBuffer
	o := env(nil)
	o.LogOutput = &buf
	o.LogFormat = "text"
	a, err := gonest.New(app.NewModule(o), gonest.WithoutStartupLog())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "seeded projects") {
		t.Errorf("OnModuleInit did not run: %s", buf.String())
	}
	if err := a.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "projects service stopped") {
		t.Errorf("OnApplicationShutdown did not run: %s", buf.String())
	}
}

// TestServeGracefulShutdown runs a real listener and cancels its context,
// the same path Listen takes on SIGTERM.
func TestServeGracefulShutdown(t *testing.T) {
	var buf safeBuffer
	o := env(nil)
	o.LogOutput = &buf
	a, err := gonest.New(app.NewModule(o), gonest.WithoutStartupLog())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	srv := &http.Server{Addr: "127.0.0.1:0", ReadHeaderTimeout: time.Second}
	done := make(chan error, 1)
	go func() { done <- a.Serve(ctx, srv) }()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Serve did not return")
	}
	if !strings.Contains(buf.String(), "projects service stopped") {
		t.Errorf("shutdown hooks did not run: %s", buf.String())
	}
}

type safeBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *safeBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *safeBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}
