package gonesttest_test

import (
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/omyo/gonest"
	"github.com/omyo/gonest/gonesttest"
)

func TestFakeRepoAndRealGuard(t *testing.T) {
	app := gonesttest.New(t, AppModule).
		OverrideProvider(gonest.Bind[ItemsRepo](NewFakeRepo)).
		Compile()

	app.Request("GET", "/items/1").Do().
		ExpectStatus(401).ExpectErrorMessage("missing bearer")
	app.Request("GET", "/items/1").WithBearer("x").Do().
		ExpectStatus(200).ExpectJSON(Item{ID: 1, Name: "pen"})
	app.Request("GET", "/items/9").WithBearer("x").Do().
		ExpectStatus(404).ExpectErrorMessage("not found")
	app.Request("POST", "/items").WithBearer("x").WithJSON(map[string]string{"name": "ink"}).Do().
		ExpectStatus(201).ExpectJSON(map[string]any{"id": 2, "name": "ink"}).
		ExpectHeader("Content-Type", "application/json; charset=utf-8")
	app.Request("POST", "/items").WithBearer("x").WithJSON(map[string]string{"name": "x"}).Do().
		ExpectStatus(400).ExpectErrorMessage("min")

	repo := gonesttest.Get[ItemsRepo](app).(*FakeRepo)
	if len(repo.items) != 2 {
		t.Fatalf("fake repo not shared with the app: %d items", len(repo.items))
	}
}

func TestOverrideEnhancerInheritsKind(t *testing.T) {
	// A plain Bind of the guard type replaces the AppGuard provider and keeps
	// it a global guard.
	app := gonesttest.New(t, AppModule).
		OverrideProvider(gonest.Bind[ItemsRepo](NewFakeRepo)).
		OverrideProvider(gonest.Provide(func() *JwtGuard { return NewJwtGuard() })).
		Compile()
	app.Request("GET", "/items/1").Do().ExpectStatus(401)

	// Replacing it with an allow-all guard needs the same token (*JwtGuard),
	// so use WithOptions for a differently typed one.
	open := gonesttest.New(t, AppModule).
		OverrideProvider(gonest.Bind[ItemsRepo](NewFakeRepo)).
		OverrideModule(AuthModule, &gonest.Module{
			Name:      "auth",
			Providers: []gonest.Provider{gonest.AppGuard(func() AllowAll { return AllowAll{} })},
		}).Compile()
	open.Request("GET", "/items/1").Do().ExpectStatus(200)
}

func TestOverridesDoNotMutateModules(t *testing.T) {
	before := len(ItemsModule.Providers)
	tok := ItemsModule.Providers[0].Token
	gonesttest.New(t, AppModule).
		OverrideProvider(gonest.Bind[ItemsRepo](NewFakeRepo)).
		OverrideModule(AuthModule, &gonest.Module{Name: "auth"}).
		Compile()
	if len(ItemsModule.Providers) != before || ItemsModule.Providers[0].Ctor == nil {
		t.Fatal("ItemsModule mutated")
	}
	if got := ItemsModule.Providers[0]; got.Token != tok || reflect.ValueOf(got.Ctor).Pointer() != reflect.ValueOf(NewDBRepo).Pointer() {
		t.Fatalf("ItemsModule provider replaced: %v", got)
	}
	if AppModule.Imports[0] != AuthModule || AppModule.Imports[1] != ItemsModule {
		t.Fatal("AppModule imports rewritten")
	}
	// The original graph still compiles with the real (failing) repository.
	app := gonesttest.New(t, AppModule).Compile()
	app.Request("GET", "/items/1").WithBearer("t").Do().ExpectStatus(500)
}

func TestSharedModuleCopiedOnce(t *testing.T) {
	shared := &gonest.Module{
		Name:      "shared",
		Providers: []gonest.Provider{gonest.Value[string]("real")},
		Exports:   []gonest.Token{gonest.TokenOf[string]()},
	}
	a := &gonest.Module{Name: "a", Imports: []*gonest.Module{shared}}
	b := &gonest.Module{Name: "b", Imports: []*gonest.Module{shared}, ReExports: []*gonest.Module{shared}}
	root := &gonest.Module{Name: "root", Imports: []*gonest.Module{a, b}}
	// Importing "shared" twice would fail with "duplicate module name" if the
	// copies were not identical.
	app := gonesttest.New(t, root).OverrideProvider(gonest.Value[string]("fake")).Compile()
	if got := gonesttest.Get[string](app); got != "fake" {
		t.Fatalf("got %q", got)
	}
	if shared.Providers[0].Value != "real" {
		t.Fatal("shared module mutated")
	}
}

func TestOverrideRootModule(t *testing.T) {
	repl := &gonest.Module{Name: "other", Providers: []gonest.Provider{gonest.Value[int](1)}}
	app := gonesttest.New(t, AppModule).OverrideModule(AppModule, repl).Compile()
	if gonesttest.Get[int](app) != 1 {
		t.Fatal("root not replaced")
	}
}

func TestNamedGetAndOptions(t *testing.T) {
	m := &gonest.Module{Name: "m", Providers: []gonest.Provider{gonest.Value[string]("a", gonest.Named("x"))}}
	app := gonesttest.New(t, m).
		OverrideProvider(gonest.Value[string]("b", gonest.Named("x"))).
		WithOptions(gonest.WithGlobalPrefix("/api")).
		Compile()
	if got := gonesttest.GetNamed[string](app, "x"); got != "b" {
		t.Fatalf("got %q", got)
	}
}

func TestLifecycleCleanup(t *testing.T) {
	var boot *Boot
	t.Run("sub", func(t *testing.T) {
		app := gonesttest.New(t, AppModule).Compile()
		boot = gonesttest.Get[*Boot](app)
		if boot.inits.Load() != 1 {
			t.Fatal("OnModuleInit not run")
		}
	})
	if boot.destroys.Load() != 1 {
		t.Fatal("Shutdown not registered with t.Cleanup")
	}
}

func TestRequestBuilding(t *testing.T) {
	m := &gonest.Module{Name: "m", Controllers: []any{newEcho}}
	app := gonesttest.New(t, m).Compile()

	var got echoOut
	app.Request("POST", "/echo?a=1").
		WithQuery("b", "2").
		WithHeader("X-Tenant", "acme").
		WithBody(strings.NewReader("raw"), "text/plain").
		Do().ExpectStatus(201).JSON(&got)
	if got.Query != "a=1&b=2" || got.Tenant != "acme" || got.CT != "text/plain" || got.Body != "raw" {
		t.Fatalf("got %+v", got)
	}
	app.Request("GET", "/echo").WithQuery("q", "x y").Do().ExpectStatus(200)
	if r := app.Request("GET", "/echo").Do(); r.Status() != 200 || len(r.Body()) == 0 || r.Header().Get("Content-Type") == "" {
		t.Fatal("accessors")
	}
}

func TestNewCtx(t *testing.T) {
	ctx := gonesttest.NewCtx(t,
		gonesttest.CtxMethod("POST"),
		gonesttest.CtxPath("/users/7?x=1"),
		gonesttest.CtxQuery("y", "2"),
		gonesttest.CtxHeader("X-A", "b"),
		gonesttest.CtxParam("id", "7"),
		gonesttest.CtxJSON(map[string]int{"n": 1}),
		gonesttest.CtxMetadata("roles", []string{"admin"}),
		gonesttest.CtxSet("user", "ada"),
		nil,
	)
	if ctx.Request().Method != "POST" || ctx.Param("id") != "7" || ctx.Header("X-A") != "b" {
		t.Fatal("request fields")
	}
	if ctx.Query("x") != "1" || ctx.Query("y") != "2" {
		t.Fatal("query")
	}
	if ctx.Request().Header.Get("Content-Type") != "application/json" {
		t.Fatal("content type")
	}
	if v, ok := ctx.Route().Metadata("roles"); !ok || v.([]string)[0] != "admin" {
		t.Fatal("metadata")
	}
	if u, _ := gonest.CtxValue[string](ctx, "user"); u != "ada" {
		t.Fatal("CtxSet")
	}
	if ctx.Route().Method != "POST" || ctx.Route().Path != "/users/7?x=1" {
		t.Fatalf("route %+v", ctx.Route())
	}
	if ctx.Logger() == nil {
		t.Fatal("logger")
	}
}

func TestNewCtxRouteMetadataMerge(t *testing.T) {
	base := &gonest.RouteInfo{Config: gonest.RouteConfig{Metadata: map[string]any{"a": 1}}}
	ctx := gonesttest.NewCtx(t, gonesttest.CtxRoute(base), gonesttest.CtxMetadata("b", 2), gonesttest.CtxLogger(nil))
	if _, ok := ctx.Route().Metadata("a"); !ok {
		t.Fatal("route metadata lost")
	}
	if _, ok := ctx.Route().Metadata("b"); !ok {
		t.Fatal("option metadata lost")
	}
	if _, ok := base.Metadata("b"); ok {
		t.Fatal("caller's route mutated")
	}
}

func TestGuardInIsolation(t *testing.T) {
	g := NewJwtGuard()
	ok, err := g.CanActivate(gonesttest.NewCtx(t, gonesttest.CtxHeader("Authorization", "Bearer t")))
	if !ok || err != nil {
		t.Fatal("expected allow")
	}
	_, err = g.CanActivate(gonesttest.NewCtx(t))
	var he *gonest.HTTPError
	if !errors.As(err, &he) || he.Status != 401 {
		t.Fatalf("err = %v", err)
	}
}

func TestFilterInIsolation(t *testing.T) {
	f := gonest.ExceptionFilterFunc(func(ctx *gonest.Ctx, err error) bool {
		ctx.Gin().JSON(418, map[string]string{"teapot": err.Error()})
		return true
	})
	ctx := gonesttest.NewCtx(t)
	if !f.Catch(ctx, errors.New("boom")) {
		t.Fatal("filter declined")
	}
	gonesttest.ResponseOf(t, ctx).ExpectStatus(418).ExpectJSON(map[string]string{"teapot": "boom"})
	if rec := gonesttest.Recorder(ctx); rec.Code != 418 {
		t.Fatal("recorder")
	}
}

func TestRecorderFlushesStatus(t *testing.T) {
	ctx := gonesttest.NewCtx(t)
	ctx.Gin().Status(http.StatusNoContent)
	if rec := gonesttest.Recorder(ctx); rec.Code != 204 {
		t.Fatalf("code %d", rec.Code)
	}
}

type echoOut struct {
	Query  string `json:"query"`
	Tenant string `json:"tenant"`
	CT     string `json:"ct"`
	Body   string `json:"body"`
}

type echoCtl struct{}

func newEcho() *echoCtl { return &echoCtl{} }

func (*echoCtl) Routes(r gonest.Router) {
	r.Prefix("/echo")
	r.GET("", gonest.HandleRaw(func(ctx *gonest.Ctx) error {
		ctx.Gin().String(200, "plain")
		return nil
	}))
	r.POST("", gonest.HandleRaw(func(ctx *gonest.Ctx) error {
		b, _ := io.ReadAll(ctx.Request().Body)
		ctx.Gin().JSON(201, echoOut{
			Query:  ctx.Request().URL.RawQuery,
			Tenant: ctx.Header("X-Tenant"),
			CT:     ctx.Header("Content-Type"),
			Body:   string(b),
		})
		return nil
	}))
}
