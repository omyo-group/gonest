# Logging

Gonest logs with the standard library's `log/slog`.

## The framework logger

Every application has a `*slog.Logger` provider: the one given with
`gonest.WithLogger(l)`, else `slog.Default()`. It is a fallback: when a
module provides its own `*slog.Logger` (for example the global
`logger.ForRoot`), that one is injected instead wherever it is visible, and
the logger resolved from the root module becomes the application logger.
The application logger prints the startup route table and the framework's
error logs (unhandled errors, 5xx `HTTPError`s, recovered panics).
`gonest.WithoutStartupLog()` silences the route table.

## modules/logger

`github.com/omyo/gonest/modules/logger` provides:

- `logger.New(opts)`: a `*slog.Logger` writing text or JSON (`Format`; the
  default is JSON when `GIN_MODE=release`, text otherwise), with `Level`,
  `Output` (default stdout), `AddSource` and static `Attrs`.
- `logger.ForRoot(opts)`: the global module `"logger"` providing that logger.
  It does not change `slog.Default()`.
- `logger.RequestLogger(l)`: Gin middleware that reads `X-Request-ID` (or
  generates a random id; incoming ids must be printable ASCII up to 128
  bytes), echoes it in the response, stores a child logger carrying
  `request_id`, and writes one record per request with method, path, status,
  latency, bytes and client IP, at error level for 5xx and warn for 4xx.
- `logger.FromCtx(ctx)`: the request logger stored by the middleware, or
  `ctx.Logger()` when the middleware did not run.

<!-- snippet: examples/guide/config_test.go#logger-module -->
```go
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
```

<!-- snippet: examples/guide/config_test.go#logger-handler -->
```go
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
```

`RequestLogger` does not change `ctx.Logger()`, which the framework uses for
its own error logs. To get `request_id` on those too, install an
interceptor:

<!-- snippet: examples/guide/config_test.go#logger-interceptor -->
```go
// RequestLoggerInterceptor makes ctx.Logger() the request logger stored by
// logger.RequestLogger, so the framework's own error logs carry request_id.
// Install it with gonest.WithGlobalInterceptors or gonest.AppInterceptor.
var RequestLoggerInterceptor = gonest.InterceptorFunc(func(ctx *gonest.Ctx, next gonest.Next) (any, error) {
	ctx.SetLogger(logger.FromCtx(ctx))
	return next()
})
```

Two requests, the second failing, produce (time and latency normalised):

```text
{"time":"T","level":"INFO","msg":"ping handled","request_id":"abc-123"}
{"time":"T","level":"INFO","msg":"request","request_id":"abc-123","method":"GET","path":"/ping","status":200,"latency":N,"bytes":6,"client_ip":"IP"}
{"time":"T","level":"INFO","msg":"ping handled","request_id":"def-456"}
{"time":"T","level":"ERROR","msg":"gonest: unhandled error","request_id":"def-456","method":"GET","path":"/ping","error":"database unavailable"}
{"time":"T","level":"ERROR","msg":"request","request_id":"def-456","method":"GET","path":"/ping","status":500,"latency":N,"bytes":84,"client_ip":"IP"}
```

The client of the failing request only receives
`{"statusCode":500,"message":"Internal Server Error","error":"Internal Server Error"}`.

Guards run before interceptors, so errors raised by guards are still logged
with the application logger.

## In the SaaS example

The root module imports `logger.ForRoot` and applies `RequestLogger` to every
route except `/health` (see [Modules](modules.md#dynamic-modules)); services
receive `*slog.Logger` by injection, and the timing interceptor logs slow
requests with `logger.FromCtx`.
