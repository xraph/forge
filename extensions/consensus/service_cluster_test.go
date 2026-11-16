package consensus

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/consensus/internal"
)

func freePorts(t *testing.T, n int) []int {
	t.Helper()

	ports := make([]int, n)

	var lc net.ListenConfig

	for i := range n {
		l, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}

		ports[i] = l.Addr().(*net.TCPAddr).Port
		_ = l.Close()
	}

	return ports
}

func clusterConfigs(t *testing.T, transportType string, size int) []internal.Config {
	t.Helper()

	ports := freePorts(t, size)
	peers := make([]internal.PeerConfig, size)

	for i := range size {
		peers[i] = internal.PeerConfig{ID: fmt.Sprintf("n%d", i+1), Address: "127.0.0.1", Port: ports[i]}
	}

	cfgs := make([]internal.Config, size)

	for i := range size {
		c := internal.DefaultConfig()
		c.NodeID = peers[i].ID
		c.BindAddr = "127.0.0.1"
		c.BindPort = ports[i]
		c.Peers = peers
		c.Transport.Type = transportType
		c.Storage.Type = "memory"
		c.Raft.HeartbeatInterval = 50 * time.Millisecond
		c.Raft.ElectionTimeoutMin = 300 * time.Millisecond
		c.Raft.ElectionTimeoutMax = 600 * time.Millisecond
		cfgs[i] = c
	}

	return cfgs
}

func startServices(t *testing.T, cfgs []internal.Config) []*Service {
	t.Helper()

	services := make([]*Service, len(cfgs))

	for i, cfg := range cfgs {
		s, err := NewConsensusService(cfg, forge.NewNoopLogger(), nil)
		if err != nil {
			t.Fatal(err)
		}

		if err := s.Start(context.Background()); err != nil {
			t.Fatalf("start %s: %v", cfg.NodeID, err)
		}

		services[i] = s
	}

	t.Cleanup(func() {
		for _, s := range services {
			_ = s.Stop(context.Background())
		}
	})

	return services
}

func waitForServiceLeader(t *testing.T, services []*Service) *Service {
	t.Helper()

	deadline := time.Now().Add(15 * time.Second)

	for time.Now().Before(deadline) {
		var leaders []*Service

		for _, s := range services {
			if s.IsLeader() {
				leaders = append(leaders, s)
			}
		}

		if len(leaders) == 1 {
			return leaders[0]
		}

		time.Sleep(20 * time.Millisecond)
	}

	t.Fatal("no single leader elected")

	return nil
}

// replicateAndRead applies a command through the leader's service and waits
// until every node's service reads it back.
func replicateAndRead(t *testing.T, services []*Service) {
	t.Helper()

	leader := waitForServiceLeader(t, services)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := Command{Type: "set", Payload: map[string]any{"key": "greeting", "value": "hello"}}
	if err := leader.Apply(ctx, cmd); err != nil {
		t.Fatalf("apply on leader %s: %v", leader.config.NodeID, err)
	}

	for _, s := range services {
		deadline := time.Now().Add(10 * time.Second)

		for {
			got, err := s.Read(ctx, map[string]any{"type": "get", "key": "greeting"})
			if err == nil && got == "hello" {
				break
			}

			if time.Now().After(deadline) {
				t.Fatalf("%s read %v, %v", s.config.NodeID, got, err)
			}

			time.Sleep(20 * time.Millisecond)
		}

		if !s.hasQuorum() {
			t.Fatalf("%s reports no quorum in a healthy cluster", s.config.NodeID)
		}
	}

	info := leader.GetClusterInfo()
	if info.TotalNodes != 3 || info.ActiveNodes != 3 || info.Leader != leader.config.NodeID {
		t.Fatalf("cluster info from leader: %+v", info)
	}
}

// Three services started from configuration elect a leader and replicate a
// command over each network transport.
func TestServiceClusterReplicates(t *testing.T) {
	for _, kind := range []string{"tcp", "http", "grpc"} {
		t.Run(kind, func(t *testing.T) {
			replicateAndRead(t, startServices(t, clusterConfigs(t, kind, 3)))
		})
	}
}

// The same cluster with security.enable_mtls: every node presents a
// certificate naming its node ID, and a node whose certificate names someone
// else cannot take part.
func TestServiceClusterReplicatesOverMTLS(t *testing.T) {
	pki := newServicePKI(t)

	for _, kind := range []string{"tcp", "http", "grpc"} {
		t.Run(kind, func(t *testing.T) {
			cfgs := clusterConfigs(t, kind, 3)

			for i := range cfgs {
				cert, key := pki.issue(t, cfgs[i].NodeID)
				cfgs[i].Security = internal.SecurityConfig{
					EnableTLS: true, EnableMTLS: true, CertFile: cert, KeyFile: key, CAFile: pki.caFile,
				}
			}

			replicateAndRead(t, startServices(t, cfgs))
		})
	}
}

// A node whose certificate names a different node is cut off: the others
// still elect a leader, but its messages are refused.
func TestServiceMTLSRejectsMismatchedCertificate(t *testing.T) {
	pki := newServicePKI(t)
	cfgs := clusterConfigs(t, "tcp", 3)

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

// Settings the service cannot honour fail with a clear error.
func TestServiceRejectsUnsupportedSettings(t *testing.T) {
	cases := map[string]func(*internal.Config){
		"quic transport":   func(c *internal.Config) { c.Transport.Type = "quic" },
		"encryption":       func(c *internal.Config) { c.Security.EnableEncryption = true },
		"dns discovery":    func(c *internal.Config) { c.Discovery.Type = "dns" },
		"postgres storage": func(c *internal.Config) { c.Storage.Type = "postgres" },
		"mtls without a ca": func(c *internal.Config) {
			c.Security = internal.SecurityConfig{EnableTLS: true, EnableMTLS: true, CertFile: "c", KeyFile: "k"}
		},
	}

	for name, mutate := range cases {
		cfg := internal.DefaultConfig()
		cfg.NodeID = "n1"
		mutate(&cfg)

		if _, err := NewConsensusService(cfg, forge.NewNoopLogger(), nil); !errors.Is(err, internal.ErrInvalidConfig) {
			t.Fatalf("%s: NewConsensusService returned %v, want ErrInvalidConfig", name, err)
		}
	}
}

// Membership changes are refused instead of being half applied.
func TestServiceRefusesMembershipChanges(t *testing.T) {
	cfg := clusterConfigs(t, "tcp", 1)[0]
	s := startServices(t, []internal.Config{cfg})[0]

	if err := s.AddNode(context.Background(), "n9", "127.0.0.1", 1); !errors.Is(err, ErrMembershipChangeUnsupported) {
		t.Fatalf("AddNode returned %v", err)
	}
}

type servicePKI struct {
	dir    string
	caFile string
	caKey  *ecdsa.PrivateKey
	ca     *x509.Certificate
}

func newServicePKI(t *testing.T) *servicePKI {
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

	ca, _ := x509.ParseCertificate(der)
	p := &servicePKI{dir: t.TempDir(), caKey: key, ca: ca}
	p.caFile = filepath.Join(p.dir, "ca.pem")
	writeTestPEM(t, p.caFile, "CERTIFICATE", der)

	return p
}

func (p *servicePKI) issue(t *testing.T, name string) (certFile, keyFile string) {
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

	der, err := x509.CreateCertificate(rand.Reader, tmpl, p.ca, &key.PublicKey, p.caKey)
	if err != nil {
		t.Fatal(err)
	}

	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}

	certFile = filepath.Join(p.dir, name+".pem")
	keyFile = filepath.Join(p.dir, name+"-key.pem")

	writeTestPEM(t, certFile, "CERTIFICATE", der)
	writeTestPEM(t, keyFile, "EC PRIVATE KEY", keyDER)

	return certFile, keyFile
}

func writeTestPEM(t *testing.T, path, typ string, der []byte) {
	t.Helper()

	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: typ, Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
}
