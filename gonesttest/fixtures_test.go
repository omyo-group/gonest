package gonesttest_test

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"

	"github.com/omyo-group/gonest"
)

// A small three-module application: auth (global guard), items (controller,
// service, repository) and a root. Modules are package-level variables on
// purpose: overrides must never mutate them.

type Item struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type ItemsRepo interface {
	Find(id int) (Item, error)
	Add(name string) (Item, error)
}

// DBRepo stands for a repository that needs a database: always failing here.
type DBRepo struct{}

func NewDBRepo() *DBRepo { return &DBRepo{} }

func (*DBRepo) Find(int) (Item, error)   { return Item{}, errors.New("database unavailable") }
func (*DBRepo) Add(string) (Item, error) { return Item{}, errors.New("database unavailable") }

type FakeRepo struct {
	items map[int]Item
}

func NewFakeRepo() *FakeRepo {
	return &FakeRepo{items: map[int]Item{1: {ID: 1, Name: "pen"}}}
}

func (f *FakeRepo) Find(id int) (Item, error) {
	it, ok := f.items[id]
	if !ok {
		return Item{}, gonest.NotFound("item not found")
	}
	return it, nil
}

func (f *FakeRepo) Add(name string) (Item, error) {
	it := Item{ID: len(f.items) + 1, Name: name}
	f.items[it.ID] = it
	return it, nil
}

type ItemsService struct{ repo ItemsRepo }

func NewItemsService(r ItemsRepo) *ItemsService { return &ItemsService{repo: r} }

type ItemsController struct{ svc *ItemsService }

func NewItemsController(s *ItemsService) *ItemsController { return &ItemsController{svc: s} }

type FindInput struct {
	ID int `path:"id" validate:"min=1"`
}

type CreateInput struct {
	Name string `json:"name" validate:"required,min=2"`
}

func (c *ItemsController) Routes(r gonest.Router) {
	r.Prefix("/items")
	r.GET("/:id", gonest.Handle(c.find))
	r.POST("", gonest.Handle(c.create))
}

func (c *ItemsController) find(_ *gonest.Ctx, in FindInput) (Item, error) {
	return c.svc.repo.Find(in.ID)
}

func (c *ItemsController) create(_ *gonest.Ctx, in CreateInput) (Item, error) {
	return c.svc.repo.Add(in.Name)
}

// JwtGuard requires a bearer token; a real one would verify a signature.
type JwtGuard struct{ calls atomic.Int32 }

func NewJwtGuard() *JwtGuard { return &JwtGuard{} }

func (g *JwtGuard) CanActivate(ctx *gonest.Ctx) (bool, error) {
	g.calls.Add(1)
	if !strings.HasPrefix(ctx.Header("Authorization"), "Bearer ") {
		return false, gonest.Unauthorized("missing bearer token")
	}
	return true, nil
}

type AllowAll struct{}

func (AllowAll) CanActivate(*gonest.Ctx) (bool, error) { return true, nil }

// Boot records lifecycle calls to prove cleanup runs.
type Boot struct {
	inits, destroys atomic.Int32
}

func NewBoot() *Boot { return &Boot{} }

func (b *Boot) OnModuleInit(context.Context) error    { b.inits.Add(1); return nil }
func (b *Boot) OnModuleDestroy(context.Context) error { b.destroys.Add(1); return nil }

var AuthModule = &gonest.Module{
	Name:      "auth",
	Providers: []gonest.Provider{gonest.AppGuard(NewJwtGuard)},
}

var ItemsModule = &gonest.Module{
	Name: "items",
	Providers: []gonest.Provider{
		gonest.Bind[ItemsRepo](NewDBRepo),
		gonest.Provide(NewItemsService),
		gonest.Provide(NewBoot),
	},
	Controllers: []any{NewItemsController},
	Exports: []gonest.Token{
		gonest.TokenOf[ItemsRepo](), gonest.TokenOf[*ItemsService](), gonest.TokenOf[*Boot](),
	},
}

var AppModule = &gonest.Module{
	Name:    "app",
	Imports: []*gonest.Module{AuthModule, ItemsModule},
}
