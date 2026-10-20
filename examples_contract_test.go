package gonest_test

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"testing"

	"github.com/omyo/gonest"
)

// This file is the "users module" from docs/contracts.md. It must keep
// compiling: it is the reference for how the public API is used.

// UserDTO is returned to clients.
type UserDTO struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

// UsersRepo stores users in memory.
type UsersRepo struct {
	mu    sync.RWMutex
	users map[int]UserDTO
	next  int
}

func NewUsersRepo() *UsersRepo { return &UsersRepo{users: map[int]UserDTO{}, next: 1} }

// UsersService holds the business logic.
type UsersService struct {
	repo *UsersRepo
	log  *slog.Logger
}

func NewUsersService(repo *UsersRepo, log *slog.Logger) *UsersService {
	return &UsersService{repo: repo, log: log}
}

// OnModuleInit seeds data (lifecycle hook).
func (s *UsersService) OnModuleInit(ctx context.Context) error {
	_, err := s.Create(ctx, "Ada", "ada@example.com")
	return err
}

func (s *UsersService) Create(_ context.Context, name, email string) (UserDTO, error) {
	s.repo.mu.Lock()
	defer s.repo.mu.Unlock()
	u := UserDTO{ID: s.repo.next, Name: name, Email: email}
	s.repo.users[u.ID] = u
	s.repo.next++
	return u, nil
}

func (s *UsersService) Find(_ context.Context, id int) (UserDTO, bool) {
	s.repo.mu.RLock()
	defer s.repo.mu.RUnlock()
	u, ok := s.repo.users[id]
	return u, ok
}

// Inputs: one struct per handler, bound from tags and validated.
type FindOneInput struct {
	ID int `path:"id" validate:"required,min=1"`
}

type CreateUserInput struct {
	Name  string `json:"name" validate:"required,min=2"`
	Email string `json:"email" validate:"required,email"`
}

// APIKeyGuard is a guard with no dependencies.
type APIKeyGuard struct{ key string }

func (g APIKeyGuard) CanActivate(ctx *gonest.Ctx) (bool, error) {
	if ctx.Header("X-API-Key") != g.key {
		return false, gonest.Unauthorized("missing or invalid API key")
	}
	return true, nil
}

// UsersController declares routes.
type UsersController struct {
	users *UsersService
}

func NewUsersController(users *UsersService) *UsersController {
	return &UsersController{users: users}
}

func (c *UsersController) Routes(r gonest.Router) {
	r.Prefix("/users")
	r.Use(gonest.Tags("users"), gonest.UseGuards(APIKeyGuard{key: "secret"}))
	r.GET("/:id", gonest.Handle(c.FindOne), gonest.Summary("Get a user"))
	r.POST("", gonest.Handle(c.Create), gonest.Summary("Create a user"))
}

func (c *UsersController) FindOne(ctx *gonest.Ctx, in FindOneInput) (UserDTO, error) {
	u, ok := c.users.Find(ctx, in.ID)
	if !ok {
		return UserDTO{}, gonest.NotFound(fmt.Sprintf("user %d not found", in.ID))
	}
	return u, nil
}

func (c *UsersController) Create(ctx *gonest.Ctx, in CreateUserInput) (UserDTO, error) {
	return c.users.Create(ctx, in.Name, in.Email)
}

// UsersModule wires it together and exports the service.
var UsersModule = &gonest.Module{
	Name:        "users",
	Providers:   []gonest.Provider{gonest.Provide(NewUsersRepo), gonest.Provide(NewUsersService)},
	Controllers: []any{NewUsersController},
	Exports:     []gonest.Token{gonest.TokenOf[*UsersService]()},
}

// AppModule is the root.
var AppModule = &gonest.Module{
	Name:      "app",
	Imports:   []*gonest.Module{UsersModule},
	Providers: []gonest.Provider{gonest.Value(slog.Default())},
}

// TestContractCompiles only checks that the example type-checks against the contracts.
func TestContractCompiles(t *testing.T) {
	var _ gonest.Controller = (*UsersController)(nil)
	var _ gonest.Guard = APIKeyGuard{}
	var _ gonest.OnModuleInit = (*UsersService)(nil)
	if UsersModule.Name != "users" || len(AppModule.Imports) != 1 {
		t.Fatal("unexpected module wiring")
	}
}
