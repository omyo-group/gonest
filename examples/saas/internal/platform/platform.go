package platform

import (
	"log/slog"
	"time"

	"github.com/omyo/gonest"
	"github.com/omyo/gonest/modules/logger"
)

// docs:timing-interceptor

// TimingHeader is set on every response produced through the pipeline.
const TimingHeader = "X-Response-Time"

// TimingInterceptor measures the time spent in the rest of the pipeline
// (inner interceptors, binding, pipes and the handler). Registered with
// gonest.AppInterceptor, it is a global interceptor built by the container,
// so its own dependencies are injected.
type TimingInterceptor struct {
	slowAfter time.Duration
}

// NewTimingInterceptor is the provider constructor. The logger parameter is
// not needed at construction; it shows that enhancers are ordinary providers.
func NewTimingInterceptor(l *slog.Logger) *TimingInterceptor {
	l.Debug("timing interceptor ready")
	return &TimingInterceptor{slowAfter: 250 * time.Millisecond}
}

// Intercept implements gonest.Interceptor. The code after next() runs before
// the response is written, so the header still reaches the client, also when
// next returns an error that an exception filter renders.
func (t *TimingInterceptor) Intercept(ctx *gonest.Ctx, next gonest.Next) (any, error) {
	start := time.Now()
	out, err := next()
	d := time.Since(start)
	ctx.Writer().Header().Set(TimingHeader, d.String())
	if d > t.slowAfter {
		// logger.FromCtx returns the request logger (with request_id) set by
		// logger.RequestLogger, or Ctx.Logger() when that middleware is absent.
		logger.FromCtx(ctx).Warn("slow handler", "route", ctx.Route().Path, "duration", d)
	}
	return out, err
}

// docs:end

// HealthStatus is the body of GET /health.
type HealthStatus struct {
	Status string `json:"status" example:"ok"`
}

// HealthController serves a public liveness probe.
type HealthController struct{}

// NewHealthController is the controller constructor.
func NewHealthController() *HealthController { return &HealthController{} }

// Routes implements gonest.Controller.
func (c *HealthController) Routes(r gonest.Router) {
	r.Prefix("/health")
	r.GET("", gonest.Handle(c.Check), gonest.Tags("platform"), gonest.Summary("Liveness probe"))
}

// Check always reports ok.
func (c *HealthController) Check(_ *gonest.Ctx, _ gonest.Empty) (HealthStatus, error) {
	return HealthStatus{Status: "ok"}, nil
}

// Module provides the global timing interceptor and the health endpoint.
var Module = &gonest.Module{
	Name:        "platform",
	Providers:   []gonest.Provider{gonest.AppInterceptor(NewTimingInterceptor)},
	Controllers: []any{NewHealthController},
}
