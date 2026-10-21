package module

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/omyo-group/gonest/internal/core"
	"github.com/omyo-group/gonest/internal/pipeline"
)

// trace records hook calls.
type trace struct {
	mu  sync.Mutex
	evs []string
}

func (t *trace) add(s string) {
	t.mu.Lock()
	t.evs = append(t.evs, s)
	t.mu.Unlock()
}

func (t *trace) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return strings.Join(t.evs, " ")
}

// hooked implements every lifecycle hook and records it.
type hooked struct {
	name string
	tr   *trace
	fail string // hook name that fails
}

func (h *hooked) hook(name string) error {
	h.tr.add(name + ":" + h.name)
	if h.fail == name {
		return errors.New("boom")
	}
	return nil
}

func (h *hooked) OnModuleInit(context.Context) error           { return h.hook("init") }
func (h *hooked) OnApplicationBootstrap(context.Context) error { return h.hook("boot") }
func (h *hooked) OnModuleDestroy(context.Context) error        { return h.hook("destroy") }
func (h *hooked) BeforeApplicationShutdown(context.Context) error {
	return h.hook("before")
}
func (h *hooked) OnApplicationShutdown(context.Context) error { return h.hook("shutdown") }

type dbSvc struct{ *hooked }
type repoSvc struct{ *hooked }
type usersSvc struct{ *hooked }

type usersCtrl struct {
	*hooked
	users *usersSvc
}

func (c *usersCtrl) Routes(r core.Router) {
	r.Prefix("/users")
	r.GET("/:id", core.Handle(c.find))
	r.GET("", core.Handle(c.list))
}

type idIn struct {
	ID int `path:"id"`
}

func (c *usersCtrl) find(ctx *core.Ctx, in idIn) (map[string]any, error) {
	mw, _ := ctx.Get("mw")
	return map[string]any{"id": in.ID, "mw": mw}, nil
}

func (c *usersCtrl) list(ctx *core.Ctx, _ core.Empty) ([]string, error) {
	mw, _ := ctx.Get("mw")
	s, _ := mw.(string)
	return []string{s}, nil
}

type healthCtrl struct{}

func (healthCtrl) Routes(r core.Router) {
	r.GET("/health", core.HandleRaw(func(ctx *core.Ctx) error {
		mw, _ := ctx.Get("mw")
		s, _ := mw.(string)
		ctx.Gin().String(200, "ok"+s)
		return nil
	}))
}

type appFixture struct {
	tr               *trace
	db, users, root  *core.Module
	failHook, failOn string
}

func newFixture() *appFixture {
	f := &appFixture{tr: &trace{}}
	return f
}

func (f *appFixture) mk(name string) *hooked {
	h := &hooked{name: name, tr: f.tr}
	if f.failOn == name {
		h.fail = f.failHook
	}
	return h
}

// build returns a 3-module app: root imports users imports db (global would be
// equivalent). Middleware: db marks every route except /health, users marks
// GET /users/:id.
func (f *appFixture) build() *core.Module {
	f.db = &core.Module{
		Name: "db",
		Providers: []core.Provider{
			core.Provide(func() *dbSvc { return &dbSvc{f.mk("db")} }),
		},
		Exports: []core.Token{core.TokenOf[*dbSvc]()},
		Configure: func(m *core.MiddlewareConsumer, d *dbSvc) {
			m.Apply(mark("db")).Exclude(core.ForPath("health")).ForAllRoutes()
		},
	}
	f.users = &core.Module{
		Name:    "users",
		Imports: []*core.Module{f.db},
		Providers: []core.Provider{
			core.Provide(func(d *dbSvc) *repoSvc { return &repoSvc{f.mk("repo")} }),
			core.Provide(func(r *repoSvc) *usersSvc { return &usersSvc{f.mk("users")} }),
		},
		Controllers: []any{func(u *usersSvc) *usersCtrl { return &usersCtrl{hooked: f.mk("usersCtrl"), users: u} }},
		Exports:     []core.Token{core.TokenOf[*usersSvc]()},
		Configure: func(m *core.MiddlewareConsumer, u *usersSvc) error {
			m.Apply(mark("users")).ForRoutes(core.ForRoute("GET", "/users/:id"))
			return nil
		},
	}
	f.root = &core.Module{
		Name:        "root",
		Imports:     []*core.Module{f.users},
		Providers:   []core.Provider{core.Provide(func(u *usersSvc) *hooked { return f.mk("root") })},
		Controllers: []any{func() healthCtrl { return healthCtrl{} }},
	}
	return f.root
}

func mark(s string) gin.HandlerFunc {
	return func(c *gin.Context) {
		prev, _ := c.Get("mw")
		p, _ := prev.(string)
		c.Set("mw", p+"+"+s)
		c.Next()
	}
}

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
}

func boot(t *testing.T, root *core.Module, opts Options) *Runtime {
	t.Helper()
	if opts.Engine == nil {
		opts.Engine = gin.New()
	}
	if opts.Logger == nil {
		opts.Logger = quietLogger()
	}
	rt, err := Bootstrap(context.Background(), root, opts)
	if err != nil {
		t.Fatal(err)
	}
	return rt
}

func get(t *testing.T, h http.Handler, path string) (int, string) {
	t.Helper()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
	return w.Code, strings.TrimSpace(w.Body.String())
}

func TestBootstrapThreeModulesEndToEnd(t *testing.T) {
	for _, prefix := range []string{"", "/api/v1/"} {
		t.Run("prefix="+prefix, func(t *testing.T) {
			f := newFixture()
			e := gin.New()
			rt := boot(t, f.build(), Options{Engine: e, GlobalPrefix: prefix})
			p := cleanPrefix(prefix)

			tests := []struct{ path, want string }{
				{p + "/users/7", `{"id":7,"mw":"+db+users"}`},
				{p + "/users", `["+db"]`},
				{p + "/health", `ok`},
			}
			for _, tt := range tests {
				code, body := get(t, e, tt.path)
				if code != 200 || body != tt.want {
					t.Errorf("GET %s = %d %s, want %s", tt.path, code, body, tt.want)
				}
			}
			if code, body := get(t, e, p+"/nope"); code != 404 || !strings.Contains(body, "Cannot GET") {
				t.Errorf("fallback = %d %s", code, body)
			}
			if n := len(rt.Routes.All()); n != 3 {
				t.Errorf("routes = %d", n)
			}

			want := "init:db init:repo init:users init:usersCtrl init:root boot:db boot:repo boot:users boot:usersCtrl boot:root"
			if got := f.tr.String(); got != want {
				t.Fatalf("start trace:\n got %s\nwant %s", got, want)
			}
			f.tr.evs = nil
			if err := rt.Shutdown(context.Background()); err != nil {
				t.Fatal(err)
			}
			want = "destroy:root destroy:usersCtrl destroy:users destroy:repo destroy:db " +
				"before:root before:usersCtrl before:users before:repo before:db " +
				"shutdown:root shutdown:usersCtrl shutdown:users shutdown:repo shutdown:db"
			if got := f.tr.String(); got != want {
				t.Fatalf("stop trace:\n got %s\nwant %s", got, want)
			}
			f.tr.evs = nil
			if err := rt.Shutdown(context.Background()); err != nil || f.tr.String() != "" {
				t.Fatalf("second shutdown ran hooks: %v %q", err, f.tr.String())
			}
		})
	}
}

func TestBootstrapFailingHookCleanup(t *testing.T) {
	tests := []struct {
		name, hook, on string
		wantErr        string
		wantTrace      string
	}{
		{
			"init fails midway", "init", "users",
			`OnModuleInit of *module.usersSvc in module "users": boom`,
			"init:db init:repo init:users destroy:repo destroy:db before:repo before:db shutdown:repo shutdown:db",
		},
		{
			"bootstrap fails", "boot", "repo",
			`OnApplicationBootstrap of *module.repoSvc in module "users": boom`,
			"init:db init:repo init:users init:usersCtrl init:root boot:db boot:repo " +
				"destroy:root destroy:usersCtrl destroy:users destroy:repo destroy:db " +
				"before:root before:usersCtrl before:users before:repo before:db " +
				"shutdown:root shutdown:usersCtrl shutdown:users shutdown:repo shutdown:db",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture()
			f.failHook, f.failOn = tt.hook, tt.on
			_, err := Bootstrap(context.Background(), f.build(), Options{Engine: gin.New(), Logger: quietLogger()})
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want %q", err, tt.wantErr)
			}
			if got := f.tr.String(); got != tt.wantTrace {
				t.Fatalf("trace:\n got %s\nwant %s", got, tt.wantTrace)
			}
		})
	}
}

func TestBootstrapCleanupErrorsJoined(t *testing.T) {
	f := newFixture()
	root := f.build()
	f.failHook, f.failOn = "boot", "root"
	// Make db's destroy fail too.
	f.db.Providers = []core.Provider{core.Provide(func() *dbSvc {
		h := f.mk("db")
		h.fail = "destroy"
		return &dbSvc{h}
	})}
	_, err := Bootstrap(context.Background(), root, Options{Engine: gin.New(), Logger: quietLogger()})
	if err == nil || !strings.Contains(err.Error(), "OnApplicationBootstrap of *module.hooked") ||
		!strings.Contains(err.Error(), "cleanup after failed startup") || !strings.Contains(err.Error(), "OnModuleDestroy of *module.dbSvc") {
		t.Fatalf("err = %v", err)
	}
}

type panicky struct{}

func (panicky) OnModuleInit(context.Context) error { panic("kaboom") }

type deadline struct{ ok bool }

func (d *deadline) OnModuleInit(ctx context.Context) error {
	_, d.ok = ctx.Deadline()
	return nil
}

func TestHookPanicAndDeadline(t *testing.T) {
	d := &deadline{}
	rt := boot(t, &core.Module{Name: "root", Providers: []core.Provider{core.Value(d)}}, Options{})
	if !d.ok {
		t.Fatal("hook context has no deadline")
	}
	_ = rt
	_, err := Bootstrap(context.Background(), &core.Module{Name: "root", Providers: []core.Provider{core.Value(panicky{})}},
		Options{Engine: gin.New(), Logger: quietLogger()})
	if err == nil || !strings.Contains(err.Error(), "panic: kaboom") {
		t.Fatalf("err = %v", err)
	}
}

func TestLifecycleDedupeAndFramework(t *testing.T) {
	tr := &trace{}
	shared := &hooked{name: "shared", tr: tr}
	fw := &hooked{name: "fw", tr: tr}
	a := &core.Module{Name: "a"}
	root := &core.Module{Name: "root", Imports: []*core.Module{a}}
	g, err := Build(root)
	if err != nil {
		t.Fatal(err)
	}
	lc := NewLifecycle(g, []Target{
		{Module: root, Value: shared},
		{Module: a, Value: shared},
		{Module: a, Value: nil},
		{Module: nil, Value: fw},
		{Module: a, Value: struct{}{}},
	})
	if n := len(lc.Targets()); n != 3 {
		t.Fatalf("targets = %d", n)
	}
	if err := lc.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := tr.String(); got != "init:fw init:shared boot:fw boot:shared" {
		t.Fatalf("trace %s", got)
	}
}

// Middleware with injected deps, Exclude/ForRoutes and prefix stripping.

func TestMiddlewareFor(t *testing.T) {
	h1, h2, h3 := mark("1"), mark("2"), mark("3")
	mw := &Middleware{rules: []core.MiddlewareRule{
		{Handlers: []gin.HandlerFunc{h1}, All: true, Exclude: []core.RouteSpec{core.ForPath("/health")}},
		{Handlers: []gin.HandlerFunc{h2}, Include: []core.RouteSpec{core.ForPath("/users/*")}, Exclude: []core.RouteSpec{core.ForRoute("DELETE", "/users/:id")}},
		{Handlers: []gin.HandlerFunc{h3, h3}, Include: []core.RouteSpec{core.ForRoute("POST", "/users")}},
	}}
	tests := []struct {
		prefix, method, path string
		want                 int
	}{
		{"", "GET", "/health", 0},
		{"", "GET", "/users", 2},
		{"", "GET", "/users/:id", 2},
		{"", "DELETE", "/users/:id", 1},
		{"", "POST", "/users", 4},
		{"", "GET", "/other", 1},
		{"/api", "GET", "/api/health", 0},
		{"api/", "POST", "/api/users", 4},
		{"/api", "GET", "/api", 1},
		{"/api", "GET", "/apiusers", 1},
		{"/", "GET", "/health", 0},
	}
	for _, tt := range tests {
		t.Run(tt.prefix+" "+tt.method+" "+tt.path, func(t *testing.T) {
			if got := len(mw.For(tt.prefix)(tt.method, tt.path)); got != tt.want {
				t.Fatalf("got %d handlers, want %d", got, tt.want)
			}
		})
	}
	if len(mw.Rules()) != 3 {
		t.Fatal("rules")
	}
}

func TestConfigureErrors(t *testing.T) {
	type missing struct{}
	tests := []struct {
		name      string
		configure any
		want      string
	}{
		{"not a func", 42, "Configure must be"},
		{"no consumer", func() {}, "Configure must be"},
		{"wrong first", func(int) {}, "Configure must be"},
		{"variadic", func(*core.MiddlewareConsumer, ...int) {}, "Configure must be"},
		{"bad result", func(*core.MiddlewareConsumer) int { return 0 }, "Configure must be"},
		{"two results", func(*core.MiddlewareConsumer) (int, error) { return 0, nil }, "Configure must be"},
		{"returns error", func(*core.MiddlewareConsumer) error { return errors.New("nope") }, "Configure: nope"},
		{"missing dep", func(*core.MiddlewareConsumer, *missing) {}, "no provider"},
		{"panics", func(*core.MiddlewareConsumer) { panic("x") }, "panic"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Bootstrap(context.Background(), &core.Module{Name: "root", Configure: tt.configure},
				Options{Engine: gin.New(), Logger: quietLogger()})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
		})
	}
}

// Global enhancers from providers and options.

type tagGuard struct{ tag string }

func (g *tagGuard) CanActivate(ctx *core.Ctx) (bool, error) {
	prev, _ := ctx.Get("order")
	p, _ := prev.(string)
	ctx.Set("order", p+g.tag)
	return ctx.Header("X-Deny") == "", nil
}

type headerInterceptor struct{}

func (headerInterceptor) Intercept(ctx *core.Ctx, next core.Next) (any, error) {
	ctx.Writer().Header().Set("X-Intercepted", "yes")
	return next()
}

type passPipe struct{ calls *int }

func (p passPipe) Transform(_ *core.Ctx, v any, _ core.ArgMeta) (any, error) {
	*p.calls++
	return v, nil
}

type teapotFilter struct{}

func (teapotFilter) Catch(ctx *core.Ctx, err error) bool {
	ctx.Gin().String(http.StatusTeapot, "filtered: "+err.Error())
	return true
}

type orderCtrl struct{}

func (orderCtrl) Routes(r core.Router) {
	r.GET("/order", core.Handle(func(ctx *core.Ctx, _ core.Empty) (string, error) {
		v, _ := ctx.Get("order")
		s, _ := v.(string)
		return s, nil
	}))
	r.GET("/fail", core.Handle(func(*core.Ctx, core.Empty) (string, error) { return "", errors.New("x") }))
}

func TestGlobalEnhancers(t *testing.T) {
	calls := 0
	root := &core.Module{
		Name: "root",
		Providers: []core.Provider{
			core.AppGuard(func() *tagGuard { return &tagGuard{tag: "P"} }),
			core.AppInterceptor(func() headerInterceptor { return headerInterceptor{} }),
			core.AppPipe(func() passPipe { return passPipe{calls: &calls} }),
			core.AppFilter(func() teapotFilter { return teapotFilter{} }),
		},
		Controllers: []any{func() orderCtrl { return orderCtrl{} }},
	}
	e := gin.New()
	boot(t, root, Options{Engine: e, Globals: pipeline.Globals{Guards: []core.Guard{&tagGuard{tag: "O"}}}})

	w := httptest.NewRecorder()
	e.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/order", nil))
	var got string
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if w.Code != 200 || got != "OP" {
		t.Fatalf("order = %d %q, want OP (options before providers)", w.Code, got)
	}
	if w.Header().Get("X-Intercepted") != "yes" || calls != 1 {
		t.Fatalf("interceptor/pipe not applied: %v %d", w.Header(), calls)
	}
	if code, body := get(t, e, "/fail"); code != http.StatusTeapot || body != "filtered: x" {
		t.Fatalf("filter = %d %s", code, body)
	}
}

// Controller errors.

type notCtrl struct{}

func TestControllerErrors(t *testing.T) {
	type dep struct{}
	tests := []struct {
		name string
		ctor any
		want string
	}{
		{"not a controller", func() *notCtrl { return &notCtrl{} }, "does not implement Controller"},
		{"no result", func() {}, "returns no value"},
		{"missing dep", func(*dep) healthCtrl { return healthCtrl{} }, "no provider"},
		{"ctor error", func() (healthCtrl, error) { return healthCtrl{}, errors.New("bad ctor") }, "bad ctor"},
		{"mount error", func() *badRoutes { return &badRoutes{} }, "duplicate route"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Bootstrap(context.Background(), &core.Module{Name: "root", Controllers: []any{tt.ctor}},
				Options{Engine: gin.New(), Logger: quietLogger()})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
		})
	}
}

type badRoutes struct{}

func (badRoutes) Routes(r core.Router) {
	h := core.HandleRaw(func(*core.Ctx) error { return nil })
	r.GET("/x", h)
	r.GET("/x", h)
}

func TestBootstrapErrors(t *testing.T) {
	type missing struct{}
	tests := []struct {
		name string
		root *core.Module
		opts Options
		want string
	}{
		{"no engine", &core.Module{Name: "r"}, Options{Logger: quietLogger()}, "requires a gin engine"},
		{"bad graph", nil, Options{Engine: gin.New()}, "root module is nil"},
		{"bad exports", &core.Module{Name: "r", Exports: []core.Token{core.TokenOf[*missing]()}}, Options{Engine: gin.New()}, "neither provides"},
		{"bad provider", &core.Module{Name: "r", Providers: []core.Provider{core.Provide(42)}}, Options{Engine: gin.New()}, "cannot register"},
		{"validate", &core.Module{Name: "r", Providers: []core.Provider{core.Provide(func(*missing) int { return 1 })}}, Options{Engine: gin.New()}, "no provider"},
		{"not visible", &core.Module{Name: "r",
			Imports:   []*core.Module{{Name: "hidden", Providers: []core.Provider{core.Value(&missing{})}}},
			Providers: []core.Provider{core.Provide(func(*missing) int { return 1 })}}, Options{Engine: gin.New()}, "not visible"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Bootstrap(context.Background(), tt.root, tt.opts)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
		})
	}
}

// Framework providers and logger selection.

type needsFw struct {
	rt  *core.RouteTable
	log *slog.Logger
}

func TestFrameworkProviders(t *testing.T) {
	optLog := quietLogger()
	modLog := quietLogger()
	var seen *needsFw
	child := &core.Module{Name: "child", Providers: []core.Provider{core.Provide(func(rt *core.RouteTable, l *slog.Logger) *needsFw {
		seen = &needsFw{rt, l}
		return seen
	})}}

	t.Run("option logger", func(t *testing.T) {
		rt := boot(t, &core.Module{Name: "root", Imports: []*core.Module{child}}, Options{Logger: optLog})
		if seen.rt != rt.Routes || seen.log != optLog || rt.Logger != optLog {
			t.Fatal("framework providers not injected")
		}
	})
	t.Run("module logger wins where visible", func(t *testing.T) {
		logMod := &core.Module{Name: "logger", Global: true, Providers: []core.Provider{core.Value(modLog)},
			Exports: []core.Token{core.TokenOf[*slog.Logger]()}}
		rt := boot(t, &core.Module{Name: "root", Imports: []*core.Module{child, logMod}}, Options{Logger: optLog})
		if seen.log != modLog || rt.Logger != modLog {
			t.Fatal("module logger should win")
		}
	})
	t.Run("private module logger falls back to framework elsewhere", func(t *testing.T) {
		root := &core.Module{Name: "root", Imports: []*core.Module{child}, Providers: []core.Provider{core.Value(modLog)}}
		rt := boot(t, root, Options{Logger: optLog})
		if seen.log != optLog || rt.Logger != modLog {
			t.Fatal("unexpected logger selection")
		}
	})
	t.Run("default logger", func(t *testing.T) {
		rt, err := Bootstrap(context.Background(), &core.Module{Name: "root"}, Options{Engine: gin.New()})
		if err != nil || rt.Logger != slog.Default() {
			t.Fatalf("%v %v", err, rt)
		}
	})
}

func TestStartupLog(t *testing.T) {
	var buf bytes.Buffer
	f := newFixture()
	boot(t, f.build(), Options{Logger: slog.New(slog.NewTextHandler(&buf, nil)), StartupLog: true, GlobalPrefix: "/api"})
	out := buf.String()
	for _, want := range []string{"modules loaded", "count=3", "db, users, root", "GET /api/users/:id -> usersCtrl.find", "application started"} {
		if !strings.Contains(out, want) {
			t.Errorf("startup log missing %q:\n%s", want, out)
		}
	}
}
