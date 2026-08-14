package middleware

import (
	"net/http"
	"strings"
	"time"
)

// registrationEndpoint namespaces the registration bucket. Like every other
// endpoint name it is part of the Redis key, so nothing else may reuse it.
const registrationEndpoint = "registration"

// kratosProxyPrefix is where the browser reaches Kratos. The proxy handler
// strips it before forwarding, so this middleware — which runs first — sees
// paths that still carry it.
const kratosProxyPrefix = "/.ory"

// KratosRegistrationLimit rate-limits account creation on the Kratos reverse
// proxy, keyed by client IP.
//
// The proxy is the one route that bypasses every other guard in the server:
// it is unauthenticated by definition, and the per-user limiter has nobody to
// key on, so registration was the single unlimited write path into the town.
// Anyone could mint pending accounts in a loop, and each one lands in the
// council's approval queue — the queue a handful of volunteers read by hand.
//
// Only the flow-init and submit paths are limited. Everything else under
// /.ory — login, session checks, the settings and recovery flows, and the
// registration flow *fetch* the SPA issues on every page render — passes
// through untouched, because those are the requests a resident makes
// constantly and throttling them would break the site rather than protect it.
//
// A nil limiter (no Redis) returns a pass-through, matching the documented
// degraded mode: rate limiting is a Redis feature and its absence has never
// been an error.
func KratosRegistrationLimit(rl *RateLimiter, trusted TrustedProxies, maxRequests int, window time.Duration) func(http.Handler) http.Handler {
	if rl == nil {
		return func(next http.Handler) http.Handler { return next }
	}

	limit := rl.LimitByIP(registrationEndpoint, maxRequests, window, trusted)
	return func(next http.Handler) http.Handler {
		limited := limit(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isRegistrationFlowPath(r.URL.Path) {
				limited.ServeHTTP(w, r)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// isRegistrationFlowPath reports whether a path starts a registration flow or
// submits one, with or without the /.ory proxy prefix.
//
// The list is exact rather than a prefix match, and that is the whole design.
// /self-service/registration/flows sits directly underneath the submit path and
// is a plain read of a flow the caller already started: the SPA fetches it on
// every render of the sign-up page, so a prefix match would spend a resident's
// budget on page loads and lock them out of the form they are trying to fill
// in.
func isRegistrationFlowPath(path string) bool {
	path = strings.TrimPrefix(path, kratosProxyPrefix)
	if path != "/" {
		path = strings.TrimSuffix(path, "/")
	}

	switch path {
	case "/self-service/registration", // submit
		"/self-service/registration/browser", // flow init, browser
		"/self-service/registration/api":     // flow init, native client
		return true
	default:
		return false
	}
}
