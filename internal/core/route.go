package core

import (
	"reflect"
	"sync"
)

// ResponseDoc documents one possible response of a route for OpenAPI.
type ResponseDoc struct {
	Status      int
	Description string
	// Type is the body type; nil for no body.
	Type reflect.Type
}

// RouteConfig holds the options set on a controller (Router.Use) or on a
// single route. Build it with RouteOption values, not by hand.
type RouteConfig struct {
	Guards       []Guard
	Interceptors []Interceptor
	Pipes        []Pipe
	Filters      []ExceptionFilter

	// Status overrides the success status code (default 200, 201 for POST).
	Status int

	// Documentation, read by the openapi package.
	Summary     string
	Description string
	Tags        []string
	Deprecated  bool
	Responses   []ResponseDoc
	Security    []string
	// Hidden excludes the route from generated documentation.
	Hidden bool

	// Metadata is arbitrary data guards and interceptors can read through
	// Ctx.Route().Metadata (NestJS SetMetadata / Reflector).
	Metadata map[string]any
}

// RouteOption configures a route or, through Router.Use, a whole controller.
type RouteOption func(*RouteConfig)

// UseGuards appends guards.
func UseGuards(g ...Guard) RouteOption {
	return func(c *RouteConfig) { c.Guards = append(c.Guards, g...) }
}

// UseInterceptors appends interceptors.
func UseInterceptors(i ...Interceptor) RouteOption {
	return func(c *RouteConfig) { c.Interceptors = append(c.Interceptors, i...) }
}

// UsePipes appends pipes.
func UsePipes(p ...Pipe) RouteOption {
	return func(c *RouteConfig) { c.Pipes = append(c.Pipes, p...) }
}

// UseFilters appends exception filters.
func UseFilters(f ...ExceptionFilter) RouteOption {
	return func(c *RouteConfig) { c.Filters = append(c.Filters, f...) }
}

// Status sets the success status code.
func Status(code int) RouteOption { return func(c *RouteConfig) { c.Status = code } }

// Summary sets the OpenAPI summary.
func Summary(s string) RouteOption { return func(c *RouteConfig) { c.Summary = s } }

// Description sets the OpenAPI description.
func Description(s string) RouteOption { return func(c *RouteConfig) { c.Description = s } }

// Tags appends OpenAPI tags.
func Tags(t ...string) RouteOption {
	return func(c *RouteConfig) { c.Tags = append(c.Tags, t...) }
}

// Deprecated marks the route deprecated in OpenAPI.
func Deprecated() RouteOption { return func(c *RouteConfig) { c.Deprecated = true } }

// Hidden excludes the route from OpenAPI.
func Hidden() RouteOption { return func(c *RouteConfig) { c.Hidden = true } }

// Security names the OpenAPI security schemes the route requires.
func Security(schemes ...string) RouteOption {
	return func(c *RouteConfig) { c.Security = append(c.Security, schemes...) }
}

// Responds documents an additional response. Pass nil for no body.
func Responds(status int, description string, body any) RouteOption {
	var t reflect.Type
	if body != nil {
		t = reflect.TypeOf(body)
	}
	return func(c *RouteConfig) {
		c.Responses = append(c.Responses, ResponseDoc{Status: status, Description: description, Type: t})
	}
}

// SetMetadata attaches a value readable through Ctx.Route().Metadata(key).
func SetMetadata(key string, value any) RouteOption {
	return func(c *RouteConfig) {
		if c.Metadata == nil {
			c.Metadata = map[string]any{}
		}
		c.Metadata[key] = value
	}
}

// BuildRouteConfig applies options to a fresh RouteConfig.
func BuildRouteConfig(opts ...RouteOption) RouteConfig {
	var c RouteConfig
	for _, o := range opts {
		if o != nil {
			o(&c)
		}
	}
	return c
}

// RouteInfo is the resolved metadata of one mounted route. It is built at
// bootstrap and never changes afterwards.
type RouteInfo struct {
	// Method is the HTTP method, upper case.
	Method string
	// Path is the full path in gin syntax, e.g. "/users/:id".
	Path string
	// Module is the name of the module that declared the controller.
	Module string
	// Controller is the dynamic type of the controller value.
	Controller reflect.Type
	// Handler is the route handler with its input and output types.
	Handler *Handler
	// ControllerConfig holds options set with Router.Use on the controller
	// and enclosing groups.
	ControllerConfig RouteConfig
	// Config holds options set on the route itself.
	Config RouteConfig
}

// Metadata returns the value set with SetMetadata, route level first, then controller level.
func (r *RouteInfo) Metadata(key string) (any, bool) {
	if r == nil {
		return nil, false
	}
	if v, ok := r.Config.Metadata[key]; ok {
		return v, true
	}
	v, ok := r.ControllerConfig.Metadata[key]
	return v, ok
}

// SuccessStatus is the status written on success: Config.Status, else
// ControllerConfig.Status, else 201 for POST and 200 otherwise. A handler whose
// Out type is NoContent always yields 204.
func (r *RouteInfo) SuccessStatus() int {
	if r.Handler != nil && r.Handler.OutType == noContentType {
		return 204
	}
	if r.Config.Status != 0 {
		return r.Config.Status
	}
	if r.ControllerConfig.Status != 0 {
		return r.ControllerConfig.Status
	}
	if r.Method == "POST" {
		return 201
	}
	return 200
}

// RouteTable is the registry of all mounted routes. The application registers a
// *RouteTable as a provider, so any provider (for example the OpenAPI module)
// can inject it. Routes are complete once OnApplicationBootstrap runs.
type RouteTable struct {
	mu     sync.RWMutex
	routes []*RouteInfo
}

// Add records a route. Called by the pipeline while mounting.
func (t *RouteTable) Add(r *RouteInfo) {
	t.mu.Lock()
	t.routes = append(t.routes, r)
	t.mu.Unlock()
}

// All returns the mounted routes in mount order.
func (t *RouteTable) All() []*RouteInfo {
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := make([]*RouteInfo, len(t.routes))
	copy(out, t.routes)
	return out
}
