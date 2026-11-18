package router

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// The IP interceptors below read the peer address from Request.RemoteAddr.
// net/http sets that to "ip:port", and the port is the client's ephemeral
// port, so comparing RemoteAddr verbatim against a configured address never
// matches: DenyIPs let every listed address through, AllowIPs refused
// everyone, and RateLimitByIP gave every connection its own bucket. The
// address is now split from its port and parsed, and the lists accept CIDR
// ranges as well as single addresses.
//
// X-Forwarded-For and X-Real-IP are deliberately ignored, the same as
// middleware.ClientIP. A header is whatever the client chose to send, so
// trusting it unconditionally would let anyone pick the address these
// interceptors judge. Behind a trusted proxy, rewrite RemoteAddr from the
// forwarded header in a middleware that knows which hops to trust.

// RemoteIP returns the client address of r, parsed from RemoteAddr with the
// port removed. An IPv4 address carried as IPv4-mapped IPv6 (::ffff:a.b.c.d),
// which a dual-stack listener reports, is returned as plain IPv4 and any IPv6
// zone is dropped, so the result compares equal to the address an operator
// would write. ok is false when RemoteAddr holds no parseable address.
func RemoteIP(r *http.Request) (addr netip.Addr, ok bool) {
	if r == nil {
		return netip.Addr{}, false
	}

	return parseRemoteAddr(r.RemoteAddr)
}

func parseRemoteAddr(remote string) (netip.Addr, bool) {
	host := remote
	if h, _, err := net.SplitHostPort(remote); err == nil {
		host = h
	}

	// SplitHostPort fails on a bare address with no port, which some test
	// servers and unix sockets produce. A bare IPv6 address may still carry
	// brackets.
	host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")

	addr, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}, false
	}

	return addr.WithZone("").Unmap(), true
}

// ipMatcher tests an address against a set of single addresses and ranges.
type ipMatcher struct {
	prefixes []netip.Prefix
}

// newIPMatcher parses each entry as a single address ("203.0.113.9",
// "2001:db8::1") or a CIDR range ("10.0.0.0/8", "2001:db8::/32"). An entry
// that is neither panics, naming the entry. These lists are configuration,
// and quietly skipping a typo would make DenyIPs let the address through.
func newIPMatcher(name string, entries []string) ipMatcher {
	m := ipMatcher{prefixes: make([]netip.Prefix, 0, len(entries))}

	for _, raw := range entries {
		p, err := parseIPEntry(raw)
		if err != nil {
			panic(fmt.Sprintf("router: %s: %v", name, err))
		}

		m.prefixes = append(m.prefixes, p)
	}

	return m
}

func parseIPEntry(raw string) (netip.Prefix, error) {
	entry := strings.TrimSpace(raw)

	if strings.Contains(entry, "/") {
		p, err := netip.ParsePrefix(entry)
		if err != nil {
			return netip.Prefix{}, fmt.Errorf("invalid CIDR range %q: %w", raw, err)
		}

		// A range written in IPv4-mapped form (::ffff:10.0.0.0/104) is
		// rewritten as the IPv4 range it covers, because client addresses
		// are unmapped before they are compared.
		if p.Addr().Is4In6() && p.Bits() >= 96 {
			p = netip.PrefixFrom(p.Addr().Unmap(), p.Bits()-96)
		}

		return p.Masked(), nil
	}

	addr, err := netip.ParseAddr(entry)
	if err != nil {
		return netip.Prefix{}, fmt.Errorf("invalid IP address %q: %w", raw, err)
	}

	addr = addr.WithZone("").Unmap()

	return netip.PrefixFrom(addr, addr.BitLen()), nil
}

func (m ipMatcher) contains(addr netip.Addr) bool {
	for _, p := range m.prefixes {
		if p.Contains(addr) {
			return true
		}
	}

	return false
}

// RateLimitByIP creates a rate limit interceptor keyed by client IP. The key
// is the address from RemoteAddr without its port, in canonical form, so every
// connection from one client shares a bucket. When RemoteAddr cannot be
// parsed, the raw RemoteAddr is used as the key.
func RateLimitByIP(checker func(ctx Context, ip string) RateLimitResult) Interceptor {
	return NewInterceptor("rate-limit:ip", func(ctx Context, route RouteInfo) InterceptorResult {
		key := ctx.Request().RemoteAddr
		if addr, ok := RemoteIP(ctx.Request()); ok {
			key = addr.String()
		}

		result := checker(ctx, key)

		values := map[string]any{
			"ratelimit.remaining": result.Remaining,
			"ratelimit.reset":     result.ResetAt,
		}

		if !result.Allowed {
			return InterceptorResult{
				Blocked: true,
				Error:   NewHTTPError(429, "rate limit exceeded"),
				Values:  values,
			}
		}

		return AllowWithValues(values)
	})
}

// --- IP/Network Interceptors ---

// AllowIPs creates an interceptor that only allows the listed clients. Each
// entry is a single address or a CIDR range, IPv4 or IPv6. A request whose
// address cannot be determined is refused. It panics on an entry that is
// neither an address nor a range.
func AllowIPs(allowedIPs ...string) Interceptor {
	m := newIPMatcher("AllowIPs", allowedIPs)

	return NewInterceptor("allow-ips", func(ctx Context, route RouteInfo) InterceptorResult {
		addr, ok := RemoteIP(ctx.Request())
		if !ok || !m.contains(addr) {
			return Block(Forbidden("IP not allowed"))
		}

		return Allow()
	})
}

// DenyIPs creates an interceptor that blocks the listed clients. Each entry is
// a single address or a CIDR range, IPv4 or IPv6. A request whose address
// cannot be determined is also blocked, because it cannot be shown not to be
// on the list. It panics on an entry that is neither an address nor a range.
func DenyIPs(deniedIPs ...string) Interceptor {
	m := newIPMatcher("DenyIPs", deniedIPs)

	return NewInterceptor("deny-ips", func(ctx Context, route RouteInfo) InterceptorResult {
		addr, ok := RemoteIP(ctx.Request())
		if !ok || m.contains(addr) {
			return Block(Forbidden("IP blocked"))
		}

		return Allow()
	})
}
