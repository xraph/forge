package plugins

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckSpecURL(t *testing.T) {
	tests := []struct {
		url     string
		allow   bool
		wantErr bool
	}{
		{url: "https://api.example.com/openapi.json"},
		{url: "http://api.example.com/openapi.json", wantErr: true},
		{url: "http://10.0.0.5:8080/openapi.json", wantErr: true},
		{url: "HTTP://api.example.com/openapi.json", wantErr: true},
		{url: "http://api.example.com/openapi.json", allow: true},
		{url: "http://localhost:8080/openapi.json"},
		{url: "http://LOCALHOST/openapi.json"},
		{url: "http://127.0.0.1:8080/openapi.json"},
		{url: "http://127.8.9.10/openapi.json"},
		{url: "http://[::1]:8080/openapi.json"},
		// A name that merely starts like loopback is not loopback.
		{url: "http://localhost.example.com/openapi.json", wantErr: true},
		{url: "http://127.0.0.1.nip.io/openapi.json", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			u, err := url.Parse(tt.url)
			require.NoError(t, err)

			err = checkSpecURL(u, tt.allow)
			if !tt.wantErr {
				assert.NoError(t, err)

				return
			}

			require.Error(t, err)
			assert.Contains(t, err.Error(), "--allow-insecure-http", "the error must name the flag opt-in")
			assert.Contains(t, err.Error(), "allow_insecure_http", "the error must name the config opt-in")
		})
	}
}

func TestFetchSpecFromURLAllowsLoopbackHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"openapi":"3.0.0"}`))
	}))
	defer srv.Close()

	data, err := fetchSpecFromURL(t.Context(), srv.URL, 0, false)
	require.NoError(t, err)
	assert.JSONEq(t, `{"openapi":"3.0.0"}`, string(data))
}

func TestFetchSpecFromURLRefusesNonLoopbackHTTP(t *testing.T) {
	// Refused before any connection is made, so the host does not need to exist.
	_, err := fetchSpecFromURL(t.Context(), "http://spec.example.invalid/openapi.json", 0, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "refusing to fetch spec over plain http")
}

func TestFetchSpecFromURLRefusesRedirectToInsecureHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://spec.example.invalid/openapi.json", http.StatusFound)
	}))
	defer srv.Close()

	_, err := fetchSpecFromURL(t.Context(), srv.URL, 0, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "refusing to fetch spec over plain http")
}

func TestFetchSpecFromURLCapsBodySize(t *testing.T) {
	old := maxSpecBytes
	maxSpecBytes = 1 << 20

	t.Cleanup(func() { maxSpecBytes = old })

	// Atomic because the handler runs on the server's goroutine.
	var size atomic.Int64
	size.Store(maxSpecBytes)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("a", int(size.Load()))))
	}))
	defer srv.Close()

	data, err := fetchSpecFromURL(t.Context(), srv.URL, 0, false)
	require.NoError(t, err, "a body of exactly the limit is accepted")
	assert.Len(t, data, int(maxSpecBytes))

	size.Add(1)

	_, err = fetchSpecFromURL(t.Context(), srv.URL, 0, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "larger than the 1 MB limit")
}

func TestAllowInsecureHTTPFlagOnSpecCommands(t *testing.T) {
	p := &ClientPlugin{}

	want := map[string]bool{"generate": false, "check": false, "watch": false, "list": false}

	for _, cmd := range p.Commands() {
		for _, sub := range cmd.Subcommands() {
			if _, ok := want[sub.Name()]; !ok {
				continue
			}

			for _, flag := range sub.Flags() {
				if flag.Name() == "allow-insecure-http" {
					want[sub.Name()] = true
				}
			}
		}
	}

	for name, has := range want {
		assert.True(t, has, "%s takes --from-url and so must take --allow-insecure-http", name)
	}
}
