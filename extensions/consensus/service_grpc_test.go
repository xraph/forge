package consensus

import (
	"context"
	"testing"
	"time"

	"github.com/xraph/forge/extensions/consensus/internal"
)

// Over gRPC with mTLS, a node whose certificate names a different node is cut
// off: the others elect a leader and replicate, but its messages are refused
// and it receives nothing.
func TestServiceGRPCMTLSRejectsMismatchedCertificate(t *testing.T) {
	pki := newServicePKI(t)
	cfgs := clusterConfigs(t, "grpc", 3)

	for i := range cfgs {
		name := cfgs[i].NodeID
		if i == 2 {
			name = "impostor"
		}

		cert, key := pki.issue(t, name)
		cfgs[i].Security = internal.SecurityConfig{
			EnableTLS: true, EnableMTLS: true, CertFile: cert, KeyFile: key, CAFile: pki.caFile,
		}
	}

	services := startServices(t, cfgs)
	leader := waitForServiceLeader(t, services[:2])

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := leader.Apply(ctx, Command{Type: "set", Payload: map[string]any{"key": "k", "value": "v"}}); err != nil {
		t.Fatal(err)
	}

	time.Sleep(500 * time.Millisecond)

	if _, err := services[2].Read(ctx, map[string]any{"type": "get", "key": "k"}); err == nil {
		t.Fatal("the node with a mismatched certificate received replicated data")
	}

	if services[2].IsLeader() {
		t.Fatal("the node with a mismatched certificate became leader")
	}
}

// A running gRPC cluster stops promptly, node by node, while the remaining
// nodes keep connections open to the ones already stopped.
func TestServiceGRPCClusterStopsPromptly(t *testing.T) {
	services := startServices(t, clusterConfigs(t, "grpc", 3))
	replicateAndRead(t, services)

	for _, s := range services {
		start := time.Now()

		if err := s.Stop(context.Background()); err != nil {
			t.Fatalf("stop %s: %v", s.config.NodeID, err)
		}

		if elapsed := time.Since(start); elapsed > 3*time.Second {
			t.Fatalf("stopping %s took %v", s.config.NodeID, elapsed)
		}
	}
}
