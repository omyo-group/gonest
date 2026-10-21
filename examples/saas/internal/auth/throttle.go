package auth

import (
	"strings"
	"sync"
	"time"
)

// Default LoginLimiter settings.
const (
	loginWindow       = 15 * time.Minute
	loginPerAccountIP = 5  // failures per email and client IP
	loginPerIP        = 50 // failures per client IP, across emails
	loginMaxKeys      = 10_000
)

// LoginLimiter throttles failed logins, the login part of NestJS
// @nestjs/throttler. Failures are counted per email and client IP, and per
// client IP across emails, in a fixed window. Once a counter reaches its
// limit, logins with that key are refused until the window ends. Counting
// per email and IP, rather than per email alone, keeps an attacker from
// locking a victim out of their account.
//
// The counters live in memory, per process: several replicas need a shared
// store such as Redis. The client IP comes from gin, so configure
// gonest.WithTrustedProxies behind a load balancer.
type LoginLimiter struct {
	now Clock

	mu   sync.Mutex
	hits map[string]*loginCounter
}

type loginCounter struct {
	n     int
	reset time.Time
}

// NewLoginLimiter returns a limiter with a 15 minute window, 5 failures per
// email and IP and 50 per IP. A nil clock means time.Now.
func NewLoginLimiter(now Clock) *LoginLimiter {
	if now == nil {
		now = time.Now
	}
	return &LoginLimiter{now: now, hits: map[string]*loginCounter{}}
}

// Check returns how long the client must wait before it may try to log in
// again; zero means the attempt is allowed.
func (l *LoginLimiter) Check(ip, email string) time.Duration {
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	var wait time.Duration
	for _, k := range limitKeys(ip, email) {
		if c := l.hits[k.key]; c != nil && now.Before(c.reset) && c.n >= k.limit {
			wait = max(wait, c.reset.Sub(now))
		}
	}
	return wait
}

// Fail records a failed login.
func (l *LoginLimiter) Fail(ip, email string) {
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.hits) >= loginMaxKeys {
		l.sweep(now)
	}
	for _, k := range limitKeys(ip, email) {
		c := l.hits[k.key]
		if c == nil || !now.Before(c.reset) {
			c = &loginCounter{reset: now.Add(loginWindow)}
			l.hits[k.key] = c
		}
		c.n++
	}
}

// Succeed clears the failures of email from ip. The per-IP counter is kept,
// so one valid account does not unlock guessing on others.
func (l *LoginLimiter) Succeed(ip, email string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.hits, accountKey(ip, email))
}

type limitKey struct {
	key   string
	limit int
}

// limitKeys returns the counters an attempt is checked against.
func limitKeys(ip, email string) [2]limitKey {
	return [2]limitKey{{"ip|" + ip, loginPerIP}, {accountKey(ip, email), loginPerAccountIP}}
}

func accountKey(ip, email string) string {
	return "account|" + ip + "|" + strings.ToLower(email)
}

// sweep drops expired counters. l.mu must be held.
func (l *LoginLimiter) sweep(now time.Time) {
	for k, c := range l.hits {
		if !now.Before(c.reset) {
			delete(l.hits, k)
		}
	}
}
