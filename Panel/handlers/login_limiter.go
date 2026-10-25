package handlers

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	// A single admin account protected only by bcrypt is brute-forceable at
	// network speed, so failed logins are throttled per client address.
	loginMaxFailures  = 5
	loginFailureReset = 15 * time.Minute
	loginLockout      = 15 * time.Minute
	loginEntryTTL     = time.Hour
	loginMaxEntries   = 4096
)

type loginAttempts struct {
	failures     int
	firstFailure time.Time
	lastFailure  time.Time
	blockedUntil time.Time
}

// loginLimiter tracks failed logins per key (client address + username).
type loginLimiter struct {
	mu          sync.Mutex
	entries     map[string]*loginAttempts
	maxFailures int
	window      time.Duration
	lockout     time.Duration
	now         func() time.Time
}

func newLoginLimiter() *loginLimiter {
	return &loginLimiter{
		entries:     make(map[string]*loginAttempts),
		maxFailures: loginMaxFailures,
		window:      loginFailureReset,
		lockout:     loginLockout,
		now:         time.Now,
	}
}

// Allow reports whether an attempt may proceed, and how long the caller should
// wait when it is blocked.
func (l *loginLimiter) Allow(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	entry, ok := l.entries[key]
	if !ok {
		return true, 0
	}
	if now.Before(entry.blockedUntil) {
		return false, entry.blockedUntil.Sub(now)
	}
	return true, 0
}

// Fail records a failed attempt and blocks the key once the limit is reached.
func (l *loginLimiter) Fail(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	l.pruneLocked(now)

	entry, ok := l.entries[key]
	if !ok || now.Sub(entry.firstFailure) > l.window {
		entry = &loginAttempts{firstFailure: now}
		l.entries[key] = entry
	}

	entry.failures++
	entry.lastFailure = now
	if entry.failures >= l.maxFailures {
		entry.blockedUntil = now.Add(l.lockout)
		entry.failures = 0
		entry.firstFailure = now
	}
}

// Reset clears the counter after a successful login.
func (l *loginLimiter) Reset(key string) {
	l.mu.Lock()
	delete(l.entries, key)
	l.mu.Unlock()
}

// pruneLocked drops stale entries so the map cannot grow without bound, then
// evicts arbitrary entries if a burst of distinct keys still exceeds the cap.
func (l *loginLimiter) pruneLocked(now time.Time) {
	if len(l.entries) < loginMaxEntries {
		return
	}
	for key, entry := range l.entries {
		if now.Sub(entry.lastFailure) > loginEntryTTL && now.After(entry.blockedUntil) {
			delete(l.entries, key)
		}
	}
	for key := range l.entries {
		if len(l.entries) < loginMaxEntries {
			break
		}
		delete(l.entries, key)
	}
}

// loginKey scopes the counter to the client address and the attempted username
// so one noisy source cannot lock out everybody else.
func loginKey(r *http.Request, username string) string {
	host := r.RemoteAddr
	if h, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		host = h
	}
	return host + "|" + strings.ToLower(strings.TrimSpace(username))
}
