package middleware

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// TrustedProxies is the set of peer addresses whose X-Forwarded-For header is
// believed.
//
// It is empty by default, and an empty set means every request is attributed to
// its TCP peer. That is the only safe default: X-Forwarded-For is a request
// header like any other, so trusting it unconditionally would let anyone claim
// a fresh identity per request and walk straight through an IP-keyed rate
// limit. The cost of the safe default is the mirror-image failure — behind a
// reverse proxy every request appears to come from the proxy, so one bucket is
// shared by the whole town — which is why the deployment docs make setting this
// part of putting The Bell behind Traefik.
type TrustedProxies []netip.Prefix

// ParseTrustedProxies reads a comma-separated list of IP addresses and CIDR
// blocks. A bare address is treated as a single-host prefix, so
// "10.0.0.7,10.1.0.0/16" is a valid list. Empty entries are ignored, so an
// unset value yields an empty set rather than an error.
func ParseTrustedProxies(spec string) (TrustedProxies, error) {
	var trusted TrustedProxies
	for _, field := range strings.Split(spec, ",") {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		if prefix, err := netip.ParsePrefix(field); err == nil {
			trusted = append(trusted, prefix.Masked())
			continue
		}
		addr, err := netip.ParseAddr(field)
		if err != nil {
			return nil, fmt.Errorf("trusted proxy %q is neither an IP address nor a CIDR block", field)
		}
		addr = addr.Unmap()
		trusted = append(trusted, netip.PrefixFrom(addr, addr.BitLen()))
	}
	return trusted, nil
}

func (t TrustedProxies) contains(addr netip.Addr) bool {
	addr = addr.Unmap()
	for _, prefix := range t {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

// ClientIP returns the address a request should be attributed to.
//
// When the TCP peer is not a trusted proxy the peer is the answer and headers
// are ignored entirely. When it is, the X-Forwarded-For chain is walked from
// the right — the end a proxy appends to, and therefore the end the caller
// cannot forge — and the first address that is not itself a trusted proxy is
// the client. Anything to the left of that hop was written by somebody we have
// no reason to believe.
//
// An unparseable entry stops the walk rather than being skipped: continuing
// would let a caller stuff the header with junk to push the scan leftward into
// values it controls. The peer address is the fallback throughout, so a
// malformed or absent header degrades to the untrusted-peer behaviour instead
// of yielding no key at all.
func (t TrustedProxies) ClientIP(r *http.Request) string {
	peer, ok := peerAddr(r.RemoteAddr)
	if !ok {
		// Neither host:port nor a bare address — nothing to parse, so the raw
		// value becomes the key. It is at least stable per connection source.
		return r.RemoteAddr
	}
	if len(t) == 0 || !t.contains(peer) {
		return peer.String()
	}

	for _, hop := range forwardedChain(r) {
		addr, err := netip.ParseAddr(hop)
		if err != nil {
			break
		}
		if addr = addr.Unmap(); !t.contains(addr) {
			return addr.String()
		}
	}
	return peer.String()
}

// forwardedChain returns the X-Forwarded-For hops in right-to-left order,
// flattening the several header lines Go keeps separate into the one list the
// header semantically is.
func forwardedChain(r *http.Request) []string {
	var chain []string
	for _, value := range r.Header.Values("X-Forwarded-For") {
		for _, hop := range strings.Split(value, ",") {
			chain = append(chain, strings.TrimSpace(hop))
		}
	}
	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
		chain[i], chain[j] = chain[j], chain[i]
	}
	return chain
}

// peerAddr parses http.Request.RemoteAddr, which carries a port in production
// but not always in a test that built the request by hand.
func peerAddr(remoteAddr string) (netip.Addr, bool) {
	if host, _, err := net.SplitHostPort(remoteAddr); err == nil {
		remoteAddr = host
	}
	addr, err := netip.ParseAddr(remoteAddr)
	if err != nil {
		return netip.Addr{}, false
	}
	return addr.Unmap(), true
}
