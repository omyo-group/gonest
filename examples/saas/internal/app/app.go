package app

import (
	"io"
	"log/slog"

	"github.com/omyo/gonest"
	"github.com/omyo/gonest/examples/saas/internal/auth"
	"github.com/omyo/gonest/examples/saas/internal/platform"
	"github.com/omyo/gonest/examples/saas/internal/projects"
	"github.com/omyo/gonest/examples/saas/internal/settings"
	"github.com/omyo/gonest/examples/saas/internal/tenants"
	"github.com/omyo/gonest/modules/config"
	"github.com/omyo/gonest/modules/logger"
	"github.com/omyo/gonest/openapi"
)

// Options are the process-level inputs of the root module: where logs go and
// how environment variables are read. Tests pass a fake Lookup and io.Discard.
type Options struct {
	// LogOutput receives the application logs (default os.Stdout).
	LogOutput io.Writer
	// LogLevel is the minimum log level.
	LogLevel slog.Level
	// LogFormat is "json" or "text" (default depends on GIN_MODE).
	LogFormat string
	// Lookup reads environment variables (default os.LookupEnv).
	Lookup func(string) (string, bool)
	// EnvFiles are .env files to load (default ".env", optional).
	EnvFiles []string
	// Docs serves the OpenAPI document and Swagger UI. They map every route
	// of the API, so the server only enables them on request (-docs).
	Docs bool
}

// docs:root-module

// NewModule returns the root module. Dynamic modules (config.ForRoot,
// logger.ForRoot, openapi.Module) are functions returning *gonest.Module, so
// the root is built by a function too.
func NewModule(o Options) *gonest.Module {
	imports := []*gonest.Module{
		// Global modules: *settings.Config and *slog.Logger are injectable everywhere.
		config.ForRoot[settings.Config](config.Options{
			Prefix:   settings.EnvPrefix,
			Lookup:   o.Lookup,
			EnvFiles: o.EnvFiles,
		}),
		logger.ForRoot(logger.Options{Level: o.LogLevel, Format: o.LogFormat, Output: o.LogOutput}),

		platform.Module,
		auth.Module,
		tenants.Module,
		projects.Module,
	}
	if o.Docs {
		imports = append(imports, openapi.Module(openapi.Options{
			Info: openapi.Info{
				Title:       "Gonest SaaS example",
				Version:     "1.0.0",
				Description: "Multi-tenant projects API. Log in with POST /auth/login, then send the token and X-Tenant-ID.",
			},
			SecuritySchemes: map[string]openapi.SecurityScheme{"bearer": openapi.BearerJWT()},
			// Enabled explicitly for local exploration: acknowledge that
			// the documentation is unguarded.
			Public: true,
		}))
	}
	return &gonest.Module{
		Name:      "app",
		Imports:   imports,
		Configure: configure,
	}
}

// docs:end

// docs:root-configure

// configure is the root module's Configure (NestJS configure(consumer)): its
// parameters after the consumer are injected. Rules apply to the routes of
// the whole application; paths are relative to the global prefix and a
// trailing * matches the path itself and everything below it.
func configure(m *gonest.MiddlewareConsumer, l *slog.Logger, tenantSvc *tenants.Service) {
	// Request logging for every route except the health probe.
	m.Apply(logger.RequestLogger(l)).
		Exclude(gonest.ForPath("/health")).
		ForAllRoutes()
	// Tenant resolution is deny by default: every route requires a valid
	// X-Tenant-ID except the tenant-independent ones listed here, so a new
	// controller is tenant-scoped without extra wiring.
	m.Apply(tenantSvc.Middleware()).
		Exclude(
			gonest.ForPath("/health"),
			gonest.ForPath("/auth/*"),
			gonest.ForPath(openapi.DefaultJSONPath),
			gonest.ForPath(openapi.DefaultUIPath+"/*"),
		).
		ForAllRoutes()
}

// docs:end
