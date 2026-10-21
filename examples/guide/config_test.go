package guide_test

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/omyo-group/gonest"
	"github.com/omyo-group/gonest/modules/config"
	"github.com/omyo-group/gonest/modules/logger"
)

// docs:config-types

// AppConfig is the root configuration. Field names become UPPER_SNAKE
// variables unless `env` names them; nested structs get a prefix.
type AppConfig struct {
	Port     int           `default:"8080" validate:"min=1,max=65535"` // PORT
	APIKey   string        `secret:"true" validate:"required"`         // API_KEY
	Timeout  time.Duration `env:"HTTP_TIMEOUT" default:"5s"`
	Origins  []string      `default:"http://localhost:3000"` // comma separated
	Database DBConfig      `envPrefix:"DB_"`
}

// DBConfig is a nested section: DB_HOST, DB_POOL_SIZE.
type DBConfig struct {
	Host     string `default:"localhost"`
	PoolSize int    `default:"10"`
}

// docs:end

func env(m map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) { v, ok := m[k]; return v, ok }
}

func Example_configLoad() {
	dir, _ := os.MkdirTemp("", "cfg")
	defer os.RemoveAll(dir)
	envFile := filepath.Join(dir, ".env")
	_ = os.WriteFile(envFile, []byte("APP_API_KEY=from-dotenv\nAPP_DB_HOST=db.local\n"), 0o600)

	// docs:config-load
	opts := config.Options{
		Prefix:   "APP_",                                     // APP_PORT, APP_DB_HOST, ...
		EnvFiles: []string{envFile},                          // default: .env (optional)
		Lookup:   env(map[string]string{"APP_PORT": "9000"}), // default: os.LookupEnv
	}
	cfg, err := config.Load[AppConfig](opts)
	// docs:end
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(cfg.Port, cfg.Timeout, cfg.Origins, cfg.Database.Host, cfg.Database.PoolSize)
	fmt.Println(config.Redacted(cfg))

	_, err = config.Load[AppConfig](config.Options{
		Prefix: "APP_", EnvFiles: []string{},
		Lookup: env(map[string]string{"APP_PORT": "0", "APP_DB_POOL_SIZE": "many"}),
	})
	fmt.Println(err)
	// Output:
	// 9000 5s [http://localhost:3000] db.local 10
	// AppConfig{Port:9000, APIKey:******, Timeout:5s, Origins:[http://localhost:3000], Database:DBConfig{Host:db.local, PoolSize:10}}
	// config: 1 problem(s): APP_DB_POOL_SIZE: invalid value, want int
}

func Example_configModule() {
	// docs:config-module
	root := &gonest.Module{
		Name: "app",
		Imports: []*gonest.Module{
			// Global: *AppConfig is injectable in every module.
			config.ForRoot[AppConfig](config.Options{Prefix: "APP_", Lookup: env(map[string]string{"APP_API_KEY": "k"})}),
			// Not global: import it where *DBConfig is needed.
			config.ForFeature[DBConfig](config.Options{Prefix: "APP_DB_", Lookup: env(nil)}),
		},
	}
	// docs:end
	app, err := gonest.New(root, quiet()...)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(gonest.MustGet[*AppConfig](app).Port, gonest.MustGet[*DBConfig](app).Host)

	_, err = gonest.New(&gonest.Module{Name: "app", Imports: []*gonest.Module{
		config.ForRoot[AppConfig](config.Options{Lookup: env(nil), EnvFiles: []string{}}),
	}}, quiet()...)
	fmt.Println(err)
	// Output:
	// 8080 localhost
	// gonest: cannot resolve *guide_test.AppConfig in module config: constructor failed: config: 1 problem(s): API_KEY: failed validation "required"
}

func Example_logger() {
	var buf bytes.Buffer
	// docs:logger-module
	root := &gonest.Module{
		Name: "app",
		Imports: []*gonest.Module{
			// Global *slog.Logger for every module (it does not call slog.SetDefault).
			logger.ForRoot(logger.Options{Format: "json", Level: slog.LevelInfo, Output: &buf}),
		},
		Controllers: []any{NewPingController},
		Configure: func(m *gonest.MiddlewareConsumer, l *slog.Logger) {
			// One log record per request, plus X-Request-ID propagation.
			m.Apply(logger.RequestLogger(l)).ForAllRoutes()
		},
	}
	// docs:end
	app, err := gonest.New(root, gonest.WithoutStartupLog())
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(call(app, "GET", "/ping", "", "X-Request-ID", "abc-123"))
	fmt.Println(call(app, "GET", "/ping?fail=true", "", "X-Request-ID", "def-456"))
	// Normalise the variable parts of the records (time, latency).
	out := regexp.MustCompile(`"time":"[^"]+"`).ReplaceAllString(buf.String(), `"time":"T"`)
	out = regexp.MustCompile(`"latency":\d+`).ReplaceAllString(out, `"latency":N`)
	fmt.Print(strings.ReplaceAll(out, `"client_ip":"192.0.2.1"`, `"client_ip":"IP"`))
	// Output:
	// 200 "pong"
	// 500 {"statusCode":500,"message":"Internal Server Error","error":"Internal Server Error"}
	// {"time":"T","level":"INFO","msg":"ping handled","request_id":"abc-123"}
	// {"time":"T","level":"INFO","msg":"request","request_id":"abc-123","method":"GET","path":"/ping","status":200,"latency":N,"bytes":6,"client_ip":"IP"}
	// {"time":"T","level":"INFO","msg":"ping handled","request_id":"def-456"}
	// {"time":"T","level":"ERROR","msg":"gonest: unhandled error","request_id":"def-456","method":"GET","path":"/ping","error":"database unavailable"}
	// {"time":"T","level":"ERROR","msg":"request","request_id":"def-456","method":"GET","path":"/ping","status":500,"latency":N,"bytes":84,"client_ip":"IP"}
}

// docs:logger-handler

// PingController logs through the request logger.
type PingController struct{ log *slog.Logger }

// NewPingController receives the global *slog.Logger.
func NewPingController(l *slog.Logger) *PingController { return &PingController{log: l} }

// Routes implements gonest.Controller.
func (c *PingController) Routes(r gonest.Router) { r.GET("/ping", gonest.Handle(c.Ping)) }

// Ping logs with the per-request logger, which carries request_id.
func (c *PingController) Ping(ctx *gonest.Ctx, in PingInput) (string, error) {
	logger.FromCtx(ctx).InfoContext(ctx, "ping handled")
	if in.Fail {
		return "", errors.New("database unavailable") // logged by the framework, sent as 500
	}
	return "pong", nil
}

// docs:end

// PingInput lets the example fail on demand.
type PingInput struct {
	Fail bool `query:"fail"`
}
