package guide_test

import (
	"fmt"
	"time"

	"github.com/omyo-group/gonest"
)

// docs:dynamic-module

// CacheOptions configure CacheModule.
type CacheOptions struct{ TTL time.Duration }

// Cache is the provider exported by CacheModule.
type Cache struct{ TTL time.Duration }

// CacheModule is a dynamic module (NestJS CacheModule.register()): a
// function returning a *gonest.Module. Call it once and share the pointer:
// two calls produce two distinct modules, which must then have different
// names.
func CacheModule(opts CacheOptions) *gonest.Module {
	return &gonest.Module{
		Name:      "cache",
		Providers: []gonest.Provider{gonest.Provide(func() *Cache { return &Cache{TTL: opts.TTL} })},
		Exports:   []gonest.Token{gonest.TokenOf[*Cache]()},
	}
}

// docs:end

// docs:reexports

// Repo is provided by DatabaseModule.
type Repo struct{}

// DatabaseModule exports *Repo.
var DatabaseModule = &gonest.Module{
	Name:      "database",
	Providers: []gonest.Provider{gonest.Provide(func() *Repo { return &Repo{} })},
	Exports:   []gonest.Token{gonest.TokenOf[*Repo]()},
}

// CoreModule re-exports DatabaseModule (NestJS: exports: [DatabaseModule]),
// so whoever imports CoreModule can inject *Repo too.
var CoreModule = &gonest.Module{
	Name:      "core",
	Imports:   []*gonest.Module{DatabaseModule},
	ReExports: []*gonest.Module{DatabaseModule},
}

// docs:end

// docs:global-module

// Clocks is provided by a global module.
type Clocks struct{}

// ClockModule is global: its exports are injectable everywhere once the
// module is reachable from the root (imported once, usually by the root).
var ClockModule = &gonest.Module{
	Name:      "clock",
	Global:    true,
	Providers: []gonest.Provider{gonest.Provide(func() *Clocks { return &Clocks{} })},
	Exports:   []gonest.Token{gonest.TokenOf[*Clocks]()},
}

// docs:end

// Feature uses everything above.
type Feature struct {
	Repo   *Repo
	Cache  *Cache
	Clocks *Clocks
}

func Example_modules() {
	// docs:modules-wiring
	featureModule := &gonest.Module{
		Name:    "feature",
		Imports: []*gonest.Module{CoreModule, CacheModule(CacheOptions{TTL: time.Minute})},
		Providers: []gonest.Provider{gonest.Provide(func(r *Repo, c *Cache, k *Clocks) *Feature {
			return &Feature{Repo: r, Cache: c, Clocks: k}
		})},
		Exports: []gonest.Token{gonest.TokenOf[*Feature]()},
	}
	root := &gonest.Module{Name: "app", Imports: []*gonest.Module{ClockModule, featureModule}}
	// docs:end

	app, err := gonest.New(root, quiet()...)
	if err != nil {
		fmt.Println(err)
		return
	}
	f := gonest.MustGet[*Feature](app)
	fmt.Println(f.Cache.TTL, f.Repo != nil, f.Clocks != nil)
	// Output: 1m0s true true
}

func Example_visibility() {
	// docs:visibility-error
	type Secret struct{}
	private := &gonest.Module{
		Name:      "private",
		Providers: []gonest.Provider{gonest.Provide(func() *Secret { return &Secret{} })},
		// no Exports
	}
	type User struct{}
	consumer := &gonest.Module{
		Name:      "consumer",
		Imports:   []*gonest.Module{private},
		Providers: []gonest.Provider{gonest.Provide(func(*Secret) *User { return &User{} })},
	}
	_, err := gonest.New(&gonest.Module{Name: "app", Imports: []*gonest.Module{consumer}}, quiet()...)
	fmt.Println(err)
	// docs:end

	// Output:
	// gonest: cannot resolve *guide_test.User -> *guide_test.Secret in module consumer: not visible from module consumer (hint: *guide_test.Secret is provided by module private; add it to the Exports of private and import private in module consumer)
}

func Example_importCycle() {
	a := &gonest.Module{Name: "a"}
	b := &gonest.Module{Name: "b", Imports: []*gonest.Module{a}}
	a.Imports = []*gonest.Module{b}
	_, err := gonest.New(&gonest.Module{Name: "app", Imports: []*gonest.Module{a}}, quiet()...)
	fmt.Println(err)
	// Output:
	// gonest: import cycle between modules: a -> b -> a
}
