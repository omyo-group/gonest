package gonest_test

import (
	"net/http"
	"testing"

	"github.com/omyo-group/gonest"
)

type iface interface{ M() }

type impl struct{}

func (impl) M() {}

type enh struct{}

func (enh) CanActivate(*gonest.Ctx) (bool, error)                         { return true, nil }
func (enh) Intercept(_ *gonest.Ctx, n gonest.Next) (any, error)           { return n() }
func (enh) Transform(_ *gonest.Ctx, v any, _ gonest.ArgMeta) (any, error) { return v, nil }
func (enh) Catch(*gonest.Ctx, error) bool                                 { return false }

// TestRootWrappers checks that the root re-exports delegate to core.
func TestRootWrappers(t *testing.T) {
	newImpl := func() impl { return impl{} }
	newEnh := func() enh { return enh{} }
	if p := gonest.Bind[iface](newImpl); p.Token != gonest.TokenOf[iface]() {
		t.Error("Bind")
	}
	if p := gonest.Factory[iface](newImpl); p.Token != gonest.TokenOf[iface]() {
		t.Error("Factory")
	}
	for i, p := range []gonest.Provider{gonest.AppGuard(newEnh), gonest.AppInterceptor(newEnh), gonest.AppPipe(newEnh), gonest.AppFilter(newEnh)} {
		if int(p.Enhancer) != i+1 {
			t.Errorf("enhancer %d = %d", i, p.Enhancer)
		}
	}
	if h := gonest.HandleRaw(func(*gonest.Ctx) error { return nil }); !h.Raw {
		t.Error("HandleRaw")
	}

	cfg := (gonest.RouteConfig{})
	for _, o := range []gonest.RouteOption{
		gonest.UseInterceptors(enh{}), gonest.UsePipes(enh{}), gonest.UseFilters(enh{}),
		gonest.Status(202), gonest.Description("d"), gonest.Deprecated(), gonest.Hidden(),
		gonest.Security("bearer"), gonest.Responds(404, "nf", nil), gonest.SetMetadata("k", 1),
	} {
		o(&cfg)
	}
	if len(cfg.Interceptors) != 1 || len(cfg.Pipes) != 1 || len(cfg.Filters) != 1 || cfg.Status != 202 ||
		cfg.Description != "d" || !cfg.Deprecated || !cfg.Hidden || len(cfg.Security) != 1 ||
		len(cfg.Responses) != 1 || cfg.Metadata["k"] != 1 {
		t.Errorf("route config %+v", cfg)
	}
	if s := gonest.ForPath("/x"); s.Path != "/x" || s.Method != "" {
		t.Error("ForPath")
	}

	errs := []struct {
		err  *gonest.HTTPError
		code int
	}{
		{gonest.NewHTTPError(418, "t"), 418},
		{gonest.BadRequest("b"), http.StatusBadRequest},
		{gonest.Forbidden("f"), http.StatusForbidden},
		{gonest.Conflict("c"), http.StatusConflict},
		{gonest.UnprocessableEntity("u"), http.StatusUnprocessableEntity},
		{gonest.TooManyRequests("t"), http.StatusTooManyRequests},
		{gonest.Internal("i"), http.StatusInternalServerError},
	}
	for _, e := range errs {
		if e.err.Status != e.code {
			t.Errorf("status %d, want %d", e.err.Status, e.code)
		}
	}
}
