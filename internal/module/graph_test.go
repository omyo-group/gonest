package module

import (
	"strings"
	"testing"

	"github.com/omyo-group/gonest/internal/core"
)

func names(ms []*core.Module) string {
	s := make([]string, len(ms))
	for i, m := range ms {
		s[i] = m.Name
	}
	return strings.Join(s, ",")
}

func TestBuildOrderAndDedupe(t *testing.T) {
	shared := &core.Module{Name: "shared"}
	a := &core.Module{Name: "a", Imports: []*core.Module{shared}}
	b := &core.Module{Name: "b", Imports: []*core.Module{shared, a}, ReExports: []*core.Module{a}}
	glob := &core.Module{Name: "glob", Global: true}
	root := &core.Module{Name: "root", Imports: []*core.Module{b, a, shared, glob}}

	g, err := Build(root)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := names(g.Modules()), "glob,shared,a,b,root"; got != want {
		t.Fatalf("order = %s, want %s", got, want)
	}
	if g.Root() != root {
		t.Fatal("root")
	}
	if got := names(g.Globals()); got != "glob" {
		t.Fatalf("globals = %s", got)
	}
	if got := names(g.Imports(b)); got != "shared,a" {
		t.Fatalf("imports(b) = %s", got)
	}
}

func TestBuildReExportIsImplicitImport(t *testing.T) {
	inner := &core.Module{Name: "inner"}
	outer := &core.Module{Name: "outer", ReExports: []*core.Module{inner}}
	g, err := Build(&core.Module{Name: "root", Imports: []*core.Module{outer}})
	if err != nil {
		t.Fatal(err)
	}
	if got := names(g.Modules()); got != "inner,outer,root" {
		t.Fatalf("order = %s", got)
	}
}

func TestBuildErrors(t *testing.T) {
	cycA := &core.Module{Name: "a"}
	cycB := &core.Module{Name: "b", Imports: []*core.Module{cycA}}
	cycA.Imports = []*core.Module{cycB}

	self := &core.Module{Name: "self"}
	self.Imports = []*core.Module{self}

	dup1 := &core.Module{Name: "dup"}
	dup2 := &core.Module{Name: "dup"}
	x := &core.Module{Name: "x", Imports: []*core.Module{dup2}}

	tests := []struct {
		name string
		root *core.Module
		want []string
	}{
		{"nil root", nil, []string{"root module is nil"}},
		{"cycle", &core.Module{Name: "root", Imports: []*core.Module{cycA}}, []string{"import cycle", "a -> b -> a"}},
		{"self cycle", self, []string{"self -> self"}},
		{"empty name", &core.Module{Name: "root", Imports: []*core.Module{{}}}, []string{"empty Name", `module "root"`}},
		{"empty root name", &core.Module{}, []string{"empty Name", "application root"}},
		{"duplicate names", &core.Module{Name: "root", Imports: []*core.Module{dup1, x}}, []string{`two different modules are named "dup"`, `module "root"`, `module "x"`}},
		{"nil import", &core.Module{Name: "root", Imports: []*core.Module{nil}}, []string{"nil entry in Imports[0]"}},
		{"nil reexport", &core.Module{Name: "root", ReExports: []*core.Module{nil}}, []string{"nil entry in ReExports[0]"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Build(tt.root)
			if err == nil {
				t.Fatal("expected error")
			}
			for _, w := range tt.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("error %q does not contain %q", err, w)
				}
			}
		})
	}
}
