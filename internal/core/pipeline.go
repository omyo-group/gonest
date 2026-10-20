package core

import "reflect"

// Guard decides whether a request may proceed, like NestJS CanActivate.
// Returning false yields 403 Forbidden; returning an error passes it to the
// exception filters (return Unauthorized(...) for 401).
type Guard interface {
	CanActivate(ctx *Ctx) (bool, error)
}

// GuardFunc adapts a function to Guard.
type GuardFunc func(ctx *Ctx) (bool, error)

// CanActivate implements Guard.
func (f GuardFunc) CanActivate(ctx *Ctx) (bool, error) { return f(ctx) }

// Next runs the rest of the pipeline (inner interceptors, pipes and the
// handler) and returns the handler result.
type Next func() (any, error)

// Interceptor wraps handler execution, like NestJS NestInterceptor. Code before
// next() runs before the handler; code after it can inspect or replace the
// result or the error. Interceptors run in registration order on the way in
// and in reverse order on the way out.
type Interceptor interface {
	Intercept(ctx *Ctx, next Next) (any, error)
}

// InterceptorFunc adapts a function to Interceptor.
type InterceptorFunc func(ctx *Ctx, next Next) (any, error)

// Intercept implements Interceptor.
func (f InterceptorFunc) Intercept(ctx *Ctx, next Next) (any, error) { return f(ctx, next) }

// ArgMeta describes the value a Pipe receives.
type ArgMeta struct {
	// Type is the handler input type (the In of Handle[In, Out]).
	Type reflect.Type
	// Route is the route being executed.
	Route *RouteInfo
}

// Pipe transforms or validates the bound handler input, like NestJS
// PipeTransform. v holds a value of type meta.Type; the pipe returns a value of
// the same type, or an error (usually BadRequest). Pipes run after the built-in
// binding and validation.
type Pipe interface {
	Transform(ctx *Ctx, v any, meta ArgMeta) (any, error)
}

// PipeFunc adapts a function to Pipe.
type PipeFunc func(ctx *Ctx, v any, meta ArgMeta) (any, error)

// Transform implements Pipe.
func (f PipeFunc) Transform(ctx *Ctx, v any, meta ArgMeta) (any, error) { return f(ctx, v, meta) }

// ExceptionFilter handles errors raised anywhere in the pipeline, like NestJS
// ExceptionFilter. Catch writes a response and returns true when it handled
// err, or returns false to let the next filter try. Filters are tried route
// first, then controller, then global; the default filter runs last.
type ExceptionFilter interface {
	Catch(ctx *Ctx, err error) bool
}

// ExceptionFilterFunc adapts a function to ExceptionFilter.
type ExceptionFilterFunc func(ctx *Ctx, err error) bool

// Catch implements ExceptionFilter.
func (f ExceptionFilterFunc) Catch(ctx *Ctx, err error) bool { return f(ctx, err) }
