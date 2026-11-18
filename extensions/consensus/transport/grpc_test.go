package transport

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/consensus/internal"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

func startGRPC(t *testing.T, cfg GRPCTransportConfig) *GRPCTransport {
	t.Helper()

	cfg.Address = "127.0.0.1"
	tr := NewGRPCTransport(cfg, forge.NewNoopLogger())

	if err := tr.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	stopOnCleanup(t, tr)

	return tr
}

// rawGRPCCall sends data to the transport's Send method as is, the way a peer
// on another build would.
func rawGRPCCall(t *testing.T, addr string, creds credentials.TransportCredentials, data []byte) error {
	t.Helper()

	conn, err := grpc.NewClient("passthrough:///"+addr,
		grpc.WithTransportCredentials(creds),
		grpc.WithDefaultCallOptions(grpc.ForceCodec(envelopeCodec{})),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	return conn.Invoke(ctx, grpcSendMethod, &envelopeFrame{data: data}, &envelopeFrame{})
}

func voteFrom(from string, requestID uint64) internal.Message {
	return internal.Message{
		Type: internal.MessageTypeRequestVote, From: from, To: "b", RequestID: requestID,
		Payload: &internal.RequestVoteRequest{Term: 9, CandidateID: from},
	}
}

// Raft messages sent over gRPC arrive typed and keep their request ID, and
// every call to a peer shares one client connection.
func TestGRPCTransportDeliversTypedMessages(t *testing.T) {
	a := startGRPC(t, GRPCTransportConfig{NodeID: "a"})
	b := startGRPC(t, GRPCTransportConfig{NodeID: "b"})

	if err := a.AddPeer("b", "127.0.0.1", portOf(t, b.GetAddress())); err != nil {
		t.Fatal(err)
	}

	for i := range 3 {
		if err := a.Send(context.Background(), "b", voteFrom("a", uint64(i+1))); err != nil {
			t.Fatal(err)
		}

		m := receive(t, b)
		if req, ok := m.Payload.(*internal.RequestVoteRequest); !ok || req.CandidateID != "a" || m.RequestID != uint64(i+1) {
			t.Fatalf("received %+v (payload %T)", m, m.Payload)
		}
	}

	snapshot := internal.Message{
		Type: internal.MessageTypeInstallSnapshot, From: "a", To: "b",
		Payload: &internal.InstallSnapshotRequest{Term: 2, LeaderID: "a", Offset: 64, Data: []byte("chunk"), Done: true},
	}
	if err := a.Send(context.Background(), "b", snapshot); err != nil {
		t.Fatal(err)
	}

	if req, ok := receive(t, b).Payload.(*internal.InstallSnapshotRequest); !ok || string(req.Data) != "chunk" || req.Offset != 64 || !req.Done {
		t.Fatalf("snapshot chunk arrived as %+v", req)
	}

	peer := a.peers["b"]
	peer.connMu.Lock()
	first := peer.conn
	peer.connMu.Unlock()

	var wg sync.WaitGroup

	for i := range 20 {
		wg.Go(func() {
			if err := a.Send(context.Background(), "b", voteFrom("a", uint64(100+i))); err != nil {
				t.Error(err)
			}
		})
	}

	wg.Wait()

	for range 20 {
		receive(t, b)
	}

	peer.connMu.Lock()
	defer peer.connMu.Unlock()

	if first == nil || peer.conn != first {
		t.Fatal("concurrent sends did not reuse the peer's client connection")
	}
}

// Messages over MaxMessageSize are refused by the sender, and by a receiver
// whose limit is smaller than the sender's.
func TestGRPCTransportRejectsOversizedMessages(t *testing.T) {
	b := startGRPC(t, GRPCTransportConfig{NodeID: "b", MaxMessageSize: 1024})
	small := startGRPC(t, GRPCTransportConfig{NodeID: "a", MaxMessageSize: 1024})
	large := startGRPC(t, GRPCTransportConfig{NodeID: "a", MaxMessageSize: 1 << 20})

	bPort := portOf(t, b.GetAddress())
	_ = small.AddPeer("b", "127.0.0.1", bPort)
	_ = large.AddPeer("b", "127.0.0.1", bPort)

	big := internal.Message{
		Type: internal.MessageTypeAppendEntries, From: "a",
		Payload: &internal.AppendEntriesRequest{Entries: []internal.LogEntry{{Data: make([]byte, 4096)}}},
	}

	if err := small.Send(context.Background(), "b", big); !errors.Is(err, ErrMessageTooLarge) {
		t.Fatalf("Send over the local limit returned %v, want ErrMessageTooLarge", err)
	}

	if err := large.Send(context.Background(), "b", big); !errors.Is(err, ErrMessageTooLarge) {
		t.Fatalf("Send over the peer's limit returned %v, want ErrMessageTooLarge", err)
	}

	expectNothing(t, b)

	// The connection is still usable for messages within the limit.
	if err := large.Send(context.Background(), "b", voteFrom("a", 1)); err != nil {
		t.Fatal(err)
	}

	receive(t, b)
}

// A message in another wire protocol version is refused, and the sender is
// told which versions disagree. Malformed envelopes are refused too.
func TestGRPCTransportRejectsProtocolVersionMismatch(t *testing.T) {
	b := startGRPC(t, GRPCTransportConfig{NodeID: "b"})

	future := []byte(`{"v":2,"type":"request_vote","from":"a","request_vote":{"term":9}}`)

	err := rawGRPCCall(t, b.GetAddress(), insecure.NewCredentials(), future)
	if status.Code(err) != codes.FailedPrecondition || !strings.Contains(err.Error(), "version 2") || !strings.Contains(err.Error(), "version 1") {
		t.Fatalf("version mismatch returned %v, want FailedPrecondition naming both versions", err)
	}

	if mapped := sendError("b", err); !errors.Is(mapped, ErrProtocolVersion) {
		t.Fatalf("Send would return %v, want ErrProtocolVersion", mapped)
	}

	for _, raw := range []string{
		`not json`,
		`{"v":1,"type":"append_entries","from":"a"}`,
		`{"v":1,"type":"append_entries","from":"a","append_entries":{},"request_vote":{}}`,
	} {
		if err := rawGRPCCall(t, b.GetAddress(), insecure.NewCredentials(), []byte(raw)); status.Code(err) != codes.InvalidArgument {
			t.Fatalf("malformed envelope %s returned %v, want InvalidArgument", raw, err)
		}
	}

	expectNothing(t, b)
}

// With mTLS and peer identity checks, a node can only send as itself, a
// client without a certificate cannot connect, and a dialed server must hold
// the certificate of the peer it was dialed as.
func TestGRPCMTLSPeerIdentity(t *testing.T) {
	pki := newTestPKI(t)

	a := startGRPC(t, GRPCTransportConfig{NodeID: "a", TLSConfig: pki.mtlsConfig(t, "a"), RequirePeerIdentity: true})
	b := startGRPC(t, GRPCTransportConfig{NodeID: "b", TLSConfig: pki.mtlsConfig(t, "b"), RequirePeerIdentity: true})
	mallory := startGRPC(t, GRPCTransportConfig{NodeID: "m", TLSConfig: pki.mtlsConfig(t, "m"), RequirePeerIdentity: true})

	bPort := portOf(t, b.GetAddress())
	_ = a.AddPeer("b", "127.0.0.1", bPort)
	_ = mallory.AddPeer("b", "127.0.0.1", bPort)

	if err := a.Send(context.Background(), "b", voteFrom("a", 1)); err != nil {
		t.Fatalf("legitimate send failed: %v", err)
	}

	if m := receive(t, b); m.From != "a" {
		t.Fatalf("received %+v", m)
	}

	// Mallory holds a valid cluster certificate but claims to be a.
	err := mallory.Send(context.Background(), "b", voteFrom("a", 2))
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("spoofed send returned %v, want PermissionDenied", err)
	}

	expectNothing(t, b)

	// Sending as itself works for mallory, so the refusal was about the
	// claimed identity and not the connection.
	if err := mallory.Send(context.Background(), "b", voteFrom("m", 3)); err != nil {
		t.Fatalf("mallory sending as itself failed: %v", err)
	}

	if m := receive(t, b); m.From != "m" {
		t.Fatalf("received %+v", m)
	}

	// A client without any certificate cannot complete the handshake.
	noCert := credentials.NewTLS(&tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12})
	if err := rawGRPCCall(t, b.GetAddress(), noCert, []byte(`{"v":1,"type":"request_vote","from":"a","request_vote":{"term":9}}`)); err == nil {
		t.Fatal("a client without a certificate delivered a message")
	}

	expectNothing(t, b)

	// a believes c lives at b's address; b's certificate does not name c, so
	// the connection is refused.
	_ = a.AddPeer("c", "127.0.0.1", bPort)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := a.Send(ctx, "c", voteFrom("a", 4)); err == nil {
		t.Fatal("a delivered a message to a server whose certificate names someone else")
	}

	expectNothing(t, b)
}

// Asking for peer identity without mutual TLS fails at startup.
func TestGRPCPeerIdentityRequiresMTLS(t *testing.T) {
	for name, cfg := range map[string]GRPCTransportConfig{
		"no tls":         {NodeID: "a", RequirePeerIdentity: true},
		"no client auth": {NodeID: "a", RequirePeerIdentity: true, TLSConfig: &tls.Config{MinVersion: tls.VersionTLS12}},
	} {
		tr := NewGRPCTransport(cfg, forge.NewNoopLogger())
		if err := tr.Start(context.Background()); err == nil {
			_ = tr.Stop(context.Background())

			t.Fatalf("%s: gRPC transport started with peer identity checks but without mutual TLS", name)
		}
	}
}

// Without an explicit address the transport listens on loopback only.
func TestGRPCTransportDefaultsToLoopback(t *testing.T) {
	if got := NewGRPCTransport(GRPCTransportConfig{NodeID: "a"}, forge.NewNoopLogger()).address; got != "127.0.0.1" {
		t.Fatalf("gRPC default address %q", got)
	}
}

// Inbound connections beyond MaxConnections wait until a slot frees up.
func TestGRPCTransportCapsInboundConnections(t *testing.T) {
	b := startGRPC(t, GRPCTransportConfig{NodeID: "b", MaxConnections: 1})
	a := startGRPC(t, GRPCTransportConfig{NodeID: "a"})
	_ = a.AddPeer("b", "127.0.0.1", portOf(t, b.GetAddress()))

	hog, err := dialTest(t, b.GetAddress())
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	if err := a.Send(ctx, "b", voteFrom("a", 1)); err == nil {
		t.Fatal("a second connection was served over a cap of 1")
	}

	_ = hog.Close()

	deadline := time.Now().Add(10 * time.Second)

	for {
		err := a.Send(context.Background(), "b", voteFrom("a", 2))
		if err == nil {
			break
		}

		if time.Now().After(deadline) {
			t.Fatalf("send still failing after the slot was freed: %v", err)
		}

		time.Sleep(50 * time.Millisecond)
	}

	if m := receive(t, b); m.RequestID != 2 {
		t.Fatalf("received %+v", m)
	}
}

// Stop returns promptly with inbound and outbound connections open, closes
// them, and later sends fail instead of hanging.
func TestGRPCTransportStopsPromptly(t *testing.T) {
	pki := newTestPKI(t)

	for name, tlsFor := range map[string]func(string) *tls.Config{
		"plaintext": func(string) *tls.Config { return nil },
		"mtls":      func(id string) *tls.Config { return pki.mtlsConfig(t, id) },
	} {
		t.Run(name, func(t *testing.T) {
			aTLS := tlsFor("a")
			identity := aTLS != nil
			a := startGRPC(t, GRPCTransportConfig{NodeID: "a", TLSConfig: aTLS, RequirePeerIdentity: identity})

			b := NewGRPCTransport(GRPCTransportConfig{
				NodeID: "b", Address: "127.0.0.1", TLSConfig: tlsFor("b"), RequirePeerIdentity: identity,
			}, forge.NewNoopLogger())
			if err := b.Start(context.Background()); err != nil {
				t.Fatal(err)
			}

			bAddr := b.GetAddress()
			_ = a.AddPeer("b", "127.0.0.1", portOf(t, bAddr))
			_ = b.AddPeer("a", "127.0.0.1", portOf(t, a.GetAddress()))

			if err := a.Send(context.Background(), "b", voteFrom("a", 1)); err != nil {
				t.Fatal(err)
			}

			receive(t, b)

			bToA := internal.Message{
				Type: internal.MessageTypeRequestVoteResponse, From: "b", To: "a", RequestID: 1,
				Payload: &internal.RequestVoteResponse{Term: 9, VoteGranted: true},
			}
			if err := b.Send(context.Background(), "a", bToA); err != nil {
				t.Fatal(err)
			}

			receive(t, a)

			// An idle raw connection that never completes a handshake.
			idle, err := dialTest(t, bAddr)
			if err != nil {
				t.Fatal(err)
			}
			defer idle.Close()

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			start := time.Now()

			if err := b.Stop(ctx); err != nil {
				t.Fatal(err)
			}

			if elapsed := time.Since(start); elapsed > 3*time.Second {
				t.Fatalf("Stop took %v", elapsed)
			}

			if err := b.Send(context.Background(), "a", bToA); !errors.Is(err, internal.ErrNotStarted) {
				t.Fatalf("Send after Stop returned %v, want ErrNotStarted", err)
			}

			_ = idle.SetReadDeadline(time.Now().Add(3 * time.Second))
			if _, err := idle.Read(make([]byte, 1)); isTimeout(err) {
				t.Fatal("Stop left an inbound connection open")
			}

			var d net.Dialer
			if c, err := d.DialContext(t.Context(), "tcp", bAddr); err == nil {
				_ = c.Close()

				t.Fatal("the listener still accepts connections after Stop")
			}

			sendCtx, sendCancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer sendCancel()

			if err := a.Send(sendCtx, "b", voteFrom("a", 2)); err == nil {
				t.Fatal("send to a stopped peer succeeded")
			}

			if err := b.Stop(ctx); !errors.Is(err, internal.ErrNotStarted) {
				t.Fatalf("second Stop returned %v", err)
			}
		})
	}
}
