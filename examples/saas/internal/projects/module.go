package projects

import (
	"github.com/omyo/gonest"
	"github.com/omyo/gonest/examples/saas/internal/auth"
	"github.com/omyo/gonest/examples/saas/internal/tenants"
)

// docs:projects-module

// Module wires the projects feature. Repository is bound to the in-memory
// implementation (NestJS useClass); tenants and auth provide the exported
// TenantService and guards.
var Module = &gonest.Module{
	Name:    "projects",
	Imports: []*gonest.Module{auth.Module, tenants.Module},
	Providers: []gonest.Provider{
		gonest.Bind[Repository](NewMemoryRepository),
		gonest.Provide(NewService),
	},
	Controllers: []any{NewProjectsController},
}

// docs:end
