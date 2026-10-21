package app_test

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/omyo-group/gonest/examples/saas/internal/app"
	"github.com/omyo-group/gonest/examples/saas/internal/auth"
	"github.com/omyo-group/gonest/examples/saas/internal/projects"
	"github.com/omyo-group/gonest/gonesttest"
)

// testSecret is the JWT secret of the fake environment.
const testSecret = "test-only-secret-0123456789abcdef"

// testDefaults complete the fake environment: a JWT secret (required) and
// the cheapest bcrypt cost, so that logins stay fast under -race.
var testDefaults = map[string]string{
	"SAAS_JWT_SECRET":  testSecret,
	"SAAS_BCRYPT_COST": "4",
}

// env is a fake environment: tests never read the real one or a .env file.
// vars take precedence over testDefaults.
func env(vars map[string]string) app.Options {
	return app.Options{
		LogOutput: io.Discard,
		EnvFiles:  []string{},
		Docs:      true,
		Lookup: func(k string) (string, bool) {
			if v, ok := vars[k]; ok {
				return v, true
			}
			v, ok := testDefaults[k]
			return v, ok
		},
	}
}

// docs:testing-app

func newApp(t *testing.T) *gonesttest.App {
	t.Helper()
	return gonesttest.New(t, app.NewModule(env(nil))).Compile()
}

func login(t *testing.T, a *gonesttest.App, email string) string {
	t.Helper()
	var tok auth.TokenResponse
	a.Request("POST", "/auth/login").
		WithJSON(auth.LoginInput{Email: email, Password: auth.DemoPassword}).
		Do().
		ExpectStatus(http.StatusOK).
		JSON(&tok)
	return tok.AccessToken
}

func TestCreateProject(t *testing.T) {
	a := newApp(t)
	tok := login(t, a, "alice@acme.test")

	var p projects.Project
	a.Request("POST", "/projects").
		WithBearer(tok).
		WithHeader("X-Tenant-ID", "acme").
		WithJSON(map[string]string{"name": "  Billing  "}).
		Do().
		ExpectStatus(http.StatusCreated).
		JSON(&p)
	if p.Name != "Billing" || p.TenantID != "acme" || p.Status != projects.StatusActive {
		t.Fatalf("unexpected project %+v", p)
	}
}

// docs:end

// TestEndpoints covers the HTTP behaviour of every route. Each case gets a
// fresh application, so state changes do not leak between cases.
func TestEndpoints(t *testing.T) {
	type call struct {
		method, path, user, tenant string
		body                       any
	}
	cases := []struct {
		name    string
		setup   []call
		req     call
		status  int
		message string         // substring of the error message or details
		json    map[string]any // subset of top-level fields to compare
	}{
		{name: "health is public", req: call{method: "GET", path: "/health"}, status: 200, json: map[string]any{"status": "ok"}},
		{name: "login rejects bad password", req: call{method: "POST", path: "/auth/login", body: auth.LoginInput{Email: "alice@acme.test", Password: "nope"}}, status: 401, message: "invalid credentials"},
		{name: "login validates email", req: call{method: "POST", path: "/auth/login", body: map[string]string{"email": "x", "password": "y"}}, status: 400, message: "email"},
		{name: "login rejects malformed JSON", req: call{method: "POST", path: "/auth/login", body: "{"}, status: 400, message: "invalid JSON"},
		{name: "me returns current user", req: call{method: "GET", path: "/auth/me", user: "bob@acme.test"}, status: 200, json: map[string]any{"id": "u2", "tenantId": "acme"}},
		{name: "me requires token", req: call{method: "GET", path: "/auth/me"}, status: 401, message: "missing bearer token"},
		{name: "current tenant", req: call{method: "GET", path: "/tenants/current", user: "carol@globex.test", tenant: "globex"}, status: 200, json: map[string]any{"id": "globex", "plan": "business"}},

		{name: "list uses pagination defaults", req: call{method: "GET", path: "/projects", user: "bob@acme.test", tenant: "acme"}, status: 200, json: map[string]any{"page": 1, "limit": 20, "total": 3}},
		{name: "list second page", req: call{method: "GET", path: "/projects?page=2&limit=2", user: "bob@acme.test", tenant: "acme"}, status: 200, json: map[string]any{"page": 2, "limit": 2, "total": 3}},
		{name: "list empty value takes default", req: call{method: "GET", path: "/projects?limit=", user: "bob@acme.test", tenant: "acme"}, status: 200, json: map[string]any{"limit": 20}},
		{name: "list search", req: call{method: "GET", path: "/projects?q=MOBILE", user: "bob@acme.test", tenant: "acme"}, status: 200, json: map[string]any{"total": 1}},
		{name: "list limit too large", req: call{method: "GET", path: "/projects?limit=500", user: "bob@acme.test", tenant: "acme"}, status: 400, message: "limit"},
		{name: "list page not a number", req: call{method: "GET", path: "/projects?page=abc", user: "bob@acme.test", tenant: "acme"}, status: 400, message: "page"},
		{name: "list bad status", req: call{method: "GET", path: "/projects?status=deleted", user: "bob@acme.test", tenant: "acme"}, status: 400, message: "status"},
		{name: "tenant header required", req: call{method: "GET", path: "/projects", user: "bob@acme.test"}, status: 400, message: "X-Tenant-ID"},
		{name: "unknown tenant", req: call{method: "GET", path: "/projects", user: "bob@acme.test", tenant: "initech"}, status: 404, message: "unknown tenant"},
		{name: "token required", req: call{method: "GET", path: "/projects", tenant: "acme"}, status: 401, message: "missing bearer token"},
		{name: "other tenant forbidden", req: call{method: "GET", path: "/projects", user: "bob@acme.test", tenant: "globex"}, status: 403, message: "does not belong"},

		{name: "get one", req: call{method: "GET", path: "/projects/2", user: "bob@acme.test", tenant: "acme"}, status: 200, json: map[string]any{"id": 2, "name": "Mobile app"}},
		{name: "get missing", req: call{method: "GET", path: "/projects/99", user: "bob@acme.test", tenant: "acme"}, status: 404, message: "project 99 not found"},
		{name: "get other tenant's project", req: call{method: "GET", path: "/projects/4", user: "bob@acme.test", tenant: "acme"}, status: 404},
		{name: "get id must be positive", req: call{method: "GET", path: "/projects/0", user: "bob@acme.test", tenant: "acme"}, status: 400, message: "id"},

		{name: "create", req: call{method: "POST", path: "/projects", user: "bob@acme.test", tenant: "acme", body: map[string]string{"name": "  Billing  "}}, status: 201, json: map[string]any{"id": 5, "name": "Billing", "status": "active"}},
		{name: "create validates", req: call{method: "POST", path: "/projects", user: "bob@acme.test", tenant: "acme", body: map[string]string{"name": "x"}}, status: 400, message: "name"},
		{name: "create pipe rejects blank name", req: call{method: "POST", path: "/projects", user: "bob@acme.test", tenant: "acme", body: map[string]string{"name": "  a   "}}, status: 400, message: "non-blank"},
		{name: "create duplicate", req: call{method: "POST", path: "/projects", user: "bob@acme.test", tenant: "acme", body: map[string]string{"name": "mobile APP"}}, status: 409, message: "already exists"},
		{
			name: "create over quota is 422",
			setup: []call{
				{method: "POST", path: "/projects", user: "bob@acme.test", tenant: "acme", body: map[string]string{"name": "Four"}},
				{method: "POST", path: "/projects", user: "bob@acme.test", tenant: "acme", body: map[string]string{"name": "Five"}},
			},
			req:    call{method: "POST", path: "/projects", user: "bob@acme.test", tenant: "acme", body: map[string]string{"name": "Six"}},
			status: 422, message: "quota_exceeded",
		},

		{name: "update", req: call{method: "PATCH", path: "/projects/1", user: "bob@acme.test", tenant: "acme", body: map[string]string{"description": "v2"}}, status: 200, json: map[string]any{"name": "Website relaunch", "description": "v2"}},
		{name: "update conflict", req: call{method: "PATCH", path: "/projects/1", user: "bob@acme.test", tenant: "acme", body: map[string]string{"name": "Mobile App"}}, status: 409},
		{name: "update missing", req: call{method: "PATCH", path: "/projects/42", user: "bob@acme.test", tenant: "acme", body: map[string]string{"name": "Whatever"}}, status: 404},
		{
			name:   "rename archived is 422",
			setup:  []call{{method: "PATCH", path: "/projects/1", user: "bob@acme.test", tenant: "acme", body: map[string]string{"status": "archived"}}},
			req:    call{method: "PATCH", path: "/projects/1", user: "bob@acme.test", tenant: "acme", body: map[string]string{"name": "Renamed"}},
			status: 422, message: "project_archived",
		},
		{
			name:   "unarchive and rename together",
			setup:  []call{{method: "PATCH", path: "/projects/1", user: "bob@acme.test", tenant: "acme", body: map[string]string{"status": "archived"}}},
			req:    call{method: "PATCH", path: "/projects/1", user: "bob@acme.test", tenant: "acme", body: map[string]string{"name": "Renamed", "status": "active"}},
			status: 200, json: map[string]any{"name": "Renamed", "status": "active"},
		},

		{name: "delete needs admin role", req: call{method: "DELETE", path: "/projects/1", user: "bob@acme.test", tenant: "acme"}, status: 403, message: "Forbidden resource"},
		{name: "delete as admin", req: call{method: "DELETE", path: "/projects/1", user: "alice@acme.test", tenant: "acme"}, status: 204},
		{
			name:   "deleted project is gone",
			setup:  []call{{method: "DELETE", path: "/projects/1", user: "alice@acme.test", tenant: "acme"}},
			req:    call{method: "DELETE", path: "/projects/1", user: "alice@acme.test", tenant: "acme"},
			status: 404,
		},
		{name: "unknown route", req: call{method: "GET", path: "/nope"}, status: 404, message: "Cannot GET /nope"},
	}

	do := func(t *testing.T, a *gonesttest.App, c call) *gonesttest.Response {
		t.Helper()
		r := a.Request(c.method, c.path)
		if c.user != "" {
			r.WithBearer(login(t, a, c.user))
		}
		if c.tenant != "" {
			r.WithHeader("X-Tenant-ID", c.tenant)
		}
		switch b := c.body.(type) {
		case nil:
		case string:
			r.WithBody(strings.NewReader(b), "application/json")
		default:
			r.WithJSON(b)
		}
		return r.Do()
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := newApp(t)
			for _, s := range tc.setup {
				if res := do(t, a, s); res.Status() >= 300 {
					t.Fatalf("setup %s %s: %d %s", s.method, s.path, res.Status(), res.Body())
				}
			}
			res := do(t, a, tc.req).ExpectStatus(tc.status)
			if tc.message != "" {
				res.ExpectErrorMessage(tc.message)
			}
			if tc.json != nil {
				var got map[string]any
				res.JSON(&got)
				for k, want := range tc.json {
					wb, _ := json.Marshal(want)
					gb, _ := json.Marshal(got[k])
					if string(wb) != string(gb) {
						t.Errorf("%s = %s, want %s", k, gb, wb)
					}
				}
			}
		})
	}
}

func TestLoginThrottled(t *testing.T) {
	a := newApp(t)
	bad := auth.LoginInput{Email: "alice@acme.test", Password: "wrong-password"}
	for range 5 {
		a.Request("POST", "/auth/login").WithJSON(bad).Do().ExpectStatus(http.StatusUnauthorized)
	}
	res := a.Request("POST", "/auth/login").WithJSON(bad).Do().
		ExpectStatus(http.StatusTooManyRequests).
		ExpectErrorMessage("too many failed login attempts")
	if res.Header().Get("Retry-After") == "" {
		t.Error("Retry-After missing on 429")
	}
	// The right password is refused too while the client is throttled, and
	// other accounts from the same client are still allowed.
	a.Request("POST", "/auth/login").
		WithJSON(auth.LoginInput{Email: "alice@acme.test", Password: auth.DemoPassword}).
		Do().ExpectStatus(http.StatusTooManyRequests)
	login(t, a, "bob@acme.test")
}

func TestNewRoutesAreTenantScoped(t *testing.T) {
	a := newApp(t)
	// /tenants/current was listed explicitly before; it is now covered by
	// the deny-by-default rule like any route not excluded.
	a.Request("GET", "/tenants/current").Do().
		ExpectStatus(http.StatusBadRequest).
		ExpectErrorMessage("missing X-Tenant-ID header")
	a.Request("GET", "/health").Do().ExpectStatus(http.StatusOK)
}
