package di

import (
	"errors"
	"fmt"
	"reflect"
	"sync"

	"github.com/omyo-group/gonest/internal/core"
)

// entry is one registered provider together with its singleton state.
type entry struct {
	tok      core.Token
	owner    *core.Module
	scope    core.Scope
	enhancer core.EnhancerKind

	// Constructor providers.
	fn   reflect.Value
	plan *plan

	// Singleton state, guarded by Container.mu. Value providers start built.
	built  bool
	value  any
	failed error // cached constructor failure, so a constructor runs at most once
}

// cacheKey identifies a provider selection: a token as seen from a module.
type cacheKey struct {
	tok  core.Token
	from *core.Module
}

// Container implements core.Container.
//
// All discovery work (constructor signature analysis, parameter objects, tag
// parsing) happens in Register. Resolution of a singleton that is already built
// is one map lookup under a read lock and does not allocate.
//
// Constructors run while the container holds its internal lock, so a
// constructor must not call back into the same container. Functions passed to
// Invoke run after the lock is released and may use the container freely.
type Container struct {
	mu        sync.RWMutex
	entries   []*entry
	byToken   map[core.Token][]*entry
	vis       core.Visibility
	cache     map[cacheKey]*entry
	plans     map[reflect.Type]*plan
	instances []core.Instance
}

// New returns an empty container whose visibility allows everything.
func New() *Container {
	return &Container{
		byToken: map[core.Token][]*entry{},
		cache:   map[cacheKey]*entry{},
		plans:   map[reflect.Type]*plan{},
	}
}

// Register implements core.Container. It validates the provider shape
// (constructor signature, assignability to the token type, scope, enhancer
// interface) and precomputes its injection plan.
func (c *Container) Register(p core.Provider, owner *core.Module) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, err := c.newEntry(p, owner)
	if err != nil {
		return fmt.Errorf("gonest: cannot register %s%s: %w", describe(p), inModule(owner), err)
	}
	for _, o := range c.byToken[e.tok] {
		if o.owner == owner {
			return fmt.Errorf("gonest: cannot register %s%s: %w", describe(p), inModule(owner), errDuplicate)
		}
	}
	c.entries = append(c.entries, e)
	c.byToken[e.tok] = append(c.byToken[e.tok], e)
	if e.built {
		c.instances = append(c.instances, core.Instance{Token: e.tok, Value: e.value, Owner: owner, Enhancer: e.enhancer})
	}
	clear(c.cache)
	return nil
}

var errDuplicate = errors.New("a provider with this token is already registered in the same module")

func describe(p core.Provider) string {
	if p.Token.Type != nil {
		return "provider " + p.Token.String()
	}
	if p.Ctor != nil {
		return "provider " + reflect.TypeOf(p.Ctor).String()
	}
	return "provider <nil>"
}

func inModule(m *core.Module) string {
	if m == nil {
		return ""
	}
	return " in module " + m.Name
}

// newEntry checks a provider and builds its entry. The caller holds c.mu.
func (c *Container) newEntry(p core.Provider, owner *core.Module) (*entry, error) {
	switch p.Scope {
	case core.Singleton, core.Transient:
	case core.RequestScoped:
		return nil, errors.New("request scope is not supported in this version; use Singleton or Transient")
	default:
		return nil, fmt.Errorf("unknown scope %s", p.Scope)
	}
	e := &entry{tok: p.Token, owner: owner, scope: p.Scope, enhancer: p.Enhancer}
	switch p.Kind {
	case core.KindValue:
		if p.Token.Type == nil {
			return nil, errors.New("value provider has no type")
		}
		if p.Value == nil {
			if !nilable(p.Token.Type) {
				return nil, fmt.Errorf("nil value for non-nilable type %s", p.Token.Type)
			}
		} else if vt := reflect.TypeOf(p.Value); !vt.AssignableTo(p.Token.Type) {
			return nil, fmt.Errorf("value of type %s is not assignable to %s", vt, p.Token.Type)
		}
		e.scope = core.Singleton
		e.built = true
		e.value = p.Value
	case core.KindConstructor:
		if err := c.checkCtor(p, e); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unknown provider kind %d", p.Kind)
	}
	if err := checkEnhancer(p.Enhancer, p.Token.Type); err != nil {
		return nil, err
	}
	return e, nil
}

func (c *Container) checkCtor(p core.Provider, e *entry) error {
	if p.Ctor == nil {
		return errors.New("constructor is nil")
	}
	ft := reflect.TypeOf(p.Ctor)
	if ft.Kind() != reflect.Func {
		return fmt.Errorf("constructor must be a function, got %s", ft)
	}
	switch {
	case ft.NumOut() == 0:
		return fmt.Errorf("constructor %s returns nothing; it must return T or (T, error)", ft)
	case ft.NumOut() > 2:
		return fmt.Errorf("constructor %s returns too many values; it must return T or (T, error)", ft)
	case ft.NumOut() == 2 && ft.Out(1) != errorType:
		return fmt.Errorf("constructor %s: second result must be error", ft)
	case ft.Out(0) == errorType:
		return fmt.Errorf("constructor %s returns only an error; it must return T or (T, error)", ft)
	}
	if p.Token.Type == nil {
		return errors.New("provider has no type")
	}
	if !ft.Out(0).AssignableTo(p.Token.Type) {
		return fmt.Errorf("constructor result %s is not assignable to %s", ft.Out(0), p.Token.Type)
	}
	pl, err := c.planFor(ft)
	if err != nil {
		return err
	}
	e.fn = reflect.ValueOf(p.Ctor)
	e.plan = pl
	return nil
}

var enhancerTypes = map[core.EnhancerKind]reflect.Type{
	core.EnhancerGuard:       reflect.TypeFor[core.Guard](),
	core.EnhancerInterceptor: reflect.TypeFor[core.Interceptor](),
	core.EnhancerPipe:        reflect.TypeFor[core.Pipe](),
	core.EnhancerFilter:      reflect.TypeFor[core.ExceptionFilter](),
}

func checkEnhancer(k core.EnhancerKind, t reflect.Type) error {
	if k == core.EnhancerNone {
		return nil
	}
	it, ok := enhancerTypes[k]
	if !ok {
		return fmt.Errorf("unknown enhancer kind %d", k)
	}
	if !t.Implements(it) {
		return fmt.Errorf("%s does not implement %s", t, it)
	}
	return nil
}

// SetVisibility implements core.Container. A nil v restores the default,
// which allows everything.
func (c *Container) SetVisibility(v core.Visibility) {
	c.mu.Lock()
	c.vis = v
	clear(c.cache)
	c.mu.Unlock()
}

// Validate implements core.Container. It resolves every registered provider
// from its owner module, instantiating all singletons. Transient providers have
// their dependencies checked but their constructors are not called. Problems
// sharing a root cause are reported once.
func (c *Container) Validate() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	var errs []error
	seen := map[string]bool{}
	for _, e := range c.entries {
		r := resolver{c: c}
		if _, err := r.resolveEntry(e, []core.Token{e.tok}, true); err != nil {
			k := rootCause(err)
			if !seen[k] {
				seen[k] = true
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

// Resolve implements core.Container. Errors are *core.ResolveError.
func (c *Container) Resolve(tok core.Token, from *core.Module) (any, error) {
	c.mu.RLock()
	if e, ok := c.cache[cacheKey{tok, from}]; ok && e.built {
		v := e.value
		c.mu.RUnlock()
		return v, nil
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()
	if tok.Type == nil {
		return nil, newError([]core.Token{tok}, from, "no provider", "the token has no type", nil)
	}
	r := resolver{c: c}
	return r.resolveToken(tok, from, nil, false, false)
}

// Invoke implements core.Container. fn's parameters (plain types or parameter
// objects) are injected as seen from module from; fn runs after the container
// lock is released. A trailing error result is returned as err and removed
// from the results. A panic in fn is returned as an error.
func (c *Container) Invoke(fn any, from *core.Module) ([]reflect.Value, error) {
	if fn == nil {
		return nil, errors.New("gonest: invoke: nil function")
	}
	ft := reflect.TypeOf(fn)
	if ft.Kind() != reflect.Func {
		return nil, fmt.Errorf("gonest: invoke: %s is not a function", ft)
	}
	root := core.Token{Type: ft}
	if ft.NumOut() > 0 && ft.Out(0) != errorType {
		root.Type = ft.Out(0)
	}

	c.mu.Lock()
	pl, err := c.planFor(ft)
	if err != nil {
		c.mu.Unlock()
		return nil, fmt.Errorf("gonest: invoke: %w", err)
	}
	r := resolver{c: c}
	args, err := r.args(pl, from, []core.Token{root}, false)
	c.mu.Unlock()
	if err != nil {
		return nil, err
	}

	out, err := call(reflect.ValueOf(fn), pl, args)
	if pe := (*panicError)(nil); errors.As(err, &pe) {
		return nil, fmt.Errorf("gonest: invoke %s: %w", ft, err)
	}
	return out, err
}

// Instances implements core.Container: built singletons (including value
// providers) in creation order. Transient instances are not tracked.
func (c *Container) Instances() []core.Instance {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]core.Instance, len(c.instances))
	copy(out, c.instances)
	return out
}

// Enhancers implements core.Container: instances of AppGuard, AppInterceptor,
// AppPipe and AppFilter providers in registration order, instantiated from
// their owner module if needed. Providers that fail to build are skipped
// (Validate reports them).
func (c *Container) Enhancers() []core.Instance {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []core.Instance
	for _, e := range c.entries {
		if e.enhancer == core.EnhancerNone {
			continue
		}
		r := resolver{c: c}
		v, err := r.resolveEntry(e, []core.Token{e.tok}, false)
		if err != nil {
			continue
		}
		out = append(out, core.Instance{Token: e.tok, Value: v, Owner: e.owner, Enhancer: e.enhancer})
	}
	return out
}

var _ core.Container = (*Container)(nil)
