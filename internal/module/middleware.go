package module

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/omyo-group/gonest/internal/core"
)

var (
	consumerType = reflect.TypeFor[*core.MiddlewareConsumer]()
	errorType    = reflect.TypeFor[error]()
)

// Middleware holds the middleware rules declared by every module's Configure,
// in module dependency order (imported modules first).
type Middleware struct {
	rules []core.MiddlewareRule
}

// CollectMiddleware invokes the Configure function of every module of g, in
// dependency order, injecting its parameters (after the leading
// *core.MiddlewareConsumer) as seen from the module.
func CollectMiddleware(c core.Container, g *Graph) (*Middleware, error) {
	mw := &Middleware{}
	for _, m := range g.order {
		if m.Configure == nil {
			continue
		}
		rules, err := Configure(c, m)
		if err != nil {
			return nil, err
		}
		mw.rules = append(mw.rules, rules...)
	}
	return mw, nil
}

// Configure invokes m.Configure, a func(*core.MiddlewareConsumer, deps...)
// optionally returning an error, and returns the rules it declared. RouteSpec
// paths are normalised to start with "/".
func Configure(c core.Container, m *core.Module) ([]core.MiddlewareRule, error) {
	fn := reflect.ValueOf(m.Configure)
	ft := fn.Type()
	if ft.Kind() != reflect.Func || ft.IsVariadic() || ft.NumIn() == 0 || ft.In(0) != consumerType ||
		ft.NumOut() > 1 || (ft.NumOut() == 1 && ft.Out(0) != errorType) {
		return nil, fmt.Errorf("gonest: module %q: Configure must be a func(*gonest.MiddlewareConsumer, deps...) [error], got %s", m.Name, ft)
	}
	consumer := core.NewMiddlewareConsumer(m.Name)

	// The container cannot inject the consumer, so invoke a function of the
	// remaining parameters that prepends it.
	in := make([]reflect.Type, ft.NumIn()-1)
	for i := range in {
		in[i] = ft.In(i + 1)
	}
	out := make([]reflect.Type, ft.NumOut())
	for i := range out {
		out[i] = ft.Out(i)
	}
	wrapper := reflect.MakeFunc(reflect.FuncOf(in, out, false), func(args []reflect.Value) []reflect.Value {
		return fn.Call(append([]reflect.Value{reflect.ValueOf(consumer)}, args...))
	})
	if _, err := c.Invoke(wrapper.Interface(), m); err != nil {
		return nil, fmt.Errorf("gonest: module %q: Configure: %w", m.Name, err)
	}

	rules := consumer.Rules()
	for i := range rules {
		rules[i].Include = normalizeSpecs(rules[i].Include)
		rules[i].Exclude = normalizeSpecs(rules[i].Exclude)
	}
	return rules, nil
}

func normalizeSpecs(specs []core.RouteSpec) []core.RouteSpec {
	out := make([]core.RouteSpec, len(specs))
	for i, s := range specs {
		if !strings.HasPrefix(s.Path, "/") {
			s.Path = "/" + s.Path
		}
		out[i] = s
	}
	return out
}

// Rules returns the collected rules in order.
func (mw *Middleware) Rules() []core.MiddlewareRule {
	out := make([]core.MiddlewareRule, len(mw.rules))
	copy(out, mw.rules)
	return out
}

// For returns a function suitable for pipeline.MiddlewareFor. It strips
// globalPrefix from the full route path, then returns the handlers of every
// matching rule, in rule order. Called once per route at mount time.
func (mw *Middleware) For(globalPrefix string) func(method, fullPath string) []gin.HandlerFunc {
	prefix := cleanPrefix(globalPrefix)
	return func(method, fullPath string) []gin.HandlerFunc {
		path := stripPrefix(prefix, fullPath)
		var out []gin.HandlerFunc
		for _, r := range mw.rules {
			if r.Matches(method, path) {
				out = append(out, r.Handlers...)
			}
		}
		return out
	}
}

// cleanPrefix normalises a global prefix to "/a/b" form, or "" for none.
func cleanPrefix(p string) string {
	var parts []string
	for _, s := range strings.Split(p, "/") {
		if s != "" {
			parts = append(parts, s)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "/" + strings.Join(parts, "/")
}

func stripPrefix(prefix, path string) string {
	switch {
	case prefix == "":
		return path
	case path == prefix:
		return "/"
	case strings.HasPrefix(path, prefix+"/"):
		return path[len(prefix):]
	default:
		return path
	}
}
