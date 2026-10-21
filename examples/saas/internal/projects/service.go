package projects

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/omyo-group/gonest"
	"github.com/omyo-group/gonest/examples/saas/internal/tenants"
)

// Service holds the project business rules.
type Service struct {
	repo    Repository
	tenants *tenants.Service
	log     *slog.Logger
}

// docs:service-ctor

// NewService is the provider constructor. Repository is an interface bound
// in the module; *tenants.Service is exported by the imported tenants
// module; *slog.Logger comes from the global logger module.
func NewService(repo Repository, ts *tenants.Service, log *slog.Logger) *Service {
	return &Service{repo: repo, tenants: ts, log: log.With("module", "projects")}
}

// docs:end

// docs:lifecycle

// OnModuleInit seeds demo data once the module's providers are built
// (NestJS OnModuleInit).
func (s *Service) OnModuleInit(ctx context.Context) error {
	seed := map[string][]string{
		"acme":   {"Website relaunch", "Mobile app", "Data warehouse"},
		"globex": {"Moonshot"},
	}
	for _, id := range s.tenants.IDs() {
		for _, name := range seed[id] {
			if _, err := s.Create(ctx, id, name, ""); err != nil {
				return fmt.Errorf("seed %s/%s: %w", id, name, err)
			}
		}
	}
	s.log.InfoContext(ctx, "seeded projects", "tenants", len(seed))
	return nil
}

// OnApplicationShutdown runs last during graceful shutdown; a real service
// would flush buffers or close connections here.
func (s *Service) OnApplicationShutdown(ctx context.Context) error {
	s.log.InfoContext(ctx, "projects service stopped")
	return nil
}

// docs:end

// List returns one page of the tenant's projects.
func (s *Service) List(_ context.Context, tenantID string, in ListInput) Page[Project] {
	all := s.repo.List(tenantID)
	q := strings.ToLower(in.Search)
	filtered := all[:0:0]
	for _, p := range all {
		if q != "" && !strings.Contains(strings.ToLower(p.Name), q) {
			continue
		}
		if in.Status != "" && p.Status != in.Status {
			continue
		}
		filtered = append(filtered, p)
	}
	page := Page[Project]{Items: []Project{}, Page: in.Page, Limit: in.Limit, Total: len(filtered)}
	start := (in.Page - 1) * in.Limit
	if start < len(filtered) {
		page.Items = filtered[start:min(start+in.Limit, len(filtered))]
	}
	return page
}

// Get returns one project or a 404 error.
func (s *Service) Get(_ context.Context, tenantID string, id int) (Project, error) {
	p, ok := s.repo.Get(tenantID, id)
	if !ok {
		return Project{}, gonest.NotFound(fmt.Sprintf("project %d not found", id))
	}
	return p, nil
}

// Create adds a project. Duplicate names give 409; exceeding the tenant's
// plan gives a *DomainError (422 through DomainErrorFilter).
func (s *Service) Create(_ context.Context, tenantID, name, description string) (Project, error) {
	t, ok := s.tenants.Find(tenantID)
	if !ok {
		return Project{}, gonest.NotFound("unknown tenant")
	}
	existing := s.repo.List(tenantID)
	if err := checkUniqueName(existing, name, 0); err != nil {
		return Project{}, err
	}
	if len(existing) >= t.MaxProjects {
		return Project{}, &DomainError{
			Code:    "quota_exceeded",
			Message: fmt.Sprintf("plan %q allows %d projects", t.Plan, t.MaxProjects),
		}
	}
	p := s.repo.Insert(Project{
		TenantID:    tenantID,
		Name:        name,
		Description: description,
		Status:      StatusActive,
		CreatedAt:   time.Now().UTC(),
	})
	return p, nil
}

// Update applies a partial update. Archived projects cannot be renamed
// (domain rule, 422).
func (s *Service) Update(ctx context.Context, tenantID string, in UpdateInput) (Project, error) {
	p, err := s.Get(ctx, tenantID, in.ID)
	if err != nil {
		return Project{}, err
	}
	if in.Name != nil && *in.Name != p.Name {
		if p.Status == StatusArchived && (in.Status == nil || *in.Status == StatusArchived) {
			return Project{}, &DomainError{Code: "project_archived", Message: "archived projects cannot be renamed"}
		}
		if err := checkUniqueName(s.repo.List(tenantID), *in.Name, p.ID); err != nil {
			return Project{}, err
		}
		p.Name = *in.Name
	}
	if in.Description != nil {
		p.Description = *in.Description
	}
	if in.Status != nil {
		p.Status = *in.Status
	}
	if !s.repo.Update(p) {
		return Project{}, gonest.NotFound(fmt.Sprintf("project %d not found", in.ID))
	}
	return p, nil
}

// Delete removes a project or returns 404.
func (s *Service) Delete(_ context.Context, tenantID string, id int) error {
	if !s.repo.Delete(tenantID, id) {
		return gonest.NotFound(fmt.Sprintf("project %d not found", id))
	}
	return nil
}

func checkUniqueName(existing []Project, name string, selfID int) error {
	for _, p := range existing {
		if p.ID != selfID && strings.EqualFold(p.Name, name) {
			return gonest.Conflict(fmt.Sprintf("a project named %q already exists", name))
		}
	}
	return nil
}
