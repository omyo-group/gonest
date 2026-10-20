package gonest

import "github.com/omyo/gonest/internal/core"

// Contracts re-exported from internal/core. See docs/contracts.md.

type (
	// Module groups providers and controllers (NestJS @Module).
	Module = core.Module
	// Provider describes an injectable value.
	Provider = core.Provider
	// ProviderOption customises a provider (scopes, Named).
	ProviderOption = core.ProviderOption
	// Scope is a provider lifetime.
	Scope = core.Scope
	// Token identifies a provider: type plus optional name.
	Token = core.Token
	// In marks a constructor parameter object.
	In = core.In

	// Controller declares routes.
	Controller = core.Controller
	// Router collects a controller's routes.
	Router = core.Router
	// Handler is a typed route handler.
	Handler = core.Handler
	// Ctx is the per-request context.
	Ctx = core.Ctx
	// NoContent is an Out type producing 204.
	NoContent = core.NoContent
	// Empty is an In type for handlers without input.
	Empty = core.Empty

	// RouteOption configures a route or controller.
	RouteOption = core.RouteOption
	// RouteConfig is the result of applying RouteOptions.
	RouteConfig = core.RouteConfig
	// RouteInfo is metadata of a mounted route.
	RouteInfo = core.RouteInfo
	// RouteTable lists all mounted routes; inject *RouteTable to read it.
	RouteTable = core.RouteTable
	// ResponseDoc documents a response for OpenAPI.
	ResponseDoc = core.ResponseDoc

	// Guard authorises requests.
	Guard = core.Guard
	// GuardFunc adapts a function to Guard.
	GuardFunc = core.GuardFunc
	// Interceptor wraps handler execution.
	Interceptor = core.Interceptor
	// InterceptorFunc adapts a function to Interceptor.
	InterceptorFunc = core.InterceptorFunc
	// Next continues the pipeline inside an interceptor.
	Next = core.Next
	// Pipe transforms or validates handler input.
	Pipe = core.Pipe
	// PipeFunc adapts a function to Pipe.
	PipeFunc = core.PipeFunc
	// ArgMeta describes a pipe's input.
	ArgMeta = core.ArgMeta
	// ExceptionFilter turns errors into responses.
	ExceptionFilter = core.ExceptionFilter
	// ExceptionFilterFunc adapts a function to ExceptionFilter.
	ExceptionFilterFunc = core.ExceptionFilterFunc

	// HTTPError is an error with an HTTP status.
	HTTPError = core.HTTPError
	// ErrorBody is the JSON error response shape.
	ErrorBody = core.ErrorBody
	// FieldError is one validation failure.
	FieldError = core.FieldError
	// ValidationErrors lists validation failures.
	ValidationErrors = core.ValidationErrors
	// ResolveError reports a dependency injection failure.
	ResolveError = core.ResolveError

	// MiddlewareConsumer binds gin middleware to routes in Module.Configure.
	MiddlewareConsumer = core.MiddlewareConsumer
	// MiddlewareBuilder completes a middleware rule.
	MiddlewareBuilder = core.MiddlewareBuilder
	// RouteSpec selects routes for middleware.
	RouteSpec = core.RouteSpec

	// OnModuleInit is a lifecycle hook.
	OnModuleInit = core.OnModuleInit
	// OnApplicationBootstrap is a lifecycle hook.
	OnApplicationBootstrap = core.OnApplicationBootstrap
	// OnModuleDestroy is a lifecycle hook.
	OnModuleDestroy = core.OnModuleDestroy
	// BeforeApplicationShutdown is a lifecycle hook.
	BeforeApplicationShutdown = core.BeforeApplicationShutdown
	// OnApplicationShutdown is a lifecycle hook.
	OnApplicationShutdown = core.OnApplicationShutdown
)

// Provider scopes.
const (
	// Singleton: one instance per application (default).
	Singleton = core.Singleton
	// Transient: a new instance per injection.
	Transient = core.Transient
	// RequestScoped: reserved, not supported in v0.1.
	RequestScoped = core.RequestScoped
)

// Provide registers a constructor; see core.Provide.
func Provide(ctor any, opts ...ProviderOption) Provider { return core.Provide(ctor, opts...) }

// Value registers an existing instance under type T.
func Value[T any](v T, opts ...ProviderOption) Provider { return core.Value[T](v, opts...) }

// Bind registers ctor under interface I (NestJS useClass).
func Bind[I any](ctor any, opts ...ProviderOption) Provider { return core.Bind[I](ctor, opts...) }

// Factory registers fn as the factory for T (NestJS useFactory).
func Factory[T any](fn any, opts ...ProviderOption) Provider { return core.Factory[T](fn, opts...) }

// Named registers a provider under a name.
func Named(name string) ProviderOption { return core.Named(name) }

// AppGuard registers a provider used as a global guard (NestJS APP_GUARD).
func AppGuard(ctor any, opts ...ProviderOption) Provider { return core.AppGuard(ctor, opts...) }

// AppInterceptor registers a provider used as a global interceptor.
func AppInterceptor(ctor any, opts ...ProviderOption) Provider {
	return core.AppInterceptor(ctor, opts...)
}

// AppPipe registers a provider used as a global pipe.
func AppPipe(ctor any, opts ...ProviderOption) Provider { return core.AppPipe(ctor, opts...) }

// AppFilter registers a provider used as a global exception filter.
func AppFilter(ctor any, opts ...ProviderOption) Provider { return core.AppFilter(ctor, opts...) }

// TokenOf returns the token of type T, for Module.Exports.
func TokenOf[T any]() Token { return core.TokenOf[T]() }

// NamedTokenOf returns the token of type T registered under name.
func NamedTokenOf[T any](name string) Token { return core.NamedTokenOf[T](name) }

// Handle wraps a typed handler func(*Ctx, In) (Out, error).
func Handle[In, Out any](fn func(ctx *Ctx, in In) (Out, error)) *Handler {
	return core.Handle(fn)
}

// HandleRaw wraps a handler that writes its own response.
func HandleRaw(fn func(ctx *Ctx) error) *Handler { return core.HandleRaw(fn) }

// CtxValue returns a request value as T.
func CtxValue[T any](c *Ctx, key string) (T, bool) { return core.CtxValue[T](c, key) }

// Route options.

// UseGuards appends guards.
func UseGuards(g ...Guard) RouteOption { return core.UseGuards(g...) }

// UseInterceptors appends interceptors.
func UseInterceptors(i ...Interceptor) RouteOption { return core.UseInterceptors(i...) }

// UsePipes appends pipes.
func UsePipes(p ...Pipe) RouteOption { return core.UsePipes(p...) }

// UseFilters appends exception filters.
func UseFilters(f ...ExceptionFilter) RouteOption { return core.UseFilters(f...) }

// Status sets the success status code.
func Status(code int) RouteOption { return core.Status(code) }

// Summary sets the OpenAPI summary.
func Summary(s string) RouteOption { return core.Summary(s) }

// Description sets the OpenAPI description.
func Description(s string) RouteOption { return core.Description(s) }

// Tags appends OpenAPI tags.
func Tags(t ...string) RouteOption { return core.Tags(t...) }

// Deprecated marks a route deprecated.
func Deprecated() RouteOption { return core.Deprecated() }

// Hidden excludes a route from OpenAPI.
func Hidden() RouteOption { return core.Hidden() }

// Security names required OpenAPI security schemes.
func Security(schemes ...string) RouteOption { return core.Security(schemes...) }

// Responds documents an extra response; body may be nil.
func Responds(status int, description string, body any) RouteOption {
	return core.Responds(status, description, body)
}

// SetMetadata attaches route metadata readable by guards.
func SetMetadata(key string, value any) RouteOption { return core.SetMetadata(key, value) }

// Middleware route selectors.

// ForPath selects every method on a path (trailing "*" = prefix).
func ForPath(path string) RouteSpec { return core.ForPath(path) }

// ForRoute selects one method on a path.
func ForRoute(method, path string) RouteSpec { return core.ForRoute(method, path) }

// HTTP error constructors.

// NewHTTPError builds an HTTPError.
func NewHTTPError(status int, message string, details ...any) *HTTPError {
	return core.NewHTTPError(status, message, details...)
}

// BadRequest returns a 400 error.
func BadRequest(message string, details ...any) *HTTPError {
	return core.BadRequest(message, details...)
}

// Unauthorized returns a 401 error.
func Unauthorized(message string, details ...any) *HTTPError {
	return core.Unauthorized(message, details...)
}

// Forbidden returns a 403 error.
func Forbidden(message string, details ...any) *HTTPError { return core.Forbidden(message, details...) }

// NotFound returns a 404 error.
func NotFound(message string, details ...any) *HTTPError { return core.NotFound(message, details...) }

// Conflict returns a 409 error.
func Conflict(message string, details ...any) *HTTPError { return core.Conflict(message, details...) }

// UnprocessableEntity returns a 422 error.
func UnprocessableEntity(message string, details ...any) *HTTPError {
	return core.UnprocessableEntity(message, details...)
}

// TooManyRequests returns a 429 error.
func TooManyRequests(message string, details ...any) *HTTPError {
	return core.TooManyRequests(message, details...)
}

// Internal returns a 500 error.
func Internal(message string, details ...any) *HTTPError { return core.Internal(message, details...) }
