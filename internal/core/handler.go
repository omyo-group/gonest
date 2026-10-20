package core

import (
	"fmt"
	"reflect"
)

// NoContent is an Out type for handlers that answer 204 No Content.
//
//	func (c *UsersController) Delete(ctx *gonest.Ctx, in IDInput) (gonest.NoContent, error)
type NoContent struct{}

var noContentType = reflect.TypeFor[NoContent]()

// Empty is an In type for handlers that take no input.
type Empty struct{}

// Handler is a route handler with its input and output types resolved at
// construction. Create it with Handle or HandleRaw.
type Handler struct {
	// InType is the type of the handler input; nil for raw handlers.
	InType reflect.Type
	// OutType is the type of the handler output; nil for raw handlers.
	OutType reflect.Type
	// Raw handlers write the response themselves through Ctx.Gin().
	Raw bool
	// Name is the handler function's name for logs ("UsersController.FindOne").
	Name string

	call func(ctx *Ctx, in any) (any, error)
	raw  func(ctx *Ctx) error
}

// Handle wraps a typed handler. In is bound from the request using struct tags
// (path, query, header, form, json) and validated with `validate` tags before
// the handler runs. Out is written as JSON.
//
//	r.GET("/:id", gonest.Handle(c.FindOne))
func Handle[In, Out any](fn func(ctx *Ctx, in In) (Out, error)) *Handler {
	if fn == nil {
		panic("gonest.Handle: nil handler")
	}
	return &Handler{
		InType:  reflect.TypeFor[In](),
		OutType: reflect.TypeFor[Out](),
		Name:    funcName(fn),
		call: func(ctx *Ctx, in any) (any, error) {
			v, ok := in.(In)
			if !ok {
				var zero In
				return nil, fmt.Errorf("gonest: handler %T received input %T", zero, in)
			}
			return fn(ctx, v)
		},
	}
}

// HandleRaw wraps a handler that writes its own response (streams, SSE, files).
// Guards, interceptors and filters still apply; there is no input binding.
func HandleRaw(fn func(ctx *Ctx) error) *Handler {
	if fn == nil {
		panic("gonest.HandleRaw: nil handler")
	}
	return &Handler{Raw: true, Name: funcName(fn), raw: fn}
}

// Call invokes a typed handler with an already bound input of type InType.
func (h *Handler) Call(ctx *Ctx, in any) (any, error) {
	if h.Raw {
		return nil, h.raw(ctx)
	}
	return h.call(ctx, in)
}

func funcName(fn any) string {
	return runtimeFuncName(reflect.ValueOf(fn).Pointer())
}
