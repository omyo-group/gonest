// Command server runs the Gonest SaaS example.
//
//	go run ./examples/saas/cmd/server
//	SAAS_PORT=9090 SAAS_JWT_SECRET=... go run ./examples/saas/cmd/server
//
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
	flag.Parse()
	if err := run(*debug); err != nil {
		log.Fatal(err)
	}
}

func run(debug bool) error {
	level := slog.LevelInfo
	if debug {
		level = slog.LevelDebug
	}
	// New validates the whole graph (configuration included), mounts every
	// route and runs OnModuleInit / OnApplicationBootstrap. Errors are
	// returned, never panicked.
	application, err := gonest.New(
		app.NewModule(app.Options{LogLevel: level}),
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
