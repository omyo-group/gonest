package auth_test

import (
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/omyo/gonest"
	"github.com/omyo/gonest/examples/saas/internal/auth"
	"github.com/omyo/gonest/examples/saas/internal/settings"
	"github.com/omyo/gonest/gonesttest"
)

var t0 = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func newTokens(secret string, now time.Time) *auth.TokenService {
	return auth.NewTokenService(auth.TokenDeps{
		Config: &settings.Config{JWTSecret: secret, TokenTTL: time.Hour},
		Clock:  func() time.Time { return now },
	})
}

var alice = auth.User{ID: "u1", Email: "alice@acme.test", TenantID: "acme", Roles: []string{auth.RoleAdmin}}

func TestTokenVerify(t *testing.T) {
	issuer := newTokens("0123456789abcdef", t0)
	tok, err := issuer.Issue(alice)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(tok, ".")
	none := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))

	cases := []struct {
		name     string
		verifier *auth.TokenService
		token    string
		want     error
	}{
		{"valid", issuer, tok, nil},
		{"two parts", issuer, parts[0] + "." + parts[1], auth.ErrMalformedToken},
		{"alg none", issuer, none + "." + parts[1] + ".", auth.ErrMalformedToken},
		{"other secret", newTokens("fedcba9876543210", t0), tok, auth.ErrBadSignature},
		{"tampered payload", issuer, parts[0] + "." + parts[1] + "x." + parts[2], auth.ErrBadSignature},
		{"expired", newTokens("0123456789abcdef", t0.Add(time.Hour)), tok, auth.ErrExpiredToken},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, err := tc.verifier.Verify(tc.token)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if err == nil && (c.Subject != "u1" || c.TenantID != "acme" || c.Expires != t0.Add(time.Hour).Unix()) {
				t.Fatalf("claims = %+v", c)
			}
		})
	}
}

func TestTokenServiceDefaultClock(t *testing.T) {
	s := auth.NewTokenService(auth.TokenDeps{Config: &settings.Config{JWTSecret: "0123456789abcdef", TokenTTL: time.Minute}})
	tok, err := s.Issue(alice)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Verify(tok); err != nil {
		t.Fatal(err)
	}
}

// docs:testing-guard

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

// docs:end

func TestRolesOption(t *testing.T) {
	cfg := gonest.RouteConfig{}
	auth.Roles(auth.RoleAdmin)(&cfg)
	if got := cfg.Metadata["auth.roles"].([]string); len(got) != 1 || got[0] != auth.RoleAdmin {
		t.Fatalf("metadata = %v", cfg.Metadata)
	}
}

func TestUserStore(t *testing.T) {
	s := auth.NewUserStore()
	if _, ok := s.Authenticate("ALICE@acme.test", auth.DemoPassword); !ok {
		t.Error("email should be case-insensitive")
	}
	if _, ok := s.Authenticate("alice@acme.test", "wrong"); ok {
		t.Error("wrong password accepted")
	}
	if _, ok := s.Authenticate("nobody@acme.test", auth.DemoPassword); ok {
		t.Error("unknown user accepted")
	}
}
