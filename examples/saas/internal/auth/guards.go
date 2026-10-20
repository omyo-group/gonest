package auth

import (
	"errors"
	"strings"

	"github.com/omyo/gonest"
)

// UserKey is the request value key under which JWTGuard stores the *User.
const UserKey = "auth.user"

// rolesKey is the route metadata key written by Roles and read by RolesGuard.
const rolesKey = "auth.roles"

// docs:current-user

// CurrentUser returns the user authenticated by JWTGuard for this request
// (the NestJS @CurrentUser() custom decorator).
func CurrentUser(ctx *gonest.Ctx) (*User, bool) {
	return gonest.CtxValue[*User](ctx, UserKey)
}

// docs:end

// docs:jwt-guard

// JWTGuard authenticates "Authorization: Bearer <jwt>" requests and stores
// the user in the request values (see CurrentUser).
type JWTGuard struct {
	tokens *TokenService
}

// NewJWTGuard is the provider constructor; TokenService is injected.
func NewJWTGuard(tokens *TokenService) *JWTGuard { return &JWTGuard{tokens: tokens} }

// CanActivate implements gonest.Guard. Returning an *HTTPError gives a 401;
// returning false would give the default 403.
func (g *JWTGuard) CanActivate(ctx *gonest.Ctx) (bool, error) {
	raw, ok := strings.CutPrefix(ctx.Header("Authorization"), "Bearer ")
	if !ok || raw == "" {
		return false, gonest.Unauthorized("missing bearer token")
	}
	claims, err := g.tokens.Verify(raw)
	if err != nil {
		msg := "invalid token"
		if errors.Is(err, ErrExpiredToken) {
			msg = "token expired"
		}
		return false, gonest.Unauthorized(msg).Wrap(err)
	}
	ctx.Set(UserKey, &User{ID: claims.Subject, Email: claims.Email, TenantID: claims.TenantID, Roles: claims.Roles})
	return true, nil
}

// docs:end

// docs:roles

// Roles requires the authenticated user to have at least one of roles
// (NestJS @Roles() built on SetMetadata). Use it on a route or, through
// Router.Use, on a whole controller, together with RolesGuard.
func Roles(roles ...string) gonest.RouteOption {
	return gonest.SetMetadata(rolesKey, roles)
}

// RolesGuard enforces Roles metadata. Routes without metadata are allowed.
// It must run after JWTGuard.
type RolesGuard struct{}

// NewRolesGuard is the provider constructor.
func NewRolesGuard() *RolesGuard { return &RolesGuard{} }

// CanActivate implements gonest.Guard. Route metadata wins over controller
// metadata (Ctx.Route().Metadata looks at the route first).
func (RolesGuard) CanActivate(ctx *gonest.Ctx) (bool, error) {
	v, ok := ctx.Route().Metadata(rolesKey)
	if !ok {
		return true, nil
	}
	user, ok := CurrentUser(ctx)
	if !ok {
		return false, gonest.Unauthorized("not authenticated")
	}
	for _, r := range v.([]string) {
		if user.HasRole(r) {
			return true, nil
		}
	}
	return false, nil // 403 Forbidden resource
}

// docs:end
