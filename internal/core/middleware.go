package core

import (
	"strings"

	"github.com/gin-gonic/gin"
)

// RouteSpec selects routes for middleware. Path matches the full route path
// in gin syntax ("/users/:id"); a trailing "*" matches any suffix
// ("/users/*" matches "/users" and everything below). An empty Method matches
// every method.
type RouteSpec struct {
	Method string
	Path   string
}

// ForPath selects every method on path.
func ForPath(path string) RouteSpec { return RouteSpec{Path: path} }

// ForRoute selects one method on path.
func ForRoute(method, path string) RouteSpec {
	return RouteSpec{Method: strings.ToUpper(method), Path: path}
}

// Matches reports whether the spec selects a route.
func (s RouteSpec) Matches(method, path string) bool {
	if s.Method != "" && !strings.EqualFold(s.Method, method) {
		return false
	}
	if strings.HasSuffix(s.Path, "*") {
		prefix := strings.TrimSuffix(strings.TrimSuffix(s.Path, "*"), "/")
		return path == prefix || strings.HasPrefix(path, prefix+"/") || prefix == ""
	}
	return trimSlash(s.Path) == trimSlash(path)
}

func trimSlash(p string) string {
	if len(p) > 1 {
		return strings.TrimSuffix(p, "/")
	}
	return p
}

// MiddlewareRule binds gin middleware to a set of routes.
type MiddlewareRule struct {
	// Module is the name of the module whose Configure declared the rule.
	Module   string
	Handlers []gin.HandlerFunc
	// All applies the rule to every route of the application.
	All     bool
	Include []RouteSpec
	Exclude []RouteSpec
}

// Matches reports whether the rule applies to a route.
func (r MiddlewareRule) Matches(method, path string) bool {
	for _, e := range r.Exclude {
		if e.Matches(method, path) {
			return false
		}
	}
	if r.All {
		return true
	}
	for _, i := range r.Include {
		if i.Matches(method, path) {
			return true
		}
	}
	return false
}

// MiddlewareConsumer collects middleware rules in Module.Configure,
// like NestJS MiddlewareConsumer:
//
//	Configure: func(m *gonest.MiddlewareConsumer, t *TenantResolver) {
//		m.Apply(t.Middleware()).Exclude(gonest.ForPath("/health")).ForAllRoutes()
//	}
type MiddlewareConsumer struct {
	module string
	rules  []MiddlewareRule
}

// NewMiddlewareConsumer creates a consumer for a module. Used by internal/module.
func NewMiddlewareConsumer(module string) *MiddlewareConsumer {
	return &MiddlewareConsumer{module: module}
}

// Apply starts a rule for the given middleware.
func (m *MiddlewareConsumer) Apply(h ...gin.HandlerFunc) *MiddlewareBuilder {
	return &MiddlewareBuilder{c: m, rule: MiddlewareRule{Module: m.module, Handlers: h}}
}

// Rules returns the collected rules in declaration order.
func (m *MiddlewareConsumer) Rules() []MiddlewareRule {
	out := make([]MiddlewareRule, len(m.rules))
	copy(out, m.rules)
	return out
}

// MiddlewareBuilder finishes a rule started by MiddlewareConsumer.Apply.
type MiddlewareBuilder struct {
	c    *MiddlewareConsumer
	rule MiddlewareRule
}

// Exclude removes routes from the rule.
func (b *MiddlewareBuilder) Exclude(specs ...RouteSpec) *MiddlewareBuilder {
	b.rule.Exclude = append(b.rule.Exclude, specs...)
	return b
}

// ForRoutes completes the rule for the given routes.
func (b *MiddlewareBuilder) ForRoutes(specs ...RouteSpec) {
	b.rule.Include = append(b.rule.Include, specs...)
	b.c.rules = append(b.c.rules, b.rule)
}

// ForAllRoutes completes the rule for every route.
func (b *MiddlewareBuilder) ForAllRoutes() {
	b.rule.All = true
	b.c.rules = append(b.c.rules, b.rule)
}
