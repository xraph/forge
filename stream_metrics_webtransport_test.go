package forge

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
	"github.com/quic-go/webtransport-go"
	"github.com/stretchr/testify/require"
)

// localhostTLS returns a server TLS config with a fresh self-signed
// certificate for 127.0.0.1, and a pool that trusts it.
func localhostTLS(t *testing.T) (*tls.Config, *x509.CertPool) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "localhost"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IPAddresses:           []net.IP{net.IPv4(127, 0, 0, 1)},
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)

	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)

	pool := x509.NewCertPool()
	pool.AddCert(cert)

	return &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}},
	}, pool
}

// freeUDPPort returns a UDP port on 127.0.0.1 that was free a moment ago.
// The Router interface doesn't expose the HTTP/3 listener, so the test picks
// the port itself.
func freeUDPPort(t *testing.T) int {
	t.Helper()

	conn, err := (&net.ListenConfig{}).ListenPacket(t.Context(), "udp", "127.0.0.1:0")
	require.NoError(t, err)

	port := conn.LocalAddr().(*net.UDPAddr).Port
	require.NoError(t, conn.Close())

	return port
}

func TestStreamMetricsWebTransport(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")

	a := New(WithAppName("streams-webtransport"))
	echoed := make(chan struct{})
	release := make(chan struct{})

	require.NoError(t, a.Router().WebTransport("/wt/:room", func(ctx Context, s WebTransportSession) error {
		for range 2 {
			data, err := s.ReceiveDatagram(s.Context())
			if err != nil {
				return err
			}

			if err := s.SendDatagram(data); err != nil {
				return err
			}
		}

		close(echoed)
		<-release

		return nil
	}))

	serverTLS, pool := localhostTLS(t)
	addr := "127.0.0.1:" + strconv.Itoa(freeUDPPort(t))

	require.NoError(t, a.Router().EnableWebTransport(DefaultWebTransportConfig()))
	require.NoError(t, a.Router().StartHTTP3(addr, serverTLS))
	t.Cleanup(func() { _ = a.Router().StopHTTP3() })

	client := &webtransport.Transport{
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS13,
			RootCAs:    pool,
			NextProtos: []string{http3.NextProtoH3},
		},
		QUICConfig: &quic.Config{EnableDatagrams: true, EnableStreamResetPartialDelivery: true},
	}

	t.Cleanup(func() { _ = client.Close() })

	dialCtx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	// The body reads the session's CONNECT stream. Closing it ends the session.
	rsp, sess, err := client.Dial(dialCtx, "https://"+addr+"/wt/lobby", nil) //nolint:bodyclose // see above
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, rsp.StatusCode)

	defer sess.CloseWithError(0, "")

	for _, msg := range []string{"one", "two"} {
		require.NoError(t, sess.SendDatagram([]byte(msg)))

		recvCtx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
		got, err := sess.ReceiveDatagram(recvCtx)

		cancel()
		require.NoError(t, err)
		require.Equal(t, msg, string(got))
	}

	<-echoed

	body := scrapeApp(t, a, false)
	require.Contains(t, body, `forge_stream_connections_active{http_route="/wt/:room",stream_kind="webtransport"} 1`)
	require.Contains(t, body, `forge_stream_messages_total{direction="received",http_route="/wt/:room",stream_kind="webtransport"} 2`)
	require.Contains(t, body, `forge_stream_messages_total{direction="sent",http_route="/wt/:room",stream_kind="webtransport"} 2`)

	// The upgrade's 200 went through the status recorders, so the request
	// series has it rather than a 500 or nothing.
	require.Contains(t, body, `http_server_request_duration_seconds_count{error_type="",http_request_method="CONNECT",http_response_status_code="200",http_route="/wt/:room",url_scheme="https"} 1`)

	close(release)

	require.Eventually(t, func() bool {
		body := scrapeApp(t, a, false)

		return strings.Contains(body, `forge_stream_connections_active{http_route="/wt/:room",stream_kind="webtransport"} 0`) &&
			strings.Contains(body, `forge_stream_connection_duration_seconds_count{error_type="",http_route="/wt/:room",stream_kind="webtransport"} 1`)
	}, 2*time.Second, 20*time.Millisecond)
}
