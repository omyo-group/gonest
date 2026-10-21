package projects

import (
	"time"

	"github.com/omyo-group/gonest/examples/saas/internal/tenants"
)

// Project statuses.
const (
	StatusActive   = "active"
	StatusArchived = "archived"
)

// Project belongs to exactly one tenant.
type Project struct {
	ID          int       `json:"id" example:"1"`
	TenantID    string    `json:"tenantId" example:"acme"`
	Name        string    `json:"name" example:"Website relaunch"`
	Description string    `json:"description,omitempty"`
	Status      string    `json:"status" validate:"oneof=active archived" example:"active"`
	CreatedAt   time.Time `json:"createdAt"`
}

// Page is a page of results. Generic types get their own OpenAPI schema
// (projects.Page_projects.Project).
type Page[T any] struct {
	Items []T `json:"items"`
	Page  int `json:"page" example:"1"`
	Limit int `json:"limit" example:"20"`
	Total int `json:"total" example:"42"`
}

// docs:inputs

// ListInput is the pagination query DTO. Missing or empty values take the
// default; out of range values fail validation with 400.
type ListInput struct {
	tenants.TenantHeader
	Page   int    `query:"page" default:"1" validate:"min=1" doc:"1-based page number"`
	Limit  int    `query:"limit" default:"20" validate:"min=1,max=100" doc:"page size"`
	Search string `query:"q" validate:"omitempty,max=64" doc:"case-insensitive name filter"`
	Status string `query:"status" validate:"omitempty,oneof=active archived"`
}

// IDInput selects one project.
type IDInput struct {
	tenants.TenantHeader
	ID int `path:"id" validate:"min=1"`
}

// CreateInput is the body of POST /projects.
type CreateInput struct {
	tenants.TenantHeader
	Name        string `json:"name" validate:"required,min=3,max=64" example:"Website relaunch"`
	Description string `json:"description" validate:"max=500"`
}

// UpdateInput is the body of PATCH /projects/:id. Nil pointers mean
// "leave unchanged".
type UpdateInput struct {
	tenants.TenantHeader
	ID          int     `path:"id" validate:"min=1"`
	Name        *string `json:"name" validate:"omitempty,min=3,max=64"`
	Description *string `json:"description" validate:"omitempty,max=500"`
	Status      *string `json:"status" validate:"omitempty,oneof=active archived"`
}

// docs:end
