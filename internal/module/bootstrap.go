package module

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/omyo-group/gonest/internal/core"
	"github.com/omyo-group/gonest/internal/di"
	"github.com/omyo-group/gonest/internal/pipeline"
)

// Options configures Bootstrap. It mirrors the root package's AppConfig.
type Options struct {
	// Engine receives the routes. Required.
	Engine *gin.Engine
	// Logger is the framework *slog.Logger provider; nil means slog.Default().
	Logger *slog.Logger
	// GlobalPrefix is prepended to every route path.
	GlobalPrefix string
	// BodyLimit caps JSON and urlencoded request bodies in bytes; <= 0
	// disables the limit.
	BodyLimit int64
	// MultipartLimit caps multipart request bodies in bytes; <= 0 disables
	// the limit.
	MultipartLimit int64
	// Globals are the application-wide enhancers set through options; they
	// come before the AppGuard/AppInterceptor/AppPipe/AppFilter providers.
	Globals pipeline.Globals
	// StartupLog logs modules, routes and boot duration.
	StartupLog bool
}

// Runtime is a bootstrapped application: container, routes and lifecycle.
type Runtime struct {
	Graph     *Graph
	Container core.Container
	Routes    *core.RouteTable
	// Logger is the *slog.Logger seen from the root module.
	Logger    *slog.Logger
	Lifecycle *Lifecycle
}

var (
	loggerToken = core.TokenOf[*slog.Logger]()
)

// Bootstrap builds and starts an application following the bootstrap order
// of docs/contracts.md: graph, providers, visibility and validation,
// middleware, controllers, global enhancers, OnModuleInit and
// OnApplicationBootstrap.
func Bootstrap(ctx context.Context, root *core.Module, opts Options) (*Runtime, error) {
	start := time.Now()
	if opts.Engine == nil {
		return nil, errors.New("gonest: bootstrap requires a gin engine")
	}
	g, err := Build(root)
	if err != nil {
		return nil, err
	}
	vis, err := NewVisibility(g)
	if err != nil {
		return nil, err
	}

	c := di.New()
	routes := &core.RouteTable{}
	if err := registerAll(c, g, routes, opts.Logger); err != nil {
		return nil, err
	}
	c.SetVisibility(vis)
	if err := c.Validate(); err != nil {
		return nil, err
	}

	log := opts.Logger
	if v, err := c.Resolve(loggerToken, g.root); err == nil {
		if l, ok := v.(*slog.Logger); ok && l != nil {
			log = l
		}
	}
	if log == nil {
		log = slog.Default()
	}

	mw, err := CollectMiddleware(c, g)
	if err != nil {
		return nil, err
	}

	mounter := pipeline.New(pipeline.Options{
		Engine:         opts.Engine,
		Logger:         log,
		Routes:         routes,
		GlobalPrefix:   opts.GlobalPrefix,
		BodyLimit:      opts.BodyLimit,
		MultipartLimit: opts.MultipartLimit,
	})
	controllers, err := mountControllers(c, g, mounter, mw.For(opts.GlobalPrefix))
	if err != nil {
		return nil, err
	}
	mounter.SetGlobals(globals(opts.Globals, c.Enhancers()))
	mounter.InstallFallbacks()

	var targets []Target
	for _, inst := range c.Instances() {
		targets = append(targets, Target{Module: inst.Owner, Value: inst.Value})
	}
	targets = append(targets, controllers...)
	lc := NewLifecycle(g, targets)
	if err := lc.Start(ctx); err != nil {
		return nil, err
	}

	rt := &Runtime{Graph: g, Container: c, Routes: routes, Logger: log, Lifecycle: lc}
	if opts.StartupLog {
		rt.logStartup(time.Since(start))
	}
	return rt, nil
}

// registerAll registers every module's providers, then the framework
// providers (*core.RouteTable and the fallback *slog.Logger) with a nil owner.
func registerAll(c core.Container, g *Graph, routes *core.RouteTable, log *slog.Logger) error {
	var errs []error
	for _, m := range g.order {
		for _, p := range m.Providers {
			if err := c.Register(p, m); err != nil {
				errs = append(errs, err)
			}
		}
	}
	if log == nil {
		log = slog.Default()
	}
	if err := c.Register(core.Value(routes), nil); err != nil {
		errs = append(errs, err)
	}
	// The framework logger is only a fallback: a module that sees a
	// module-provided *slog.Logger gets that one (the container prefers
	// visible module providers over framework ones).
	if err := c.Register(core.Value(log), nil); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// mountControllers invokes each controller constructor from its module and
// mounts it. It returns the controller instances as lifecycle targets.
func mountControllers(c core.Container, g *Graph, m *pipeline.Mounter, mw pipeline.MiddlewareFor) ([]Target, error) {
	var out []Target
	for _, mod := range g.order {
		for i, ctor := range mod.Controllers {
			res, err := c.Invoke(ctor, mod)
			if err != nil {
				return nil, fmt.Errorf("gonest: module %q: controller %d: %w", mod.Name, i, err)
			}
			if len(res) == 0 {
				return nil, fmt.Errorf("gonest: module %q: controller constructor %T returns no value", mod.Name, ctor)
			}
			v := res[0].Interface()
			ctrl, ok := v.(core.Controller)
			if !ok {
				return nil, fmt.Errorf("gonest: module %q: controller constructor %T returns %T, which does not implement Controller (missing Routes(gonest.Router))", mod.Name, ctor, v)
			}
			if _, err := m.Mount(ctrl, mod.Name, mw); err != nil {
				return nil, fmt.Errorf("gonest: module %q: %w", mod.Name, err)
			}
			out = append(out, Target{Module: mod, Value: v})
		}
	}
	return out, nil
}

// globals appends the enhancer provider instances, by kind, after the
// option-supplied globals.
func globals(base pipeline.Globals, enh []core.Instance) pipeline.Globals {
	g := pipeline.Globals{
		Guards:       append([]core.Guard(nil), base.Guards...),
		Interceptors: append([]core.Interceptor(nil), base.Interceptors...),
		Pipes:        append([]core.Pipe(nil), base.Pipes...),
		Filters:      append([]core.ExceptionFilter(nil), base.Filters...),
	}
	for _, e := range enh {
		switch e.Enhancer {
		case core.EnhancerGuard:
			if v, ok := e.Value.(core.Guard); ok {
				g.Guards = append(g.Guards, v)
			}
		case core.EnhancerInterceptor:
			if v, ok := e.Value.(core.Interceptor); ok {
				g.Interceptors = append(g.Interceptors, v)
			}
		case core.EnhancerPipe:
			if v, ok := e.Value.(core.Pipe); ok {
				g.Pipes = append(g.Pipes, v)
			}
		case core.EnhancerFilter:
			if v, ok := e.Value.(core.ExceptionFilter); ok {
				g.Filters = append(g.Filters, v)
			}
		}
	}
	return g
}

func (rt *Runtime) logStartup(d time.Duration) {
	names := make([]string, len(rt.Graph.order))
	for i, m := range rt.Graph.order {
		names[i] = m.Name
	}
	rt.Logger.Info("gonest: modules loaded", "count", len(names), "modules", strings.Join(names, ", "))
	for _, r := range rt.Routes.All() {
		name := "handler"
		if r.Handler != nil && r.Handler.Name != "" {
			name = r.Handler.Name
		}
		rt.Logger.Info("gonest: mapped route", "route", r.Method+" "+r.Path+" -> "+name)
	}
	rt.Logger.Info("gonest: application started", "routes", len(rt.Routes.All()), "duration", d)
}

// Shutdown runs the shutdown hooks; see Lifecycle.Stop.
func (rt *Runtime) Shutdown(ctx context.Context) error {
	return rt.Lifecycle.Stop(ctx)
}
