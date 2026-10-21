// Command server runs the Gonest SaaS example.
//
//	SAAS_JWT_SECRET=$(openssl rand -hex 32) go run ./examples/saas/cmd/server -docs
//
// SAAS_JWT_SECRET is required (see examples/saas/.env.example). With -docs,
// Swagger UI is served at http://localhost:8080/docs.
package main

import (
	"flag"
	"fmt"
	"log"
	"log/slog"
	"time"

	"github.com/omyo/gonest"
	"github.com/omyo/gonest/examples/saas/internal/app"
	"github.com/omyo/gonest/examples/saas/internal/settings"
	"github.com/omyo/gonest/modules/config"
)

// docs:main

func main() {
	debug := flag.Bool("debug", false, "log at debug level")
	docs := flag.Bool("docs", false, "serve the OpenAPI document and Swagger UI")
	flag.Parse()
	if err := run(*debug, *docs); err != nil {
		log.Fatal(err)
	}
}

func run(debug, docs bool) error {
	level := slog.LevelInfo
	if debug {
		level = slog.LevelDebug
	}
	// New validates the whole graph (configuration included), mounts every
	// route and runs OnModuleInit / OnApplicationBootstrap. Errors are
	// returned, never panicked.
	application, err := gonest.New(
		app.NewModule(app.Options{LogLevel: level, Docs: docs}),
		gonest.WithShutdownTimeout(15*time.Second),
	)
	if err != nil {
		return err
	}
	cfg, err := gonest.Get[*settings.Config](application)
	if err != nil {
		return err
	}
	logger := gonest.MustGet[*slog.Logger](application)
	logger.Info("configuration", "config", config.Redacted(cfg))

	// Listen blocks until SIGINT/SIGTERM, drains in-flight requests (up to the
	// shutdown timeout), then runs OnModuleDestroy,
	// BeforeApplicationShutdown and OnApplicationShutdown hooks.
	if err := application.Listen(fmt.Sprintf(":%d", cfg.Port)); err != nil {
		return err
	}
	logger.Info("bye")
	return nil
}

// docs:end
