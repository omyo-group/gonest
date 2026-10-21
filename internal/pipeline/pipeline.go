package pipeline

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
	"github.com/omyo/gonest/internal/core"
)

// Options configures a Mounter.
type Options struct {
	// Engine receives the routes. Required.
	Engine *gin.Engine
	// Logger is the framework logger; nil means slog.Default().
	Logger *slog.Logger
	// Routes receives every mounted route. Required.
	Routes *core.RouteTable
	// GlobalPrefix is prepended to every path, e.g. "/api".
	GlobalPrefix string
	// Validator overrides the shared validator instance; nil means a default one.
	Validator *validator.Validate
	// BodyLimit caps the size of JSON and urlencoded form request bodies in
	// bytes; requests above it get 413. Zero or negative means no limit.
	BodyLimit int64
	// MultipartLimit caps the size of multipart request bodies in bytes;
	// requests above it get 413. Zero or negative means no limit.
	MultipartLimit int64
}

// Globals are the application-wide pipeline components.
type Globals struct {
	Guards       []core.Guard
	Interceptors []core.Interceptor
	Pipes        []core.Pipe
	Filters      []core.ExceptionFilter
}

// MiddlewareFor returns the gin middleware to run before the pipeline of the
// route with the given method and full path. May be nil.
type MiddlewareFor func(method, fullPath string) []gin.HandlerFunc

// Mounter mounts controllers on a gin engine. It compiles, once per route,
// the input binder and the guard, interceptor, pipe and filter chains; the
// gin handler it registers only walks precomputed slices.
//
// Mount, SetGlobals and InstallFallbacks are meant to be called during
// bootstrap. Serving requests concurrently with SetGlobals is safe.
type Mounter struct {
	opts     Options
	validate *validator.Validate
	globals  atomic.Pointer[Globals]

	mu      sync.Mutex
	seen    map[string]struct{}
	binders map[reflect.Type]*binder
}

// New creates a Mounter.
func New(opts Options) *Mounter {
	v := opts.Validator
	if v == nil {
		v = newValidator()
	}
	m := &Mounter{
		opts:     opts,
		validate: v,
		seen:     map[string]struct{}{},
		binders:  map[reflect.Type]*binder{},
	}
	m.globals.Store(&Globals{})
	return m
}

// SetGlobals installs global components. It may be called before or after
// Mount but must be called before the first request is served. The slices are
// copied; later calls replace the previous globals.
func (m *Mounter) SetGlobals(g Globals) {
	m.globals.Store(&Globals{
		Guards:       clone(g.Guards),
		Interceptors: clone(g.Interceptors),
		Pipes:        clone(g.Pipes),
		Filters:      clone(g.Filters),
	})
}

// Mount calls ctrl.Routes, compiles each route and registers it on the engine
// and in the route table. Duplicate method+path returns an error instead of
// panicking.
//
// fullPath passed to mw includes Options.GlobalPrefix, exactly as in
// RouteInfo.Path. When any route of the controller is invalid (bad input
// type, unknown path parameter, duplicate route, ...) nothing is registered
// and all problems are returned joined.
func (m *Mounter) Mount(ctrl core.Controller, module string, mw MiddlewareFor) ([]*core.RouteInfo, error) {
	if m.opts.Engine == nil || m.opts.Routes == nil {
		return nil, errors.New("gonest: pipeline.Options.Engine and Routes are required")
	}
	if ctrl == nil || isNilPointer(ctrl) {
		return nil, fmt.Errorf("gonest: cannot mount nil controller in module %q", module)
	}
	ctrlType := reflect.TypeOf(ctrl)
	st, err := collectRoutes(ctrl)
	if err != nil {
		return nil, fmt.Errorf("gonest: controller %s: %w", ctrlType, err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	var errs []error
	batch := map[string]struct{}{}
	compiled := make([]*route, 0, len(st.specs))
	for _, s := range st.specs {
		info := &core.RouteInfo{
			Method:           s.method,
			Path:             joinPaths(m.opts.GlobalPrefix, st.prefix, s.path),
			Module:           module,
			Controller:       ctrlType,
			Handler:          s.handler,
			ControllerConfig: core.BuildRouteConfig(s.ctrlOpts...),
			Config:           core.BuildRouteConfig(s.routeOpts...),
		}
		key := info.Method + " " + info.Path
		_, dupOld := m.seen[key]
		_, dupNew := batch[key]
		if dupOld || dupNew {
			errs = append(errs, fmt.Errorf("gonest: duplicate route %s (controller %s)", key, ctrlType))
			continue
		}
		batch[key] = struct{}{}
		r, err := m.compile(info)
		if err != nil {
			errs = append(errs, fmt.Errorf("gonest: route %s (controller %s): %w", key, ctrlType, err))
			continue
		}
		compiled = append(compiled, r)
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}

	out := make([]*core.RouteInfo, 0, len(compiled))
	for _, r := range compiled {
		var handlers []gin.HandlerFunc
		if mw != nil {
			handlers = append(handlers, mw(r.info.Method, r.info.Path)...)
		}
		handlers = append(handlers, r.serve)
		if err := register(m.opts.Engine, r.info.Method, r.info.Path, handlers); err != nil {
			return out, fmt.Errorf("gonest: route %s %s (controller %s): %w", r.info.Method, r.info.Path, ctrlType, err)
		}
		m.seen[r.info.Method+" "+r.info.Path] = struct{}{}
		m.opts.Routes.Add(r.info)
		out = append(out, r.info)
	}
	return out, nil
}

// InstallFallbacks makes unknown routes and methods answer with the standard
// JSON error body (404 / 405).
func (m *Mounter) InstallFallbacks() {
	e := m.opts.Engine
	if e == nil {
		return
	}
	e.HandleMethodNotAllowed = true
	e.NoRoute(func(c *gin.Context) {
		c.JSON(http.StatusNotFound, core.ErrorBody{
			StatusCode: http.StatusNotFound,
			Message:    "Cannot " + c.Request.Method + " " + c.Request.URL.Path,
			Error:      http.StatusText(http.StatusNotFound),
		})
	})
	e.NoMethod(func(c *gin.Context) {
		c.JSON(http.StatusMethodNotAllowed, core.ErrorBody{
			StatusCode: http.StatusMethodNotAllowed,
			Message:    "Cannot " + c.Request.Method + " " + c.Request.URL.Path,
			Error:      http.StatusText(http.StatusMethodNotAllowed),
		})
	})
}

// compile builds the executable route for info.
func (m *Mounter) compile(info *core.RouteInfo) (*route, error) {
	h := info.Handler
	r := &route{
		m:      m,
		info:   info,
		status: info.SuccessStatus(),
		raw:    h.Raw,
		meta:   core.ArgMeta{Type: h.InType, Route: info},
	}
	cc, rc := &info.ControllerConfig, &info.Config
	r.guards = concat(cc.Guards, rc.Guards)
	r.interceptors = concat(cc.Interceptors, rc.Interceptors)
	r.pipes = concat(cc.Pipes, rc.Pipes)
	r.filters = concat(rc.Filters, cc.Filters)
	if h.Raw {
		return r, nil
	}
	b, err := m.binderFor(h.InType)
	if err != nil {
		return nil, err
	}
	params := pathParams(info.Path)
	for _, f := range b.fields {
		if f.src == srcPath {
			if _, ok := params[f.name]; !ok {
				return nil, fmt.Errorf("input %s: field %s binds path parameter %q, which is not in the route path", h.InType, f.goPath, f.name)
			}
		}
	}
	r.bind = b
	return r, nil
}

// binderFor returns the cached binder for t, compiling it on first use.
// m.mu must be held.
func (m *Mounter) binderFor(t reflect.Type) (*binder, error) {
	if b, ok := m.binders[t]; ok {
		return b, nil
	}
	b, err := compileBinder(t, m.validate)
	if err != nil {
		return nil, err
	}
	b.bodyLimit = m.opts.BodyLimit
	b.multipartLimit = m.opts.MultipartLimit
	m.binders[t] = b
	return b, nil
}

// register adds the route to gin, turning gin's panics (wildcard conflicts,
// invalid paths) into errors.
func register(e *gin.Engine, method, path string, handlers []gin.HandlerFunc) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("gin rejected the route: %v", p)
		}
	}()
	e.Handle(method, path, handlers...)
	return nil
}

// joinPaths joins path fragments with single slashes. The result starts with
// "/" and has no trailing slash (except the root "/").
func joinPaths(parts ...string) string {
	var b strings.Builder
	for _, p := range parts {
		for _, seg := range strings.Split(p, "/") {
			if seg != "" {
				b.WriteByte('/')
				b.WriteString(seg)
			}
		}
	}
	if b.Len() == 0 {
		return "/"
	}
	return b.String()
}

// pathParams returns the parameter names (":id", "*file") of a gin path.
func pathParams(path string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, seg := range strings.Split(path, "/") {
		if len(seg) > 1 && (seg[0] == ':' || seg[0] == '*') {
			out[seg[1:]] = struct{}{}
		}
	}
	return out
}

func isNilPointer(v any) bool {
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Func, reflect.Interface, reflect.Chan:
		return rv.IsNil()
	default:
		return false
	}
}

func clone[T any](s []T) []T {
	if len(s) == 0 {
		return nil
	}
	return append([]T(nil), s...)
}

func concat[T any](a, b []T) []T {
	if len(a)+len(b) == 0 {
		return nil
	}
	out := make([]T, 0, len(a)+len(b))
	out = append(out, a...)
	return append(out, b...)
}
