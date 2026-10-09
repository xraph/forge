package providers

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLDAPRejectsInsecureVerificationBeforeDial(t *testing.T) {
	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, listener.Close()) })

	host, port, err := net.SplitHostPort(listener.Addr().String())
	require.NoError(t, err)
	portNumber, err := strconv.Atoi(port)
	require.NoError(t, err)

	for _, useTLS := range []bool{true, false} {
		config := DefaultLDAPConfig()
		config.Host, config.Port = host, portNumber
		config.BaseDN, config.BindDN = "dc=example,dc=com", "cn=service,dc=example,dc=com"
		config.InsecureSkipVerify, config.UseTLS = true, useTLS
		config.PoolSize, config.MaxRetries = 1, 1
		provider, err := NewLDAPProvider(config, newMockLogger())
		require.ErrorContains(t, err, "trust your LDAP certificate authority")
		require.Nil(t, provider)
	}

	tcpListener, ok := listener.(*net.TCPListener)
	require.True(t, ok)
	require.NoError(t, tcpListener.SetDeadline(time.Now().Add(20*time.Millisecond)))

	conn, err := listener.Accept()
	if conn != nil {
		require.NoError(t, conn.Close())
	}

	var timeout net.Error
	require.True(t, errors.As(err, &timeout))
	require.True(t, timeout.Timeout(), "rejected configuration must never dial")
}

func TestLDAPTLSVerifiesCertificates(t *testing.T) {
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	server.Config.ErrorLog = log.New(io.Discard, "", 0)

	server.StartTLS()
	defer server.Close()

	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())

	for _, tt := range []struct {
		name, host string
		trusted    bool
		wantError  bool
	}{
		{"trusted certificate", "example.com", true, false},
		{"wrong hostname", "wrong.invalid", true, true},
		{"untrusted certificate", "example.com", false, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			config := ldapTLSConfig(tt.host)
			require.GreaterOrEqual(t, config.MinVersion, uint16(tls.VersionTLS12))

			if tt.trusted {
				config.RootCAs = roots
			}

			dialer := tls.Dialer{Config: config, NetDialer: &net.Dialer{Timeout: time.Second}}

			conn, err := dialer.DialContext(t.Context(), "tcp", server.Listener.Addr().String())
			if tt.wantError {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)
			require.NoError(t, conn.Close())
		})
	}
}
