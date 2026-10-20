package module

import (
	"strings"
	"testing"

	"github.com/omyo/gonest/internal/core"
)

type (
	svcA   struct{}
	svcB   struct{}
	svcC   struct{}
	svcG   struct{}
	svcP   struct{}
	svcFw  struct{}
	svcMid struct{}
)

func TestVisibilityMatrix(t *testing.T) {
	tokA := core.TokenOf[*svcA]()
	tokAPriv := core.NamedTokenOf[*svcA]("private")
	tokB := core.TokenOf[*svcB]()
	tokC := core.TokenOf[*svcC]()
	tokG := core.TokenOf[*svcG]()
	tokP := core.TokenOf[*svcP]()
	tokMid := core.TokenOf[*svcMid]()

	// a provides A (exported) and A[private] (not exported).
	a := &core.Module{Name: "a",
		Providers: []core.Provider{core.Value(&svcA{}), core.Value(&svcA{}, core.Named("private"))},
		Exports:   []core.Token{tokA}}
	// b re-exports module a (chain b -> a) and provides B (exported).
	b := &core.Module{Name: "b", Imports: []*core.Module{a}, ReExports: []*core.Module{a},
		Providers: []core.Provider{core.Value(&svcB{})}, Exports: []core.Token{tokB}}
	// c re-exports b (chain c -> b -> a).
	c := &core.Module{Name: "c", ReExports: []*core.Module{b},
		Providers: []core.Provider{core.Value(&svcC{})}}
	// p provides P, exported.
	p := &core.Module{Name: "p", Providers: []core.Provider{core.Value(&svcP{})}, Exports: []core.Token{tokP}}
	// mid imports p and re-exports P as a provider token (not the module).
	mid := &core.Module{Name: "mid", Imports: []*core.Module{p}, Exports: []core.Token{tokP, tokMid},
		Providers: []core.Provider{core.Value(&svcMid{})}}
	// glob is global and exports G.
	glob := &core.Module{Name: "glob", Global: true, Providers: []core.Provider{core.Value(&svcG{})}, Exports: []core.Token{tokG}}
	// d imports a directly.
	d := &core.Module{Name: "d", Imports: []*core.Module{a}}
	root := &core.Module{Name: "root", Imports: []*core.Module{c, d, mid, glob}}

	g, err := Build(root)
	if err != nil {
		t.Fatal(err)
	}
	v, err := NewVisibility(g)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name        string
		from, owner *core.Module
		tok         core.Token
		want        bool
	}{
		{"own provider", a, a, tokAPriv, true},
		{"framework provider", d, nil, core.TokenOf[*svcFw](), true},
		{"imported and exported", d, a, tokA, true},
		{"imported but not exported", d, a, tokAPriv, false},
		{"not imported", d, b, tokB, false},
		{"re-export one level", c, a, tokA, true},
		{"re-export chain root->c->b->a", root, a, tokA, true},
		{"re-export chain exported provider of b", root, b, tokB, true},
		{"re-exporter's own unexported provider", root, c, tokC, false},
		{"re-export chain does not leak private", root, a, tokAPriv, false},
		{"global exported", d, glob, tokG, true},
		{"global exported seen by itself", glob, glob, tokG, true},
		{"global exported seen from a module that does not import it", a, glob, tokG, true},
		{"global not providing token", d, glob, tokA, false},
		{"provider re-exported as token", root, p, tokP, true},
		{"provider token re-export only from importer", d, p, tokP, false},
		{"mid sees p", mid, p, tokP, true},
		{"mid own exported", root, mid, tokMid, true},
		{"importer does not see importer", a, d, tokA, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := v.Visible(tt.from, tt.owner, tt.tok); got != tt.want {
				t.Fatalf("Visible(%s, %s, %s) = %v, want %v", tt.from, tt.owner, tt.tok, got, tt.want)
			}
		})
	}
}

func TestVisibilityGlobalReExports(t *testing.T) {
	tokA := core.TokenOf[*svcA]()
	a := &core.Module{Name: "a", Providers: []core.Provider{core.Value(&svcA{})}, Exports: []core.Token{tokA}}
	glob := &core.Module{Name: "glob", Global: true, Imports: []*core.Module{a}, ReExports: []*core.Module{a}}
	other := &core.Module{Name: "other"}
	g, err := Build(&core.Module{Name: "root", Imports: []*core.Module{glob, other}})
	if err != nil {
		t.Fatal(err)
	}
	v, err := NewVisibility(g)
	if err != nil {
		t.Fatal(err)
	}
	if !v.Visible(other, a, tokA) {
		t.Fatal("a module re-exported by a global module should be visible everywhere")
	}
}

func TestVisibilityGlobalCycle(t *testing.T) {
	// Two global modules re-exporting each other's provider tokens: the fixpoint
	// must terminate.
	tokA := core.TokenOf[*svcA]()
	tokB := core.TokenOf[*svcB]()
	ga := &core.Module{Name: "ga", Global: true, Providers: []core.Provider{core.Value(&svcA{})}, Exports: []core.Token{tokA, tokB}}
	gb := &core.Module{Name: "gb", Global: true, Providers: []core.Provider{core.Value(&svcB{})}, Exports: []core.Token{tokB, tokA}}
	user := &core.Module{Name: "user"}
	g, err := Build(&core.Module{Name: "root", Imports: []*core.Module{ga, gb, user}})
	if err != nil {
		t.Fatal(err)
	}
	v, err := NewVisibility(g)
	if err != nil {
		t.Fatal(err)
	}
	if !v.Visible(user, gb, tokB) || !v.Visible(user, ga, tokA) {
		t.Fatal("global exports should be visible")
	}
}

func TestExportsValidation(t *testing.T) {
	tokA := core.TokenOf[*svcA]()
	tests := []struct {
		name string
		root *core.Module
		want string
	}{
		{"exports unknown token", &core.Module{Name: "root", Exports: []core.Token{tokA}},
			`module "root" exports *module.svcA, but neither provides it nor imports a module that exports it`},
		{"exports unexported token of import", &core.Module{Name: "root",
			Imports: []*core.Module{{Name: "a", Providers: []core.Provider{core.Value(&svcA{})}}},
			Exports: []core.Token{tokA}}, `module "root" exports *module.svcA`},
		{"zero token", &core.Module{Name: "root", Exports: []core.Token{{}}}, "Exports[0] is a zero token"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g, err := Build(tt.root)
			if err != nil {
				t.Fatal(err)
			}
			_, err = NewVisibility(g)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
		})
	}
}
