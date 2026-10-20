package di

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"sort"
	"strings"

	"github.com/omyo/gonest/internal/core"
)

// Reasons used in *core.ResolveError.
const (
	reasonNoProvider       = "no provider"
	reasonCycle            = "cycle"
	reasonCtorFailed       = "constructor failed"
	reasonAmbiguous        = "ambiguous"
	reasonNotVisiblePrefix = "not visible from "
)

// resolver performs one resolution while Container.mu is held for writing.
// stack holds the providers under construction, for cycle detection.
type resolver struct {
	c     *Container
	stack []*entry
}

// resolveToken selects the provider of tok visible from module from and
// returns its instance. path is the chain leading to tok (tok excluded). When
// optional is set, a missing or invisible provider yields (nil, nil). When dry
// is set, transient constructors are not called.
func (r *resolver) resolveToken(tok core.Token, from *core.Module, path []core.Token, optional, dry bool) (any, error) {
	path = append(path[:len(path):len(path)], tok)
	e, absent, err := r.c.selectEntry(tok, from, path)
	if err != nil {
		if optional && absent {
			return nil, nil
		}
		return nil, err
	}
	return r.resolveEntry(e, path, dry)
}

// resolveEntry builds (or returns the cached instance of) e. path ends with e's token.
func (r *resolver) resolveEntry(e *entry, path []core.Token, dry bool) (any, error) {
	if e.built {
		return e.value, nil
	}
	if e.failed != nil {
		return nil, newError(path, e.owner, reasonCtorFailed, "", e.failed)
	}
	if slices.Contains(r.stack, e) {
		return nil, newError(path, e.owner, reasonCycle, "break the cycle by extracting the shared logic into a new provider", nil)
	}
	r.stack = append(r.stack, e)
	defer func() { r.stack = r.stack[:len(r.stack)-1] }()

	transient := e.scope == core.Transient
	args, err := r.args(e.plan, e.owner, path, dry && transient)
	if err != nil {
		return nil, err
	}
	if dry && transient {
		return nil, nil
	}
	out, err := call(e.fn, e.plan, args)
	if err != nil {
		if !transient {
			e.failed = err
		}
		return nil, newError(path, e.owner, reasonCtorFailed, "", err)
	}
	v := out[0].Interface()
	if !transient {
		e.built = true
		e.value = v
		r.c.instances = append(r.c.instances, core.Instance{Token: e.tok, Value: v, Owner: e.owner, Enhancer: e.enhancer})
	}
	return v, nil
}

// args resolves the arguments of a function described by p, as seen from module from.
func (r *resolver) args(p *plan, from *core.Module, path []core.Token, dry bool) ([]reflect.Value, error) {
	args := make([]reflect.Value, len(p.params))
	for i, pp := range p.params {
		if !pp.isObj {
			v, err := r.resolveToken(pp.tok, from, path, false, dry)
			if err != nil {
				return nil, err
			}
			args[i] = valueOf(v, pp.typ)
			continue
		}
		obj := reflect.New(pp.typ).Elem()
		for _, f := range pp.object {
			v, err := r.resolveToken(f.tok, from, path, f.optional, dry)
			if err != nil {
				return nil, err
			}
			if v != nil {
				obj.Field(f.index).Set(reflect.ValueOf(v))
			}
		}
		args[i] = obj
	}
	return args, nil
}

// selectEntry chooses the provider of tok seen from module from: the one owned
// by from, else the single visible module provider, else a framework provider
// (nil owner). absent reports that no provider is usable (missing or not
// visible), as opposed to ambiguous. The caller holds c.mu for writing.
func (c *Container) selectEntry(tok core.Token, from *core.Module, path []core.Token) (e *entry, absent bool, err error) {
	key := cacheKey{tok, from}
	if e, ok := c.cache[key]; ok {
		return e, false, nil
	}
	cands := c.byToken[tok]
	if len(cands) == 0 {
		return nil, true, newError(path, from, reasonNoProvider, c.missingHint(tok), nil)
	}
	var framework *entry
	var visible []*entry
	for _, cand := range cands {
		switch {
		case cand.owner == from:
			c.cache[key] = cand
			return cand, false, nil
		case cand.owner == nil:
			framework = cand
		case c.vis == nil || c.vis.Visible(from, cand.owner, tok):
			visible = append(visible, cand)
		}
	}
	switch {
	case len(visible) == 1:
		e = visible[0]
	case len(visible) > 1:
		return nil, false, newError(path, from, reasonAmbiguous,
			fmt.Sprintf("%s is provided by modules %s; import only one of them or register the providers with Named", tok, moduleNames(visible)), nil)
	case framework != nil:
		e = framework
	default:
		return nil, true, newError(path, from, reasonNotVisiblePrefix+moduleLabel(from), notVisibleHint(tok, from, cands), nil)
	}
	c.cache[key] = e
	return e, false, nil
}

func moduleLabel(m *core.Module) string {
	if m == nil {
		return "the application root"
	}
	return "module " + m.Name
}

func moduleNames(es []*entry) string {
	names := make([]string, len(es))
	for i, e := range es {
		names[i] = e.owner.Name
	}
	return strings.Join(names, ", ")
}

func notVisibleHint(tok core.Token, from *core.Module, cands []*entry) string {
	names := moduleNames(cands)
	if from == nil {
		return fmt.Sprintf("%s is provided by module %s; export it from %s and import that module in the root module", tok, names, names)
	}
	return fmt.Sprintf("%s is provided by module %s; add it to the Exports of %s and import %s in module %s", tok, names, names, names, from.Name)
}

// missingHint suggests close matches for a token with no provider.
func (c *Container) missingHint(tok core.Token) string {
	var similar, names, impls []string
	for t := range c.byToken {
		switch {
		case t.Type == tok.Type && t.Name != tok.Name:
			names = append(names, t.String())
		case t.Name == tok.Name && related(t.Type, tok.Type):
			similar = append(similar, "did you mean "+t.String()+"?")
		case t.Name == tok.Name && tok.Type.Kind() == reflect.Interface && t.Type.Implements(tok.Type):
			impls = append(impls, t.Type.String())
		}
	}
	sort.Strings(similar)
	hints := similar
	if len(names) > 0 {
		sort.Strings(names)
		h := "registered with other names: " + strings.Join(names, ", ")
		if tok.Name == "" {
			h += "; inject a named provider through a parameter object field tagged name:\"...\""
		}
		hints = append(hints, h)
	}
	if len(impls) > 0 {
		sort.Strings(impls)
		hints = append(hints, fmt.Sprintf("%s implements %s; register it with Bind[%s](ctor)", strings.Join(impls, ", "), tok.Type, tok.Type))
	}
	if len(hints) == 0 {
		return "register a provider for " + tok.String() + " in this module or import a module that exports it"
	}
	return strings.Join(hints, "; ")
}

// related reports whether a and b differ only by one level of pointer.
func related(a, b reflect.Type) bool {
	return (a.Kind() == reflect.Pointer && a.Elem() == b) || (b.Kind() == reflect.Pointer && b.Elem() == a)
}

func newError(path []core.Token, m *core.Module, reason, hint string, err error) *core.ResolveError {
	var name string
	if m != nil {
		name = m.Name
	}
	return &core.ResolveError{Path: slices.Clone(path), Module: name, Reason: reason, Hint: hint, Err: err}
}

// rootCause returns a key identifying the underlying problem of a resolution
// error, so Validate reports each problem once.
func rootCause(err error) string {
	var re *core.ResolveError
	if !errors.As(err, &re) || len(re.Path) == 0 {
		return err.Error()
	}
	last := re.Path[len(re.Path)-1]
	if re.Reason == reasonCycle {
		start := slices.Index(re.Path, last)
		members := make([]string, 0, len(re.Path)-start)
		for _, t := range re.Path[start : len(re.Path)-1] {
			members = append(members, t.String())
		}
		sort.Strings(members)
		return "cycle|" + strings.Join(members, ",")
	}
	return re.Reason + "|" + re.Module + "|" + last.String()
}
