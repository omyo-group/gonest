package pipeline

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/omyo/gonest/internal/core"
)

// tracer records the order in which pipeline components run.
type tracer struct {
	mu    sync.Mutex
	steps []string
}

func (tr *tracer) add(s string) {
	tr.mu.Lock()
	tr.steps = append(tr.steps, s)
	tr.mu.Unlock()
}

func (tr *tracer) take() []string {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	s := tr.steps
	tr.steps = nil
	return s
}

func (tr *tracer) guard(name string) core.Guard {
	return core.GuardFunc(func(*core.Ctx) (bool, error) { tr.add("guard:" + name); return true, nil })
}

func (tr *tracer) interceptor(name string) core.Interceptor {
	return core.InterceptorFunc(func(_ *core.Ctx, next core.Next) (any, error) {
		tr.add("before:" + name)
		v, err := next()
		tr.add("after:" + name)
		return v, err
	})
}

func (tr *tracer) pipe(name string) core.Pipe {
	return core.PipeFunc(func(_ *core.Ctx, v any, meta core.ArgMeta) (any, error) {
		in := v.(orderIn)
		tr.add(fmt.Sprintf("pipe:%s:%d", name, in.N))
		if meta.Type != reflect.TypeFor[orderIn]() || meta.Route == nil {
			return nil, errors.New("bad meta")
		}
		in.N++
		return in, nil
	})
}

func (tr *tracer) filter(name string, handle bool) core.ExceptionFilter {
	return core.ExceptionFilterFunc(func(ctx *core.Ctx, err error) bool {
		tr.add("filter:" + name)
		if handle {
			ctx.Gin().JSON(http.StatusTeapot, map[string]string{"by": name, "err": err.Error()})
		}
		return handle
	})
}

type orderIn struct {
	N int `query:"n"`
}

func TestPipelineOrder(t *testing.T) {
	tr := &tracer{}
	h := newHarness(t, "")
	h.mount(func(r core.Router) {
		r.Use(core.UseGuards(tr.guard("ctrl")), core.UseInterceptors(tr.interceptor("ctrl")), core.UsePipes(tr.pipe("ctrl")))
		r.GET("/order", core.Handle(func(_ *core.Ctx, in orderIn) (int, error) {
			tr.add(fmt.Sprintf("handler:%d", in.N))
			return in.N, nil
		}), core.UseGuards(tr.guard("route")), core.UseInterceptors(tr.interceptor("route")), core.UsePipes(tr.pipe("route")))
	})
	h.m.SetGlobals(Globals{
		Guards:       []core.Guard{tr.guard("global")},
		Interceptors: []core.Interceptor{tr.interceptor("global")},
		Pipes:        []core.Pipe{tr.pipe("global")},
	})
	rec := h.do(http.MethodGet, "/order?n=10", "")
	if rec.Code != 200 || rec.Body.String() != "13" {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	want := []string{
		"guard:global", "guard:ctrl", "guard:route",
		"before:global", "before:ctrl", "before:route",
		"pipe:global:10", "pipe:ctrl:11", "pipe:route:12",
		"handler:13",
		"after:route", "after:ctrl", "after:global",
	}
	if got := tr.take(); !reflect.DeepEqual(got, want) {
		t.Fatalf("order\n got %v\nwant %v", got, want)
	}
}

func TestGuards(t *testing.T) {
	tr := &tracer{}
	deny := core.GuardFunc(func(*core.Ctx) (bool, error) { return false, nil })
	unauth := core.GuardFunc(func(*core.Ctx) (bool, error) { return false, core.Unauthorized("login first") })
	handler := core.Handle(func(*core.Ctx, core.Empty) (string, error) { tr.add("handler"); return "ok", nil })
	h := newHarness(t, "")
	h.mount(func(r core.Router) {
		r.GET("/deny", handler, core.UseGuards(deny, tr.guard("after-deny")))
		r.GET("/unauth", handler, core.UseGuards(unauth))
		r.GET("/meta", core.Handle(func(*core.Ctx, core.Empty) (string, error) { return "ok", nil }),
			core.SetMetadata("roles", "admin"),
			core.UseGuards(core.GuardFunc(func(c *core.Ctx) (bool, error) {
				v, _ := c.Route().Metadata("roles")
				return v == "admin", nil
			})))
	})
	tests := []struct {
		target string
		status int
		msg    string
	}{
		{"/deny", 403, "Forbidden resource"},
		{"/unauth", 401, "login first"},
		{"/meta", 200, ""},
	}
	for _, tt := range tests {
		rec := h.do(http.MethodGet, tt.target, "")
		if rec.Code != tt.status {
			t.Fatalf("%s: %d %s", tt.target, rec.Code, rec.Body)
		}
		if tt.msg != "" {
			if eb := decodeError(t, rec); eb.Message != tt.msg || eb.StatusCode != tt.status {
				t.Fatalf("%s: %+v", tt.target, eb)
			}
		}
	}
	if steps := tr.take(); len(steps) != 0 {
		t.Fatalf("nothing should run after a denying guard: %v", steps)
	}
}

type bodyIn struct {
	Name string `json:"name"`
}

func TestInterceptors(t *testing.T) {
	calls := 0
	wrap := core.InterceptorFunc(func(_ *core.Ctx, next core.Next) (any, error) {
		v, err := next()
		if err != nil {
			return nil, err
		}
		return map[string]any{"data": v}, nil
	})
	recoverErr := core.InterceptorFunc(func(_ *core.Ctx, next core.Next) (any, error) {
		if _, err := next(); err != nil {
			return "recovered: " + err.Error(), nil //nolint:nilerr // the interceptor deliberately swallows the error
		}
		return "no error", nil
	})
	retry := core.InterceptorFunc(func(_ *core.Ctx, next core.Next) (any, error) {
		v, err := next()
		if err != nil {
			return next()
		}
		return v, nil
	})
	shortCircuit := core.InterceptorFunc(func(*core.Ctx, core.Next) (any, error) { return "cached", nil })
	failing := core.InterceptorFunc(func(*core.Ctx, core.Next) (any, error) { return nil, core.Conflict("busy") })

	h := newHarness(t, "")
	h.mount(func(r core.Router) {
		r.GET("/wrap", core.Handle(func(*core.Ctx, core.Empty) (int, error) { return 7, nil }), core.UseInterceptors(wrap))
		r.GET("/recover", core.Handle(func(*core.Ctx, core.Empty) (int, error) { return 0, errors.New("boom") }), core.UseInterceptors(recoverErr))
		r.POST("/retry", core.Handle(func(_ *core.Ctx, in bodyIn) (string, error) {
			calls++
			if calls == 1 {
				return "", errors.New("transient")
			}
			return in.Name, nil
		}), core.UseInterceptors(retry))
		r.GET("/short", core.Handle(func(*core.Ctx, core.Empty) (string, error) {
			t.Error("handler must not run")
			return "", nil
		}), core.UseInterceptors(shortCircuit))
		r.GET("/fail", core.Handle(func(*core.Ctx, core.Empty) (string, error) { return "x", nil }), core.UseInterceptors(failing))
	})
	tests := []struct {
		method, target, body string
		status               int
		want                 string
	}{
		{"GET", "/wrap", "", 200, `{"data":7}`},
		{"GET", "/recover", "", 200, `"recovered: boom"`},
		{"POST", "/retry", `{"name":"bob"}`, 201, `"bob"`},
		{"GET", "/short", "", 200, `"cached"`},
		{"GET", "/fail", "", 409, `{"statusCode":409,"message":"busy","error":"Conflict"}`},
	}
	for _, tt := range tests {
		rec := h.do(tt.method, tt.target, tt.body)
		if rec.Code != tt.status || rec.Body.String() != tt.want {
			t.Fatalf("%s: %d %s, want %d %s", tt.target, rec.Code, rec.Body, tt.status, tt.want)
		}
	}
	if calls != 2 {
		t.Fatalf("retry calls = %d", calls)
	}
}

func TestExceptionFilters(t *testing.T) {
	tr := &tracer{}
	failing := core.Handle(func(*core.Ctx, core.Empty) (string, error) { return "", core.NotFound("nope") })
	h := newHarness(t, "")
	h.mount(func(r core.Router) {
		r.Use(core.UseFilters(tr.filter("ctrl", true)))
		r.GET("/route", failing, core.UseFilters(tr.filter("route-skip", false), tr.filter("route", true)))
		r.GET("/ctrl", failing)
	})
	h.mount(func(r core.Router) {
		r.Prefix("/g")
		r.GET("/global", failing, core.UseFilters(tr.filter("route-skip", false)))
		r.GET("/default", failing)
		r.GET("/panicky", failing, core.UseFilters(core.ExceptionFilterFunc(func(*core.Ctx, error) bool { panic("filter bug") })))
	})
	h.m.SetGlobals(Globals{Filters: []core.ExceptionFilter{tr.filter("global-skip", false), tr.filter("global", true)}})
	tests := []struct {
		target string
		status int
		by     string
		steps  []string
	}{
		{"/route", 418, "route", []string{"filter:route-skip", "filter:route"}},
		{"/ctrl", 418, "ctrl", []string{"filter:ctrl"}},
		{"/g/global", 418, "global", []string{"filter:route-skip", "filter:global-skip", "filter:global"}},
		{"/g/panicky", 404, "", nil},
	}
	for _, tt := range tests {
		rec := h.do(http.MethodGet, tt.target, "")
		if rec.Code != tt.status {
			t.Fatalf("%s: %d %s", tt.target, rec.Code, rec.Body)
		}
		if tt.by != "" {
			got := decodeJSONBody[map[string]string](t, rec)
			if got["by"] != tt.by || got["err"] != "404 nope" {
				t.Fatalf("%s: %v", tt.target, got)
			}
		}
		if steps := tr.take(); !reflect.DeepEqual(steps, tt.steps) {
			t.Fatalf("%s: steps %v, want %v", tt.target, steps, tt.steps)
		}
	}
	if !strings.Contains(h.logs.String(), "exception filter panicked") {
		t.Fatalf("filter panic not logged: %s", h.logs)
	}
	// Without global filters, the default filter answers.
	h.m.SetGlobals(Globals{})
	rec := h.do(http.MethodGet, "/g/default", "")
	if eb := decodeError(t, rec); rec.Code != 404 || eb.Message != "nope" || eb.Error != "Not Found" {
		t.Fatalf("%d %+v", rec.Code, eb)
	}
}

func TestDefaultFilter(t *testing.T) {
	h := newHarness(t, "")
	fail := func(err error) *core.Handler {
		return core.Handle(func(*core.Ctx, core.Empty) (string, error) { return "", err })
	}
	h.mount(func(r core.Router) {
		r.GET("/wrapped", fail(fmt.Errorf("ctx: %w", core.Forbidden("no access"))))
		r.GET("/plain", fail(errors.New("db down")))
		r.GET("/validation", fail(core.ValidationErrors{{Field: "x", Source: "query", Rule: "required", Message: "is required"}}))
		r.GET("/badstatus", fail(&core.HTTPError{Status: 0, Message: "weird"}))
		r.GET("/internal", fail(core.Internal("oops").Wrap(errors.New("secret cause"))))
		r.GET("/panic", core.Handle(func(*core.Ctx, core.Empty) (string, error) { panic("kaboom") }))
		r.GET("/panicerr", core.Handle(func(*core.Ctx, core.Empty) (string, error) { panic(core.NotFound("hidden")) }))
		r.GET("/written", core.HandleRaw(func(c *core.Ctx) error {
			c.Gin().String(http.StatusAccepted, "partial")
			return errors.New("late failure")
		}))
	})
	tests := []struct {
		target string
		status int
		msg    string
		log    string
	}{
		{"/wrapped", 403, "no access", ""},
		{"/plain", 500, "Internal Server Error", "db down"},
		{"/validation", 400, "validation failed", ""},
		{"/badstatus", 500, "Internal Server Error", "invalid status"},
		{"/internal", 500, "oops", "secret cause"},
		{"/panic", 500, "Internal Server Error", "kaboom"},
		{"/panicerr", 500, "Internal Server Error", "panic recovered"},
	}
	for _, tt := range tests {
		rec := h.do(http.MethodGet, tt.target, "")
		eb := decodeError(t, rec)
		if rec.Code != tt.status || eb.Message != tt.msg || eb.StatusCode != tt.status {
			t.Fatalf("%s: %d %+v", tt.target, rec.Code, eb)
		}
		if strings.Contains(rec.Body.String(), "secret") {
			t.Fatalf("%s leaked the cause: %s", tt.target, rec.Body)
		}
		if tt.log != "" && !strings.Contains(h.logs.String(), tt.log) {
			t.Fatalf("%s: log missing %q: %s", tt.target, tt.log, h.logs)
		}
	}
	if !strings.Contains(h.logs.String(), "goroutine") {
		t.Fatal("panic stack not logged")
	}
	rec := h.do(http.MethodGet, "/written", "")
	if rec.Code != 202 || rec.Body.String() != "partial" || !strings.Contains(h.logs.String(), "response already written") {
		t.Fatalf("written: %d %s", rec.Code, rec.Body)
	}
}

func TestAbortHandlerPanicPropagates(t *testing.T) {
	h := newHarness(t, "")
	h.mount(func(r core.Router) {
		r.GET("/abort", core.Handle(func(*core.Ctx, core.Empty) (string, error) { panic(http.ErrAbortHandler) }))
	})
	defer func() {
		if p := recover(); p != http.ErrAbortHandler { //nolint:errorlint // identity check
			t.Fatalf("recovered %v", p)
		}
	}()
	h.do(http.MethodGet, "/abort", "")
	t.Fatal("expected panic")
}

func TestPanicErrorUnwrap(t *testing.T) {
	inner := errors.New("inner")
	if !errors.Is(&panicError{value: inner}, inner) {
		t.Fatal("panicError must unwrap error values")
	}
	if (&panicError{value: "x"}).Unwrap() != nil {
		t.Fatal("non-error panic values unwrap to nil")
	}
}

func TestPipeErrors(t *testing.T) {
	h := newHarness(t, "")
	badType := core.PipeFunc(func(*core.Ctx, any, core.ArgMeta) (any, error) { return "wrong", nil })
	reject := core.PipeFunc(func(*core.Ctx, any, core.ArgMeta) (any, error) { return nil, core.BadRequest("rejected") })
	h.mount(func(r core.Router) {
		r.GET("/badtype", echo[orderIn](), core.UsePipes(badType))
		r.GET("/reject", echo[orderIn](), core.UsePipes(reject))
	})
	if rec := h.do(http.MethodGet, "/badtype", ""); rec.Code != 500 || !strings.Contains(h.logs.String(), "returned string") {
		t.Fatalf("badtype: %d %s", rec.Code, h.logs)
	}
	if rec := h.do(http.MethodGet, "/reject", ""); rec.Code != 400 || decodeError(t, rec).Message != "rejected" {
		t.Fatalf("reject: %d %s", rec.Code, rec.Body)
	}
	h.m.SetGlobals(Globals{Pipes: []core.Pipe{reject}})
	if rec := h.do(http.MethodGet, "/badtype", ""); rec.Code != 400 {
		t.Fatalf("global reject: %d", rec.Code)
	}
}

func TestStatusRules(t *testing.T) {
	h := newHarness(t, "")
	ok := core.Handle(func(*core.Ctx, core.Empty) (string, error) { return "ok", nil })
	h.mount(func(r core.Router) {
		r.GET("/get", ok)
		r.POST("/post", ok)
		r.POST("/post-200", ok, core.Status(200))
		r.PUT("/set", core.Handle(func(c *core.Ctx, _ core.Empty) (string, error) { c.SetStatus(http.StatusAccepted); return "ok", nil }))
		r.DELETE("/nc", core.Handle(func(*core.Ctx, core.Empty) (core.NoContent, error) { return core.NoContent{}, nil }))
		r.POST("/nc-post", core.Handle(func(*core.Ctx, core.Empty) (core.NoContent, error) { return core.NoContent{}, nil }))
		r.PATCH("/nil", core.Handle(func(*core.Ctx, core.Empty) (*string, error) { return nil, nil }))
		r.Group("/grp", func(r core.Router) {
			r.GET("/x", ok)
			r.GET("/y", ok, core.Status(203))
		}, core.Status(206))
	})
	tests := []struct {
		method, target string
		status         int
		body           string
	}{
		{"GET", "/get", 200, `"ok"`},
		{"POST", "/post", 201, `"ok"`},
		{"POST", "/post-200", 200, `"ok"`},
		{"PUT", "/set", 202, `"ok"`},
		{"DELETE", "/nc", 204, ""},
		{"POST", "/nc-post", 204, ""},
		{"PATCH", "/nil", 200, "null"},
		{"GET", "/grp/x", 206, `"ok"`},
		{"GET", "/grp/y", 203, `"ok"`},
	}
	for _, tt := range tests {
		rec := h.do(tt.method, tt.target, "")
		if rec.Code != tt.status || rec.Body.String() != tt.body {
			t.Fatalf("%s %s: %d %q", tt.method, tt.target, rec.Code, rec.Body)
		}
	}
}

func TestRawHandlers(t *testing.T) {
	tr := &tracer{}
	h := newHarness(t, "")
	h.mount(func(r core.Router) {
		r.GET("/raw", core.HandleRaw(func(c *core.Ctx) error {
			c.Gin().String(http.StatusOK, "hello "+c.Query("name"))
			return nil
		}), core.UseGuards(tr.guard("raw")), core.UseInterceptors(tr.interceptor("raw")), core.UsePipes(tr.pipe("never")))
		r.GET("/rawerr", core.HandleRaw(func(*core.Ctx) error { return core.TooManyRequests("slow down") }))
	})
	rec := h.do(http.MethodGet, "/raw?name=bob", "")
	if rec.Code != 200 || rec.Body.String() != "hello bob" {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if got, want := tr.take(), []string{"guard:raw", "before:raw", "after:raw"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("steps %v", got)
	}
	if rec := h.do(http.MethodGet, "/rawerr", ""); rec.Code != 429 {
		t.Fatalf("rawerr: %d", rec.Code)
	}
}

func TestGroupUseScopingAndRouteInfo(t *testing.T) {
	type ctrl struct{ ctrlFunc }
	h := newHarness(t, "/api/")
	tag := func(s string) core.RouteOption { return core.Tags(s) }
	infos, err := h.m.Mount(ctrl{func(r core.Router) {
		r.GET("/before", echo[core.Empty]())
		r.Use(tag("ctrl"), core.SetMetadata("k", "ctrl"))
		r.Prefix("/users/")
		r.GET("", echo[core.Empty](), tag("route"), core.Summary("list"))
		r.Group("admin", func(g core.Router) {
			g.GET("/", echo[core.Empty]())
			g.Use(tag("late"))
			g.Group("/deep/", func(d core.Router) {
				d.GET(":id", echo[ptrIn](), core.SetMetadata("k", "route"))
			}, tag("deep"))
		}, tag("group"))
		r.GET("/after", echo[core.Empty]())
		r.Handle("custom", "/c", echo[core.Empty]())
		r.HEAD("/h", echo[core.Empty]())
		r.OPTIONS("/o", echo[core.Empty]())
	}}, "users-module", nil)
	if err != nil {
		t.Fatal(err)
	}
	type row struct {
		method, path string
		ctrlTags     []string
		routeTags    []string
	}
	var got []row
	for _, ri := range infos {
		got = append(got, row{ri.Method, ri.Path, ri.ControllerConfig.Tags, ri.Config.Tags})
		if ri.Module != "users-module" || ri.Controller != reflect.TypeFor[ctrl]() || ri.Handler == nil {
			t.Fatalf("route info %+v", ri)
		}
	}
	want := []row{
		{"GET", "/api/users/before", nil, nil},
		{"GET", "/api/users", []string{"ctrl"}, []string{"route"}},
		{"GET", "/api/users/admin", []string{"ctrl", "group"}, nil},
		{"GET", "/api/users/admin/deep/:id", []string{"ctrl", "group", "late", "deep"}, nil},
		{"GET", "/api/users/after", []string{"ctrl"}, nil},
		{"CUSTOM", "/api/users/c", []string{"ctrl"}, nil},
		{"HEAD", "/api/users/h", []string{"ctrl"}, nil},
		{"OPTIONS", "/api/users/o", []string{"ctrl"}, nil},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("routes\n got %+v\nwant %+v", got, want)
	}
	if v, _ := infos[3].Metadata("k"); v != "route" {
		t.Fatalf("metadata %v", v)
	}
	if v, _ := infos[2].Metadata("k"); v != "ctrl" {
		t.Fatalf("metadata %v", v)
	}
	if infos[1].Config.Summary != "list" {
		t.Fatal("summary lost")
	}
	if all := h.routes.All(); len(all) != len(infos) {
		t.Fatalf("route table has %d routes", len(all))
	}
	if rec := h.do(http.MethodGet, "/api/users/admin/deep/9", ""); rec.Code != 200 || rec.Body.String() != `{"ID":9}` {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if rec := h.do("CUSTOM", "/api/users/c", ""); rec.Code != 200 {
		t.Fatalf("custom method: %d", rec.Code)
	}
}

func TestJoinPaths(t *testing.T) {
	tests := []struct {
		parts []string
		want  string
	}{
		{nil, "/"},
		{[]string{"", "", ""}, "/"},
		{[]string{"/", "/"}, "/"},
		{[]string{"api", "users", ""}, "/api/users"},
		{[]string{"/api/", "//users//", "/:id/"}, "/api/users/:id"},
		{[]string{"", "files", "*path"}, "/files/*path"},
	}
	for _, tt := range tests {
		if got := joinPaths(tt.parts...); got != tt.want {
			t.Errorf("joinPaths(%q) = %q, want %q", tt.parts, got, tt.want)
		}
	}
}

func TestMiddleware(t *testing.T) {
	h := newHarness(t, "/api")
	var asked []string
	mw := func(method, path string) []gin.HandlerFunc {
		asked = append(asked, method+" "+path)
		if strings.HasSuffix(path, "/open") {
			return nil
		}
		return []gin.HandlerFunc{
			func(c *gin.Context) {
				if c.GetHeader("X-Key") != "k" {
					c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"mw": "denied"})
					return
				}
				c.Set("user", "bob")
			},
		}
	}
	_, err := h.m.Mount(ctrlFunc(func(r core.Router) {
		r.GET("/me", core.Handle(func(c *core.Ctx, _ core.Empty) (string, error) {
			u, _ := core.CtxValue[string](c, "user")
			return u, nil
		}))
		r.GET("/open", echo[core.Empty]())
	}), "m", mw)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"GET /api/me", "GET /api/open"}; !reflect.DeepEqual(asked, want) {
		t.Fatalf("mw asked %v", asked)
	}
	if rec := h.do(http.MethodGet, "/api/me", ""); rec.Code != 401 {
		t.Fatalf("no key: %d", rec.Code)
	}
	if rec := h.do(http.MethodGet, "/api/me", "", withHeader("X-Key", "k")); rec.Code != 200 || rec.Body.String() != `"bob"` {
		t.Fatalf("key: %d %s", rec.Code, rec.Body)
	}
	if rec := h.do(http.MethodGet, "/api/open", ""); rec.Code != 200 {
		t.Fatalf("open: %d", rec.Code)
	}
	if len(asked) != 2 {
		t.Fatal("mw must be resolved at mount time only")
	}
}

func TestFallbacks(t *testing.T) {
	h := newHarness(t, "")
	h.mount(func(r core.Router) { r.GET("/only-get", echo[core.Empty]()) })
	h.m.InstallFallbacks()
	tests := []struct {
		method, target string
		status         int
		msg, errText   string
	}{
		{"GET", "/missing/x", 404, "Cannot GET /missing/x", "Not Found"},
		{"POST", "/only-get", 405, "Cannot POST /only-get", "Method Not Allowed"},
	}
	for _, tt := range tests {
		rec := h.do(tt.method, tt.target, "")
		eb := decodeError(t, rec)
		if rec.Code != tt.status || eb.Message != tt.msg || eb.Error != tt.errText || eb.StatusCode != tt.status {
			t.Fatalf("%s %s: %d %+v", tt.method, tt.target, rec.Code, eb)
		}
	}
	New(Options{}).InstallFallbacks() // no engine: no-op
}

type pathTypo struct {
	ID int `path:"idd"`
}

type nilCtrl struct{}

func (*nilCtrl) Routes(core.Router) {}

func TestMountErrors(t *testing.T) {
	h := newHarness(t, "")
	h.mount(func(r core.Router) { r.GET("/taken/:id", echo[ptrIn]()) })
	var nc *nilCtrl
	tests := []struct {
		name string
		ctrl core.Controller
		want string
	}{
		{"nil controller", nil, "nil controller"},
		{"typed nil controller", nc, "nil controller"},
		{"duplicate across controllers", ctrlFunc(func(r core.Router) { r.GET("/taken/:id", echo[ptrIn]()) }), "duplicate route GET /taken/:id"},
		{"duplicate in controller", ctrlFunc(func(r core.Router) {
			r.GET("/dup", echo[core.Empty]())
			r.GET("dup/", echo[core.Empty]())
		}), "duplicate route GET /dup"},
		{"wildcard conflict", ctrlFunc(func(r core.Router) { r.GET("/taken/:name/x", echo[core.Empty]()) }), "gin rejected the route"},
		{"unknown path param", ctrlFunc(func(r core.Router) { r.GET("/p/:id", echo[pathTypo]()) }), `path parameter "idd"`},
		{"bad input", ctrlFunc(func(r core.Router) { r.GET("/bad", echo[int]()) }), "must be a struct"},
		{"nil handler", ctrlFunc(func(r core.Router) { r.GET("/nil", nil) }), "gonest.Handle"},
		{"zero handler", ctrlFunc(func(r core.Router) { r.GET("/zero", &core.Handler{}) }), "gonest.Handle"},
		{"bad method", ctrlFunc(func(r core.Router) { r.Handle("GE T", "/x", echo[core.Empty]()) }), "invalid HTTP method"},
		{"empty method", ctrlFunc(func(r core.Router) { r.Handle("", "/x", echo[core.Empty]()) }), "invalid HTTP method"},
		{"nil group", ctrlFunc(func(r core.Router) { r.Group("/g", nil) }), "nil function"},
		{"panic in Routes", ctrlFunc(func(r core.Router) { r.GET("/p", core.Handle[core.Empty, int](nil)) }), "panic in Routes"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			infos, err := h.m.Mount(tt.ctrl, "m", nil)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
			if tt.name != "wildcard conflict" && len(infos) != 0 {
				t.Fatalf("infos = %v", infos)
			}
		})
	}
	if _, err := New(Options{}).Mount(ctrlFunc(func(core.Router) {}), "m", nil); err == nil {
		t.Fatal("missing engine must fail")
	}
	// A failing controller registers nothing.
	if n := len(h.routes.All()); n != 1 {
		t.Fatalf("route table has %d routes", n)
	}
	rec := h.do(http.MethodGet, "/dup", "")
	if rec.Code != 404 {
		t.Fatalf("/dup should not be registered: %d", rec.Code)
	}
}

func TestSetGlobalsConcurrentWithRequests(t *testing.T) {
	h := newHarness(t, "")
	h.mount(func(r core.Router) {
		r.GET("/x", core.Handle(func(*core.Ctx, core.Empty) (int, error) { return 1, nil }))
	})
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				req := httptest.NewRequest(http.MethodGet, "/x", nil)
				rec := httptest.NewRecorder()
				h.engine.ServeHTTP(rec, req)
				if rec.Code != 200 && rec.Code != 403 {
					t.Errorf("status %d", rec.Code)
				}
			}
		}()
	}
	for j := 0; j < 50; j++ {
		allow := j%2 == 0
		h.m.SetGlobals(Globals{Guards: []core.Guard{core.GuardFunc(func(*core.Ctx) (bool, error) { return allow, nil })}})
	}
	wg.Wait()
}

func TestCtxLoggerAndContext(t *testing.T) {
	h := newHarness(t, "")
	h.mount(func(r core.Router) {
		r.GET("/log", core.Handle(func(c *core.Ctx, _ core.Empty) (string, error) {
			c.Logger().Info("hello from handler")
			if c.Err() != nil || c.Route().Path != "/log" {
				return "", errors.New("bad ctx")
			}
			return "ok", nil
		}))
	})
	if rec := h.do(http.MethodGet, "/log", ""); rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if !strings.Contains(h.logs.String(), "hello from handler") {
		t.Fatal("ctx logger is not the mounter logger")
	}
}
