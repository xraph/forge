package router

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"io"
	"math"
	"math/big"
	"net"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
	"github.com/quic-go/webtransport-go"
	"github.com/stretchr/testify/require"
)

// h3TLS returns a server TLS config with a fresh self-signed certificate for
// 127.0.0.1, and a pool that trusts it.
func h3TLS(t *testing.T) (*tls.Config, *x509.CertPool) {
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
		DNSNames:              []string{"localhost"},
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

// startH3 enables WebTransport on r, starts its HTTP/3 server on 127.0.0.1
// and returns the server's base URL and a pool that trusts its certificate.
func startH3(t *testing.T, r *router) (string, *x509.CertPool) {
	t.Helper()

	return startH3With(t, r, DefaultWebTransportConfig())
}

// startH3With is startH3 with a WebTransport config of your own.
func startH3With(t *testing.T, r *router, config WebTransportConfig) (string, *x509.CertPool) {
	t.Helper()

	require.NoError(t, r.EnableWebTransport(config))

	serverTLS, pool := h3TLS(t)

	require.NoError(t, r.StartHTTP3("127.0.0.1:0", serverTLS))
	t.Cleanup(func() { _ = r.StopHTTP3() })

	addr := r.http3Addr()
	require.NotNil(t, addr)

	return "https://127.0.0.1:" + strconv.Itoa(addr.(*net.UDPAddr).Port), pool
}

// dialWT opens a WebTransport session the way a webtransport-go client does
// and returns the response status.
func dialWT(t *testing.T, pool *x509.CertPool, url string) (int, *webtransport.Session, error) {
	t.Helper()

	d := &webtransport.Transport{
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS13,
			RootCAs:    pool,
			NextProtos: []string{http3.NextProtoH3},
		},
		QUICConfig: &quic.Config{EnableDatagrams: true, EnableStreamResetPartialDelivery: true},
	}

	t.Cleanup(func() { _ = d.Close() })

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	// The body reads the session's CONNECT stream. Closing it ends the session.
	rsp, sess, err := d.Dial(ctx, url, nil) //nolint:bodyclose // see above
	if rsp == nil {
		return 0, sess, err
	}

	return rsp.StatusCode, sess, err
}

// unwrapWriter stands in for forge's telemetry.StatusRecorder: it hides the
// HTTP/3 writer's own methods and exposes it only through Unwrap.
type unwrapWriter struct {
	http.ResponseWriter

	status int
}

func (w *unwrapWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}

	w.ResponseWriter.WriteHeader(code)
}

func (w *unwrapWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func TestWebTransportUpgradeSucceeds(t *testing.T) {
	r := newRouter()
	served := make(chan struct{})

	require.NoError(t, r.WebTransport("/wt", func(ctx Context, s WebTransportSession) error {
		close(served)

		<-s.Context().Done()

		return nil
	}))

	base, pool := startH3(t, r)

	status, sess, err := dialWT(t, pool, base+"/wt")
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, status)

	defer sess.CloseWithError(0, "")

	select {
	case <-served:
	case <-time.After(5 * time.Second):
		t.Fatal("handler never ran")
	}
}

func TestWebTransportUpgradeThroughWrappedWriter(t *testing.T) {
	r := newRouter()
	recorded := make(chan int, 1)
	served := make(chan struct{})

	r.UseGlobal(PureMiddleware(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			rec := &unwrapWriter{ResponseWriter: w}
			next.ServeHTTP(rec, req)

			recorded <- rec.status
		})
	}).ToMiddleware())

	require.NoError(t, r.WebTransport("/wt", func(ctx Context, s WebTransportSession) error {
		close(served)

		return nil
	}))

	base, pool := startH3(t, r)

	status, sess, err := dialWT(t, pool, base+"/wt")
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, status)

	defer sess.CloseWithError(0, "")

	<-served

	// The 200 has to go through the wrapper, or a status recorder in the
	// middleware chain reports the wrong status for the upgrade.
	select {
	case status := <-recorded:
		require.Equal(t, http.StatusOK, status)
	case <-time.After(5 * time.Second):
		t.Fatal("request never finished")
	}
}

func TestWebTransportRejectsNonHTTP3Writer(t *testing.T) {
	r := newRouter()

	require.NoError(t, r.EnableWebTransport(DefaultWebTransportConfig()))
	require.NoError(t, r.WebTransport("/wt", func(ctx Context, s WebTransportSession) error { return nil }))

	// A CONNECT that claims WebTransport but did not arrive over HTTP/3 must
	// get an error status, not a panic.
	req, err := http.NewRequestWithContext(t.Context(), http.MethodConnect, "/wt", nil)
	require.NoError(t, err)

	req.Proto = "webtransport"

	rec := &statusOnlyWriter{header: http.Header{}}

	require.NotPanics(t, func() { r.ServeHTTP(rec, req) })
	require.GreaterOrEqual(t, rec.status, http.StatusBadRequest)
}

type statusOnlyWriter struct {
	header http.Header
	status int
}

func (w *statusOnlyWriter) Header() http.Header { return w.header }

func (w *statusOnlyWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}

	return len(b), nil
}

func (w *statusOnlyWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
}

func TestWebTransportCloseDoesNotWaitForReceive(t *testing.T) {
	r := newRouter()
	closed := make(chan error, 1)
	receiveErr := make(chan error, 1)

	require.NoError(t, r.WebTransport("/wt", func(ctx Context, s WebTransportSession) error {
		go func() {
			// No datagram ever arrives, so this blocks until the session goes.
			_, err := s.ReceiveDatagram(context.Background())
			receiveErr <- err
		}()

		// Give the receive time to block inside the session.
		time.Sleep(100 * time.Millisecond)

		closed <- s.Close()

		return nil
	}))

	base, pool := startH3(t, r)

	_, sess, err := dialWT(t, pool, base+"/wt")
	require.NoError(t, err)

	defer sess.CloseWithError(0, "")

	select {
	case <-closed:
	case <-time.After(3 * time.Second):
		t.Fatal("Close blocked behind ReceiveDatagram")
	}

	select {
	case err := <-receiveErr:
		require.Error(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("ReceiveDatagram did not return after Close")
	}
}

func TestWebTransportObservesSessionAndDatagrams(t *testing.T) {
	obs := &fakeObserver{}
	r := newRouter(WithStreamObserver(obs))
	probe := &establishProbe{}

	r.UseGlobal(PureMiddleware(probe.wrap).ToMiddleware())

	require.NoError(t, r.WebTransport("/wt/:room", func(ctx Context, s WebTransportSession) error {
		for range 2 {
			data, err := s.ReceiveDatagram(s.Context())
			if err != nil {
				return err
			}

			if err := s.SendDatagram(data); err != nil {
				return err
			}
		}

		return nil
	}))

	base, pool := startH3(t, r)

	status, sess, err := dialWT(t, pool, base+"/wt/lobby")
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, status)

	defer sess.CloseWithError(0, "")

	for _, msg := range []string{"one", "two"} {
		require.NoError(t, sess.SendDatagram([]byte(msg)))

		ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
		got, err := sess.ReceiveDatagram(ctx)

		cancel()
		require.NoError(t, err)
		require.Equal(t, msg, string(got))
	}

	h := obs.only(t)

	require.Equal(t, []string{"webtransport /wt/:room"}, obs.opened)
	require.Equal(t, int64(2), h.received.Load())
	require.Equal(t, int64(2), h.sent.Load())

	if v := h.closeErr.Load(); v != nil {
		require.NoError(t, v.(error))
	}

	probe.mu.Lock()
	defer probe.mu.Unlock()

	require.True(t, probe.fired)
	require.Equal(t, http.StatusOK, probe.status)
}

func TestWebTransportStreamCloseDoesNotWaitForRead(t *testing.T) {
	r := newRouter()
	results := make(chan string, 4)

	require.NoError(t, r.WebTransport("/wt", func(ctx Context, s WebTransportSession) error {
		bidi, err := s.AcceptStream(s.Context())
		if err != nil {
			return err
		}

		uni, err := s.AcceptUniStream(s.Context())
		if err != nil {
			return err
		}

		for name, str := range map[string]WebTransportStream{"bidi": bidi, "uni": uni} {
			buf := make([]byte, 1)
			if _, err := io.ReadFull(str, buf); err != nil {
				return err
			}

			// The client sends nothing more, so this read blocks.
			go func() { _, _ = str.Read(buf) }()

			time.Sleep(50 * time.Millisecond)

			done := make(chan struct{})

			go func() {
				_ = str.Close()

				close(done)
			}()

			select {
			case <-done:
				results <- name + " closed"
			case <-time.After(2 * time.Second):
				results <- name + " Close blocked behind Read"
			}
		}

		return nil
	}))

	base, pool := startH3(t, r)

	_, sess, err := dialWT(t, pool, base+"/wt")
	require.NoError(t, err)

	defer sess.CloseWithError(0, "")

	bidi, err := sess.OpenStream()
	require.NoError(t, err)

	_, err = bidi.Write([]byte("x"))
	require.NoError(t, err)

	uni, err := sess.OpenUniStream()
	require.NoError(t, err)

	_, err = uni.Write([]byte("x"))
	require.NoError(t, err)

	var got []string

	for range 2 {
		select {
		case res := <-results:
			got = append(got, res)
		case <-time.After(10 * time.Second):
			t.Fatal("handler never finished")
		}
	}

	require.ElementsMatch(t, []string{"bidi closed", "uni closed"}, got)
}

func TestWebTransportStreamJSONOverHTTP3(t *testing.T) {
	r := newRouter()
	results := make(chan []int, 2)

	require.NoError(t, r.WebTransport("/wt", func(ctx Context, s WebTransportSession) error {
		bidi, err := s.AcceptStream(s.Context())
		if err != nil {
			return err
		}

		uni, err := s.AcceptUniStream(s.Context())
		if err != nil {
			return err
		}

		for _, str := range []WebTransportStream{bidi, uni} {
			var got []int

			for {
				var msg jsonMsg
				if err := str.ReadJSON(&msg); err != nil {
					break
				}

				got = append(got, msg.N)
			}

			results <- got
		}

		// Reply on the bidirectional stream with two bare numbers.
		if err := bidi.WriteJSON(7); err != nil {
			return err
		}

		if err := bidi.WriteJSON(8); err != nil {
			return err
		}

		return bidi.Close()
	}))

	base, pool := startH3(t, r)

	_, sess, err := dialWT(t, pool, base+"/wt")
	require.NoError(t, err)

	defer sess.CloseWithError(0, "")

	// Each stream gets three values in one write, then the client finishes it.
	bidi, err := sess.OpenStream()
	require.NoError(t, err)

	_, err = bidi.Write([]byte(`{"n":1}{"n":2}{"n":3}`))
	require.NoError(t, err)
	require.NoError(t, bidi.Close())

	uni, err := sess.OpenUniStream()
	require.NoError(t, err)

	_, err = uni.Write([]byte(`{"n":4}` + "\n" + `{"n":5}` + "\n" + `{"n":6}` + "\n"))
	require.NoError(t, err)
	require.NoError(t, uni.Close())

	for _, want := range [][]int{{1, 2, 3}, {4, 5, 6}} {
		select {
		case got := <-results:
			require.Equal(t, want, got)
		case <-time.After(5 * time.Second):
			t.Fatal("handler never read the stream")
		}
	}

	require.NoError(t, bidi.SetReadDeadline(time.Now().Add(5*time.Second)))

	reply, err := io.ReadAll(bidi)
	require.NoError(t, err)
	require.Equal(t, "7\n8\n", string(reply))
}

// dialWTLikeBrowser is dialWT for a client that opens the QPACK encoder and
// decoder streams, as browsers do. quic-go's own client opens only the
// control stream, so without them a test would see two spare
// unidirectional streams that a browser never gets.
func dialWTLikeBrowser(t *testing.T, pool *x509.CertPool, url string) *webtransport.Session {
	t.Helper()

	d := &webtransport.Transport{
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS13,
			RootCAs:    pool,
			NextProtos: []string{http3.NextProtoH3},
		},
		QUICConfig: &quic.Config{EnableDatagrams: true, EnableStreamResetPartialDelivery: true},
		DialAddr: func(ctx context.Context, addr string, tlsConf *tls.Config, conf *quic.Config) (*quic.Conn, error) {
			conn, err := quic.DialAddr(ctx, addr, tlsConf, conf)
			if err != nil {
				return nil, err
			}

			// Stream types 0x02 and 0x03 from RFC 9204 section 4.2.
			for _, streamType := range []byte{0x02, 0x03} {
				str, err := conn.OpenUniStream()
				if err != nil {
					return nil, err
				}

				if _, err := str.Write([]byte{streamType}); err != nil {
					return nil, err
				}
			}

			return conn, nil
		},
	}

	t.Cleanup(func() { _ = d.Close() })

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	rsp, sess, err := d.Dial(ctx, url, nil) //nolint:bodyclose // closing the body ends the session
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, rsp.StatusCode)

	t.Cleanup(func() { _ = sess.CloseWithError(0, "") })

	return sess
}

func TestWebTransportStreamLimits(t *testing.T) {
	const limit = 5

	tests := []struct {
		name   string
		config func(*WebTransportConfig)
		accept func(WebTransportSession) (WebTransportStream, error)
		open   func(*webtransport.Session) (io.Writer, error)
	}{
		{
			name:   "bidirectional",
			config: func(c *WebTransportConfig) { c.MaxBidiStreams = limit },
			accept: func(s WebTransportSession) (WebTransportStream, error) {
				return s.AcceptStream(s.Context())
			},
			open: func(s *webtransport.Session) (io.Writer, error) { return s.OpenStream() },
		},
		{
			name:   "unidirectional",
			config: func(c *WebTransportConfig) { c.MaxUniStreams = limit },
			accept: func(s WebTransportSession) (WebTransportStream, error) {
				return s.AcceptUniStream(s.Context())
			},
			open: func(s *webtransport.Session) (io.Writer, error) { return s.OpenUniStream() },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRouter()
			accepted := make(chan int, 1)

			// The handler reads one byte from each stream and leaves it open,
			// so every stream keeps its place under the limit.
			require.NoError(t, r.WebTransport("/wt", func(ctx Context, s WebTransportSession) error {
				for i := range limit {
					str, err := tt.accept(s)
					if err != nil {
						return err
					}

					if _, err := io.ReadFull(str, make([]byte, 1)); err != nil {
						return err
					}

					accepted <- i + 1
				}

				<-s.Context().Done()

				return nil
			}))

			// The other limit stays at its default of 100. Before the fix it
			// either raised this one or did nothing.
			config := DefaultWebTransportConfig()
			tt.config(&config)

			base, pool := startH3With(t, r, config)
			sess := dialWTLikeBrowser(t, pool, base+"/wt")

			for i := range limit {
				str, err := tt.open(sess)
				require.NoError(t, err, "stream %d of %d", i+1, limit)

				_, err = str.Write([]byte("x"))
				require.NoError(t, err)

				select {
				case n := <-accepted:
					require.Equal(t, i+1, n)
				case <-time.After(5 * time.Second):
					t.Fatalf("handler never accepted stream %d", i+1)
				}
			}

			_, err := tt.open(sess)

			var limitErr *quic.StreamLimitReachedError
			require.ErrorAs(t, err, &limitErr)
		})
	}
}

func TestQUICStreamLimit(t *testing.T) {
	tests := []struct {
		name            string
		limit, reserved int64
		want            int64
	}{
		{"set", 5, 3, 8},
		{"zero takes the default", 0, 3, 103},
		{"negative allows none", -1, 3, 3},
		{"huge stays at quic-go's maximum", math.MaxInt64, 3, 1 << 60},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, quicStreamLimit(tt.limit, tt.reserved))
		})
	}
}
