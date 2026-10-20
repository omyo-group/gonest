package gonesttest

import (
	"context"
	"io"
	"log/slog"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/omyo/gonest"
)

// Builder assembles a testing application. Create it with New.
type Builder struct {
	t         testing.TB
	root      *gonest.Module
	providers map[gonest.Token]gonest.Provider
	order     []gonest.Token
	modules   map[*gonest.Module]*gonest.Module
	opts      []gonest.Option
}

// New starts a testing module rooted at root.
func New(t testing.TB, root *gonest.Module) *Builder {
	return &Builder{
		t:         t,
		root:      root,
		providers: map[gonest.Token]gonest.Provider{},
		modules:   map[*gonest.Module]*gonest.Module{},
	}
}

// OverrideProvider replaces every provider with the same token, anywhere in
// the module graph, by p. When p does not declare a global enhancer kind it
// inherits the one of the provider it replaces. Compile fails the test if no
// provider has p's token. The caller's modules are not modified.
func (b *Builder) OverrideProvider(p gonest.Provider) *Builder {
	if _, ok := b.providers[p.Token]; !ok {
		b.order = append(b.order, p.Token)
	}
	b.providers[p.Token] = p
	return b
}

// OverrideModule replaces the module old by repl wherever old is imported or
// re-exported (or as the root). Provider overrides also apply inside repl.
// Compile fails the test if old is not reachable from the root.
func (b *Builder) OverrideModule(old, repl *gonest.Module) *Builder {
	b.modules[old] = repl
	return b
}

// WithOptions appends gonest.New options, applied after the defaults
// (WithoutStartupLog and a discarding logger).
func (b *Builder) WithOptions(opts ...gonest.Option) *Builder {
	b.opts = append(b.opts, opts...)
	return b
}

// Compile builds the application with gonest.New and fails the test on error.
// Shutdown is registered with t.Cleanup.
func (b *Builder) Compile() *App {
	b.t.Helper()
	if b.root == nil {
		b.t.Fatal("gonesttest: nil root module")
	}
	sh := newShadow(b.providers, b.modules)
	root := sh.copy(b.root)
	if un := sh.unused(); len(un) > 0 {
		sort.Strings(un)
		b.t.Fatalf("gonesttest: overrides matched nothing in the module graph: %s", strings.Join(un, ", "))
	}
	opts := append([]gonest.Option{
		gonest.WithoutStartupLog(),
		gonest.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))),
	}, b.opts...)
	inner, err := gonest.New(root, opts...)
	if err != nil {
		b.t.Fatalf("gonesttest: gonest.New: %v", err)
	}
	app := &App{App: inner, t: b.t}
	b.t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := inner.Shutdown(ctx); err != nil {
			b.t.Errorf("gonesttest: shutdown: %v", err)
		}
	})
	return app
}

// App is a compiled testing application. It embeds *gonest.App, so Handler,
// Engine, Routes and Shutdown are available.
type App struct {
	*gonest.App
	t testing.TB
}

// Get resolves a provider of type T as seen from the root module and fails the
// test on error.
func Get[T any](app *App) T {
	app.t.Helper()
	v, err := gonest.Get[T](app.App)
	if err != nil {
		app.t.Fatalf("gonesttest: Get: %v", err)
	}
	return v
}

// GetNamed resolves a named provider of type T and fails the test on error.
func GetNamed[T any](app *App, name string) T {
	app.t.Helper()
	v, err := gonest.GetNamed[T](app.App, name)
	if err != nil {
		app.t.Fatalf("gonesttest: GetNamed(%q): %v", name, err)
	}
	return v
}
