package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fireynis/the-bell/internal/middleware"
)

func TestParseTrustedProxies(t *testing.T) {
	tests := []struct {
		name    string
		spec    string
		wantErr bool
		// probe is an address the parsed set must contain, or "" to skip.
		probe string
		inSet bool
	}{
		{name: "empty spec is an empty set", spec: ""},
		{name: "whitespace only is an empty set", spec: "  ,  , "},
		{name: "single address", spec: "10.0.0.7", probe: "10.0.0.7", inSet: true},
		{name: "single address excludes its neighbour", spec: "10.0.0.7", probe: "10.0.0.8"},
		{name: "cidr block", spec: "10.0.0.0/8", probe: "10.4.5.6", inSet: true},
		{name: "cidr block excludes outsiders", spec: "10.0.0.0/8", probe: "192.0.2.1"},
		{name: "list of both", spec: "10.0.0.0/8, 192.0.2.7", probe: "192.0.2.7", inSet: true},
		{name: "ipv6 cidr", spec: "fd00::/8", probe: "fd12::1", inSet: true},
		// A typo must stop the process at wiring time rather than silently
		// trusting nothing, which would look identical to a correct empty value.
		{name: "garbage is rejected", spec: "not-an-address", wantErr: true},
		{name: "a bad entry in a good list is rejected", spec: "10.0.0.0/8,nonsense", wantErr: true},
		{name: "a host:port pair is rejected", spec: "10.0.0.1:8080", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			trusted, err := middleware.ParseTrustedProxies(tt.spec)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseTrustedProxies(%q) accepted an unusable value", tt.spec)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseTrustedProxies(%q): %v", tt.spec, err)
			}
			if tt.probe == "" {
				return
			}

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tt.probe + ":1234"
			req.Header.Set("X-Forwarded-For", "198.51.100.99")

			// Membership is observable through ClientIP: a trusted peer has its
			// forwarded header believed, an untrusted one does not.
			got := trusted.ClientIP(req)
			believed := got == "198.51.100.99"
			if believed != tt.inSet {
				t.Errorf("ClientIP with peer %s = %q; forwarded header believed = %v, want %v",
					tt.probe, got, believed, tt.inSet)
			}
		})
	}
}

func TestTrustedProxies_ClientIP(t *testing.T) {
	trusted, err := middleware.ParseTrustedProxies("10.0.0.0/8,172.16.0.1")
	if err != nil {
		t.Fatalf("ParseTrustedProxies: %v", err)
	}

	tests := []struct {
		name       string
		trusted    middleware.TrustedProxies
		remoteAddr string
		forwarded  []string
		want       string
	}{
		{
			name:       "no trusted proxies means the peer, header or not",
			remoteAddr: "198.51.100.4:9000",
			forwarded:  []string{"203.0.113.9"},
			want:       "198.51.100.4",
		},
		{
			// The spoofing case. A caller reaching the app directly can write
			// any header they like; believing it would hand them a fresh rate
			// limit bucket per request.
			name:       "an untrusted peer cannot forge a client address",
			trusted:    trusted,
			remoteAddr: "198.51.100.4:9000",
			forwarded:  []string{"10.0.0.1"},
			want:       "198.51.100.4",
		},
		{
			name:       "a trusted peer's forwarded client is believed",
			trusted:    trusted,
			remoteAddr: "10.0.0.1:9000",
			forwarded:  []string{"203.0.113.9"},
			want:       "203.0.113.9",
		},
		{
			name:       "the rightmost non-proxy hop wins",
			trusted:    trusted,
			remoteAddr: "10.0.0.1:9000",
			forwarded:  []string{"203.0.113.9, 172.16.0.1"},
			want:       "203.0.113.9",
		},
		{
			// Only the rightmost hop was written by our own proxy. Everything
			// left of the first untrusted address is the caller's own claim, so
			// the scan must stop at that address and not walk past it.
			name:       "addresses left of the real client are ignored",
			trusted:    trusted,
			remoteAddr: "10.0.0.1:9000",
			forwarded:  []string{"1.1.1.1, 2.2.2.2, 203.0.113.9"},
			want:       "203.0.113.9",
		},
		{
			name:       "several header lines are one chain",
			trusted:    trusted,
			remoteAddr: "10.0.0.1:9000",
			forwarded:  []string{"1.1.1.1", "203.0.113.9, 172.16.0.1"},
			want:       "203.0.113.9",
		},
		{
			name:       "a trusted peer with no header falls back to the peer",
			trusted:    trusted,
			remoteAddr: "10.0.0.1:9000",
			want:       "10.0.0.1",
		},
		{
			// Junk at the right-hand end stops the walk rather than being
			// skipped: skipping is how a caller pushes the scan leftward into
			// values it controls.
			name:       "an unparseable hop falls back to the peer",
			trusted:    trusted,
			remoteAddr: "10.0.0.1:9000",
			forwarded:  []string{"203.0.113.9, not-an-address"},
			want:       "10.0.0.1",
		},
		{
			name:       "a chain of nothing but trusted proxies falls back to the peer",
			trusted:    trusted,
			remoteAddr: "10.0.0.1:9000",
			forwarded:  []string{"172.16.0.1, 10.0.0.2"},
			want:       "10.0.0.1",
		},
		{
			name:       "an IPv4-mapped IPv6 peer is reported as IPv4",
			trusted:    trusted,
			remoteAddr: "[::ffff:10.0.0.1]:9000",
			forwarded:  []string{"203.0.113.9"},
			want:       "203.0.113.9",
		},
		{
			name:       "a peer with no port still parses",
			remoteAddr: "198.51.100.4",
			want:       "198.51.100.4",
		},
		{
			// Not reachable through net/http, but a key that is stable per
			// source beats no key at all.
			name:       "an unparseable peer is used verbatim",
			remoteAddr: "a-unix-socket",
			want:       "a-unix-socket",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tt.remoteAddr
			for _, value := range tt.forwarded {
				req.Header.Add("X-Forwarded-For", value)
			}

			if got := tt.trusted.ClientIP(req); got != tt.want {
				t.Errorf("ClientIP() = %q, want %q", got, tt.want)
			}
		})
	}
}
