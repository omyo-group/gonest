package openapi_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/omyo/gonest"
	"github.com/omyo/gonest/openapi"
)

// A global guard that admits only routes marked public must be able to let
// the documentation through, via Options.RouteOptions.
func TestModuleRouteOptions(t *testing.T) {
	const publicKey = "public"
	guard := gonest.GuardFunc(func(ctx *gonest.Ctx) (bool, error) {
		v, _ := ctx.Route().Metadata(publicKey)
		return v == true, nil
	})
	for _, tt := range []struct {
		name   string
		opts   []gonest.RouteOption
		status int
	}{
		{"guarded by default", nil, http.StatusForbidden},
		{"marked public", []gonest.RouteOption{gonest.SetMetadata(publicKey, true)}, http.StatusOK},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root := &gonest.Module{
				Name:    "app",
				Imports: []*gonest.Module{openapi.Module(openapi.Options{RouteOptions: tt.opts})},
			}
			app, err := gonest.New(root, gonest.WithoutStartupLog(), gonest.WithGlobalGuards(guard),
				gonest.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))))
			if err != nil {
				t.Fatal(err)
			}
			for _, path := range []string{"/openapi.json", "/docs", "/docs/swagger-ui.css"} {
				rec := httptest.NewRecorder()
				app.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
				if rec.Code != tt.status {
					t.Errorf("%s: %d, want %d", path, rec.Code, tt.status)
				}
			}
			for _, r := range app.Routes() {
				if !r.ControllerConfig.Hidden && !r.Config.Hidden {
					t.Errorf("%s %s is not hidden", r.Method, r.Path)
				}
			}
		})
	}
}

// In release mode (the gonest default), unguarded documentation is reported
// at bootstrap unless Options.Public acknowledges it.
func TestModuleUnguardedWarning(t *testing.T) {
	guard := gonest.GuardFunc(func(*gonest.Ctx) (bool, error) { return true, nil })
	for _, tt := range []struct {
		name string
		opts openapi.Options
		warn bool
	}{
		{"no options", openapi.Options{}, true},
		{"metadata only", openapi.Options{RouteOptions: []gonest.RouteOption{gonest.SetMetadata("public", true)}}, true},
		{"route guard", openapi.Options{RouteOptions: []gonest.RouteOption{gonest.UseGuards(guard)}}, false},
		{"public", openapi.Options{Public: true}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var buf strings.Builder
			root := &gonest.Module{Name: "app", Imports: []*gonest.Module{openapi.Module(tt.opts)}}
			_, err := gonest.New(root, gonest.WithoutStartupLog(),
				gonest.WithLogger(slog.New(slog.NewTextHandler(&buf, nil))))
			if err != nil {
				t.Fatal(err)
			}
			if gin.Mode() != gin.ReleaseMode {
				t.Skip("GIN_MODE is not release")
			}
			if got := strings.Contains(buf.String(), "served without a guard"); got != tt.warn {
				t.Errorf("warning logged = %v, want %v: %s", got, tt.warn, buf.String())
			}
		})
	}
}
