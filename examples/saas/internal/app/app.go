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
}

// docs:root-module

// NewModule returns the root module. Dynamic modules (config.ForRoot,
// logger.ForRoot, openapi.Module) are functions returning *gonest.Module, so
// the root is built by a function too.
func NewModule(o Options) *gonest.Module {
	return &gonest.Module{
		Name: "app",
		Imports: []*gonest.Module{
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

			openapi.Module(openapi.Options{
				Info: openapi.Info{
					Title:       "Gonest SaaS example",
					Version:     "1.0.0",
					Description: "Multi-tenant projects API. Log in with POST /auth/login, then send the token and X-Tenant-ID.",
				},
				SecuritySchemes: map[string]openapi.SecurityScheme{"bearer": openapi.BearerJWT()},
			}),
		},
		// Request logging for every route except the health probe.
		Configure: func(m *gonest.MiddlewareConsumer, l *slog.Logger) {
			m.Apply(logger.RequestLogger(l)).
				Exclude(gonest.ForPath("/health")).
				ForAllRoutes()
		},
	}
}

// docs:end
