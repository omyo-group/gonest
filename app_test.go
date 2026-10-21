package gonest_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/omyo-group/gonest"
)

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func do(t *testing.T, h http.Handler, method, path, body string, hdr map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, r)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

// TestContractExampleServes boots the users module of examples_contract_test.go
// and exercises it over HTTP.
func TestContractExampleServes(t *testing.T) {
	app, err := gonest.New(AppModule, gonest.WithLogger(quiet()), gonest.WithGlobalPrefix("/api"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := app.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()
	key := map[string]string{"X-API-Key": "secret"}
	h := app.Handler()

	tests := []struct {
		name, method, path, body string
		hdr                      map[string]string
		code                     int
		contains                 string
	}{
		{"seeded by OnModuleInit", "GET", "/api/users/1", "", key, 200, `"name":"Ada"`},
		{"guard", "GET", "/api/users/1", "", nil, 401, "missing or invalid API key"},
		{"not found", "GET", "/api/users/99", "", key, 404, "user 99 not found"},
		{"validation", "GET", "/api/users/0", "", key, 400, "validation failed"},
		{"create", "POST", "/api/users", `{"name":"Bob","email":"bob@example.com"}`, key, 201, `"id":2`},
		{"create invalid", "POST", "/api/users", `{"name":"B","email":"x"}`, key, 400, "validation failed"},
		{"unknown route", "GET", "/users/1", "", key, 404, "Cannot GET /users/1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := do(t, h, tt.method, tt.path, tt.body, tt.hdr)
			if w.Code != tt.code || !strings.Contains(w.Body.String(), tt.contains) {
				t.Fatalf("%s %s = %d %s", tt.method, tt.path, w.Code, w.Body.String())
			}
		})
	}
	if n := len(app.Routes()); n != 2 {
		t.Fatalf("routes = %d", n)
	}
	svc := gonest.MustGet[*slog.Logger](app)
	if svc != slog.Default() {
		t.Fatal("root module's logger should be returned")
	}
}

// A three-module application through the public API.

type Clock struct{ now time.Time }

type Store struct {
	mu    sync.Mutex
	items []string
	trace *[]string
}

func (s *Store) OnModuleInit(context.Context) error {
	*s.trace = append(*s.trace, "store:init")
	return nil
}

func (s *Store) OnApplicationShutdown(context.Context) error {
	*s.trace = append(*s.trace, "store:shutdown")
	return nil
}

type ItemsService struct {
	store *Store
	clock *Clock
}

type ItemsDeps struct {
	gonest.In
	Store   *Store
	Clock   *Clock        `name:"fixed"`
	Missing *bytes.Buffer `optional:"true"`
}

func NewItemsService(d ItemsDeps) *ItemsService { return &ItemsService{store: d.Store, clock: d.Clock} }

func (s *ItemsService) OnApplicationBootstrap(context.Context) error {
	*s.store.trace = append(*s.store.trace, "items:bootstrap")
	return nil
}

func (s *ItemsService) OnModuleDestroy(context.Context) error {
	*s.store.trace = append(*s.store.trace, "items:destroy")
	return nil
}

type ItemsController struct{ svc *ItemsService }

type AddItemInput struct {
	Name string `json:"name" validate:"required"`
}

func (c *ItemsController) Routes(r gonest.Router) {
	r.Prefix("/items")
	r.POST("", gonest.Handle(c.Add))
	r.GET("", gonest.Handle(c.List))
}

func (c *ItemsController) Add(_ *gonest.Ctx, in AddItemInput) (gonest.NoContent, error) {
	c.svc.store.mu.Lock()
	defer c.svc.store.mu.Unlock()
	c.svc.store.items = append(c.svc.store.items, in.Name)
	return gonest.NoContent{}, nil
}

func (c *ItemsController) List(ctx *gonest.Ctx, _ gonest.Empty) (map[string]any, error) {
	c.svc.store.mu.Lock()
	defer c.svc.store.mu.Unlock()
	tenant, _ := gonest.CtxValue[string](ctx, "tenant")
	return map[string]any{"items": c.svc.store.items, "year": c.svc.clock.now.Year(), "tenant": tenant}, nil
}

func threeModules(trace *[]string) *gonest.Module {
	storage := &gonest.Module{
		Name:      "storage",
		Global:    true,
		Providers: []gonest.Provider{gonest.Provide(func() *Store { return &Store{trace: trace} })},
		Exports:   []gonest.Token{gonest.TokenOf[*Store]()},
	}
	clock := &gonest.Module{
		Name:      "clock",
		Providers: []gonest.Provider{gonest.Value(&Clock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}, gonest.Named("fixed"))},
		Exports:   []gonest.Token{gonest.NamedTokenOf[*Clock]("fixed")},
	}
	items := &gonest.Module{
		Name:        "items",
		Imports:     []*gonest.Module{clock},
		Providers:   []gonest.Provider{gonest.Provide(NewItemsService)},
		Controllers: []any{func(s *ItemsService) *ItemsController { return &ItemsController{svc: s} }},
		Exports:     []gonest.Token{gonest.TokenOf[*ItemsService]()},
		ReExports:   []*gonest.Module{clock},
		Configure: func(m *gonest.MiddlewareConsumer, s *Store) {
			m.Apply(func(c *gin.Context) { c.Set("tenant", "acme"); c.Next() }).ForRoutes(gonest.ForRoute("GET", "/items"))
		},
	}
	return &gonest.Module{Name: "app", Imports: []*gonest.Module{storage, items}}
}

func TestThreeModuleApp(t *testing.T) {
	var trace []string
	var logBuf bytes.Buffer
	engine := gin.New()
	app, err := gonest.New(threeModules(&trace),
		gonest.WithEngine(engine),
		gonest.WithLogger(slog.New(slog.NewTextHandler(&logBuf, nil))),
		gonest.WithGlobalInterceptors(gonest.InterceptorFunc(func(ctx *gonest.Ctx, next gonest.Next) (any, error) {
			ctx.Writer().Header().Set("X-Global", "1")
			return next()
		})),
		gonest.WithGlobalGuards(gonest.GuardFunc(func(ctx *gonest.Ctx) (bool, error) { return ctx.Header("X-Block") == "", nil })),
		gonest.WithGlobalPipes(gonest.PipeFunc(func(_ *gonest.Ctx, v any, _ gonest.ArgMeta) (any, error) { return v, nil })),
		gonest.WithGlobalFilters(gonest.ExceptionFilterFunc(func(*gonest.Ctx, error) bool { return false })),
		gonest.WithShutdownTimeout(time.Second),
	)
	if err != nil {
		t.Fatal(err)
	}
	if app.Engine() != engine {
		t.Fatal("WithEngine not used")
	}
	if w := do(t, app.Handler(), "POST", "/items", `{"name":"pen"}`, nil); w.Code != 204 || w.Header().Get("X-Global") != "1" {
		t.Fatalf("add = %d %v", w.Code, w.Header())
	}
	w := do(t, app.Handler(), "GET", "/items", "", nil)
	var body struct {
		Items  []string
		Year   int
		Tenant string
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || w.Code != 200 {
		t.Fatal(w.Code, err)
	}
	if len(body.Items) != 1 || body.Year != 2026 || body.Tenant != "acme" {
		t.Fatalf("body %+v", body)
	}
	if w := do(t, app.Handler(), "GET", "/items", "", map[string]string{"X-Block": "1"}); w.Code != 403 {
		t.Fatalf("guard = %d", w.Code)
	}
	if !strings.Contains(logBuf.String(), "POST /items -> ItemsController.Add") {
		t.Fatalf("startup log: %s", logBuf.String())
	}

	// Get / GetNamed see the root's imports and re-exports.
	if _, err := gonest.Get[*ItemsService](app); err != nil {
		t.Fatal(err)
	}
	if c, err := gonest.GetNamed[*Clock](app, "fixed"); err != nil || c.now.Year() != 2026 {
		t.Fatal(c, err)
	}
	if _, err := gonest.Get[*Store](app); err != nil {
		t.Fatal("global store", err)
	}
	if _, err := gonest.Get[*Clock](app); err == nil {
		t.Fatal("unnamed clock should not resolve")
	}
	if _, err := gonest.Get[*gonest.RouteTable](app); err != nil {
		t.Fatal(err)
	}

	if err := app.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := app.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := "store:init items:bootstrap items:destroy store:shutdown"
	if got := strings.Join(trace, " "); got != want {
		t.Fatalf("trace %q, want %q", got, want)
	}
}

type privateSvc struct{}

func TestGetVisibility(t *testing.T) {
	hidden := &gonest.Module{Name: "hidden", Providers: []gonest.Provider{gonest.Value(&privateSvc{})}}
	app, err := gonest.New(&gonest.Module{Name: "root", Imports: []*gonest.Module{hidden}}, gonest.WithoutStartupLog())
	if err != nil {
		t.Fatal(err)
	}
	_, err = gonest.Get[*privateSvc](app)
	var re *gonest.ResolveError
	if !errors.As(err, &re) || !strings.Contains(err.Error(), "not visible") {
		t.Fatalf("err = %v", err)
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("MustGet should panic")
			}
		}()
		gonest.MustGet[*privateSvc](app)
	}()
	if _, err := gonest.Get[int](&gonest.App{}); err == nil {
		t.Fatal("zero App should fail")
	}
	if _, err := gonest.Get[int](nil); err == nil {
		t.Fatal("nil App should fail")
	}
}

type nilable interface{ X() }

func TestGetNilInterfaceValue(t *testing.T) {
	var n nilable
	app, err := gonest.New(&gonest.Module{Name: "root", Providers: []gonest.Provider{gonest.Value(n)}}, gonest.WithoutStartupLog())
	if err != nil {
		t.Fatal(err)
	}
	v, err := gonest.Get[nilable](app)
	if err != nil || v != nil {
		t.Fatal(v, err)
	}
}

func TestNewErrors(t *testing.T) {
	_, err := gonest.New(nil)
	if err == nil {
		t.Fatal("nil root")
	}
	bad := &gonest.Module{Name: "root", Providers: []gonest.Provider{gonest.Provide(func() (*Store, error) { return nil, errors.New("db down") })}}
	if _, err := gonest.New(bad, gonest.WithoutStartupLog(), nil, gonest.WithShutdownTimeout(-1)); err == nil || !strings.Contains(err.Error(), "db down") {
		t.Fatalf("err = %v", err)
	}
}

// Serve / Listen.

func freeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	_ = l.Close()
	return addr
}

func waitUp(t *testing.T, url string) {
	t.Helper()
	for i := 0; i < 200; i++ {
		resp, err := http.Get(url)
		if err == nil {
			_ = resp.Body.Close()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("server did not start")
}

type slowCtrl struct{ started chan struct{} }

func (c *slowCtrl) Routes(r gonest.Router) {
	r.GET("/slow", gonest.Handle(func(*gonest.Ctx, gonest.Empty) (string, error) {
		close(c.started)
		time.Sleep(200 * time.Millisecond)
		return "done", nil
	}))
	r.GET("/ping", gonest.Handle(func(*gonest.Ctx, gonest.Empty) (string, error) { return "pong", nil }))
}

type shutdownRecorder struct {
	mu   sync.Mutex
	done bool
}

func (s *shutdownRecorder) OnApplicationShutdown(context.Context) error {
	s.mu.Lock()
	s.done = true
	s.mu.Unlock()
	return nil
}

func (s *shutdownRecorder) wasDone() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.done
}

func TestServeGracefulShutdown(t *testing.T) {
	ctrl := &slowCtrl{started: make(chan struct{})}
	rec := &shutdownRecorder{}
	root := &gonest.Module{
		Name:        "root",
		Providers:   []gonest.Provider{gonest.Value(rec)},
		Controllers: []any{func() *slowCtrl { return ctrl }},
	}
	app, err := gonest.New(root, gonest.WithoutStartupLog(), gonest.WithShutdownTimeout(5*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	addr := freeAddr(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- app.Serve(ctx, &http.Server{Addr: addr, ReadHeaderTimeout: time.Second}) }()
	waitUp(t, "http://"+addr+"/ping")

	type result struct {
		body string
		err  error
	}
	res := make(chan result, 1)
	go func() {
		resp, err := http.Get("http://" + addr + "/slow")
		if err != nil {
			res <- result{err: err}
			return
		}
		defer resp.Body.Close()
		b, err := io.ReadAll(resp.Body)
		res <- result{string(b), err}
	}()
	<-ctrl.started
	cancel() // shutdown while /slow is in flight: it must complete

	r := <-res
	if r.err != nil || r.body != `"done"` {
		t.Fatalf("in-flight request: %q %v", r.body, r.err)
	}
	if err := <-done; err != nil {
		t.Fatalf("Serve = %v", err)
	}
	if !rec.wasDone() {
		t.Fatal("shutdown hooks not run")
	}
}

func TestServeErrors(t *testing.T) {
	app, err := gonest.New(&gonest.Module{Name: "root"}, gonest.WithoutStartupLog())
	if err != nil {
		t.Fatal(err)
	}
	if err := app.Serve(context.Background(), nil); err == nil {
		t.Fatal("nil server")
	}
	// Address in use: the server fails, hooks still run, error returned.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	err = app.Serve(context.Background(), &http.Server{Addr: l.Addr().String(), ReadHeaderTimeout: time.Second})
	if err == nil || !strings.Contains(err.Error(), "gonest: server") {
		t.Fatalf("err = %v", err)
	}
}

func TestServeShutdownTimeout(t *testing.T) {
	ctrl := &slowCtrl{started: make(chan struct{})}
	app, err := gonest.New(&gonest.Module{Name: "root", Controllers: []any{func() *slowCtrl { return ctrl }}},
		gonest.WithoutStartupLog(), gonest.WithShutdownTimeout(20*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	addr := freeAddr(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- app.Serve(ctx, &http.Server{Addr: addr, ReadHeaderTimeout: time.Second}) }()
	waitUp(t, "http://"+addr+"/ping")
	go func() {
		resp, err := http.Get("http://" + addr + "/slow")
		if err == nil {
			_ = resp.Body.Close()
		}
	}()
	<-ctrl.started
	cancel()
	if err := <-done; err == nil || !strings.Contains(err.Error(), "graceful shutdown") {
		t.Fatalf("err = %v", err)
	}
}

func TestListenStopsOnSignal(t *testing.T) {
	rec := &shutdownRecorder{}
	app, err := gonest.New(&gonest.Module{
		Name:        "root",
		Providers:   []gonest.Provider{gonest.Value(rec)},
		Controllers: []any{func() *slowCtrl { return &slowCtrl{started: make(chan struct{})} }},
	}, gonest.WithoutStartupLog())
	if err != nil {
		t.Fatal(err)
	}
	addr := freeAddr(t)
	done := make(chan error, 1)
	go func() { done <- app.Listen(addr) }()
	waitUp(t, "http://"+addr+"/ping")
	if err := syscall.Kill(syscall.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Listen did not return")
	}
	if !rec.wasDone() {
		t.Fatal("shutdown hooks not run")
	}
}

func ExampleNew() {
	app, err := gonest.New(&gonest.Module{Name: "app"}, gonest.WithoutStartupLog())
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(len(app.Routes()))
	_ = app.Shutdown(context.Background())
	// Output: 0
}
