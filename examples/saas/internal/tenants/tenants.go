package tenants

import (
	"net/http"
	"sort"

	"github.com/gin-gonic/gin"
	"github.com/omyo-group/gonest"
	"github.com/omyo-group/gonest/examples/saas/internal/auth"
)

// HeaderName is the request header that selects the tenant.
const HeaderName = "X-Tenant-ID"

// tenantKey is the request value key under which the middleware stores the *Tenant.
const tenantKey = "tenants.tenant"

// Tenant is a customer organisation.
type Tenant struct {
	ID          string `json:"id" example:"acme"`
	Name        string `json:"name" example:"Acme Corp"`
	Plan        string `json:"plan" example:"starter"`
	MaxProjects int    `json:"maxProjects" example:"5"`
}

// Service looks tenants up. It is exported by Module, so other modules can
// inject it (the projects module reads plan limits from it).
type Service struct {
	byID map[string]Tenant
}

// NewService returns a service seeded with two tenants.
func NewService() *Service {
	return &Service{byID: map[string]Tenant{
		"acme":   {ID: "acme", Name: "Acme Corp", Plan: "starter", MaxProjects: 5},
		"globex": {ID: "globex", Name: "Globex", Plan: "business", MaxProjects: 100},
	}}
}

// Find returns the tenant with id.
func (s *Service) Find(id string) (Tenant, bool) {
	t, ok := s.byID[id]
	return t, ok
}

// IDs lists the known tenant ids, sorted.
func (s *Service) IDs() []string {
	ids := make([]string, 0, len(s.byID))
	for id := range s.byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// docs:tenant-middleware

// Middleware is plain gin middleware (NestMiddleware). It resolves the tenant
// from the X-Tenant-ID header and stores it in the request values; missing
// or unknown tenants are rejected before guards run.
func (s *Service) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader(HeaderName)
		if id == "" {
			c.AbortWithStatusJSON(http.StatusBadRequest, gonest.BadRequest("missing "+HeaderName+" header").Body())
			return
		}
		t, ok := s.Find(id)
		if !ok {
			c.AbortWithStatusJSON(http.StatusNotFound, gonest.NotFound("unknown tenant").Body())
			return
		}
		c.Set(tenantKey, &t)
		c.Next()
	}
}

// docs:end

// FromCtx returns the tenant resolved by the middleware for this request.
func FromCtx(ctx *gonest.Ctx) (*Tenant, bool) {
	return gonest.CtxValue[*Tenant](ctx, tenantKey)
}

// MemberGuard allows the request only when the authenticated user belongs to
// the tenant selected by the header. It runs after auth.JWTGuard.
var MemberGuard = gonest.GuardFunc(func(ctx *gonest.Ctx) (bool, error) {
	t, ok := FromCtx(ctx)
	if !ok {
		return false, gonest.BadRequest("tenant not resolved")
	}
	u, ok := auth.CurrentUser(ctx)
	if !ok {
		return false, gonest.Unauthorized("not authenticated")
	}
	if u.TenantID != t.ID {
		return false, gonest.Forbidden("user does not belong to tenant " + t.ID)
	}
	return true, nil
})

// TenantHeader documents the X-Tenant-ID header in OpenAPI. Embed it in a
// handler input: the binder and the OpenAPI generator flatten embedded
// structs. The middleware has already checked the value; the validate tag
// only marks the header as required in the document.
type TenantHeader struct {
	TenantID string `header:"X-Tenant-ID" validate:"required" doc:"Tenant selector" example:"acme"`
}

// TenantsController exposes the current tenant.
type TenantsController struct {
	jwt *auth.JWTGuard
}

// NewTenantsController is the controller constructor.
func NewTenantsController(jwt *auth.JWTGuard) *TenantsController {
	return &TenantsController{jwt: jwt}
}

// Routes implements gonest.Controller.
func (c *TenantsController) Routes(r gonest.Router) {
	r.Prefix("/tenants")
	r.Use(gonest.Tags("tenants"), gonest.Security("bearer"), gonest.UseGuards(c.jwt, MemberGuard))
	r.GET("/current", gonest.Handle(c.Current), gonest.Summary("The tenant selected by X-Tenant-ID"))
}

// CurrentInput only documents the tenant header.
type CurrentInput struct {
	TenantHeader
}

// Current returns the tenant resolved by the middleware.
func (c *TenantsController) Current(ctx *gonest.Ctx, _ CurrentInput) (Tenant, error) {
	t, ok := FromCtx(ctx)
	if !ok {
		return Tenant{}, gonest.BadRequest("tenant not resolved")
	}
	return *t, nil
}

// docs:tenants-module

// Module provides and exports Service. The root module applies
// Service.Middleware to every tenant-scoped route of the application.
var Module = &gonest.Module{
	Name:        "tenants",
	Imports:     []*gonest.Module{auth.Module},
	Providers:   []gonest.Provider{gonest.Provide(NewService)},
	Controllers: []any{NewTenantsController},
	Exports:     []gonest.Token{gonest.TokenOf[*Service]()},
}

// docs:end
