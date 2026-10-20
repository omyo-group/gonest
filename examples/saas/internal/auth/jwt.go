package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/omyo/gonest"
	"github.com/omyo/gonest/examples/saas/internal/settings"
)

// Claims is the payload of an access token.
type Claims struct {
	Subject  string   `json:"sub"`
	Email    string   `json:"email"`
	TenantID string   `json:"tenant"`
	Roles    []string `json:"roles"`
	IssuedAt int64    `json:"iat"`
	Expires  int64    `json:"exp"`
}

// Errors returned by TokenService.Verify.
var (
	ErrMalformedToken = errors.New("malformed token")
	ErrBadSignature   = errors.New("invalid token signature")
	ErrExpiredToken   = errors.New("token expired")
)

// docs:token-deps

// Clock returns the current time. The auth module provides time.Now; tests
// override it with gonesttest.OverrideProvider.
type Clock func() time.Time

// TokenDeps is the parameter object of NewTokenService: Clock is optional,
// so the service also works in a module that does not provide one.
type TokenDeps struct {
	gonest.In
	Config *settings.Config
	Clock  Clock `optional:"true"`
}

// docs:end

// TokenService issues and verifies HS256 JSON Web Tokens using only the
// standard library (crypto/hmac). It supports exactly what this example
// needs; use a vetted JWT library in production.
type TokenService struct {
	secret []byte
	ttl    time.Duration
	now    Clock
}

// jwtHeader is the fixed, pre-encoded header of every token we issue.
var jwtHeader = base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))

// NewTokenService builds the service from the global configuration.
func NewTokenService(d TokenDeps) *TokenService {
	now := d.Clock
	if now == nil {
		now = time.Now
	}
	return &TokenService{secret: []byte(d.Config.JWTSecret), ttl: d.Config.TokenTTL, now: now}
}

// TTL is the lifetime of issued tokens.
func (s *TokenService) TTL() time.Duration { return s.ttl }

// Issue signs claims for u, valid for TTL.
func (s *TokenService) Issue(u User) (string, error) {
	now := s.now()
	c := Claims{
		Subject:  u.ID,
		Email:    u.Email,
		TenantID: u.TenantID,
		Roles:    u.Roles,
		IssuedAt: now.Unix(),
		Expires:  now.Add(s.ttl).Unix(),
	}
	payload, err := json.Marshal(c)
	if err != nil {
		return "", fmt.Errorf("auth: encode claims: %w", err)
	}
	unsigned := jwtHeader + "." + base64.RawURLEncoding.EncodeToString(payload)
	return unsigned + "." + s.sign(unsigned), nil
}

// Verify checks the signature and expiry of token and returns its claims.
func (s *TokenService) Verify(token string) (Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return Claims{}, ErrMalformedToken
	}
	if parts[0] != jwtHeader {
		// Only our exact header is accepted: this rejects alg=none and
		// algorithm confusion attacks.
		return Claims{}, ErrMalformedToken
	}
	want := s.sign(parts[0] + "." + parts[1])
	if !hmac.Equal([]byte(want), []byte(parts[2])) {
		return Claims{}, ErrBadSignature
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return Claims{}, ErrMalformedToken
	}
	var c Claims
	if err := json.Unmarshal(raw, &c); err != nil {
		return Claims{}, ErrMalformedToken
	}
	if s.now().Unix() >= c.Expires {
		return Claims{}, ErrExpiredToken
	}
	return c, nil
}

func (s *TokenService) sign(unsigned string) string {
	m := hmac.New(sha256.New, s.secret)
	m.Write([]byte(unsigned))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}
