package auth

import (
	"crypto/sha256"
	"crypto/subtle"
	"strings"
)

// Role names used with Roles.
const (
	RoleAdmin  = "admin"
	RoleMember = "member"
)

// User is an authenticated principal.
type User struct {
	ID       string   `json:"id" example:"u1"`
	Email    string   `json:"email" example:"alice@acme.test"`
	TenantID string   `json:"tenantId" example:"acme"`
	Roles    []string `json:"roles" example:"admin"`
}

// HasRole reports whether u has role.
func (u User) HasRole(role string) bool {
	for _, r := range u.Roles {
		if r == role {
			return true
		}
	}
	return false
}

type account struct {
	user User
	hash [32]byte
}

// UserStore is a tiny in-memory account store seeded with demo users.
// It is read-only after construction, so it needs no locking.
type UserStore struct {
	byEmail map[string]account
}

// DemoPassword is the password of every seeded demo account.
const DemoPassword = "demo-password"

// NewUserStore returns a store with three demo accounts in two tenants:
// alice (admin) and bob (member) in "acme", carol (admin) in "globex".
func NewUserStore() *UserStore {
	s := &UserStore{byEmail: map[string]account{}}
	for _, u := range []User{
		{ID: "u1", Email: "alice@acme.test", TenantID: "acme", Roles: []string{RoleAdmin, RoleMember}},
		{ID: "u2", Email: "bob@acme.test", TenantID: "acme", Roles: []string{RoleMember}},
		{ID: "u3", Email: "carol@globex.test", TenantID: "globex", Roles: []string{RoleAdmin, RoleMember}},
	} {
		s.byEmail[u.Email] = account{user: u, hash: sha256.Sum256([]byte(DemoPassword))}
	}
	return s
}

// Authenticate returns the user whose credentials match. Demo only: real
// applications store salted hashes (bcrypt, argon2).
func (s *UserStore) Authenticate(email, password string) (User, bool) {
	a, ok := s.byEmail[strings.ToLower(email)]
	h := sha256.Sum256([]byte(password))
	if !ok || subtle.ConstantTimeCompare(a.hash[:], h[:]) != 1 {
		return User{}, false
	}
	return a.user, true
}
