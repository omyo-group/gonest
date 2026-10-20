package openapi

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/omyo/gonest"
)

func TestRelativePath(t *testing.T) {
	tests := []struct{ from, to, want string }{
		{"/", "/openapi.json", "openapi.json"},
		{"/api", "/api/openapi.json", "openapi.json"},
		{"/", "/docs/openapi.json", "docs/openapi.json"},
		{"/a/b", "/a/c/spec.json", "../c/spec.json"},
		{"/a/b", "/x.json", "../../x.json"},
		{"/a", "/a/b/c.json", "b/c.json"},
	}
	for _, tt := range tests {
		if got := relativePath(tt.from, tt.to); got != tt.want {
			t.Errorf("relativePath(%q,%q) = %q, want %q", tt.from, tt.to, got, tt.want)
		}
	}
}

func TestCleanPath(t *testing.T) {
	for in, want := range map[string]string{"": "/d", " ": "/d", "x": "/x", "/x/": "/x", "/": "/d", "/a/b": "/a/b"} {
		if got := cleanPath(in, "/d"); got != want {
			t.Errorf("cleanPath(%q) = %q, want %q", in, got, want)
		}
	}
	o := Options{}.normalised()
	if o.JSONPath != DefaultJSONPath || o.UIPath != DefaultUIPath {
		t.Errorf("defaults: %+v", o)
	}
}

func TestServePage(t *testing.T) {
	rec := httptest.NewRecorder()
	servePage(rec, Options{Info: Info{Title: "Shop <API>"}, UIPath: "/api/docs", JSONPath: "/api/openapi.json"})
	body := rec.Body.String()
	for _, want := range []string{
		`href="docs/swagger-ui.css"`, `src="docs/swagger-ui-bundle.js"`, `url: "openapi.json"`,
		"Shop &lt;API&gt; - API docs",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page misses %q:\n%s", want, body)
		}
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("content type %q", ct)
	}
	if strings.Contains(body, "http://") || strings.Contains(body, "https://") {
		t.Error("page must not reference external hosts")
	}
	rec = httptest.NewRecorder()
	servePage(rec, Options{UIPath: "/docs", JSONPath: "/openapi.json"})
	if !strings.Contains(rec.Body.String(), "<title>API - API docs</title>") {
		t.Error("default title missing")
	}
}

func TestServeAsset(t *testing.T) {
	tests := []struct {
		name, method, url, file string
		status                  int
		ctype                   string
	}{
		{"js", "GET", "/docs/swagger-ui-bundle.js", "swagger-ui-bundle.js", 200, "text/javascript; charset=utf-8"},
		{"css", "GET", "/docs/swagger-ui.css", "swagger-ui.css", 200, "text/css; charset=utf-8"},
		{"license", "GET", "/docs/LICENSE", "LICENSE", 200, "text/plain; charset=utf-8"},
		{"head", "HEAD", "/docs/swagger-ui.css", "swagger-ui.css", 200, "text/css; charset=utf-8"},
		{"missing", "GET", "/docs/nope.js", "nope.js", 404, ""},
		{"traversal", "GET", "/docs/x", "../ui.go", 404, ""},
		{"nested", "GET", "/docs/x", "a/b", 404, ""},
		{"root", "GET", "/docs/", "", 302, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			serveAsset(rec, httptest.NewRequest(tt.method, tt.url, nil), tt.file)
			if rec.Code != tt.status {
				t.Fatalf("status %d, want %d", rec.Code, tt.status)
			}
			if tt.ctype != "" && rec.Header().Get("Content-Type") != tt.ctype {
				t.Errorf("content type %q", rec.Header().Get("Content-Type"))
			}
			if tt.status == 200 && tt.method == "GET" && rec.Body.Len() == 0 {
				t.Error("empty body")
			}
			if tt.status == 200 && tt.method == "HEAD" && rec.Body.Len() != 0 {
				t.Error("HEAD wrote a body")
			}
			if tt.name == "root" && rec.Header().Get("Location") != "/docs" {
				t.Errorf("location %q", rec.Header().Get("Location"))
			}
		})
	}
}

func TestControllerBuildAndJSON(t *testing.T) {
	table := &gonest.RouteTable{}
	h := gonest.Handle(func(*gonest.Ctx, gonest.Empty) (string, error) { return "", nil })
	table.Add(&gonest.RouteInfo{Method: "GET", Path: "/ping", Handler: h})
	table.Add(&gonest.RouteInfo{Method: "GET", Path: "/docs", Handler: h, Config: gonest.RouteConfig{Hidden: true}})

	c := &docsController{
		opts:  Options{Info: Info{Title: "T", Version: "9"}, Servers: []Server{{URL: "/"}}, SecuritySchemes: map[string]SecurityScheme{"k": APIKeyHeader("K")}}.normalised(),
		table: table,
	}
	if err := c.OnApplicationBootstrap(t.Context()); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	if err := c.writeJSON(rec); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
		t.Errorf("status %d, type %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	for _, want := range []string{`"/ping"`, `"title": "T"`, `"securitySchemes"`, `"servers"`} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("document misses %s", want)
		}
	}
	if strings.Contains(rec.Body.String(), `"/docs"`) {
		t.Error("hidden route documented")
	}
}

func TestControllerBuildError(t *testing.T) {
	table := &gonest.RouteTable{}
	h := gonest.Handle(func(*gonest.Ctx, gonest.Empty) (string, error) { return "", nil })
	table.Add(&gonest.RouteInfo{Method: "GET", Path: "/p", Handler: h, Config: gonest.RouteConfig{Security: []string{"missing"}}})
	c := &docsController{opts: Options{}.normalised(), table: table}
	if err := c.OnApplicationBootstrap(t.Context()); err == nil {
		t.Fatal("bootstrap must fail on an undefined security scheme")
	}
	rec := httptest.NewRecorder()
	err := c.writeJSON(rec)
	var he *gonest.HTTPError
	if err == nil || !asHTTPError(err, &he) || he.Status != 500 {
		t.Fatalf("writeJSON error = %v", err)
	}
}

func asHTTPError(err error, target **gonest.HTTPError) bool { return errors.As(err, target) }

type recordingRouter struct {
	gonest.Router
	paths []string
}

func (r *recordingRouter) GET(path string, _ *gonest.Handler, opts ...gonest.RouteOption) {
	r.paths = append(r.paths, path)
}

func TestRoutes(t *testing.T) {
	for _, disable := range []bool{false, true} {
		c := &docsController{opts: Options{UIPath: "/api/docs", DisableUI: disable}.normalised()}
		rr := &recordingRouter{}
		c.Routes(rr)
		want := 3
		if disable {
			want = 1
		}
		if len(rr.paths) != want {
			t.Errorf("disable=%v: routes %v", disable, rr.paths)
		}
	}
}

func TestModuleShape(t *testing.T) {
	m := Module(Options{})
	if m.Name != "openapi" || len(m.Controllers) != 1 {
		t.Errorf("module: %+v", m)
	}
}
