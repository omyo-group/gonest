package core

import (
	"fmt"
	"reflect"
)

// Scope controls how often a provider is instantiated.
type Scope int

const (
	// Singleton providers are created once per application (the default).
	Singleton Scope = iota
	// Transient providers are created anew for every injection.
	Transient
	// RequestScoped providers are created once per HTTP request.
	// Reserved: not supported in v0.1; registering one returns an error.
	RequestScoped
)

// String returns the scope name.
func (s Scope) String() string {
	switch s {
	case Singleton:
		return "singleton"
	case Transient:
		return "transient"
	case RequestScoped:
		return "request"
	default:
		return fmt.Sprintf("scope(%d)", int(s))
	}
}

func (s Scope) applyProvider(p *Provider) { p.Scope = s }

// ProviderOption customises a Provider. Scopes (Singleton, Transient) and
// Named(...) are ProviderOptions.
type ProviderOption interface {
	applyProvider(*Provider)
}

type providerOptionFunc func(*Provider)

func (f providerOptionFunc) applyProvider(p *Provider) { f(p) }

// Named registers the provider under a name, so several providers of the same
// type can coexist. Inject a named provider through a parameter object (see In).
func Named(name string) ProviderOption {
	return providerOptionFunc(func(p *Provider) { p.Token.Name = name })
}

// ProviderKind tells how a provider produces its instance.
type ProviderKind int

const (
	// KindConstructor providers call Ctor with injected parameters.
	KindConstructor ProviderKind = iota
	// KindValue providers return Value as is.
	KindValue
)

// EnhancerKind marks a provider whose instance is also registered as a global
// pipeline component, the equivalent of NestJS APP_GUARD, APP_INTERCEPTOR,
// APP_PIPE and APP_FILTER.
type EnhancerKind int

const (
	// EnhancerNone is an ordinary provider.
	EnhancerNone EnhancerKind = iota
	// EnhancerGuard instances must implement Guard.
	EnhancerGuard
	// EnhancerInterceptor instances must implement Interceptor.
	EnhancerInterceptor
	// EnhancerPipe instances must implement Pipe.
	EnhancerPipe
	// EnhancerFilter instances must implement ExceptionFilter.
	EnhancerFilter
)

// Provider describes how the container builds one injectable value.
// Build providers with Provide, Value, Bind, Factory or the App* helpers rather
// than by filling the struct.
type Provider struct {
	// Token is the key under which the value is registered.
	Token Token
	// Kind says whether Ctor or Value is used.
	Kind ProviderKind
	// Ctor is a function returning (T) or (T, error). Its parameters are injected
	// by type; a struct parameter embedding In is a parameter object.
	Ctor any
	// Value is the instance for KindValue providers.
	Value any
	// Scope is Singleton unless set.
	Scope Scope
	// Enhancer marks global guards, interceptors, pipes and filters.
	Enhancer EnhancerKind
}

// String describes the provider for error messages.
func (p Provider) String() string {
	return fmt.Sprintf("provider %s (%s)", p.Token, p.Scope)
}

// Provide registers a constructor. The token is the constructor's first return
// type. The constructor returns (T) or (T, error); its parameters are injected.
//
//	gonest.Provide(NewUsersService)
//	gonest.Provide(NewCache, gonest.Transient)
func Provide(ctor any, opts ...ProviderOption) Provider {
	p := Provider{Kind: KindConstructor, Ctor: ctor}
	if t := reflect.TypeOf(ctor); t != nil && t.Kind() == reflect.Func && t.NumOut() > 0 {
		p.Token.Type = t.Out(0)
	}
	return applyOpts(p, opts)
}

// Value registers an existing instance under type T.
//
//	gonest.Value(db)                       // *sql.DB
//	gonest.Value[Clock](realClock{})       // interface token
func Value[T any](v T, opts ...ProviderOption) Provider {
	p := Provider{Kind: KindValue, Value: v, Token: TokenOf[T]()}
	return applyOpts(p, opts)
}

// Bind registers ctor under the interface (or any other type) I. The
// constructor's first return type must be assignable to I. This is the
// equivalent of NestJS { provide: I, useClass: Impl }.
//
//	gonest.Bind[UserRepository](NewPostgresUserRepository)
func Bind[I any](ctor any, opts ...ProviderOption) Provider {
	p := Provider{Kind: KindConstructor, Ctor: ctor, Token: TokenOf[I]()}
	return applyOpts(p, opts)
}

// Factory registers fn as the factory for type T, like NestJS useFactory.
// fn's parameters are injected and its first result must be assignable to T.
func Factory[T any](fn any, opts ...ProviderOption) Provider {
	return Bind[T](fn, opts...)
}

// AppGuard registers a provider whose instance becomes a global guard.
func AppGuard(ctor any, opts ...ProviderOption) Provider {
	return enhancer(EnhancerGuard, ctor, opts)
}

// AppInterceptor registers a provider whose instance becomes a global interceptor.
func AppInterceptor(ctor any, opts ...ProviderOption) Provider {
	return enhancer(EnhancerInterceptor, ctor, opts)
}

// AppPipe registers a provider whose instance becomes a global pipe.
func AppPipe(ctor any, opts ...ProviderOption) Provider {
	return enhancer(EnhancerPipe, ctor, opts)
}

// AppFilter registers a provider whose instance becomes a global exception filter.
func AppFilter(ctor any, opts ...ProviderOption) Provider {
	return enhancer(EnhancerFilter, ctor, opts)
}

func enhancer(kind EnhancerKind, ctor any, opts []ProviderOption) Provider {
	p := Provide(ctor, opts...)
	p.Enhancer = kind
	return p
}

func applyOpts(p Provider, opts []ProviderOption) Provider {
	for _, o := range opts {
		if o != nil {
			o.applyProvider(&p)
		}
	}
	return p
}

// In marks a parameter object. A constructor parameter whose type is a struct
// embedding In has each exported field injected by type. Field tags:
//
//	name:"primary"   inject the provider registered with Named("primary")
//	optional:"true"  leave the zero value when no provider exists
//
// Example:
//
//	type ReportDeps struct {
//		gonest.In
//		Primary *sql.DB `name:"primary"`
//		Replica *sql.DB `name:"replica" optional:"true"`
//	}
//	func NewReportService(d ReportDeps) *ReportService
type In struct{}

// InType is the reflect.Type of In, used by the container to detect parameter objects.
var InType = reflect.TypeFor[In]()
