package pipeline

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/omyo-group/gonest/internal/core"
)

// routeSpec is a route as declared by a controller, before compilation.
type routeSpec struct {
	method    string
	path      string // group prefixes + route path, without controller prefix
	handler   *core.Handler
	ctrlOpts  []core.RouteOption
	routeOpts []core.RouteOption
}

// routerState is shared by a controller's router and all its groups.
type routerState struct {
	prefix string
	specs  []routeSpec
	errs   []error
}

// router implements core.Router. Groups share the state of their parent and
// carry their own path prefix and option list.
type router struct {
	st   *routerState
	base string
	opts []core.RouteOption
}

var _ core.Router = (*router)(nil)

// collectRoutes runs ctrl.Routes on a fresh router, converting panics into errors.
func collectRoutes(ctrl core.Controller) (st *routerState, err error) {
	st = &routerState{}
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("panic in Routes: %v", p)
		}
	}()
	ctrl.Routes(&router{st: st})
	if len(st.errs) > 0 {
		return nil, errors.Join(st.errs...)
	}
	return st, nil
}

// Prefix sets the controller prefix. Called from a group it still sets the
// controller-wide prefix; it applies to every route regardless of call order.
func (r *router) Prefix(prefix string) { r.st.prefix = prefix }

// Use appends options for the routes declared after it in this router or group.
func (r *router) Use(opts ...core.RouteOption) {
	// Full slice expression: groups and specs keep their own snapshot.
	r.opts = append(r.opts[:len(r.opts):len(r.opts)], opts...)
}

// Group declares routes under prefix with extra options.
func (r *router) Group(prefix string, fn func(r core.Router), opts ...core.RouteOption) {
	if fn == nil {
		r.st.errs = append(r.st.errs, fmt.Errorf("group %q: nil function", prefix))
		return
	}
	g := &router{
		st:   r.st,
		base: r.base + "/" + prefix,
		opts: append(r.opts[:len(r.opts):len(r.opts)], opts...),
	}
	fn(g)
}

// GET declares a GET route.
func (r *router) GET(path string, h *core.Handler, opts ...core.RouteOption) {
	r.Handle(http.MethodGet, path, h, opts...)
}

// POST declares a POST route.
func (r *router) POST(path string, h *core.Handler, opts ...core.RouteOption) {
	r.Handle(http.MethodPost, path, h, opts...)
}

// PUT declares a PUT route.
func (r *router) PUT(path string, h *core.Handler, opts ...core.RouteOption) {
	r.Handle(http.MethodPut, path, h, opts...)
}

// PATCH declares a PATCH route.
func (r *router) PATCH(path string, h *core.Handler, opts ...core.RouteOption) {
	r.Handle(http.MethodPatch, path, h, opts...)
}

// DELETE declares a DELETE route.
func (r *router) DELETE(path string, h *core.Handler, opts ...core.RouteOption) {
	r.Handle(http.MethodDelete, path, h, opts...)
}

// OPTIONS declares an OPTIONS route.
func (r *router) OPTIONS(path string, h *core.Handler, opts ...core.RouteOption) {
	r.Handle(http.MethodOptions, path, h, opts...)
}

// HEAD declares a HEAD route.
func (r *router) HEAD(path string, h *core.Handler, opts ...core.RouteOption) {
	r.Handle(http.MethodHead, path, h, opts...)
}

// Handle declares a route for any method.
func (r *router) Handle(method, path string, h *core.Handler, opts ...core.RouteOption) {
	method = strings.ToUpper(strings.TrimSpace(method))
	if !validMethod(method) {
		r.st.errs = append(r.st.errs, fmt.Errorf("route %q: invalid HTTP method %q", path, method))
		return
	}
	if h == nil || (!h.Raw && h.InType == nil) {
		r.st.errs = append(r.st.errs, fmt.Errorf("route %s %q: handler must be built with gonest.Handle or gonest.HandleRaw", method, path))
		return
	}
	r.st.specs = append(r.st.specs, routeSpec{
		method:    method,
		path:      r.base + "/" + path,
		handler:   h,
		ctrlOpts:  r.opts[:len(r.opts):len(r.opts)],
		routeOpts: append([]core.RouteOption(nil), opts...),
	})
}

func validMethod(m string) bool {
	if m == "" {
		return false
	}
	for i := 0; i < len(m); i++ {
		if m[i] < 'A' || m[i] > 'Z' {
			return false
		}
	}
	return true
}
