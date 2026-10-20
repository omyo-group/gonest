package di

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/omyo/gonest/internal/core"
)

// ---- fixtures ----

type DB struct{ name string }
type Repo struct{ db *DB }
type Service struct{ repo *Repo }

func NewDB() *DB                              { return &DB{name: "main"} }
func NewRepo(db *DB) *Repo                    { return &Repo{db: db} }
func NewService(r *Repo) *Service             { return &Service{repo: r} }
func NewServiceErr(r *Repo) (*Service, error) { return &Service{repo: r}, nil }

type Store interface{ Get() string }
type memStore struct{ v string }

func (m *memStore) Get() string { return m.v }

func newMemStore() *memStore { return &memStore{v: "mem"} }

type CycA struct{}
type CycB struct{}
type CycC struct{}

func newCycA(*CycB) *CycA { return &CycA{} }
func newCycB(*CycC) *CycB { return &CycB{} }
func newCycC(*CycA) *CycC { return &CycC{} }

type Report struct {
	primary, replica *DB
	cache            Store
}

type ReportDeps struct {
	core.In
	Primary *DB   `name:"primary"`
	Replica *DB   `name:"replica" optional:"true"`
	Cache   Store `optional:"true"`
}

func newReport(d ReportDeps) *Report {
	return &Report{primary: d.Primary, replica: d.Replica, cache: d.Cache}
}

type guard struct{}

func (guard) CanActivate(*core.Ctx) (bool, error) { return true, nil }

type pipe struct{}

func (pipe) Transform(_ *core.Ctx, v any, _ core.ArgMeta) (any, error) { return v, nil }

type icpt struct{}

func (icpt) Intercept(_ *core.Ctx, next core.Next) (any, error) { return next() }

type filter struct{}

func (filter) Catch(*core.Ctx, error) bool { return false }

var errBoom = errors.New("boom")

func mustRegister(t *testing.T, c *Container, owner *core.Module, ps ...core.Provider) {
	t.Helper()
	for _, p := range ps {
		if err := c.Register(p, owner); err != nil {
			t.Fatalf("Register(%s): %v", p, err)
		}
	}
}

func asResolveError(t *testing.T, err error) *core.ResolveError {
	t.Helper()
	var re *core.ResolveError
	if !errors.As(err, &re) {
		t.Fatalf("error %v (%T) is not a *core.ResolveError", err, err)
	}
	return re
}

func pathString(p []core.Token) string {
	s := make([]string, len(p))
	for i, t := range p {
		s[i] = t.String()
	}
	return strings.Join(s, " -> ")
}

// ---- tests ----

func TestRegisterErrors(t *testing.T) {
	type unexportedObj struct {
		core.In
		db *DB //nolint:unused // an unexported field must be rejected
	}
	type badOptional struct {
		core.In
		DB *DB `optional:"maybe"`
	}
	mod := &core.Module{Name: "m"}
	tests := []struct {
		name string
		p    core.Provider
		want string
	}{
		{"nil ctor", core.Provide(nil), "constructor is nil"},
		{"non func", core.Provider{Kind: core.KindConstructor, Ctor: 42, Token: core.TokenOf[int]()}, "must be a function"},
		{"no results", core.Provide(func() {}), "returns nothing"},
		{"too many results", core.Provide(func() (*DB, *Repo, error) { return nil, nil, nil }), "too many"},
		{"second not error", core.Provide(func() (*DB, *Repo) { return nil, nil }), "second result must be error"},
		{"only error", core.Provide(func() error { return nil }), "only an error"},
		{"variadic", core.Provide(func(...int) *DB { return nil }), "variadic"},
		{"bind not assignable", core.Bind[Store](NewDB), "not assignable"},
		{"ctor no token", core.Provider{Kind: core.KindConstructor, Ctor: NewDB}, "no type"},
		{"request scoped", core.Provide(NewDB, core.RequestScoped), "request scope"},
		{"unknown scope", core.Provide(NewDB, core.Scope(9)), "unknown scope"},
		{"value not assignable", core.Provider{Kind: core.KindValue, Value: 3, Token: core.TokenOf[string]()}, "not assignable"},
		{"nil value non nilable", core.Provider{Kind: core.KindValue, Token: core.TokenOf[int]()}, "non-nilable"},
		{"value no type", core.Provider{Kind: core.KindValue, Value: 1}, "no type"},
		{"unknown kind", core.Provider{Kind: core.ProviderKind(7), Token: core.TokenOf[int]()}, "unknown provider kind"},
		{"guard not implemented", core.AppGuard(NewDB), "does not implement"},
		{"unknown enhancer", core.Provider{Kind: core.KindValue, Value: 1, Token: core.TokenOf[int](), Enhancer: 99}, "unknown enhancer"},
		{"unexported field", core.Provide(func(unexportedObj) *DB { return nil }), "unexported"},
		{"bad optional tag", core.Provide(func(badOptional) *DB { return nil }), "invalid optional tag"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := New().Register(tt.p, mod)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Register error = %v, want containing %q", err, tt.want)
			}
			if !strings.Contains(err.Error(), "in module m") {
				t.Fatalf("error %q does not name the module", err)
			}
		})
	}
}

func TestRegisterDuplicate(t *testing.T) {
	a, b := &core.Module{Name: "a"}, &core.Module{Name: "b"}
	c := New()
	mustRegister(t, c, a, core.Provide(NewDB))
	if err := c.Register(core.Provide(NewDB), a); err == nil || !strings.Contains(err.Error(), "already registered") {
		t.Fatalf("duplicate in same module: err = %v", err)
	}
	if err := c.Register(core.Value(&DB{}), b); err != nil {
		t.Fatalf("same token in another module: %v", err)
	}
	mustRegister(t, c, nil, core.Value(&DB{}))
	if err := c.Register(core.Value(&DB{}), nil); err == nil {
		t.Fatal("duplicate framework provider accepted")
	}
	if err := c.Register(core.Value(&DB{}, core.Named("x")), a); err != nil {
		t.Fatalf("named provider of same type: %v", err)
	}
}

func TestResolveKinds(t *testing.T) {
	db := &DB{name: "value"}
	var nilStore Store
	tests := []struct {
		name  string
		ps    []core.Provider
		tok   core.Token
		check func(t *testing.T, v any)
	}{
		{
			name: "chain",
			ps:   []core.Provider{core.Provide(NewDB), core.Provide(NewRepo), core.Provide(NewService)},
			tok:  core.TokenOf[*Service](),
			check: func(t *testing.T, v any) {
				if s := v.(*Service); s.repo == nil || s.repo.db == nil || s.repo.db.name != "main" {
					t.Fatalf("chain not wired: %+v", s)
				}
			},
		},
		{
			name: "value",
			ps:   []core.Provider{core.Value(db), core.Provide(NewRepo)},
			tok:  core.TokenOf[*Repo](),
			check: func(t *testing.T, v any) {
				if v.(*Repo).db != db {
					t.Fatal("value not injected")
				}
			},
		},
		{
			name: "nil interface value",
			ps:   []core.Provider{core.Value(nilStore)},
			tok:  core.TokenOf[Store](),
			check: func(t *testing.T, v any) {
				if v != nil {
					t.Fatalf("got %v, want nil", v)
				}
			},
		},
		{
			name: "bind interface",
			ps:   []core.Provider{core.Bind[Store](newMemStore)},
			tok:  core.TokenOf[Store](),
			check: func(t *testing.T, v any) {
				if v.(Store).Get() != "mem" {
					t.Fatal("wrong store")
				}
			},
		},
		{
			name: "factory with error result",
			ps:   []core.Provider{core.Provide(NewDB), core.Provide(NewRepo), core.Factory[*Service](NewServiceErr)},
			tok:  core.TokenOf[*Service](),
			check: func(t *testing.T, v any) {
				if v.(*Service).repo == nil {
					t.Fatal("factory deps not injected")
				}
			},
		},
		{
			name: "named and optional",
			ps: []core.Provider{
				core.Value(&DB{name: "p"}, core.Named("primary")),
				core.Provide(newReport),
			},
			tok: core.TokenOf[*Report](),
			check: func(t *testing.T, v any) {
				r := v.(*Report)
				if r.primary == nil || r.primary.name != "p" || r.replica != nil || r.cache != nil {
					t.Fatalf("report = %+v", r)
				}
			},
		},
		{
			name: "named all present",
			ps: []core.Provider{
				core.Value(&DB{name: "p"}, core.Named("primary")),
				core.Value(&DB{name: "r"}, core.Named("replica")),
				core.Bind[Store](newMemStore),
				core.Provide(newReport),
			},
			tok: core.TokenOf[*Report](),
			check: func(t *testing.T, v any) {
				r := v.(*Report)
				if r.replica == nil || r.replica.name != "r" || r.cache == nil {
					t.Fatalf("report = %+v", r)
				}
			},
		},
		{
			name: "named resolve directly",
			ps:   []core.Provider{core.Value(&DB{name: "p"}, core.Named("primary"))},
			tok:  core.NamedTokenOf[*DB]("primary"),
			check: func(t *testing.T, v any) {
				if v.(*DB).name != "p" {
					t.Fatal("wrong db")
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := New()
			mustRegister(t, c, nil, tt.ps...)
			if err := c.Validate(); err != nil {
				t.Fatalf("Validate: %v", err)
			}
			v, err := c.Resolve(tt.tok, nil)
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			tt.check(t, v)
		})
	}
}

func TestResolveLazyWithoutValidate(t *testing.T) {
	c := New()
	mustRegister(t, c, nil, core.Provide(NewDB), core.Provide(NewRepo))
	v1, err := c.Resolve(core.TokenOf[*Repo](), nil)
	if err != nil {
		t.Fatal(err)
	}
	v2, _ := c.Resolve(core.TokenOf[*Repo](), nil)
	if v1 != v2 {
		t.Fatal("singleton resolved twice")
	}
}

func TestResolveErrors(t *testing.T) {
	users := &core.Module{Name: "users"}
	app := &core.Module{Name: "app"}
	tests := []struct {
		name       string
		ps         []core.Provider
		owner      *core.Module
		tok        core.Token
		from       *core.Module
		wantReason string
		wantPath   string
		wantHint   string
		wantIs     error
		graphOK    bool // the graph itself is valid; only the request fails
	}{
		{
			name:       "missing dependency",
			ps:         []core.Provider{core.Provide(NewRepo), core.Provide(NewService)},
			tok:        core.TokenOf[*Service](),
			wantReason: "no provider",
			wantPath:   "*di.Service -> *di.Repo -> *di.DB",
			wantHint:   "register a provider for *di.DB",
		},
		{
			name:       "missing with named alternatives",
			ps:         []core.Provider{core.Value(&DB{}, core.Named("a")), core.Provide(NewRepo)},
			tok:        core.TokenOf[*Repo](),
			wantReason: "no provider",
			wantPath:   "*di.Repo -> *di.DB",
			wantHint:   "registered with other names: *di.DB[a]; inject a named provider",
		},
		{
			name:       "missing named with default available",
			ps:         []core.Provider{core.Value(&DB{})},
			tok:        core.NamedTokenOf[*DB]("x"),
			wantReason: "no provider",
			wantHint:   "registered with other names: *di.DB",
			graphOK:    true,
		},
		{
			name:       "pointer mismatch",
			ps:         []core.Provider{core.Value(DB{})},
			tok:        core.TokenOf[*DB](),
			wantReason: "no provider",
			wantHint:   "did you mean di.DB?",
			graphOK:    true,
		},
		{
			name:       "interface not bound",
			ps:         []core.Provider{core.Provide(newMemStore)},
			tok:        core.TokenOf[Store](),
			wantReason: "no provider",
			wantHint:   "*di.memStore implements di.Store; register it with Bind[di.Store](ctor)",
			graphOK:    true,
		},
		{
			name:       "cycle",
			ps:         []core.Provider{core.Provide(newCycA), core.Provide(newCycB), core.Provide(newCycC)},
			tok:        core.TokenOf[*CycA](),
			wantReason: "cycle",
			wantPath:   "*di.CycA -> *di.CycB -> *di.CycC -> *di.CycA",
		},
		{
			name:       "self cycle transient",
			ps:         []core.Provider{core.Provide(func(*CycA) *CycA { return nil }, core.Transient)},
			tok:        core.TokenOf[*CycA](),
			wantReason: "cycle",
			wantPath:   "*di.CycA -> *di.CycA",
		},
		{
			name: "constructor error wrapped",
			ps: []core.Provider{core.Provide(NewDB), core.Provide(func(*DB) (*Repo, error) {
				return nil, errBoom
			})},
			tok:        core.TokenOf[*Repo](),
			wantReason: "constructor failed",
			wantPath:   "*di.Repo",
			wantIs:     errBoom,
		},
		{
			name:       "constructor panic",
			ps:         []core.Provider{core.Provide(func() *DB { panic("kaput") })},
			tok:        core.TokenOf[*DB](),
			wantReason: "constructor failed",
		},
		{
			name:       "not visible",
			ps:         []core.Provider{core.Provide(NewDB)},
			owner:      users,
			tok:        core.TokenOf[*DB](),
			from:       app,
			wantReason: "not visible from module app",
			wantHint:   "add it to the Exports of users and import users in module app",
			graphOK:    true,
		},
		{
			name:       "not visible from root",
			ps:         []core.Provider{core.Provide(NewDB)},
			owner:      users,
			tok:        core.TokenOf[*DB](),
			wantReason: "not visible from the application root",
			wantHint:   "import that module in the root module",
			graphOK:    true,
		},
		{
			name: "optional field with failing provider propagates",
			ps: []core.Provider{
				core.Value(&DB{}, core.Named("primary")),
				core.Bind[Store](func() (*memStore, error) { return nil, errBoom }),
				core.Provide(newReport),
			},
			tok:        core.TokenOf[*Report](),
			wantReason: "constructor failed",
			wantPath:   "*di.Report -> di.Store",
			wantIs:     errBoom,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := New()
			if tt.owner != nil {
				c.SetVisibility(core.VisibilityFunc(func(from, owner *core.Module, _ core.Token) bool { return from == owner }))
			}
			mustRegister(t, c, tt.owner, tt.ps...)
			_, err := c.Resolve(tt.tok, tt.from)
			re := asResolveError(t, err)
			if re.Reason != tt.wantReason {
				t.Errorf("Reason = %q, want %q (%v)", re.Reason, tt.wantReason, err)
			}
			if tt.wantPath != "" && pathString(re.Path) != tt.wantPath {
				t.Errorf("Path = %q, want %q", pathString(re.Path), tt.wantPath)
			}
			if !strings.Contains(re.Hint, tt.wantHint) {
				t.Errorf("Hint = %q, want containing %q", re.Hint, tt.wantHint)
			}
			if tt.wantIs != nil && !errors.Is(err, tt.wantIs) {
				t.Errorf("errors.Is(%v, %v) = false", err, tt.wantIs)
			}
			if err.Error() == "" || !strings.HasPrefix(err.Error(), "gonest: cannot resolve") {
				t.Errorf("Error() = %q", err.Error())
			}
			if err := c.Validate(); (err == nil) != tt.graphOK {
				t.Errorf("Validate = %v, graphOK = %v", err, tt.graphOK)
			}
		})
	}
}

func TestResolveNilToken(t *testing.T) {
	_, err := New().Resolve(core.Token{}, nil)
	if re := asResolveError(t, err); re.Reason != "no provider" {
		t.Fatalf("Reason = %q", re.Reason)
	}
}

func TestVisibility(t *testing.T) {
	db := &core.Module{Name: "db"}
	users := &core.Module{Name: "users"}
	orders := &core.Module{Name: "orders"}
	other := &core.Module{Name: "other"}
	exported := map[*core.Module]map[*core.Module]bool{
		users:  {db: true},
		orders: {db: true, users: true},
	}
	vis := core.VisibilityFunc(func(from, owner *core.Module, _ core.Token) bool {
		return exported[from][owner]
	})

	c := New()
	c.SetVisibility(vis)
	mustRegister(t, c, db, core.Provide(NewDB))
	mustRegister(t, c, users, core.Provide(NewRepo))
	mustRegister(t, c, orders, core.Provide(NewService))
	if err := c.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	// orders sees its own *Service and the exported *Repo of users.
	own, err := c.Resolve(core.TokenOf[*Repo](), users)
	if err != nil {
		t.Fatal(err)
	}
	svc, err := c.Resolve(core.TokenOf[*Service](), orders)
	if err != nil {
		t.Fatal(err)
	}
	if svc.(*Service).repo != own {
		t.Fatal("orders did not get the repo of users")
	}
	// Dependencies resolve as seen from the provider's owner: other cannot see db.
	c2 := New()
	c2.SetVisibility(vis)
	mustRegister(t, c2, db, core.Provide(NewDB))
	mustRegister(t, c2, other, core.Provide(NewRepo))
	_, err = c2.Resolve(core.TokenOf[*Repo](), other)
	re := asResolveError(t, err)
	if re.Reason != "not visible from module other" || re.Module != "other" {
		t.Fatalf("Reason = %q Module = %q", re.Reason, re.Module)
	}
	if pathString(re.Path) != "*di.Repo -> *di.DB" {
		t.Fatalf("Path = %s", pathString(re.Path))
	}
	// Changing visibility invalidates selections.
	c2.SetVisibility(nil)
	if _, err := c2.Resolve(core.TokenOf[*Repo](), other); err != nil {
		t.Fatalf("after SetVisibility(nil): %v", err)
	}
}

func TestVisibilityValidateFailsForHiddenDependency(t *testing.T) {
	db := &core.Module{Name: "db"}
	other := &core.Module{Name: "other"}
	c := New()
	c.SetVisibility(core.VisibilityFunc(func(from, owner *core.Module, _ core.Token) bool { return from == owner }))
	mustRegister(t, c, db, core.Provide(NewDB))
	mustRegister(t, c, other, core.Provide(NewRepo))
	err := c.Validate()
	if err == nil || !strings.Contains(err.Error(), "not visible from module other") {
		t.Fatalf("Validate = %v", err)
	}
}

func TestAmbiguity(t *testing.T) {
	a, b, app := &core.Module{Name: "a"}, &core.Module{Name: "b"}, &core.Module{Name: "app"}
	c := New()
	mustRegister(t, c, a, core.Provide(NewDB))
	mustRegister(t, c, b, core.Provide(NewDB))
	_, err := c.Resolve(core.TokenOf[*DB](), app)
	re := asResolveError(t, err)
	if re.Reason != "ambiguous" || !strings.Contains(re.Hint, "modules a, b") {
		t.Fatalf("got %v", err)
	}
	// The owner always wins over other visible providers.
	if _, err := c.Resolve(core.TokenOf[*DB](), a); err != nil {
		t.Fatal(err)
	}
}

func TestFrameworkProviders(t *testing.T) {
	mod := &core.Module{Name: "m"}
	app := &core.Module{Name: "app"}
	fw := &DB{name: "framework"}
	c := New()
	c.SetVisibility(core.VisibilityFunc(func(from, owner *core.Module, _ core.Token) bool { return false }))
	mustRegister(t, c, nil, core.Value(fw))
	mustRegister(t, c, mod, core.Provide(NewRepo))
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	r, err := c.Resolve(core.TokenOf[*Repo](), mod)
	if err != nil || r.(*Repo).db != fw {
		t.Fatalf("framework provider not injected: %v", err)
	}
	// A visible module provider is preferred over the framework one.
	c2 := New()
	own := &DB{name: "module"}
	mustRegister(t, c2, nil, core.Value(fw))
	mustRegister(t, c2, mod, core.Value(own))
	v, err := c2.Resolve(core.TokenOf[*DB](), app)
	if err != nil || v != own {
		t.Fatalf("got %v, %v; want module provider", v, err)
	}
	// From nil, the framework provider is the owner match.
	v, _ = c2.Resolve(core.TokenOf[*DB](), nil)
	if v != fw {
		t.Fatal("nil module did not get the framework provider")
	}
}

func TestScopes(t *testing.T) {
	var built atomic.Int32
	newCounted := func() *DB { built.Add(1); return &DB{} }
	tests := []struct {
		name      string
		scope     core.Scope
		wantSame  bool
		wantBuilt int32 // after Validate + 2 resolves
	}{
		{"singleton", core.Singleton, true, 1},
		{"transient", core.Transient, false, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			built.Store(0)
			c := New()
			mustRegister(t, c, nil, core.Provide(newCounted, tt.scope))
			if err := c.Validate(); err != nil {
				t.Fatal(err)
			}
			a, _ := c.Resolve(core.TokenOf[*DB](), nil)
			b, _ := c.Resolve(core.TokenOf[*DB](), nil)
			if (a == b) != tt.wantSame {
				t.Fatalf("same instance = %v, want %v", a == b, tt.wantSame)
			}
			if got := built.Load(); got != tt.wantBuilt {
				t.Fatalf("constructor calls = %d, want %d", got, tt.wantBuilt)
			}
			insts := c.Instances()
			if tt.scope == core.Transient && len(insts) != 0 {
				t.Fatalf("transient tracked in Instances: %v", insts)
			}
		})
	}
}

func TestTransientDependents(t *testing.T) {
	c := New()
	mustRegister(t, c, nil,
		core.Provide(NewDB, core.Transient),
		core.Provide(NewRepo, core.Transient),
		core.Provide(NewService),
	)
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	r1, _ := c.Resolve(core.TokenOf[*Repo](), nil)
	r2, _ := c.Resolve(core.TokenOf[*Repo](), nil)
	if r1 == r2 || r1.(*Repo).db == r2.(*Repo).db {
		t.Fatal("transient dependencies shared")
	}
	s, _ := c.Resolve(core.TokenOf[*Service](), nil)
	if s.(*Service).repo == nil || s.(*Service).repo.db == nil {
		t.Fatal("singleton with transient deps not wired")
	}
}

func TestConstructorRunsOnce(t *testing.T) {
	var calls int
	c := New()
	mustRegister(t, c, nil, core.Provide(func() (*DB, error) { calls++; return nil, errBoom }), core.Provide(NewRepo))
	err := c.Validate()
	if !errors.Is(err, errBoom) {
		t.Fatalf("Validate = %v", err)
	}
	if strings.Count(err.Error(), "boom") != 1 {
		t.Fatalf("problem reported more than once: %v", err)
	}
	_, err = c.Resolve(core.TokenOf[*Repo](), nil)
	if !errors.Is(err, errBoom) {
		t.Fatalf("Resolve = %v", err)
	}
	if calls != 1 {
		t.Fatalf("constructor called %d times", calls)
	}
}

func TestValidateCollectsDistinctProblems(t *testing.T) {
	c := New()
	mustRegister(t, c, nil,
		core.Provide(NewRepo),    // missing *DB
		core.Provide(NewService), // missing *DB through *Repo: same root cause
		core.Provide(newCycA), core.Provide(newCycB), core.Provide(newCycC),
		core.Provide(func() (Store, error) { return nil, errBoom }),
	)
	err := c.Validate()
	if err == nil {
		t.Fatal("Validate passed")
	}
	type unwrapper interface{ Unwrap() []error }
	var u unwrapper
	if !errors.As(err, &u) {
		t.Fatalf("not a joined error: %T", err)
	}
	if n := len(u.Unwrap()); n != 3 {
		t.Fatalf("got %d problems, want 3:\n%v", n, err)
	}
}

func TestValidateDoesNotCallTransientConstructors(t *testing.T) {
	var calls int
	c := New()
	mustRegister(t, c, nil,
		core.Provide(NewDB),
		core.Provide(func(*DB) *Repo { calls++; return &Repo{} }, core.Transient),
		core.Provide(func(*Repo) *CycA { calls++; return &CycA{} }, core.Transient),
	)
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatalf("transient constructors called %d times during Validate", calls)
	}
	if len(c.Instances()) != 1 {
		t.Fatal("singleton dependency of transient not built")
	}
	// A transient whose dependency is missing still fails validation.
	c2 := New()
	mustRegister(t, c2, nil, core.Provide(NewRepo, core.Transient))
	if err := c2.Validate(); err == nil {
		t.Fatal("missing dependency of transient not reported")
	}
}

func TestInvoke(t *testing.T) {
	mod := &core.Module{Name: "m"}
	tests := []struct {
		name    string
		fn      any
		wantN   int
		wantErr string
		wantIs  error
		check   func(t *testing.T, out []reflect.Value)
	}{
		{
			name:  "results",
			fn:    func(s *Service, d *DB) (*Service, int) { return s, len(d.name) },
			wantN: 2,
			check: func(t *testing.T, out []reflect.Value) {
				if out[1].Int() != 4 || out[0].Interface().(*Service) == nil {
					t.Fatal("bad results")
				}
			},
		},
		{name: "no params no results", fn: func() {}, wantN: 0},
		{name: "trailing nil error", fn: func(*DB) (*CycA, error) { return &CycA{}, nil }, wantN: 1},
		{name: "trailing error", fn: func(*DB) (*CycA, error) { return &CycA{}, errBoom }, wantN: 1, wantIs: errBoom},
		{name: "only error", fn: func() error { return errBoom }, wantN: 0, wantIs: errBoom},
		{
			name: "parameter object",
			fn: func(d ReportDeps) string {
				return d.Primary.name
			},
			wantErr: "no provider",
		},
		{name: "missing", fn: func(*CycB) *CycA { return nil }, wantErr: "*di.CycA -> *di.CycB in module m: no provider"},
		{name: "nil", fn: nil, wantErr: "nil function"},
		{name: "not a func", fn: 3, wantErr: "not a function"},
		{name: "variadic", fn: func(...int) {}, wantErr: "variadic"},
		{name: "panic", fn: func() { panic("oops") }, wantErr: "panic: oops"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := New()
			mustRegister(t, c, mod, core.Provide(NewDB), core.Provide(NewRepo), core.Provide(NewService))
			out, err := c.Invoke(tt.fn, mod)
			switch {
			case tt.wantErr != "":
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
				}
				return
			case tt.wantIs != nil:
				if !errors.Is(err, tt.wantIs) {
					t.Fatalf("err = %v, want %v", err, tt.wantIs)
				}
			case err != nil:
				t.Fatal(err)
			}
			if len(out) != tt.wantN {
				t.Fatalf("got %d results, want %d", len(out), tt.wantN)
			}
			if tt.check != nil {
				tt.check(t, out)
			}
		})
	}
}

func TestInvokeCanUseContainer(t *testing.T) {
	c := New()
	mustRegister(t, c, nil, core.Provide(NewDB), core.Value(c))
	out, err := c.Invoke(func(cc *Container) (any, error) {
		return cc.Resolve(core.TokenOf[*DB](), nil)
	}, nil)
	if err != nil || out[0].Interface() == nil {
		t.Fatalf("re-entrant Invoke: %v", err)
	}
}

func TestInstancesOrder(t *testing.T) {
	mod := &core.Module{Name: "m"}
	c := New()
	// Register dependents first so creation order differs from registration order.
	mustRegister(t, c, mod,
		core.Provide(NewService),
		core.Provide(NewRepo),
		core.Provide(NewDB),
		core.AppGuard(func(*DB) guard { return guard{} }),
	)
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, in := range c.Instances() {
		if in.Owner != mod {
			t.Fatalf("owner = %v", in.Owner)
		}
		got = append(got, fmt.Sprintf("%s:%d", in.Token, in.Enhancer))
	}
	want := "*di.DB:0 *di.Repo:0 *di.Service:0 di.guard:1"
	if strings.Join(got, " ") != want {
		t.Fatalf("Instances = %v, want %s", got, want)
	}
}

func TestEnhancers(t *testing.T) {
	mod := &core.Module{Name: "m"}
	var guardBuilt int
	c := New()
	mustRegister(t, c, mod,
		core.AppFilter(func() filter { return filter{} }),
		core.Provide(NewDB),
		core.AppGuard(func(*DB) guard { guardBuilt++; return guard{} }),
		core.AppPipe(func() pipe { return pipe{} }, core.Named("p")),
		core.AppInterceptor(func() (icpt, error) { return icpt{}, nil }),
		core.AppGuard(func() (*CycA, error) { return nil, errBoom }, core.Named("broken")),
	)
	_ = c.Register(core.AppGuard(func() core.Guard { return guard{} }), mod) // interface token is accepted
	ens := c.Enhancers()
	kinds := make([]core.EnhancerKind, len(ens))
	for i, e := range ens {
		kinds[i] = e.Enhancer
		if e.Value == nil || e.Owner != mod {
			t.Fatalf("enhancer %d = %+v", i, e)
		}
	}
	want := []core.EnhancerKind{core.EnhancerFilter, core.EnhancerGuard, core.EnhancerPipe, core.EnhancerInterceptor, core.EnhancerGuard}
	if !reflect.DeepEqual(kinds, want) {
		t.Fatalf("kinds = %v, want %v", kinds, want)
	}
	_ = c.Enhancers()
	if guardBuilt != 1 {
		t.Fatalf("singleton guard built %d times", guardBuilt)
	}
}

func (*CycA) CanActivate(*core.Ctx) (bool, error) { return true, nil }

func TestRegisterAfterValidate(t *testing.T) {
	a, app := &core.Module{Name: "a"}, &core.Module{Name: "app"}
	c := New()
	mustRegister(t, c, a, core.Provide(NewDB))
	if _, err := c.Resolve(core.TokenOf[*DB](), app); err != nil {
		t.Fatal(err)
	}
	// A second provider makes the selection ambiguous: the cache must not hide it.
	mustRegister(t, c, &core.Module{Name: "b"}, core.Provide(NewDB))
	if _, err := c.Resolve(core.TokenOf[*DB](), app); err == nil {
		t.Fatal("stale selection served after Register")
	}
}

func TestConcurrentResolve(t *testing.T) {
	c := New()
	mustRegister(t, c, nil,
		core.Provide(NewDB), core.Provide(NewRepo), core.Provide(NewService),
		core.Provide(func(*DB) *CycA { return &CycA{} }, core.Transient),
	)
	// Half the goroutines race on first construction (no Validate).
	var wg sync.WaitGroup
	results := make([]any, 32)
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			v, err := c.Resolve(core.TokenOf[*Service](), nil)
			if err != nil {
				t.Error(err)
			}
			results[i] = v
			if _, err := c.Resolve(core.TokenOf[*CycA](), nil); err != nil {
				t.Error(err)
			}
			_ = c.Instances()
		}(i)
	}
	wg.Wait()
	for _, r := range results {
		if r != results[0] {
			t.Fatal("singleton built more than once under concurrency")
		}
	}
	if n := len(c.Instances()); n != 3 {
		t.Fatalf("Instances = %d, want 3", n)
	}
}

func BenchmarkResolveSingleton(b *testing.B) {
	c := New()
	mod := &core.Module{Name: "m"}
	for _, p := range []core.Provider{core.Provide(NewDB), core.Provide(NewRepo), core.Provide(NewService)} {
		if err := c.Register(p, mod); err != nil {
			b.Fatal(err)
		}
	}
	if err := c.Validate(); err != nil {
		b.Fatal(err)
	}
	tok := core.TokenOf[*Service]()
	if _, err := c.Resolve(tok, mod); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := c.Resolve(tok, mod); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkResolveSingletonParallel(b *testing.B) {
	c := New()
	if err := c.Register(core.Provide(NewDB), nil); err != nil {
		b.Fatal(err)
	}
	tok := core.TokenOf[*DB]()
	if _, err := c.Resolve(tok, nil); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if _, err := c.Resolve(tok, nil); err != nil {
				b.Error(err)
				return
			}
		}
	})
}

func BenchmarkResolveTransient(b *testing.B) {
	c := New()
	for _, p := range []core.Provider{core.Provide(NewDB), core.Provide(NewRepo, core.Transient)} {
		if err := c.Register(p, nil); err != nil {
			b.Fatal(err)
		}
	}
	tok := core.TokenOf[*Repo]()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := c.Resolve(tok, nil); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkInvoke(b *testing.B) {
	c := New()
	for _, p := range []core.Provider{core.Provide(NewDB), core.Provide(NewRepo)} {
		if err := c.Register(p, nil); err != nil {
			b.Fatal(err)
		}
	}
	fn := func(r *Repo, d *DB) *Service { return &Service{repo: r} }
	b.ReportAllocs()
	for b.Loop() {
		if _, err := c.Invoke(fn, nil); err != nil {
			b.Fatal(err)
		}
	}
}
