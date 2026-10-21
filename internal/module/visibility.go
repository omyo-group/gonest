package module

import (
	"errors"
	"fmt"

	"github.com/omyo-group/gonest/internal/core"
)

// ownerSet is a set of provider owners.
type ownerSet map[*core.Module]struct{}

// Visibility implements core.Visibility for a Graph. A provider with token T
// owned by module O is visible from module F when:
//
//   - F == O, or O is nil (framework provider);
//   - F imports a module M (directly, or M is a global module) through which
//     O's T is exported: M == O and M exports T; or M exports T and sees O's T
//     through its own imports (re-exporting a provider); or M re-exports (via
//     ReExports, transitively) a module through which O's T is exported.
//
// Everything is precomputed by NewVisibility; Visible is two map lookups.
type Visibility struct {
	// reach[F][T] is the set of owners whose T module F sees through imports
	// and global modules (its own providers excluded).
	reach map[*core.Module]map[core.Token]ownerSet
}

// NewVisibility computes the visibility rules of g and validates every
// module's Exports: an exported token must be provided by the module itself
// or be visible to it from an imported (or global) module.
func NewVisibility(g *Graph) (*Visibility, error) {
	mods := g.order
	owned := make(map[*core.Module]map[core.Token]bool, len(mods))
	for _, m := range mods {
		set := map[core.Token]bool{}
		for _, p := range m.Providers {
			if !p.Token.IsZero() {
				set[p.Token] = true
			}
		}
		owned[m] = set
	}

	// sources[M] = modules whose exports M can see: imports + globals.
	sources := make(map[*core.Module][]*core.Module, len(mods))
	for _, m := range mods {
		src := append([]*core.Module(nil), g.edges[m]...)
		for _, gm := range g.globals {
			if gm != m && !contains(src, gm) {
				src = append(src, gm)
			}
		}
		sources[m] = src
	}

	// exp[M][T]: owners of T reachable by importing M. Computed as a fixpoint
	// because global modules can make the "sees" relation cyclic.
	exp := make(map[*core.Module]map[core.Token]ownerSet, len(mods))
	for _, m := range mods {
		e := map[core.Token]ownerSet{}
		for _, t := range m.Exports {
			if owned[m][t] {
				e[t] = ownerSet{m: {}}
			}
		}
		exp[m] = e
	}
	for changed := true; changed; {
		changed = false
		for _, m := range mods {
			for _, t := range m.Exports {
				if owned[m][t] {
					continue
				}
				for _, n := range sources[m] {
					if addAll(exp[m], t, exp[n][t]) {
						changed = true
					}
				}
			}
			for _, r := range m.ReExports {
				for t, owners := range exp[r] {
					if addAll(exp[m], t, owners) {
						changed = true
					}
				}
			}
		}
	}

	v := &Visibility{reach: make(map[*core.Module]map[core.Token]ownerSet, len(mods))}
	for _, m := range mods {
		r := map[core.Token]ownerSet{}
		for _, n := range sources[m] {
			for t, owners := range exp[n] {
				addAll(r, t, owners)
			}
		}
		v.reach[m] = r
	}

	var errs []error
	for _, m := range mods {
		for i, t := range m.Exports {
			switch {
			case t.IsZero():
				errs = append(errs, fmt.Errorf("gonest: module %q: Exports[%d] is a zero token; use gonest.TokenOf[T]()", m.Name, i))
			case owned[m][t], len(v.reach[m][t]) > 0:
			default:
				errs = append(errs, fmt.Errorf("gonest: module %q exports %s, but neither provides it nor imports a module that exports it", m.Name, t))
			}
		}
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return v, nil
}

// Visible implements core.Visibility.
func (v *Visibility) Visible(from, owner *core.Module, tok core.Token) bool {
	if from == owner || owner == nil {
		return true
	}
	_, ok := v.reach[from][tok][owner]
	return ok
}

var _ core.Visibility = (*Visibility)(nil)

func addAll(dst map[core.Token]ownerSet, t core.Token, src ownerSet) bool {
	if len(src) == 0 {
		return false
	}
	set := dst[t]
	if set == nil {
		set = ownerSet{}
		dst[t] = set
	}
	changed := false
	for o := range src {
		if _, ok := set[o]; !ok {
			set[o] = struct{}{}
			changed = true
		}
	}
	return changed
}

func contains(list []*core.Module, m *core.Module) bool {
	for _, x := range list {
		if x == m {
			return true
		}
	}
	return false
}
