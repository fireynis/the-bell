package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/fireynis/the-bell/internal/middleware"
)

// registrationRequest builds a request as it arrives at the proxy middleware —
// path still carrying the /.ory prefix, and a peer address the IP key is
// derived from.
func registrationRequest(method, path, peer string) *http.Request {
	req := httptest.NewRequest(method, path, nil)
	req.RemoteAddr = peer
	return req
}

// serveRegistration runs one request through the limiter and reports the status.
func serveRegistration(h http.Handler, method, path, peer string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, registrationRequest(method, path, peer))
	return rec
}

func TestKratosRegistrationLimit_AllowsUnderLimit(t *testing.T) {
	rl := middleware.NewRateLimiter(newMemoryRateLimiterClient(), testLogger())
	h := middleware.KratosRegistrationLimit(rl, nil, 3, time.Hour)(okHandler())

	for i := range 3 {
		rec := serveRegistration(h, http.MethodPost, "/.ory/self-service/registration", "203.0.113.5:4000")
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: status = %d, want %d", i+1, rec.Code, http.StatusOK)
		}
	}
}

// The whole point: an unlimited registration path lets one caller fill the
// council's approval queue with accounts nobody asked for.
func TestKratosRegistrationLimit_BlocksOverLimit(t *testing.T) {
	rl := middleware.NewRateLimiter(newMemoryRateLimiterClient(), testLogger())
	h := middleware.KratosRegistrationLimit(rl, nil, 2, time.Hour)(okHandler())

	for i := range 2 {
		if rec := serveRegistration(h, http.MethodPost, "/.ory/self-service/registration", "203.0.113.6:4000"); rec.Code != http.StatusOK {
			t.Fatalf("request %d: status = %d, want %d", i+1, rec.Code, http.StatusOK)
		}
	}

	rec := serveRegistration(h, http.MethodPost, "/.ory/self-service/registration", "203.0.113.6:4000")
	assertStatus(t, rec, http.StatusTooManyRequests)
	assertErrorBody(t, rec, "rate limit exceeded")
	if got := rec.Header().Get("Retry-After"); got != "3600" {
		t.Errorf("Retry-After = %q, want %q", got, "3600")
	}
}

// The bucket is per address, so one household exhausting its budget must not
// close registration for the rest of the town.
func TestKratosRegistrationLimit_DifferentIPsIndependent(t *testing.T) {
	rl := middleware.NewRateLimiter(newMemoryRateLimiterClient(), testLogger())
	h := middleware.KratosRegistrationLimit(rl, nil, 1, time.Hour)(okHandler())

	if rec := serveRegistration(h, http.MethodPost, "/.ory/self-service/registration", "203.0.113.7:1"); rec.Code != http.StatusOK {
		t.Fatalf("first address: status = %d, want %d", rec.Code, http.StatusOK)
	}
	if rec := serveRegistration(h, http.MethodPost, "/.ory/self-service/registration", "203.0.113.8:1"); rec.Code != http.StatusOK {
		t.Fatalf("second address: status = %d, want %d", rec.Code, http.StatusOK)
	}
	if rec := serveRegistration(h, http.MethodPost, "/.ory/self-service/registration", "203.0.113.7:1"); rec.Code != http.StatusTooManyRequests {
		t.Errorf("first address again: status = %d, want %d", rec.Code, http.StatusTooManyRequests)
	}
}

// Everything else under /.ory is a path a signed-in resident hits constantly.
// Throttling any of them would break the site rather than protect it — and
// /self-service/registration/flows is the trap, because it sits directly under
// the submit path but is only the SPA reading back a flow already in progress.
func TestKratosRegistrationLimit_LeavesOtherOryPathsAlone(t *testing.T) {
	paths := []struct {
		name string
		path string
	}{
		{"session check", "/.ory/sessions/whoami"},
		{"login flow init", "/.ory/self-service/login/browser"},
		{"login submit", "/.ory/self-service/login"},
		{"registration flow fetch", "/.ory/self-service/registration/flows?id=abc"},
		{"settings flow", "/.ory/self-service/settings/browser"},
		{"recovery flow", "/.ory/self-service/recovery/browser"},
		{"verification flow", "/.ory/self-service/verification/browser"},
		{"logout", "/.ory/self-service/logout/browser"},
	}

	for _, tt := range paths {
		t.Run(tt.name, func(t *testing.T) {
			rl := middleware.NewRateLimiter(newMemoryRateLimiterClient(), testLogger())
			h := middleware.KratosRegistrationLimit(rl, nil, 1, time.Hour)(okHandler())

			// Well past a limit of one. If the path were being counted, every
			// call after the first would be a 429.
			for i := range 5 {
				rec := serveRegistration(h, http.MethodGet, tt.path, "203.0.113.9:1")
				if rec.Code != http.StatusOK {
					t.Fatalf("request %d to %s: status = %d, want %d — this path is being rate limited",
						i+1, tt.path, rec.Code, http.StatusOK)
				}
			}
		})
	}
}

// Both ways of starting a flow are counted alongside the submit, since a flood
// that only ever initialises still costs Kratos a row per attempt.
func TestKratosRegistrationLimit_CoversFlowInitAndSubmit(t *testing.T) {
	paths := []string{
		"/.ory/self-service/registration",
		"/.ory/self-service/registration/",
		"/.ory/self-service/registration/browser",
		"/.ory/self-service/registration/api",
	}

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			rl := middleware.NewRateLimiter(newMemoryRateLimiterClient(), testLogger())
			h := middleware.KratosRegistrationLimit(rl, nil, 1, time.Hour)(okHandler())

			if rec := serveRegistration(h, http.MethodGet, path, "203.0.113.10:1"); rec.Code != http.StatusOK {
				t.Fatalf("first request: status = %d, want %d", rec.Code, http.StatusOK)
			}
			if rec := serveRegistration(h, http.MethodGet, path, "203.0.113.10:1"); rec.Code != http.StatusTooManyRequests {
				t.Errorf("second request: status = %d, want %d — %s is not limited", rec.Code, http.StatusTooManyRequests, path)
			}
		})
	}
}

// Rate limiting is a Redis feature and its absence is a documented degraded
// mode, not an error. A town running without Redis must still be able to
// register residents.
func TestKratosRegistrationLimit_FailsOpenWithoutRedis(t *testing.T) {
	cases := map[string]*middleware.RateLimiter{
		"no limiter at all":            nil,
		"limiter with no redis client": middleware.NewRateLimiter(nil, testLogger()),
	}

	for name, rl := range cases {
		t.Run(name, func(t *testing.T) {
			h := middleware.KratosRegistrationLimit(rl, nil, 1, time.Hour)(okHandler())

			for i := range 5 {
				rec := serveRegistration(h, http.MethodPost, "/.ory/self-service/registration", "203.0.113.11:1")
				if rec.Code != http.StatusOK {
					t.Fatalf("request %d: status = %d, want %d", i+1, rec.Code, http.StatusOK)
				}
			}
		})
	}
}

// A Redis that answers with an error is the same story as no Redis: the town
// keeps registering people.
func TestKratosRegistrationLimit_FailsOpenOnRedisError(t *testing.T) {
	rl := middleware.NewRateLimiter(&errorRateLimiterClient{}, testLogger())
	h := middleware.KratosRegistrationLimit(rl, nil, 1, time.Hour)(okHandler())

	for i := range 5 {
		rec := serveRegistration(h, http.MethodPost, "/.ory/self-service/registration", "203.0.113.12:1")
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: status = %d, want %d", i+1, rec.Code, http.StatusOK)
		}
	}
}

// Behind a reverse proxy every request has the same peer, so without trusted
// proxies configured the whole town shares one bucket — and with them
// configured, each resident gets their own. This is the difference the
// deployment docs are about.
func TestKratosRegistrationLimit_KeysOnForwardedClientWhenProxyIsTrusted(t *testing.T) {
	const proxy = "10.0.0.1:5000"

	t.Run("untrusted peer shares one bucket", func(t *testing.T) {
		rl := middleware.NewRateLimiter(newMemoryRateLimiterClient(), testLogger())
		h := middleware.KratosRegistrationLimit(rl, nil, 1, time.Hour)(okHandler())

		first := registrationRequest(http.MethodPost, "/.ory/self-service/registration", proxy)
		first.Header.Set("X-Forwarded-For", "198.51.100.1")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, first)
		assertStatus(t, rec, http.StatusOK)

		second := registrationRequest(http.MethodPost, "/.ory/self-service/registration", proxy)
		second.Header.Set("X-Forwarded-For", "198.51.100.2")
		rec = httptest.NewRecorder()
		h.ServeHTTP(rec, second)

		// Different claimed clients, same peer, and the peer is not trusted —
		// so the header is ignored and both spend the same budget.
		assertStatus(t, rec, http.StatusTooManyRequests)
	})

	t.Run("trusted peer gives each forwarded client its own bucket", func(t *testing.T) {
		trusted, err := middleware.ParseTrustedProxies("10.0.0.0/8")
		if err != nil {
			t.Fatalf("ParseTrustedProxies: %v", err)
		}
		rl := middleware.NewRateLimiter(newMemoryRateLimiterClient(), testLogger())
		h := middleware.KratosRegistrationLimit(rl, trusted, 1, time.Hour)(okHandler())

		for _, client := range []string{"198.51.100.1", "198.51.100.2"} {
			req := registrationRequest(http.MethodPost, "/.ory/self-service/registration", proxy)
			req.Header.Set("X-Forwarded-For", client)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("client %s: status = %d, want %d", client, rec.Code, http.StatusOK)
			}
		}

		repeat := registrationRequest(http.MethodPost, "/.ory/self-service/registration", proxy)
		repeat.Header.Set("X-Forwarded-For", "198.51.100.1")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, repeat)
		assertStatus(t, rec, http.StatusTooManyRequests)
	})
}
