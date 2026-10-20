package gonesttest_test

import (
	"fmt"
	"testing"

	"github.com/omyo/gonest"
	"github.com/omyo/gonest/gonesttest"
)

// exampleT is a minimal testing.TB for Examples, which have no *testing.T.
type exampleT struct {
	testing.TB
	cleanups []func()
}

func newExampleT() *exampleT { return &exampleT{} }

func (*exampleT) Helper()                   {}
func (e *exampleT) Cleanup(f func())        { e.cleanups = append(e.cleanups, f) }
func (*exampleT) Fatalf(f string, a ...any) { panic(fmt.Sprintf(f, a...)) }
func (*exampleT) Fatal(a ...any)            { panic(fmt.Sprint(a...)) }
func (*exampleT) Errorf(f string, a ...any) { panic(fmt.Sprintf(f, a...)) }

func ExampleNew() {
	t := newExampleT() // in a real test, use the *testing.T you are given

	app := gonesttest.New(t, AppModule).
		OverrideProvider(gonest.Bind[ItemsRepo](NewFakeRepo)).
		Compile()

	res := app.Request("GET", "/items/1").WithBearer("token").Do()
	fmt.Println(res.Status(), string(res.Body()))
	// Output: 200 {"id":1,"name":"pen"}
}

func ExampleGet() {
	t := newExampleT()
	app := gonesttest.New(t, AppModule).
		OverrideProvider(gonest.Bind[ItemsRepo](NewFakeRepo)).
		Compile()

	svc := gonesttest.Get[*ItemsService](app)
	it, _ := svc.repo.Find(1)
	fmt.Println(it.Name)
	// Output: pen
}

func ExampleApp_Request() {
	t := newExampleT()
	app := gonesttest.New(t, AppModule).
		OverrideProvider(gonest.Bind[ItemsRepo](NewFakeRepo)).
		Compile()

	app.Request("POST", "/items").
		WithBearer("token").
		WithJSON(map[string]string{"name": "ink"}).
		Do().
		ExpectStatus(201).
		ExpectJSON(Item{ID: 2, Name: "ink"})

	app.Request("GET", "/items/1").Do().
		ExpectStatus(401).
		ExpectErrorMessage("bearer")
	fmt.Println("ok")
	// Output: ok
}

func ExampleNewCtx() {
	t := newExampleT()
	guard := NewJwtGuard()

	ctx := gonesttest.NewCtx(t,
		gonesttest.CtxHeader("Authorization", "Bearer abc"),
		gonesttest.CtxMetadata("roles", []string{"admin"}),
	)
	ok, err := guard.CanActivate(ctx)
	fmt.Println(ok, err)
	// Output: true <nil>
}

func ExampleResponseOf() {
	t := newExampleT()
	filter := gonest.ExceptionFilterFunc(func(ctx *gonest.Ctx, err error) bool {
		ctx.Gin().JSON(418, map[string]string{"error": err.Error()})
		return true
	})

	ctx := gonesttest.NewCtx(t)
	filter.Catch(ctx, fmt.Errorf("short and stout"))
	res := gonesttest.ResponseOf(t, ctx)
	fmt.Println(res.Status(), string(res.Body()))
	// Output: 418 {"error":"short and stout"}
}
