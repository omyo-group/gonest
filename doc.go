// Package gonest is a Go web framework on top of Gin that follows the NestJS
// architecture: modules, providers with dependency injection, controllers,
// guards, pipes, interceptors, exception filters and lifecycle hooks.
//
// All wiring happens once, at bootstrap: modules are walked, constructors are
// analysed and invoked, input binders are compiled and pipelines are composed.
// Serving a request is then a plain Gin handler call with no dependency lookups.
//
// A minimal application:
//
//	type HelloController struct{}
//
//	func NewHelloController() *HelloController { return &HelloController{} }
//
//	type HelloInput struct {
//		Name string `query:"name" validate:"required"`
//	}
//
//	func (c *HelloController) Routes(r gonest.Router) {
//		r.Prefix("/hello")
//		r.GET("", gonest.Handle(c.Hello))
//	}
//
//	func (c *HelloController) Hello(ctx *gonest.Ctx, in HelloInput) (map[string]string, error) {
//		return map[string]string{"hello": in.Name}, nil
//	}
//
//	func main() {
//		app, err := gonest.New(&gonest.Module{Name: "app", Controllers: []any{NewHelloController}})
//		if err != nil {
//			log.Fatal(err)
//		}
//		log.Fatal(app.Listen(":8080"))
//	}
package gonest
