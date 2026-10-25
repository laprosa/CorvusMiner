package handlers

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestLoginLimiterBlocksAfterFailures(t *testing.T) {
	limiter := newLoginLimiter()
	key := "127.0.0.1|admin"

	for i := 0; i < loginMaxFailures; i++ {
		if allowed, _ := limiter.Allow(key); !allowed {
			t.Fatalf("attempt %d should be allowed", i+1)
		}
		limiter.Fail(key)
	}

	allowed, retryAfter := limiter.Allow(key)
	if allowed {
		t.Fatal("expected the key to be blocked after the failure limit")
	}
	if retryAfter <= 0 || retryAfter > loginLockout+time.Second {
		t.Fatalf("retryAfter = %v, want within lockout window", retryAfter)
	}
}

func TestLoginLimiterResetsOnSuccess(t *testing.T) {
	limiter := newLoginLimiter()
	key := "10.0.0.1|admin"

	for i := 0; i < loginMaxFailures-1; i++ {
		limiter.Fail(key)
	}
	limiter.Reset(key)

	if allowed, _ := limiter.Allow(key); !allowed {
		t.Fatal("a successful login must clear the failure counter")
	}

	// The counter restarted: the next failures must not immediately block.
	for i := 0; i < loginMaxFailures-1; i++ {
		limiter.Fail(key)
	}
	if allowed, _ := limiter.Allow(key); !allowed {
		t.Fatal("counter should have been reset by Reset")
	}
}

func TestLoginLimiterUnblocksAfterLockout(t *testing.T) {
	limiter := newLoginLimiter()
	key := "10.0.0.2|admin"

	current := time.Now()
	limiter.now = func() time.Time { return current }

	for i := 0; i < loginMaxFailures; i++ {
		limiter.Fail(key)
	}
	if allowed, _ := limiter.Allow(key); allowed {
		t.Fatal("expected to be blocked")
	}

	current = current.Add(loginLockout + time.Second)
	if allowed, _ := limiter.Allow(key); !allowed {
		t.Fatal("expected the lockout to expire")
	}
}

func TestLoginLimiterScopesPerKey(t *testing.T) {
	limiter := newLoginLimiter()
	for i := 0; i < loginMaxFailures; i++ {
		limiter.Fail("10.0.0.3|admin")
	}

	if allowed, _ := limiter.Allow("10.0.0.3|admin"); allowed {
		t.Fatal("the noisy key should be blocked")
	}
	if allowed, _ := limiter.Allow("10.0.0.4|admin"); !allowed {
		t.Fatal("a different client address must not be affected")
	}
	if allowed, _ := limiter.Allow("10.0.0.3|operator"); !allowed {
		t.Fatal("a different username must not be affected")
	}
}

func TestLoginKeyUsesHostOnly(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(""))
	req.RemoteAddr = "203.0.113.9:54321"
	if got := loginKey(req, "Admin"); got != "203.0.113.9|admin" {
		t.Errorf("loginKey = %q", got)
	}

	req.RemoteAddr = "2001:db8::1"
	if got := loginKey(req, "admin"); got != "2001:db8::1|admin" {
		t.Errorf("ipv6 loginKey = %q", got)
	}
}

func TestValidateMinerSettings(t *testing.T) {
	tests := []struct {
		name    string
		enabled bool
		algo    string
		url     string
		wallet  string
		wantErr string
	}{
		{"disabled skips validation", false, "", "", "", ""},
		{"valid", true, "rx/0", "pool.example.com:3333", "WALLET", ""},
		{"valid with scheme", true, "rx/0", "stratum+ssl://pool.example.com:3333", "WALLET", ""},
		{"pearlhash bare endpoint", true, "pearlhash", "pool.example.com:3333", "WALLET", ""},
		{"missing url", true, "rx/0", "", "WALLET", "mining URL is required"},
		{"missing port", true, "rx/0", "pool.example.com", "WALLET", "host:port"},
		{"bad port", true, "rx/0", "pool.example.com:99999", "WALLET", "invalid port"},
		{"missing host", true, "rx/0", ":3333", "WALLET", "no host"},
		{"missing wallet", true, "rx/0", "pool.example.com:3333", "", "wallet is required"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			problems := validateMinerSettings("CPU", tc.enabled, tc.algo, tc.url, tc.wallet)
			if tc.wantErr == "" {
				if len(problems) != 0 {
					t.Fatalf("unexpected problems: %v", problems)
				}
				return
			}
			if len(problems) == 0 || !strings.Contains(strings.Join(problems, "; "), tc.wantErr) {
				t.Fatalf("want problem containing %q, got %v", tc.wantErr, problems)
			}
		})
	}
}

func TestPercentValue(t *testing.T) {
	tests := []struct {
		in   interface{}
		want float64
		ok   bool
	}{
		{float64(50), 50, true},
		{"25.5", 25.5, true},
		{float64(-1), -1, false},
		{float64(101), 101, false},
		{"abc", 0, false},
		{nil, 0, true}, // absent: keep the default
	}
	for _, tc := range tests {
		got, ok := percentValue(tc.in)
		if ok != tc.ok || got != tc.want {
			t.Errorf("percentValue(%v) = (%v,%v), want (%v,%v)", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

// TestLoginLimiterBoundsMemory makes sure a flood of distinct usernames cannot
// grow the limiter map without bound.
func TestLoginLimiterBoundsMemory(t *testing.T) {
	limiter := newLoginLimiter()
	for i := 0; i < loginMaxEntries+500; i++ {
		limiter.Fail("10.0.0.9|user" + strconv.Itoa(i))
	}
	limiter.mu.Lock()
	size := len(limiter.entries)
	limiter.mu.Unlock()
	if size > loginMaxEntries {
		t.Fatalf("limiter grew to %d entries, cap is %d", size, loginMaxEntries)
	}
}
