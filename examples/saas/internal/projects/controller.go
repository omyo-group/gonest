package projects

import (
	"net/http"
	"strings"

	"github.com/omyo/gonest"
	"github.com/omyo/gonest/examples/saas/internal/auth"
	"github.com/omyo/gonest/examples/saas/internal/tenants"
)

// docs:controller-deps

// ControllerDeps is a parameter object (struct embedding gonest.In): each
// field is injected by type, like constructor parameters in NestJS.
type ControllerDeps struct {
	gonest.In
	Service *Service
	JWT     *auth.JWTGuard
	Roles   *auth.RolesGuard
}

// ProjectsController serves /projects.
type ProjectsController struct {
	svc   *Service
	jwt   *auth.JWTGuard
	roles *auth.RolesGuard
}

// NewProjectsController is the controller constructor.
func NewProjectsController(d ControllerDeps) *ProjectsController {
	return &ProjectsController{svc: d.Service, jwt: d.JWT, roles: d.Roles}
}

// docs:end

// docs:routes

// Routes implements gonest.Controller. Options passed to r.Use apply to every
// route below it (controller-level decorators in NestJS).
func (c *ProjectsController) Routes(r gonest.Router) {
	r.Prefix("/projects")
	r.Use(
		gonest.Tags("projects"),
		gonest.Security("bearer"),
		// Guards run in order: authenticate, check tenant membership, check roles.
		gonest.UseGuards(c.jwt, tenants.MemberGuard, c.roles),
		gonest.UseFilters(DomainErrorFilter),
	)

	r.GET("", gonest.Handle(c.List), gonest.Summary("List projects"))
	r.POST("", gonest.Handle(c.Create),
		gonest.Summary("Create a project"),
		gonest.UsePipes(TrimPipe),
		gonest.Responds(http.StatusConflict, "Name already used", gonest.ErrorBody{}),
		gonest.Responds(http.StatusUnprocessableEntity, "Plan quota exceeded", gonest.ErrorBody{}))
	r.GET("/:id", gonest.Handle(c.FindOne),
		gonest.Summary("Get a project"),
		gonest.Responds(http.StatusNotFound, "Not found", gonest.ErrorBody{}))
	r.PATCH("/:id", gonest.Handle(c.Update),
		gonest.Summary("Update a project"),
		gonest.Responds(http.StatusNotFound, "Not found", gonest.ErrorBody{}),
		gonest.Responds(http.StatusConflict, "Name already used", gonest.ErrorBody{}),
		gonest.Responds(http.StatusUnprocessableEntity, "Archived project", gonest.ErrorBody{}))
	r.DELETE("/:id", gonest.Handle(c.Delete),
		gonest.Summary("Delete a project (admins only)"),
		auth.Roles(auth.RoleAdmin),
		gonest.Responds(http.StatusNotFound, "Not found", gonest.ErrorBody{}))
}

// docs:end

// docs:handlers

// List returns a page of the current tenant's projects.
func (c *ProjectsController) List(ctx *gonest.Ctx, in ListInput) (Page[Project], error) {
	t, err := tenantOf(ctx)
	if err != nil {
		return Page[Project]{}, err
	}
	return c.svc.List(ctx, t, in), nil
}

// FindOne returns one project.
func (c *ProjectsController) FindOne(ctx *gonest.Ctx, in IDInput) (Project, error) {
	t, err := tenantOf(ctx)
	if err != nil {
		return Project{}, err
	}
	return c.svc.Get(ctx, t, in.ID)
}

// Create adds a project (201 Created, the default for POST).
func (c *ProjectsController) Create(ctx *gonest.Ctx, in CreateInput) (Project, error) {
	t, err := tenantOf(ctx)
	if err != nil {
		return Project{}, err
	}
	return c.svc.Create(ctx, t, in.Name, in.Description)
}

// Update changes a project.
func (c *ProjectsController) Update(ctx *gonest.Ctx, in UpdateInput) (Project, error) {
	t, err := tenantOf(ctx)
	if err != nil {
		return Project{}, err
	}
	return c.svc.Update(ctx, t, in)
}

// Delete removes a project and answers 204 No Content.
func (c *ProjectsController) Delete(ctx *gonest.Ctx, in IDInput) (gonest.NoContent, error) {
	t, err := tenantOf(ctx)
	if err != nil {
		return gonest.NoContent{}, err
	}
	return gonest.NoContent{}, c.svc.Delete(ctx, t, in.ID)
}

// docs:end

func tenantOf(ctx *gonest.Ctx) (string, error) {
	t, ok := tenants.FromCtx(ctx)
	if !ok {
		return "", gonest.BadRequest("tenant not resolved")
	}
	return t.ID, nil
}

// docs:pipe

// TrimPipe trims the name and description of a CreateInput. Pipes run after
// binding and validation and must return a value of the same type
// (meta.Type), or an error.
var TrimPipe = gonest.PipeFunc(func(_ *gonest.Ctx, v any, _ gonest.ArgMeta) (any, error) {
	in, ok := v.(CreateInput)
	if !ok {
		return v, nil
	}
	in.Name = strings.TrimSpace(in.Name)
	in.Description = strings.TrimSpace(in.Description)
	if len(in.Name) < 3 {
		return nil, gonest.BadRequest("name must have at least 3 non-blank characters")
	}
	return in, nil
})

// docs:end
