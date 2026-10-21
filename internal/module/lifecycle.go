package module

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/omyo-group/gonest/internal/core"
)

// HookTimeout bounds each lifecycle hook call.
const HookTimeout = 30 * time.Second

// Target is an instance that may implement lifecycle hooks.
type Target struct {
	// Module owns the instance; nil for framework providers.
	Module *core.Module
	// Value is the provider or controller instance.
	Value any
}

func (t Target) describe() string {
	name := "<framework>"
	if t.Module != nil {
		name = t.Module.Name
	}
	return fmt.Sprintf("%T in module %q", t.Value, name)
}

// Lifecycle runs hooks over a fixed, ordered list of targets: startup hooks in
// order, shutdown hooks in reverse order.
type Lifecycle struct {
	targets []Target
	// started is the number of leading targets that passed OnModuleInit and
	// therefore receive shutdown hooks.
	started int
	timeout time.Duration
}

// NewLifecycle orders targets by module dependency order (g.Modules()),
// keeping the given relative order within a module. Framework targets (nil
// module) come first. A pointer value present several times is kept once.
func NewLifecycle(g *Graph, targets []Target) *Lifecycle {
	byModule := map[*core.Module][]Target{}
	seen := map[any]bool{}
	for _, t := range targets {
		if t.Value == nil {
			continue
		}
		if reflect.TypeOf(t.Value).Kind() == reflect.Pointer {
			if seen[t.Value] {
				continue
			}
			seen[t.Value] = true
		}
		byModule[t.Module] = append(byModule[t.Module], t)
	}
	ordered := append([]Target(nil), byModule[nil]...)
	for _, m := range g.order {
		ordered = append(ordered, byModule[m]...)
	}
	return &Lifecycle{targets: ordered, timeout: HookTimeout}
}

// Targets returns the ordered targets.
func (l *Lifecycle) Targets() []Target {
	out := make([]Target, len(l.targets))
	copy(out, l.targets)
	return out
}

// Start runs OnModuleInit then OnApplicationBootstrap on every target. On the
// first failure it runs the shutdown hooks of the targets that completed
// OnModuleInit (in reverse order) and returns the hook error joined with any
// cleanup errors.
func (l *Lifecycle) Start(ctx context.Context) error {
	for i, t := range l.targets {
		if h, ok := t.Value.(core.OnModuleInit); ok {
			if err := l.call(ctx, h.OnModuleInit); err != nil {
				l.started = i
				return l.abort(ctx, fmt.Errorf("gonest: OnModuleInit of %s: %w", t.describe(), err))
			}
		}
	}
	l.started = len(l.targets)
	for _, t := range l.targets {
		if h, ok := t.Value.(core.OnApplicationBootstrap); ok {
			if err := l.call(ctx, h.OnApplicationBootstrap); err != nil {
				return l.abort(ctx, fmt.Errorf("gonest: OnApplicationBootstrap of %s: %w", t.describe(), err))
			}
		}
	}
	return nil
}

func (l *Lifecycle) abort(ctx context.Context, err error) error {
	if cerr := l.Stop(context.WithoutCancel(ctx)); cerr != nil {
		return errors.Join(err, fmt.Errorf("gonest: cleanup after failed startup: %w", cerr))
	}
	return err
}

// Stop runs OnModuleDestroy, BeforeApplicationShutdown and
// OnApplicationShutdown, each phase over all started targets in reverse
// order. Every hook runs even if an earlier one failed; all errors are joined.
// Calling Stop again runs nothing.
func (l *Lifecycle) Stop(ctx context.Context) error {
	n := l.started
	l.started = 0
	var errs []error
	phase := func(name string, get func(any) func(context.Context) error) {
		for i := n - 1; i >= 0; i-- {
			t := l.targets[i]
			if fn := get(t.Value); fn != nil {
				if err := l.call(ctx, fn); err != nil {
					errs = append(errs, fmt.Errorf("gonest: %s of %s: %w", name, t.describe(), err))
				}
			}
		}
	}
	phase("OnModuleDestroy", func(v any) func(context.Context) error {
		if h, ok := v.(core.OnModuleDestroy); ok {
			return h.OnModuleDestroy
		}
		return nil
	})
	phase("BeforeApplicationShutdown", func(v any) func(context.Context) error {
		if h, ok := v.(core.BeforeApplicationShutdown); ok {
			return h.BeforeApplicationShutdown
		}
		return nil
	})
	phase("OnApplicationShutdown", func(v any) func(context.Context) error {
		if h, ok := v.(core.OnApplicationShutdown); ok {
			return h.OnApplicationShutdown
		}
		return nil
	})
	return errors.Join(errs...)
}

// call runs one hook with a bounded context, turning panics into errors.
func (l *Lifecycle) call(parent context.Context, fn func(context.Context) error) (err error) {
	ctx, cancel := context.WithTimeout(parent, l.timeout)
	defer cancel()
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("panic: %v", p)
		}
	}()
	return fn(ctx)
}
