package auth

import (
	"fmt"
	"strings"

	"github.com/omyo/gonest/examples/saas/internal/settings"
	"golang.org/x/crypto/bcrypt"
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
	hash []byte // bcrypt: salted and slow on purpose
}

// UserStore is a tiny in-memory account store seeded with demo users.
// It is read-only after construction, so it needs no locking.
type UserStore struct {
	byEmail map[string]account
	// dummy is compared when the email is unknown, so that a login takes
	// as long for an unknown account as for a wrong password.
	dummy []byte
}

// DemoPassword is the password of every seeded demo account.
const DemoPassword = "demo-password"

// NewUserStore returns a store with three demo accounts in two tenants:
// alice (admin) and bob (member) in "acme", carol (admin) in "globex".
// Passwords are hashed with bcrypt at cfg.BcryptCost.
func NewUserStore(cfg *settings.Config) (*UserStore, error) {
	// One hash for every demo account keeps startup fast; real accounts
	// each get their own salt from bcrypt.GenerateFromPassword.
	hash, err := bcrypt.GenerateFromPassword([]byte(DemoPassword), cfg.BcryptCost)
	if err != nil {
		return nil, fmt.Errorf("hashing the demo password: %w", err)
	}
	s := &UserStore{byEmail: map[string]account{}, dummy: hash}
	for _, u := range []User{
		{ID: "u1", Email: "alice@acme.test", TenantID: "acme", Roles: []string{RoleAdmin, RoleMember}},
		{ID: "u2", Email: "bob@acme.test", TenantID: "acme", Roles: []string{RoleMember}},
		{ID: "u3", Email: "carol@globex.test", TenantID: "globex", Roles: []string{RoleAdmin, RoleMember}},
	} {
		s.byEmail[u.Email] = account{user: u, hash: hash}
	}
	return s, nil
}

// Authenticate returns the user whose credentials match. Unknown emails
// still pay for a bcrypt comparison, so response times do not reveal which
// accounts exist.
func (s *UserStore) Authenticate(email, password string) (User, bool) {
	a, ok := s.byEmail[strings.ToLower(email)]
	hash := a.hash
	if !ok {
		hash = s.dummy
	}
	if bcrypt.CompareHashAndPassword(hash, []byte(password)) != nil || !ok {
		return User{}, false
	}
	return a.user, true
}
