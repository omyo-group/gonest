package guide_test

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/omyo/gonest"
)

// Trace records the steps of one request.
type Trace struct {
	mu    sync.Mutex
	steps []string
}

// Add records a step.
func (t *Trace) Add(s string) { t.mu.Lock(); t.steps = append(t.steps, s); t.mu.Unlock() }

// Flush returns and clears the steps.
func (t *Trace) Flush() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	s := strings.Join(t.steps, "\n")
	t.steps = nil
	return s
}

func guard(t *Trace, name string) gonest.Guard {
	return gonest.GuardFunc(func(*gonest.Ctx) (bool, error) { t.Add("guard " + name); return true, nil })
}

func interceptor(t *Trace, name string) gonest.Interceptor {
	return gonest.InterceptorFunc(func(_ *gonest.Ctx, next gonest.Next) (any, error) {
		t.Add("interceptor " + name + " before")
		out, err := next()
		t.Add("interceptor " + name + " after")
		return out, err
	})
}

func pipe(t *Trace, name string) gonest.Pipe {
	return gonest.PipeFunc(func(_ *gonest.Ctx, v any, _ gonest.ArgMeta) (any, error) {
		t.Add("pipe " + name)
		return v, nil
	})
}

func filter(t *Trace, name string, handle bool) gonest.ExceptionFilter {
	return gonest.ExceptionFilterFunc(func(ctx *gonest.Ctx, err error) bool {
		t.Add(fmt.Sprintf("filter %s (handled=%v)", name, handle))
		if handle {
			ctx.Gin().JSON(418, gonest.ErrorBody{StatusCode: 418, Message: err.Error(), Error: "I'm a teapot"})
		}
		return handle
	})
}

// OrderController declares one route with enhancers at every level.
type OrderController struct{ t *Trace }

// Routes implements gonest.Controller.
func (c *OrderController) Routes(r gonest.Router) {
	r.Prefix("/order")
	r.Use(
		gonest.UseGuards(guard(c.t, "controller")),
		gonest.UseInterceptors(interceptor(c.t, "controller")),
		gonest.UsePipes(pipe(c.t, "controller")),
		gonest.UseFilters(filter(c.t, "controller", false)),
	)
	r.GET("", gonest.Handle(c.Run),
		gonest.UseGuards(guard(c.t, "route")),
		gonest.UseInterceptors(interceptor(c.t, "route")),
		gonest.UsePipes(pipe(c.t, "route")),
		gonest.UseFilters(filter(c.t, "route", false)),
	)
}

// RunInput makes the handler fail on demand.
type RunInput struct {
	Fail bool `query:"fail"`
}

// Run is the handler.
func (c *OrderController) Run(_ *gonest.Ctx, in RunInput) (string, error) {
	c.t.Add("handler")
	if in.Fail {
		return "", errors.New("boom")
	}
	return "ok", nil
}

func Example_pipelineOrder() {
	t := &Trace{}
	mod := &gonest.Module{
		Name: "app",
		Providers: []gonest.Provider{
			gonest.Value(t),
			gonest.AppGuard(func(t *Trace) gonest.Guard { return guard(t, "global") }),
			gonest.AppInterceptor(func(t *Trace) gonest.Interceptor { return interceptor(t, "global") }),
			gonest.AppPipe(func(t *Trace) gonest.Pipe { return pipe(t, "global") }),
			gonest.AppFilter(func(t *Trace) gonest.ExceptionFilter { return filter(t, "global", true) }),
		},
		Controllers: []any{func(t *Trace) *OrderController { return &OrderController{t: t} }},
		Configure: func(m *gonest.MiddlewareConsumer, t *Trace) {
			m.Apply(func(c *gin.Context) { t.Add("middleware"); c.Next() }).ForAllRoutes()
		},
	}
	app, err := gonest.New(mod, quiet()...)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(call(app, "GET", "/order", ""))
	fmt.Println(t.Flush())
	fmt.Println("---")
	fmt.Println(call(app, "GET", "/order?fail=true", ""))
	fmt.Println(t.Flush())
	// Output:
	// 200 "ok"
	// middleware
	// guard global
	// guard controller
	// guard route
	// interceptor global before
	// interceptor controller before
	// interceptor route before
	// pipe global
	// pipe controller
	// pipe route
	// handler
	// interceptor route after
	// interceptor controller after
	// interceptor global after
	// ---
	// 418 {"statusCode":418,"message":"boom","error":"I'm a teapot"}
	// middleware
	// guard global
	// guard controller
	// guard route
	// interceptor global before
	// interceptor controller before
	// interceptor route before
	// pipe global
	// pipe controller
	// pipe route
	// handler
	// interceptor route after
	// interceptor controller after
	// interceptor global after
	// filter route (handled=false)
	// filter controller (handled=false)
	// filter global (handled=true)
}

// docs:public-metadata

// isPublic is the metadata key of Public.
const isPublic = "public"

// Public marks a route that the global AuthGuard lets through
// (NestJS @Public() + Reflector).
func Public() gonest.RouteOption { return gonest.SetMetadata(isPublic, true) }

// AuthGuard is registered with gonest.AppGuard, so it runs for every route.
type AuthGuard struct{}

// CanActivate implements gonest.Guard.
func (AuthGuard) CanActivate(ctx *gonest.Ctx) (bool, error) {
	if v, ok := ctx.Route().Metadata(isPublic); ok && v == true {
		return true, nil
	}
	if ctx.Header("Authorization") == "" {
		return false, gonest.Unauthorized("login required")
	}
	return true, nil
}

// docs:end

// StatusController has one public and one protected route.
type StatusController struct{}

// Routes implements gonest.Controller.
func (StatusController) Routes(r gonest.Router) {
	r.GET("/status", gonest.Handle(func(*gonest.Ctx, gonest.Empty) (string, error) { return "up", nil }), Public())
	r.GET("/admin", gonest.Handle(func(*gonest.Ctx, gonest.Empty) (string, error) { return "secret", nil }))
}

func Example_publicMetadata() {
	// docs:app-guard
	mod := &gonest.Module{
		Name:        "app",
		Providers:   []gonest.Provider{gonest.AppGuard(func() AuthGuard { return AuthGuard{} })},
		Controllers: []any{func() StatusController { return StatusController{} }},
	}
	// docs:end
	app, err := gonest.New(mod, quiet()...)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(call(app, "GET", "/status", ""))
	fmt.Println(call(app, "GET", "/admin", ""))
	fmt.Println(call(app, "GET", "/admin", "", "Authorization", "Bearer x"))
	// Output:
	// 200 "up"
	// 401 {"statusCode":401,"message":"login required","error":"Unauthorized"}
	// 200 "secret"
}
