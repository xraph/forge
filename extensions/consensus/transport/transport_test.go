package transport

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/consensus/internal"
)

func stopOnCleanup(t *testing.T, tr internal.Transport) {
	t.Helper()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		_ = tr.Stop(ctx)
	})
}

func startTCP(t *testing.T, cfg TCPTransportConfig) *TCPTransport {
	t.Helper()

	cfg.Address = "127.0.0.1"
	tr := NewTCPTransport(cfg, forge.NewNoopLogger())

	if err := tr.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	stopOnCleanup(t, tr)

	return tr
}

func startHTTP(t *testing.T, cfg HTTPTransportConfig) *HTTPTransport {
	t.Helper()

	cfg.Address = "127.0.0.1"
	tr := NewHTTPTransport(cfg, forge.NewNoopLogger())

	if err := tr.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	stopOnCleanup(t, tr)

	return tr
}

func receive(t *testing.T, tr internal.Transport) internal.Message {
	t.Helper()

	select {
	case m := <-tr.Receive():
		return m
	case <-time.After(3 * time.Second):
		t.Fatal("no message received")

		return internal.Message{}
	}
}

func expectNothing(t *testing.T, tr internal.Transport) {
	t.Helper()

	select {
	case m := <-tr.Receive():
		t.Fatalf("unexpected message %+v", m)
	case <-time.After(200 * time.Millisecond):
	}
}

func portOf(t *testing.T, addr string) int {
	t.Helper()

	_, p, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}

	var port int
	if _, err := fmt.Sscan(p, &port); err != nil {
		t.Fatal(err)
	}

	return port
}

// Every Raft payload survives the wire with its concrete pointer type, which
// is what the raft node's type switches need.
func TestEnvelopeRoundTripsEveryRaftPayload(t *testing.T) {
	payloads := map[internal.MessageType]any{
		internal.MessageTypeAppendEntries: &internal.AppendEntriesRequest{
			Term: 3, LeaderID: "a", PrevLogIndex: 4, PrevLogTerm: 2, LeaderCommit: 4,
			Entries: []internal.LogEntry{{Index: 5, Term: 3, Data: []byte("cmd")}},
		},
		internal.MessageTypeAppendEntriesResponse:   &internal.AppendEntriesResponse{Term: 3, Success: true, MatchIndex: 5},
		internal.MessageTypeRequestVote:             internal.RequestVoteRequest{Term: 3, CandidateID: "a"},
		internal.MessageTypeRequestVoteResponse:     &internal.RequestVoteResponse{Term: 3, VoteGranted: true},
		internal.MessageTypeInstallSnapshot:         &internal.InstallSnapshotRequest{Term: 3, Data: []byte{1, 2}},
		internal.MessageTypeInstallSnapshotResponse: &internal.InstallSnapshotResponse{Term: 3},
	}

	for typ, payload := range payloads {
		data, _, err := encodeMessage(internal.Message{Type: typ, From: "a", To: "b", RequestID: 7, Payload: payload})
		if err != nil {
			t.Fatalf("%s: encode: %v", typ, err)
		}

		msg, err := decodeMessage(data)
		if err != nil {
			t.Fatalf("%s: decode: %v", typ, err)
		}

		if msg.RequestID != 7 || msg.From != "a" {
			t.Fatalf("%s: header lost: %+v", typ, msg)
		}

		var ok bool

		switch typ {
		case internal.MessageTypeAppendEntries:
			var p *internal.AppendEntriesRequest

			p, ok = msg.Payload.(*internal.AppendEntriesRequest)
			ok = ok && len(p.Entries) == 1 && string(p.Entries[0].Data) == "cmd"
		case internal.MessageTypeAppendEntriesResponse:
			_, ok = msg.Payload.(*internal.AppendEntriesResponse)
		case internal.MessageTypeRequestVote:
			_, ok = msg.Payload.(*internal.RequestVoteRequest)
		case internal.MessageTypeRequestVoteResponse:
			_, ok = msg.Payload.(*internal.RequestVoteResponse)
		case internal.MessageTypeInstallSnapshot:
			_, ok = msg.Payload.(*internal.InstallSnapshotRequest)
		case internal.MessageTypeInstallSnapshotResponse:
			_, ok = msg.Payload.(*internal.InstallSnapshotResponse)
		}

		if !ok {
			t.Fatalf("%s: decoded payload %T (%+v)", typ, msg.Payload, msg.Payload)
		}
	}
}

func TestEnvelopeRejectsInconsistentMessages(t *testing.T) {
	if _, _, err := encodeMessage(internal.Message{
		Type: internal.MessageTypeAppendEntries, Payload: &internal.RequestVoteRequest{},
	}); err == nil {
		t.Fatal("encoded a payload that does not match the message type")
	}

	for _, raw := range []string{
		`{"v":1,"type":"append_entries","from":"a"}`,
		`{"v":1,"type":"append_entries","from":"a","append_entries":{},"request_vote":{}}`,
		`{"v":1,"type":"append_entries","from":"a","payload":{"term":9}}`,
		`{"v":1,"type":"bogus","from":"a"}`,
	} {
		if _, err := decodeMessage([]byte(raw)); err == nil {
			t.Fatalf("decoded invalid message %s", raw)
		}
	}
}

// Raft messages sent over TCP arrive promptly and typed. The old transport
// never registered the payload types with gob and never flushed its writer.
func TestTCPTransportDeliversTypedMessages(t *testing.T) {
	a := startTCP(t, TCPTransportConfig{NodeID: "a"})
	b := startTCP(t, TCPTransportConfig{NodeID: "b"})

	if err := a.AddPeer("b", "127.0.0.1", portOf(t, b.GetAddress())); err != nil {
		t.Fatal(err)
	}

	for i := range 3 {
		err := a.Send(context.Background(), "b", internal.Message{
			Type: internal.MessageTypeRequestVote, From: "a", To: "b", RequestID: uint64(i + 1),
			Payload: &internal.RequestVoteRequest{Term: 1, CandidateID: "a"},
		})
		if err != nil {
			t.Fatal(err)
		}

		m := receive(t, b)
		if req, ok := m.Payload.(*internal.RequestVoteRequest); !ok || req.CandidateID != "a" || m.RequestID != uint64(i+1) {
			t.Fatalf("received %+v (payload %T)", m, m.Payload)
		}
	}
}

// A frame header announcing more than MaxMessageSize closes the connection
// before anything is allocated for it.
func TestTCPTransportRejectsOversizedFrames(t *testing.T) {
	b := startTCP(t, TCPTransportConfig{NodeID: "b", MaxMessageSize: 1024})

	conn, err := dialTest(t, b.GetAddress())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	var header [4]byte
	binary.BigEndian.PutUint32(header[:], 1<<30)

	if _, err := conn.Write(header[:]); err != nil {
		t.Fatal(err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))

	if _, err := conn.Read(make([]byte, 1)); !errors.Is(err, io.EOF) && !isConnReset(err) {
		t.Fatalf("connection stayed open after a 1 GiB frame header: %v", err)
	}

	a := startTCP(t, TCPTransportConfig{NodeID: "a", MaxMessageSize: 1024})
	_ = a.AddPeer("b", "127.0.0.1", portOf(t, b.GetAddress()))

	err = a.Send(context.Background(), "b", internal.Message{
		Type: internal.MessageTypeAppendEntries, From: "a",
		Payload: &internal.AppendEntriesRequest{Entries: []internal.LogEntry{{Data: make([]byte, 4096)}}},
	})
	if !errors.Is(err, ErrMessageTooLarge) {
		t.Fatalf("Send of an oversized message returned %v, want ErrMessageTooLarge", err)
	}
}

func isConnReset(err error) bool {
	return err != nil && strings.Contains(err.Error(), "reset")
}

// Inbound connections beyond MaxConnections are closed, and Stop closes the
// ones that are open instead of waiting for their read deadline.
func TestTCPTransportCapsAndClosesInboundConnections(t *testing.T) {
	tr := NewTCPTransport(TCPTransportConfig{NodeID: "b", Address: "127.0.0.1", MaxConnections: 2}, forge.NewNoopLogger())
	if err := tr.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	var conns []net.Conn

	for range 3 {
		c, err := dialTest(t, tr.GetAddress())
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()

		conns = append(conns, c)
	}

	_ = conns[2].SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, err := conns[2].Read(make([]byte, 1)); err == nil || isTimeout(err) {
		t.Fatalf("third connection over a cap of 2 was not closed: %v", err)
	}

	start := time.Now()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := tr.Stop(ctx); err != nil {
		t.Fatal(err)
	}

	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("Stop took %v with idle inbound connections open", elapsed)
	}

	_ = conns[0].SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, err := conns[0].Read(make([]byte, 1)); isTimeout(err) {
		t.Fatal("Stop left an inbound connection open")
	}
}

func isTimeout(err error) bool {
	var ne net.Error

	return errors.As(err, &ne) && ne.Timeout()
}

// Raft messages sent over HTTP arrive typed. They used to arrive as
// map[string]any, so every type assertion in the raft node failed.
func TestHTTPTransportDeliversTypedMessages(t *testing.T) {
	a := startHTTP(t, HTTPTransportConfig{NodeID: "a"})
	b := startHTTP(t, HTTPTransportConfig{NodeID: "b"})
	_ = a.AddPeer("b", "127.0.0.1", portOf(t, b.GetAddress()))

	err := a.Send(context.Background(), "b", internal.Message{
		Type: internal.MessageTypeAppendEntries, From: "a", To: "b",
		Payload: &internal.AppendEntriesRequest{Term: 1, LeaderID: "a"},
	})
	if err != nil {
		t.Fatal(err)
	}

	m := receive(t, b)
	if req, ok := m.Payload.(*internal.AppendEntriesRequest); !ok || req.LeaderID != "a" {
		t.Fatalf("received payload %T %+v", m.Payload, m.Payload)
	}
}

// Request bodies are capped at MaxMessageSize instead of read in full.
func TestHTTPTransportRejectsOversizedBodies(t *testing.T) {
	b := startHTTP(t, HTTPTransportConfig{NodeID: "b", MaxMessageSize: 1024})

	body := bytes.Repeat([]byte(" "), 64<<10)

	resp, err := postTest(t, http.DefaultClient, "http://"+b.GetAddress()+"/raft/message", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}

	resp.Body.Close()

	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", resp.StatusCode)
	}

	expectNothing(t, b)
}

// Without an explicit address the transports listen on loopback only.
func TestTransportsDefaultToLoopback(t *testing.T) {
	if got := NewTCPTransport(TCPTransportConfig{NodeID: "a"}, forge.NewNoopLogger()).address; got != "127.0.0.1" {
		t.Fatalf("TCP default address %q", got)
	}

	if got := NewHTTPTransport(HTTPTransportConfig{NodeID: "a"}, forge.NewNoopLogger()).address; got != "127.0.0.1" {
		t.Fatalf("HTTP default address %q", got)
	}
}

// The gRPC transport is not implemented and says so instead of pretending.
func TestGRPCTransportFailsLoudly(t *testing.T) {
	g := NewGRPCTransport(GRPCTransportConfig{NodeID: "a", Address: "127.0.0.1"}, forge.NewNoopLogger())

	if err := g.Start(context.Background()); !errors.Is(err, ErrGRPCNotImplemented) {
		t.Fatalf("Start returned %v", err)
	}

	_ = g.AddPeer("b", "127.0.0.1", 1)

	if err := g.Send(context.Background(), "b", internal.Message{Type: internal.MessageTypeRequestVote}); !errors.Is(err, ErrGRPCNotImplemented) {
		t.Fatalf("Send returned %v", err)
	}
}

// testPKI writes a CA and per-node certificates (node ID as DNS SAN) to dir.
type testPKI struct {
	dir    string
	caFile string
	caKey  *ecdsa.PrivateKey
	caCert *x509.Certificate
}

func newTestPKI(t *testing.T) *testPKI {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}

	cert, _ := x509.ParseCertificate(der)
	p := &testPKI{dir: t.TempDir(), caKey: key, caCert: cert}
	p.caFile = filepath.Join(p.dir, "ca.pem")
	writePEM(t, p.caFile, "CERTIFICATE", der)

	return p
}

func (p *testPKI) node(t *testing.T, name string) (certFile, keyFile string) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: name},
		DNSNames:     []string{name},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, p.caCert, &key.PublicKey, p.caKey)
	if err != nil {
		t.Fatal(err)
	}

	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}

	certFile = filepath.Join(p.dir, name+".pem")
	keyFile = filepath.Join(p.dir, name+"-key.pem")

	writePEM(t, certFile, "CERTIFICATE", der)
	writePEM(t, keyFile, "EC PRIVATE KEY", keyDER)

	return certFile, keyFile
}

func writePEM(t *testing.T, path, typ string, der []byte) {
	t.Helper()

	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: typ, Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
}

// mtlsConfig builds a node's TLS config with the existing TLSManager helper.
func (p *testPKI) mtlsConfig(t *testing.T, name string) *tls.Config {
	t.Helper()

	certFile, keyFile := p.node(t, name)

	tm, err := NewTLSManager(MTLSConfig(certFile, keyFile, p.caFile), forge.NewNoopLogger())
	if err != nil {
		t.Fatal(err)
	}

	return tm.GetTLSConfig()
}

// With mTLS and peer identity checks, a node can only send as itself.
func TestMTLSPeerIdentity(t *testing.T) {
	pki := newTestPKI(t)

	for _, kind := range []string{"tcp", "http"} {
		t.Run(kind, func(t *testing.T) {
			var a, b, mallory internal.Transport

			if kind == "tcp" {
				a = startTCP(t, TCPTransportConfig{NodeID: "a", TLSConfig: pki.mtlsConfig(t, "a"), RequirePeerIdentity: true})
				b = startTCP(t, TCPTransportConfig{NodeID: "b", TLSConfig: pki.mtlsConfig(t, "b"), RequirePeerIdentity: true})
				mallory = startTCP(t, TCPTransportConfig{NodeID: "m", TLSConfig: pki.mtlsConfig(t, "m"), RequirePeerIdentity: true})
			} else {
				a = startHTTP(t, HTTPTransportConfig{NodeID: "a", TLSConfig: pki.mtlsConfig(t, "a"), RequirePeerIdentity: true})
				b = startHTTP(t, HTTPTransportConfig{NodeID: "b", TLSConfig: pki.mtlsConfig(t, "b"), RequirePeerIdentity: true})
				mallory = startHTTP(t, HTTPTransportConfig{NodeID: "m", TLSConfig: pki.mtlsConfig(t, "m"), RequirePeerIdentity: true})
			}

			bPort := portOf(t, b.GetAddress())
			_ = a.AddPeer("b", "127.0.0.1", bPort)
			_ = mallory.AddPeer("b", "127.0.0.1", bPort)

			vote := func(from string) internal.Message {
				return internal.Message{
					Type: internal.MessageTypeRequestVote, From: from, To: "b",
					Payload: &internal.RequestVoteRequest{Term: 9, CandidateID: from},
				}
			}

			if err := a.Send(context.Background(), "b", vote("a")); err != nil {
				t.Fatalf("legitimate send failed: %v", err)
			}

			if m := receive(t, b); m.From != "a" {
				t.Fatalf("received %+v", m)
			}

			// Mallory holds a valid cluster certificate but claims to be a.
			_ = mallory.Send(context.Background(), "b", vote("a"))

			expectNothing(t, b)

			// A client without any certificate cannot connect at all.
			plain := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			}}}

			if kind == "http" {
				body := `{"v":1,"type":"request_vote","from":"a","request_vote":{"term":9}}`
				if resp, err := postTest(t, plain, "https://"+b.GetAddress()+"/raft/message", strings.NewReader(body)); err == nil {
					resp.Body.Close()
					t.Fatalf("request without a client certificate got status %d", resp.StatusCode)
				}
			}

			expectNothing(t, b)
		})
	}
}

// Asking for peer identity without mutual TLS fails at startup.
func TestPeerIdentityRequiresMTLS(t *testing.T) {
	tcp := NewTCPTransport(TCPTransportConfig{NodeID: "a", RequirePeerIdentity: true}, forge.NewNoopLogger())
	if err := tcp.Start(context.Background()); err == nil {
		_ = tcp.Stop(context.Background())

		t.Fatal("TCP transport started with peer identity checks but no TLS")
	}

	h := NewHTTPTransport(HTTPTransportConfig{NodeID: "a", RequirePeerIdentity: true, TLSConfig: &tls.Config{MinVersion: tls.VersionTLS12}}, forge.NewNoopLogger())
	if err := h.Start(context.Background()); err == nil {
		_ = h.Stop(context.Background())

		t.Fatal("HTTP transport started with peer identity checks but no client certificate requirement")
	}
}

func dialTest(t *testing.T, addr string) (net.Conn, error) {
	t.Helper()

	var d net.Dialer

	return d.DialContext(t.Context(), "tcp", addr)
}

func postTest(t *testing.T, client *http.Client, url string, body io.Reader) (*http.Response, error) {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, url, body)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")

	return client.Do(req)
}

// A message in another wire protocol version is rejected with an error that
// names both versions. Over HTTP the sender gets it back; over TCP the
// receiver logs it and drops the connection.
func TestWireProtocolVersionMismatchIsRejected(t *testing.T) {
	future := []byte(`{"v":2,"type":"request_vote","from":"a","request_vote":{"term":9}}`)

	_, err := decodeMessage(future)
	if !errors.Is(err, ErrProtocolVersion) || !strings.Contains(err.Error(), "version 2") || !strings.Contains(err.Error(), "version 1") {
		t.Fatalf("decode error %v does not name both versions", err)
	}

	missing := []byte(`{"type":"request_vote","from":"a","request_vote":{"term":9}}`)
	if _, err := decodeMessage(missing); !errors.Is(err, ErrProtocolVersion) {
		t.Fatalf("message without a version decoded: %v", err)
	}

	h := startHTTP(t, HTTPTransportConfig{NodeID: "b"})

	resp, err := postTest(t, http.DefaultClient, "http://"+h.GetAddress()+"/raft/message", bytes.NewReader(future))
	if err != nil {
		t.Fatal(err)
	}

	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), "version 2") {
		t.Fatalf("HTTP status %d body %q, want 400 naming the versions", resp.StatusCode, body)
	}

	expectNothing(t, h)

	tcp := startTCP(t, TCPTransportConfig{NodeID: "b"})

	conn, err := dialTest(t, tcp.GetAddress())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(future)))

	if _, err := conn.Write(append(header[:], future...)); err != nil {
		t.Fatal(err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, err := conn.Read(make([]byte, 1)); isTimeout(err) {
		t.Fatal("TCP connection stayed open after a version mismatch")
	}

	expectNothing(t, tcp)
}
