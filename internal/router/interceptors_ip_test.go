package router

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// serveFrom sends one GET to r with RemoteAddr set to remote and returns the
// status.
func serveFrom(t *testing.T, r Router, remote string) int {
	t.Helper()

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/x", nil)
	req.RemoteAddr = remote

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	return rec.Code
}

func ipRouter(t *testing.T, in Interceptor) Router {
	t.Helper()

	r := NewRouter()
	require.NoError(t, r.GET("/x", func(ctx Context) error {
		return ctx.String(http.StatusOK, "ok")
	}, WithInterceptor(in)))

	return r
}

// net/http sets RemoteAddr to "ip:port". The IP interceptors used to compare
// that string verbatim, so DenyIPs never matched and AllowIPs matched nobody.
func TestDenyIPs_MatchesRemoteAddrWithPort(t *testing.T) {
	r := ipRouter(t, DenyIPs("203.0.113.9", "10.0.0.0/8", "2001:db8::/32"))

	cases := []struct {
		remote string
		want   int
	}{
		{"203.0.113.9:51234", http.StatusForbidden},
		{"10.20.30.40:443", http.StatusForbidden},
		{"[2001:db8::7]:8080", http.StatusForbidden},
		{"[::ffff:203.0.113.9]:51234", http.StatusForbidden}, // dual-stack listener
		{"[2001:db8::7%eth0]:8080", http.StatusForbidden},    // zone dropped
		{"203.0.113.9", http.StatusForbidden},                // no port
		{"198.51.100.1:51234", http.StatusOK},
		{"[2001:db9::1]:8080", http.StatusOK},
		{"not-an-address", http.StatusForbidden}, // unparseable fails closed
	}

	for _, tc := range cases {
		t.Run(tc.remote, func(t *testing.T) {
			assert.Equal(t, tc.want, serveFrom(t, r, tc.remote))
		})
	}
}

func TestAllowIPs_MatchesRemoteAddrWithPort(t *testing.T) {
	r := ipRouter(t, AllowIPs("203.0.113.9", "192.168.0.0/16", "::ffff:172.16.0.0/108", "2001:db8::1"))

	cases := []struct {
		remote string
		want   int
	}{
		{"203.0.113.9:51234", http.StatusOK},
		{"192.168.4.5:1", http.StatusOK},
		{"172.16.9.9:1", http.StatusOK}, // mapped range written by the operator
		{"[::ffff:192.168.4.5]:1", http.StatusOK},
		{"[2001:db8::1]:443", http.StatusOK},
		{"203.0.113.10:51234", http.StatusForbidden},
		{"[2001:db8::2]:443", http.StatusForbidden},
		{"", http.StatusForbidden},
	}

	for _, tc := range cases {
		t.Run(tc.remote, func(t *testing.T) {
			assert.Equal(t, tc.want, serveFrom(t, r, tc.remote))
		})
	}
}

func TestRateLimitByIP_KeysOnAddressNotConnection(t *testing.T) {
	keys := map[string]int{}
	r := ipRouter(t, RateLimitByIP(func(ctx Context, ip string) RateLimitResult {
		keys[ip]++

		return RateLimitResult{Allowed: true}
	}))

	for i := range 5 {
		serveFrom(t, r, fmt.Sprintf("203.0.113.9:%d", 50000+i))
		serveFrom(t, r, fmt.Sprintf("[::ffff:203.0.113.9]:%d", 50000+i))
	}

	assert.Equal(t, map[string]int{"203.0.113.9": 10}, keys)
}

func TestIPInterceptors_PanicOnInvalidEntry(t *testing.T) {
	defer func() {
		msg := fmt.Sprint(recover())
		assert.Contains(t, msg, "DenyIPs")
		assert.Contains(t, msg, `"203.0.113.9:80"`)
	}()

	assert.Panics(t, func() { AllowIPs("10.0.0.0/33") })
	assert.Panics(t, func() { AllowIPs("example.com") })

	DenyIPs("203.0.113.9:80")
}
