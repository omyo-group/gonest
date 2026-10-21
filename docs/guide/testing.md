# Testing

`github.com/omyo-group/gonest/gonesttest` is the equivalent of NestJS
`Test.createTestingModule`. It uses only the standard library `testing` and
`net/http/httptest` packages.

## Testing an application

`gonesttest.New(t, rootModule)` returns a builder; `Compile()` runs
`gonest.New` (startup log off, logs discarded), fails the test on error and
registers `Shutdown` with `t.Cleanup`. Requests go through the real
pipeline, without a network listener:

<!-- snippet: examples/saas/internal/app/app_test.go#testing-app -->
```go
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
```

Request builder: `WithHeader`, `WithQuery`, `WithBearer`, `WithJSON`,
`WithBody`, then `Do()`. Response helpers: `Status`, `Body`, `Header`,
`JSON(&v)`, and the chainable assertions `ExpectStatus`, `ExpectJSON`
(compares JSON values, so a struct or a map works), `ExpectHeader` and
`ExpectErrorMessage` (searches the error message and the validation
details). Each `Expect` fails the test immediately and shows the body.

The SaaS tests (`examples/saas/internal/app/app_test.go`) are a table of
about 35 requests: each case builds a fresh application, so state never
leaks between cases, and the whole file runs in about a second with `-race`.

## Overriding providers

`OverrideProvider(p)` replaces every provider with the same token anywhere
in the graph. Your module values are never modified (the builder works on a
copy), so package-level modules can be shared between tests. An override
that matches nothing fails `Compile`, which catches typos.

<!-- snippet: examples/saas/internal/app/wiring_test.go#testing-override -->
```go
// TestTokenExpiry replaces the auth module's Clock provider, so the test
// controls time without sleeping.
func TestTokenExpiry(t *testing.T) {
	var mu sync.Mutex
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { mu.Lock(); defer mu.Unlock(); return now }

	a := gonesttest.New(t, app.NewModule(env(map[string]string{"SAAS_TOKEN_TTL": "5m"}))).
		OverrideProvider(gonest.Value[auth.Clock](clock)).
		Compile()

	tok := login(t, a, "alice@acme.test")
	a.Request("GET", "/auth/me").WithBearer(tok).Do().ExpectStatus(http.StatusOK)

	mu.Lock()
	now = now.Add(6 * time.Minute)
	mu.Unlock()
	a.Request("GET", "/auth/me").WithBearer(tok).Do().
		ExpectStatus(http.StatusUnauthorized).
		ExpectErrorMessage("token expired")
}
```

Other tools:

- `OverrideModule(old, replacement)` swaps a whole module wherever it is
  imported.
- `WithOptions(opts...)` passes `gonest.New` options (for example
  `gonest.WithGlobalGuards`).
- `gonesttest.Get[T](app)` / `GetNamed` resolve providers from the root
  module and fail the test on error.

A replacement that is not itself an enhancer inherits the enhancer kind of
the provider it replaces, so overriding an `AppGuard` provider with
`gonest.Provide(newFake)` keeps it a global guard.

## Unit testing guards, pipes, interceptors and filters

`gonesttest.NewCtx(t, opts...)` builds a `*gonest.Ctx` backed by a Gin test
context: `CtxMethod`, `CtxPath`, `CtxHeader`, `CtxQuery`, `CtxParam`,
`CtxBody`, `CtxJSON`, `CtxMetadata` (route metadata), `CtxRoute`, `CtxSet`
(request values) and `CtxLogger`. `ctx.Route()` is never nil.

<!-- snippet: examples/saas/internal/auth/auth_test.go#testing-guard -->
```go
func TestJWTGuard(t *testing.T) {
	tokens := newTokens("0123456789abcdef", t0)
	tok, _ := tokens.Issue(alice)
	guard := auth.NewJWTGuard(tokens)

	cases := []struct {
		name    string
		header  string
		wantErr string
	}{
		{"valid", "Bearer " + tok, ""},
		{"missing", "", "missing bearer token"},
		{"wrong scheme", "Basic abc", "missing bearer token"},
		{"garbage", "Bearer abc", "invalid token"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := gonesttest.NewCtx(t, gonesttest.CtxHeader("Authorization", tc.header))
			ok, err := guard.CanActivate(ctx)
			if tc.wantErr == "" {
				if !ok || err != nil {
					t.Fatalf("got %v, %v", ok, err)
				}
				if u, _ := auth.CurrentUser(ctx); u == nil || u.Email != alice.Email {
					t.Fatalf("current user = %+v", u)
				}
				return
			}
			var he *gonest.HTTPError
			if !errors.As(err, &he) || he.Status != http.StatusUnauthorized || he.Message != tc.wantErr {
				t.Fatalf("err = %v, want 401 %q", err, tc.wantErr)
			}
		})
	}
}

func TestRolesGuard(t *testing.T) {
	member := &auth.User{ID: "u2", Roles: []string{auth.RoleMember}}
	cases := []struct {
		name    string
		opts    []gonesttest.CtxOption
		want    bool
		wantErr bool
	}{
		{"no metadata", nil, true, false},
		{"role present", []gonesttest.CtxOption{
			gonesttest.CtxMetadata("auth.roles", []string{auth.RoleMember}),
			gonesttest.CtxSet(auth.UserKey, member)}, true, false},
		{"role missing", []gonesttest.CtxOption{
			gonesttest.CtxMetadata("auth.roles", []string{auth.RoleAdmin}),
			gonesttest.CtxSet(auth.UserKey, member)}, false, false},
		{"not authenticated", []gonesttest.CtxOption{
			gonesttest.CtxMetadata("auth.roles", []string{auth.RoleAdmin})}, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ok, err := auth.NewRolesGuard().CanActivate(gonesttest.NewCtx(t, tc.opts...))
			if ok != tc.want || (err != nil) != tc.wantErr {
				t.Fatalf("got %v, %v", ok, err)
			}
		})
	}
}
```

`gonesttest.Recorder(ctx)` returns the response recorder behind such a
context, and `gonesttest.ResponseOf(t, ctx)` wraps it with the `Expect`
helpers, which is how filters are tested:

<!-- snippet: examples/saas/internal/projects/projects_test.go#testing-filter -->
```go
func TestDomainErrorFilter(t *testing.T) {
	cases := []struct {
		name    string
		err     error
		handled bool
		message string
	}{
		{"domain error", &projects.DomainError{Code: "quota_exceeded", Message: "too many"}, true, "too many"},
		{"wrapped domain error", fmt.Errorf("create: %w", &projects.DomainError{Code: "archived", Message: "read only"}), true, "read only"},
		{"http error passes through", gonest.NotFound("nope"), false, ""},
		{"plain error passes through", errors.New("boom"), false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := gonesttest.NewCtx(t)
			if got := projects.DomainErrorFilter.Catch(ctx, tc.err); got != tc.handled {
				t.Fatalf("Catch = %v, want %v", got, tc.handled)
			}
			if tc.handled {
				gonesttest.ResponseOf(t, ctx).
					ExpectStatus(422).
					ExpectErrorMessage(tc.message)
			}
		})
	}
}
```

## Running the example tests

```sh
go test -race ./examples/...
go test -race -coverpkg=./examples/saas/... ./examples/saas/...
```
