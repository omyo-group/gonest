package gonesttest

import (
	"fmt"

	"github.com/omyo/gonest"
)

// shadow copies the module graph reachable from root, replacing modules and
// providers. The caller's modules are never modified.
type shadow struct {
	providers   map[gonest.Token]gonest.Provider
	modules     map[*gonest.Module]*gonest.Module
	copies      map[*gonest.Module]*gonest.Module
	usedTokens  map[gonest.Token]bool
	usedModules map[*gonest.Module]bool
}

func newShadow(providers map[gonest.Token]gonest.Provider, modules map[*gonest.Module]*gonest.Module) *shadow {
	return &shadow{
		providers:   providers,
		modules:     modules,
		copies:      map[*gonest.Module]*gonest.Module{},
		usedTokens:  map[gonest.Token]bool{},
		usedModules: map[*gonest.Module]bool{},
	}
}

// copy returns the shadow of m. A module reached through several paths is
// copied once, so identity (and the module graph shape) is preserved.
func (s *shadow) copy(m *gonest.Module) *gonest.Module {
	if m == nil {
		return nil
	}
	if repl, ok := s.modules[m]; ok {
		s.usedModules[m] = true
		m = repl
	}
	if c, ok := s.copies[m]; ok {
		return c
	}
	c := new(gonest.Module)
	*c = *m
	s.copies[m] = c // before recursing: tolerates cycles (B1 reports them)

	c.Imports = s.copyAll(m.Imports)
	c.ReExports = s.copyAll(m.ReExports)
	if m.Providers != nil {
		c.Providers = make([]gonest.Provider, len(m.Providers))
		for i, p := range m.Providers {
			c.Providers[i] = s.replace(p)
		}
	}
	if m.Controllers != nil {
		c.Controllers = append([]any(nil), m.Controllers...)
	}
	if m.Exports != nil {
		c.Exports = append([]gonest.Token(nil), m.Exports...)
	}
	return c
}

func (s *shadow) copyAll(in []*gonest.Module) []*gonest.Module {
	if in == nil {
		return nil
	}
	out := make([]*gonest.Module, len(in))
	for i, m := range in {
		out[i] = s.copy(m)
	}
	return out
}

// replace returns the override for p's token, keeping the original enhancer
// kind when the replacement declares none (so Value[Guard](allowAll) can
// replace a guard registered with AppGuard).
func (s *shadow) replace(p gonest.Provider) gonest.Provider {
	repl, ok := s.providers[p.Token]
	if !ok {
		return p
	}
	s.usedTokens[p.Token] = true
	if repl.Enhancer == 0 {
		repl.Enhancer = p.Enhancer
	}
	return repl
}

// unused lists overrides that matched nothing in the graph.
func (s *shadow) unused() []string {
	var out []string
	for tok := range s.providers {
		if !s.usedTokens[tok] {
			out = append(out, fmt.Sprintf("provider %s", tok))
		}
	}
	for old := range s.modules {
		if !s.usedModules[old] {
			out = append(out, fmt.Sprintf("module %s", old))
		}
	}
	return out
}
