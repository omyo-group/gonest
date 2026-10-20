package core

// Controller declares HTTP routes, like a NestJS @Controller class.
// Controllers are listed in Module.Controllers as constructors; their
// dependencies are injected.
//
//	func (c *UsersController) Routes(r gonest.Router) {
//		r.Prefix("/users")
//		r.Use(gonest.Tags("users"), gonest.UseGuards(c.auth))
//		r.GET("/:id", gonest.Handle(c.FindOne), gonest.Summary("Get a user"))
//		r.POST("", gonest.Handle(c.Create))
//	}
type Controller interface {
	Routes(r Router)
}

// Router collects the routes of one controller. Implemented by internal/pipeline.
type Router interface {
	// Prefix sets the path prefix of every route of the controller, e.g. "/users".
	// Call it before declaring routes.
	Prefix(prefix string)
	// Use applies options to every route declared after it in this router or group
	// (controller-level guards, interceptors, pipes, filters, tags, metadata).
	Use(opts ...RouteOption)
	// Group declares routes under an additional prefix with extra options.
	Group(prefix string, fn func(r Router), opts ...RouteOption)

	GET(path string, h *Handler, opts ...RouteOption)
	POST(path string, h *Handler, opts ...RouteOption)
	PUT(path string, h *Handler, opts ...RouteOption)
	PATCH(path string, h *Handler, opts ...RouteOption)
	DELETE(path string, h *Handler, opts ...RouteOption)
	OPTIONS(path string, h *Handler, opts ...RouteOption)
	HEAD(path string, h *Handler, opts ...RouteOption)
	// Handle declares a route for any method.
	Handle(method, path string, h *Handler, opts ...RouteOption)
}
