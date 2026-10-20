package core

import "reflect"

// Container is the dependency injection container. internal/di implements it;
// internal/module drives it during bootstrap.
//
// Lifecycle: Register every provider, set Visibility, call Validate (which
// checks the whole graph and instantiates all singletons), then Resolve or
// Invoke freely and concurrently.
type Container interface {
	// Register adds a provider owned by a module. A nil owner makes the
	// provider visible from every module (used for framework providers such as
	// *RouteTable and *slog.Logger). Registering the same token twice in the
	// same module is an error; different modules may each register a token,
	// and visibility decides which one a module sees (its own first).
	Register(p Provider, owner *Module) error

	// SetVisibility installs the rule deciding which providers a module may
	// inject. The default allows everything.
	SetVisibility(v Visibility)

	// Validate checks every constructor's dependencies (missing providers,
	// cycles, visibility) and instantiates all singletons, so a broken graph
	// fails at startup rather than on a request. It reports all problems it
	// finds, joined with errors.Join.
	Validate() error

	// Resolve returns the instance for tok as seen from module from.
	// Errors are *ResolveError.
	Resolve(tok Token, from *Module) (any, error)

	// Invoke calls fn with its parameters injected as seen from module from
	// and returns its results. A trailing error result is returned as err.
	Invoke(fn any, from *Module) ([]reflect.Value, error)

	// Instances returns every singleton instantiated so far, in creation order
	// (dependencies before dependents).
	Instances() []Instance

	// Enhancers returns the instances of providers registered with AppGuard,
	// AppInterceptor, AppPipe or AppFilter, in registration order.
	Enhancers() []Instance
}

// Instance is a created singleton.
type Instance struct {
	Token    Token
	Value    any
	Owner    *Module
	Enhancer EnhancerKind
}

// Visibility decides whether module from may inject the provider tok owned by owner.
type Visibility interface {
	Visible(from, owner *Module, tok Token) bool
}

// VisibilityFunc adapts a function to Visibility.
type VisibilityFunc func(from, owner *Module, tok Token) bool

// Visible implements Visibility.
func (f VisibilityFunc) Visible(from, owner *Module, tok Token) bool { return f(from, owner, tok) }
