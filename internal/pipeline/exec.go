package pipeline

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"reflect"
	"runtime/debug"

	"github.com/gin-gonic/gin"
	ginjson "github.com/gin-gonic/gin/codec/json"
	"github.com/omyo/gonest/internal/core"
)

// route is a compiled route: everything the request path needs, precomputed.
type route struct {
	m      *Mounter
	info   *core.RouteInfo
	status int
	raw    bool
	bind   *binder
	meta   core.ArgMeta

	// Controller then route level, in execution order.
	guards       []core.Guard
	interceptors []core.Interceptor
	pipes        []core.Pipe
	// Route then controller level, in the order filters are tried.
	filters []core.ExceptionFilter
}

// errForbidden is returned when a guard denies access. HTTPError is mutable
// (Wrap), so a fresh value is built on each use.
func errForbidden() error { return core.Forbidden("Forbidden resource") }

// panicError carries a recovered panic to the exception filters.
type panicError struct {
	value any
	stack []byte
}

func (p *panicError) Error() string { return fmt.Sprintf("panic: %v", p.value) }

// Unwrap exposes a panicked error value to errors.Is / errors.As.
func (p *panicError) Unwrap() error {
	if err, ok := p.value.(error); ok {
		return err
	}
	return nil
}

// LoggerKey is the gin context key under which middleware may store a
// request-scoped *slog.Logger (for example one carrying a request id). When
// present, it becomes Ctx.Logger() for the request, so the framework's own
// error logs use it too. modules/logger.RequestLogger stores its child
// logger under this key (logger.ContextKey has the same value).
const LoggerKey = "gonest.logger"

// serve is the gin handler of the route.
func (r *route) serve(gc *gin.Context) {
	ctx := core.NewCtx(gc, r.info, r.logger(gc))
	g := r.m.globals.Load()
	out, err := r.run(ctx, g)
	if err != nil {
		r.handleError(ctx, g, err)
		return
	}
	if r.raw {
		return
	}
	r.write(ctx, g, out)
}

// logger returns the request logger stored by middleware under LoggerKey,
// else the application logger.
func (r *route) logger(gc *gin.Context) *slog.Logger {
	if v, ok := gc.Get(LoggerKey); ok {
		if l, ok := v.(*slog.Logger); ok && l != nil {
			return l
		}
	}
	return r.m.opts.Logger
}

// run executes guards, interceptors, binding, pipes and the handler,
// converting panics into errors.
func (r *route) run(ctx *core.Ctx, g *Globals) (out any, err error) {
	defer func() {
		if p := recover(); p != nil {
			if p == http.ErrAbortHandler { //nolint:errorlint // sentinel compared by identity, as net/http does
				panic(p)
			}
			out, err = nil, &panicError{value: p, stack: debug.Stack()}
		}
	}()
	if err := runGuards(ctx, g.Guards); err != nil {
		return nil, err
	}
	if err := runGuards(ctx, r.guards); err != nil {
		return nil, err
	}
	if len(g.Interceptors)+len(r.interceptors) == 0 {
		return r.invoke(ctx, g, nil)
	}
	c := &call{r: r, ctx: ctx, g: g}
	return c.chain(0)
}

func runGuards(ctx *core.Ctx, guards []core.Guard) error {
	for _, gd := range guards {
		ok, err := gd.CanActivate(ctx)
		if err != nil {
			return err
		}
		if !ok {
			return errForbidden()
		}
	}
	return nil
}

// call is the per-request state of an interceptor chain. The bound input is
// memoised so an interceptor calling next more than once (retry) does not
// read the request body twice.
type call struct {
	r     *route
	ctx   *core.Ctx
	g     *Globals
	bound bool
	in    any
	err   error
}

func (c *call) chain(i int) (any, error) {
	var ic core.Interceptor
	if n := len(c.g.Interceptors); i < n {
		ic = c.g.Interceptors[i]
	} else if i-n < len(c.r.interceptors) {
		ic = c.r.interceptors[i-n]
	} else {
		return c.r.invoke(c.ctx, c.g, c)
	}
	return ic.Intercept(c.ctx, func() (any, error) { return c.chain(i + 1) })
}

// invoke binds and validates the input, runs the pipes and calls the handler.
func (r *route) invoke(ctx *core.Ctx, g *Globals, c *call) (any, error) {
	h := r.info.Handler
	if r.raw {
		return h.Call(ctx, nil)
	}
	var (
		in  any
		err error
	)
	switch {
	case c == nil:
		in, err = r.bind.bind(ctx.Gin())
	case c.bound:
		in, err = c.in, c.err
	default:
		in, err = r.bind.bind(ctx.Gin())
		c.bound, c.in, c.err = true, in, err
	}
	if err != nil {
		return nil, err
	}
	if in, err = r.runPipes(ctx, g.Pipes, in); err != nil {
		return nil, err
	}
	if in, err = r.runPipes(ctx, r.pipes, in); err != nil {
		return nil, err
	}
	return h.Call(ctx, in)
}

func (r *route) runPipes(ctx *core.Ctx, pipes []core.Pipe, in any) (any, error) {
	for _, p := range pipes {
		v, err := p.Transform(ctx, in, r.meta)
		if err != nil {
			return nil, err
		}
		if reflect.TypeOf(v) != r.meta.Type {
			return nil, fmt.Errorf("gonest: pipe %T returned %T, want %s", p, v, r.meta.Type)
		}
		in = v
	}
	return in, nil
}

// jsonContentType is the Content-Type gin's JSON renderer writes.
const jsonContentType = "application/json; charset=utf-8"

// write renders a successful result. The value is encoded before anything is
// written (with gin's JSON codec, as gin.Context.JSON does), so a value that
// cannot be encoded (NaN, a channel, a failing MarshalJSON) goes through the
// exception filters as a 500 instead of a silent empty 200.
func (r *route) write(ctx *core.Ctx, g *Globals, out any) {
	gc := ctx.Gin()
	status := ctx.StatusOverride()
	if status == 0 {
		status = r.status
	}
	if _, ok := out.(core.NoContent); ok {
		gc.Status(status)
		gc.Writer.WriteHeaderNow()
		return
	}
	body, err := ginjson.API.Marshal(out)
	if err != nil {
		r.handleError(ctx, g, fmt.Errorf("gonest: encoding the response of %s: %w", r.info.Handler.Name, err))
		return
	}
	gc.Data(status, jsonContentType, body)
}

// handleError runs the exception filters: route, controller, global, then the
// default filter.
func (r *route) handleError(ctx *core.Ctx, g *Globals, err error) {
	if r.tryFilters(ctx, r.filters, err) || r.tryFilters(ctx, g.Filters, err) {
		return
	}
	defaultFilter(ctx, err)
}

func (r *route) tryFilters(ctx *core.Ctx, filters []core.ExceptionFilter, err error) bool {
	for _, f := range filters {
		handled, perr := catchSafely(ctx, f, err)
		if perr != nil {
			ctx.Logger().Error("gonest: exception filter panicked",
				"method", r.info.Method, "path", r.info.Path, "filter", fmt.Sprintf("%T", f),
				"panic", perr.value, "stack", string(perr.stack))
			defaultFilter(ctx, err)
			return true
		}
		if handled {
			return true
		}
	}
	return false
}

func catchSafely(ctx *core.Ctx, f core.ExceptionFilter, err error) (handled bool, perr *panicError) {
	defer func() {
		if p := recover(); p != nil {
			if p == http.ErrAbortHandler { //nolint:errorlint // sentinel compared by identity, as net/http does
				panic(p)
			}
			handled, perr = false, &panicError{value: p, stack: debug.Stack()}
		}
	}()
	return f.Catch(ctx, err), nil
}

// defaultFilter renders err as ErrorBody. HTTPErrors (possibly wrapped) keep
// their status; ValidationErrors become 400; anything else becomes 500 and is
// logged with the request logger.
func defaultFilter(ctx *core.Ctx, err error) {
	gc := ctx.Gin()
	route := ctx.Route()
	method, path := gc.Request.Method, gc.Request.URL.Path
	if route != nil {
		path = route.Path
	}
	var (
		he *core.HTTPError
		ve core.ValidationErrors
		pe *panicError
	)
	switch {
	case errors.As(err, &pe):
		ctx.Logger().Error("gonest: panic recovered",
			"method", method, "path", path, "panic", pe.value, "stack", string(pe.stack))
		he = core.Internal(http.StatusText(http.StatusInternalServerError))
	case errors.As(err, &he):
		if he.Status >= http.StatusInternalServerError {
			ctx.Logger().Error("gonest: request failed", "method", method, "path", path, "status", he.Status, "error", err)
		}
	case errors.As(err, &ve):
		he = core.BadRequest("validation failed", ve)
	default:
		ctx.Logger().Error("gonest: unhandled error", "method", method, "path", path, "error", err)
		he = core.Internal(http.StatusText(http.StatusInternalServerError))
	}
	if he.Status < 100 || he.Status > 599 {
		ctx.Logger().Error("gonest: HTTPError with invalid status", "method", method, "path", path, "status", he.Status, "error", err)
		he = core.Internal(http.StatusText(http.StatusInternalServerError))
	}
	if gc.Writer.Written() {
		ctx.Logger().Warn("gonest: response already written, error not sent", "method", method, "path", path, "error", err)
		return
	}
	gc.JSON(he.Status, he.Body())
}
