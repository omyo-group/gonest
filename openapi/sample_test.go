package openapi_test

import (
	"mime/multipart"
	"reflect"
	"time"

	"github.com/omyo/gonest"
)

// Sample controllers and DTOs used by the golden test. Routes are built by
// hand (no gonest.New), the way the pipeline would record them.

type Role string

type Address struct {
	Street string `json:"street" validate:"required"`
	Zip    string `json:"zip,omitempty" validate:"len=5"`
}

type Audit struct {
	CreatedAt time.Time `json:"createdAt"`
	CreatedBy string    `json:"createdBy"`
}

type UserDTO struct {
	Audit
	ID       int               `json:"id" doc:"Unique identifier" example:"7"`
	Name     string            `json:"name" example:"Ada"`
	Email    string            `json:"email" validate:"required,email"`
	Role     Role              `json:"role" validate:"oneof=admin user"`
	Address  *Address          `json:"address,omitempty"`
	Tags     []string          `json:"tags"`
	Meta     map[string]string `json:"meta,omitempty"`
	Manager  *UserDTO          `json:"manager,omitempty"`
	Timeout  time.Duration     `json:"timeout"`
	Nickname *string           `json:"nickname"`
	Secret   string            `json:"-"`
	hidden   string
}

var _ = UserDTO{}.hidden // unexported fields must be ignored

type Page[T any] struct {
	Items []T `json:"items"`
	Total int `json:"total"`
}

type ListInput struct {
	Page   int      `query:"page" default:"1" validate:"min=1" doc:"Page number"`
	Limit  int      `query:"limit" default:"20" validate:"min=1,max=100"`
	Sort   string   `query:"sort" validate:"oneof=name email" example:"name"`
	Roles  []string `query:"role" validate:"max=3,dive,oneof=admin user"`
	Tenant string   `header:"X-Tenant" validate:"required"`
}

type FindOneInput struct {
	ID int `path:"id" validate:"required,min=1"`
}

type CreateInput struct {
	Name    string   `json:"name" validate:"required,min=2,max=50"`
	Email   string   `json:"email" validate:"required,email"`
	Website string   `json:"website,omitempty" validate:"omitempty,url"`
	Ref     string   `json:"ref,omitempty" validate:"uuid"`
	Age     *int     `json:"age,omitempty" validate:"gte=18,lt=130"`
	Role    Role     `json:"role" validate:"oneof=admin user"`
	Address *Address `json:"address,omitempty"`
	Skip    string   // not bound: no source tag
}

type UpdateInput struct {
	ID   int    `path:"id" validate:"required"`
	Name string `json:"name" validate:"required"`
}

type UploadInput struct {
	Title string                  `form:"title" validate:"required" doc:"Document title"`
	File  *multipart.FileHeader   `form:"file" validate:"required"`
	Extra []*multipart.FileHeader `form:"extra"`
}

type LoginInput struct {
	Username string `form:"username" validate:"required"`
	Password string `form:"password" validate:"required"`
}

type Node struct {
	Name     string  `json:"name"`
	Children []*Node `json:"children"`
}

type UsersController struct{}

func (c *UsersController) List(*gonest.Ctx, ListInput) (Page[UserDTO], error) {
	return Page[UserDTO]{}, nil
}
func (c *UsersController) FindOne(*gonest.Ctx, FindOneInput) (*UserDTO, error) { return nil, nil }
func (c *UsersController) Create(*gonest.Ctx, CreateInput) (UserDTO, error)    { return UserDTO{}, nil }
func (c *UsersController) Update(*gonest.Ctx, *UpdateInput) (UserDTO, error)   { return UserDTO{}, nil }
func (c *UsersController) Delete(*gonest.Ctx, FindOneInput) (gonest.NoContent, error) {
	return gonest.NoContent{}, nil
}
func (c *UsersController) Tree(*gonest.Ctx, gonest.Empty) (Node, error) { return Node{}, nil }
func (c *UsersController) Secret(*gonest.Ctx, gonest.Empty) (string, error) {
	return "", nil
}
func (c *UsersController) Legacy(*gonest.Ctx, gonest.Empty) (map[string]any, error) {
	return nil, nil
}

type FilesController struct{}

func (c *FilesController) Upload(*gonest.Ctx, UploadInput) (gonest.NoContent, error) {
	return gonest.NoContent{}, nil
}
func (c *FilesController) Login(*gonest.Ctx, LoginInput) (gonest.Empty, error) {
	return gonest.Empty{}, nil
}
func (c *FilesController) Download(*gonest.Ctx) error { return nil }

func allow(*gonest.Ctx) (bool, error) { return true, nil }

func route(method, path string, ctrl any, h *gonest.Handler, ctrlCfg, cfg gonest.RouteConfig) *gonest.RouteInfo {
	return &gonest.RouteInfo{
		Method: method, Path: path, Module: "test",
		Controller: reflect.TypeOf(ctrl), Handler: h,
		ControllerConfig: ctrlCfg, Config: cfg,
	}
}

func sampleRoutes() []*gonest.RouteInfo {
	u := &UsersController{}
	f := &FilesController{}
	users := gonest.RouteConfig{Tags: []string{"users"}}
	guarded := gonest.RouteConfig{Tags: []string{"users"}, Guards: []gonest.Guard{gonest.GuardFunc(allow)}}
	return []*gonest.RouteInfo{
		route("GET", "/api/users", u, gonest.Handle(u.List), users, gonest.RouteConfig{
			Summary: "List users", Description: "Paginated list.",
		}),
		route("GET", "/api/users/:id", u, gonest.Handle(u.FindOne), users, gonest.RouteConfig{
			Summary: "Get one user",
			Responses: []gonest.ResponseDoc{
				{Status: 404, Description: "User not found", Type: reflect.TypeFor[gonest.ErrorBody]()},
			},
		}),
		route("POST", "/api/users", u, gonest.Handle(u.Create), guarded, gonest.RouteConfig{
			Summary: "Create a user", Security: []string{"bearer"},
		}),
		route("PUT", "/api/users/:id", u, gonest.Handle(u.Update), guarded, gonest.RouteConfig{
			Status: 202, Security: []string{"apiKey"},
		}),
		route("DELETE", "/api/users/:id", u, gonest.Handle(u.Delete), users, gonest.RouteConfig{
			Deprecated: true,
		}),
		route("GET", "/api/users/tree", u, gonest.Handle(u.Tree), gonest.RouteConfig{}, gonest.RouteConfig{}),
		route("GET", "/api/users/secret", u, gonest.Handle(u.Secret), gonest.RouteConfig{}, gonest.RouteConfig{Hidden: true}),
		route("GET", "/api/users/legacy", u, gonest.Handle(u.Legacy), users, gonest.RouteConfig{Tags: []string{"legacy"}}),
		route("POST", "/files", f, gonest.Handle(f.Upload), gonest.RouteConfig{}, gonest.RouteConfig{}),
		route("POST", "/login", f, gonest.Handle(f.Login), gonest.RouteConfig{}, gonest.RouteConfig{
			Status: 200,
			Responses: []gonest.ResponseDoc{
				{Status: 200, Description: "Logged in"},
				{Status: 302, Description: "Redirect"},
			},
		}),
		route("GET", "/files/*path", f, gonest.HandleRaw(f.Download), gonest.RouteConfig{}, gonest.RouteConfig{}),
	}
}
