package module

import (
	"errors"
	"fmt"
	"strings"

	"github.com/omyo-group/gonest/internal/core"
)

// Graph is the validated module graph reachable from a root module.
//
// Edges are Module.Imports plus Module.ReExports (a re-exported module is
// implicitly imported). Modules are identified by pointer, so importing the
// same *Module twice yields one node.
type Graph struct {
	root    *core.Module
	order   []*core.Module // dependency order: imports before importers
	globals []*core.Module // global modules, in discovery order
	edges   map[*core.Module][]*core.Module
}

// Build walks the module graph from root. It rejects nil modules, empty or
// duplicate names (two different *Module sharing a Name) and import cycles,
// reporting the cycle as "a -> b -> a".
func Build(root *core.Module) (*Graph, error) {
	if root == nil {
		return nil, errors.New("gonest: root module is nil")
	}
	g := &Graph{root: root, edges: map[*core.Module][]*core.Module{}}
	b := builder{
		g:      g,
		state:  map[*core.Module]int{},
		byName: map[string]*core.Module{},
		parent: map[*core.Module]string{},
	}
	if err := b.visit(root, nil, "the application root"); err != nil {
		return nil, err
	}
	g.order = g.dependencyOrder()
	return g, nil
}

// Root returns the root module.
func (g *Graph) Root() *core.Module { return g.root }

// Modules returns every module in dependency order: a module comes after all
// the modules it imports, and global modules come as early as possible.
func (g *Graph) Modules() []*core.Module {
	out := make([]*core.Module, len(g.order))
	copy(out, g.order)
	return out
}

// Globals returns the global modules in discovery order.
func (g *Graph) Globals() []*core.Module {
	out := make([]*core.Module, len(g.globals))
	copy(out, g.globals)
	return out
}

// Imports returns the direct imports of m (Imports then ReExports, deduped).
func (g *Graph) Imports(m *core.Module) []*core.Module { return g.edges[m] }

const (
	unvisited = iota
	visiting
	done
)

type builder struct {
	g      *Graph
	state  map[*core.Module]int
	byName map[string]*core.Module
	parent map[*core.Module]string // who first imported the module, for errors
}

func (b *builder) visit(m *core.Module, stack []*core.Module, via string) error {
	switch b.state[m] {
	case done:
		return nil
	case visiting:
		return cycleError(stack, m)
	}
	if m.Name == "" {
		return fmt.Errorf("gonest: a module imported by %s has an empty Name; every module needs a unique name", via)
	}
	if other, ok := b.byName[m.Name]; ok && other != m {
		return fmt.Errorf("gonest: two different modules are named %q (one imported by %s, the other by %s); module names must be unique",
			m.Name, b.parent[other], via)
	}
	b.byName[m.Name] = m
	b.parent[m] = via
	b.state[m] = visiting
	if m.Global {
		b.g.globals = append(b.g.globals, m)
	}
	stack = append(stack, m)

	var edges []*core.Module
	seen := map[*core.Module]bool{}
	add := func(list []*core.Module, field string) error {
		for i, dep := range list {
			if dep == nil {
				return fmt.Errorf("gonest: module %q has a nil entry in %s[%d]", m.Name, field, i)
			}
			if !seen[dep] {
				seen[dep] = true
				edges = append(edges, dep)
			}
		}
		return nil
	}
	if err := add(m.Imports, "Imports"); err != nil {
		return err
	}
	if err := add(m.ReExports, "ReExports"); err != nil {
		return err
	}
	b.g.edges[m] = edges
	for _, dep := range edges {
		if err := b.visit(dep, stack, fmt.Sprintf("module %q", m.Name)); err != nil {
			return err
		}
	}
	b.state[m] = done
	return nil
}

func cycleError(stack []*core.Module, m *core.Module) error {
	start := 0
	for i, s := range stack {
		if s == m {
			start = i
			break
		}
	}
	names := make([]string, 0, len(stack)-start+1)
	for _, s := range stack[start:] {
		names = append(names, s.Name)
	}
	names = append(names, m.Name)
	return fmt.Errorf("gonest: import cycle between modules: %s", strings.Join(names, " -> "))
}

// dependencyOrder returns a post-order DFS: global modules (and their imports)
// first, then the rest from the root. Every module follows its imports.
func (g *Graph) dependencyOrder() []*core.Module {
	seen := map[*core.Module]bool{}
	var out []*core.Module
	var walk func(m *core.Module)
	walk = func(m *core.Module) {
		if seen[m] {
			return
		}
		seen[m] = true
		for _, dep := range g.edges[m] {
			walk(dep)
		}
		out = append(out, m)
	}
	for _, gm := range g.globals {
		walk(gm)
	}
	walk(g.root)
	return out
}
