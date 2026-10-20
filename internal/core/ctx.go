package core

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// Ctx is the per-request context passed to handlers, guards, pipes,
// interceptors and filters. It wraps *gin.Context and implements
// context.Context (deadline and cancellation follow the HTTP request), so it can
// be passed to any function taking a context.
type Ctx struct {
	g      *gin.Context
	route  *RouteInfo
	logger *slog.Logger
	status int
}

// NewCtx builds a Ctx. Used by internal/pipeline and testing helpers.
func NewCtx(g *gin.Context, route *RouteInfo, logger *slog.Logger) *Ctx {
	if logger == nil {
		logger = slog.Default()
	}
	return &Ctx{g: g, route: route, logger: logger}
}

// Gin returns the underlying gin context, for raw handlers and gin interop.
func (c *Ctx) Gin() *gin.Context { return c.g }

// Request returns the HTTP request.
func (c *Ctx) Request() *http.Request { return c.g.Request }

// Writer returns the response writer.
func (c *Ctx) Writer() gin.ResponseWriter { return c.g.Writer }

// Route returns metadata of the matched route.
func (c *Ctx) Route() *RouteInfo { return c.route }

// Logger returns the request logger. Middleware may replace it with SetLogger.
func (c *Ctx) Logger() *slog.Logger { return c.logger }

// SetLogger replaces the request logger, e.g. to add a request id attribute.
func (c *Ctx) SetLogger(l *slog.Logger) {
	if l != nil {
		c.logger = l
	}
}

// Param returns a path parameter.
func (c *Ctx) Param(name string) string { return c.g.Param(name) }

// Query returns a query parameter.
func (c *Ctx) Query(name string) string { return c.g.Query(name) }

// Header returns a request header.
func (c *Ctx) Header(name string) string { return c.g.GetHeader(name) }

// Set stores a request-scoped value (shared with gin middleware via gin keys).
func (c *Ctx) Set(key string, value any) { c.g.Set(key, value) }

// Get returns a request-scoped value set by Set or by gin middleware.
func (c *Ctx) Get(key string) (any, bool) { return c.g.Get(key) }

// SetStatus overrides the success status code for this request.
func (c *Ctx) SetStatus(code int) { c.status = code }

// StatusOverride returns the status set with SetStatus, or 0.
func (c *Ctx) StatusOverride() int { return c.status }

// Deadline implements context.Context.
func (c *Ctx) Deadline() (time.Time, bool) { return c.std().Deadline() }

// Done implements context.Context.
func (c *Ctx) Done() <-chan struct{} { return c.std().Done() }

// Err implements context.Context.
func (c *Ctx) Err() error { return c.std().Err() }

// Value implements context.Context. String keys also see values stored with Set.
func (c *Ctx) Value(key any) any {
	if k, ok := key.(string); ok {
		if v, ok := c.g.Get(k); ok {
			return v
		}
	}
	return c.std().Value(key)
}

func (c *Ctx) std() context.Context {
	if c.g != nil && c.g.Request != nil {
		return c.g.Request.Context()
	}
	return context.Background()
}

// CtxValue returns the request value stored under key as a T.
func CtxValue[T any](c *Ctx, key string) (T, bool) {
	v, ok := c.Get(key)
	if !ok {
		var zero T
		return zero, false
	}
	t, ok := v.(T)
	return t, ok
}

var _ context.Context = (*Ctx)(nil)
