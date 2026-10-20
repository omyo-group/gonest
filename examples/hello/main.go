// Command hello is the smallest Gonest application, used by the README and
// docs/guide/getting-started.md.
package main

// docs:hello
import (
	"log"

	"github.com/omyo/gonest"
)

type Greeter struct{}

func NewGreeter() *Greeter { return &Greeter{} }

func (g *Greeter) Greet(name string) string { return "Hello, " + name + "!" }

type GreetInput struct {
	Name string `query:"name" validate:"required,min=2"`
}

type GreetController struct{ greeter *Greeter }

func NewGreetController(g *Greeter) *GreetController { return &GreetController{greeter: g} }

func (c *GreetController) Routes(r gonest.Router) {
	r.GET("/hello", gonest.Handle(c.Greet))
}

func (c *GreetController) Greet(_ *gonest.Ctx, in GreetInput) (map[string]string, error) {
	return map[string]string{"message": c.greeter.Greet(in.Name)}, nil
}

var AppModule = &gonest.Module{
	Name:        "app",
	Providers:   []gonest.Provider{gonest.Provide(NewGreeter)},
	Controllers: []any{NewGreetController},
}

func main() {
	app, err := gonest.New(AppModule)
	if err != nil {
		log.Fatal(err)
	}
	log.Fatal(app.Listen(":8080"))
}

// docs:end
