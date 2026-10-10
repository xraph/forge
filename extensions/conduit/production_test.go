package conduit_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	js "github.com/nats-io/nats.go/jetstream"
	"github.com/xraph/forge/extensions/conduit"
	"github.com/xraph/forge/extensions/conduit/providers/jetstream"
)

func reservePort(t *testing.T) int {
	t.Helper()

	l, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	port := l.Addr().(*net.TCPAddr).Port
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}

	return port
}
func clusterNode(t *testing.T, options *server.Options) *server.Server {
	t.Helper()

	s, err := server.NewServer(options)
	if err != nil {
		t.Fatal(err)
	}

	go s.Start()

	if !s.ReadyForConnections(10 * time.Second) {
		t.Fatal("cluster node did not start")
	}

	t.Cleanup(func() { s.Shutdown(); s.WaitForShutdown() })

	return s
}
func TestThreeNodeFailoverAndRollingRestarts(t *testing.T) {
	if testing.Short() {
		t.Skip("cluster fault integration runs in the mandatory integration job")
	}

	routes := []int{reservePort(t), reservePort(t), reservePort(t)}
	options := make([]*server.Options, 3)

	nodes := make([]*server.Server, 3)
	for i := range nodes {
		peers := []*url.URL{}

		for j, port := range routes {
			if i != j {
				peers = append(peers, &url.URL{Scheme: "nats-route", Host: fmt.Sprintf("127.0.0.1:%d", port)})
			}
		}

		options[i] = &server.Options{ServerName: fmt.Sprintf("conduit-%d", i), Host: "127.0.0.1", Port: reservePort(t), JetStream: true, StoreDir: t.TempDir(), NoLog: true, NoSigs: true, Cluster: server.ClusterOpts{Name: "conduit", Host: "127.0.0.1", Port: routes[i]}, Routes: peers}
		nodes[i] = clusterNode(t, options[i])
	}

	wait(t, func() bool {
		return slices.ContainsFunc(nodes, func(s *server.Server) bool {
			return s.JetStreamIsLeader() && len(s.JetStreamClusterPeers()) == len(nodes)
		})
	})

	addresses := []string{}
	for _, node := range nodes {
		addresses = append(addresses, node.ClientURL())
	}

	address := strings.Join(addresses, ",")
	provider := func() *jetstream.Provider {
		return jetstream.New(jetstream.Options{URL: address, DeadLetterReplicas: 3, NATS: []nats.Option{nats.ReconnectWait(20 * time.Millisecond), nats.MaxReconnects(-1)}})
	}
	cfg := config("billing", "old", true, conduit.Competing)
	streamConfig := cfg.Streams["orders"]
	streamConfig.Replicas = 3
	cfg.Streams["orders"] = streamConfig

	var handled atomic.Int32

	consumer := runtimeFor(t, cfg, provider())
	if err := conduit.Subscribe(consumer, placed, func(context.Context, conduit.Message[order]) error {
		handled.Add(1)

		return nil
	}, conduit.Consumer("process")); err != nil {
		t.Fatal(err)
	}

	start(t, consumer)

	publisherCfg := cfg
	publisherCfg.Identity = conduit.Identity{Namespace: "test", ServiceID: "orders", InstanceID: "publisher"}
	publisher := runtimeFor(t, publisherCfg, provider())
	start(t, publisher)

	conn, err := nats.Connect(address)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	client, err := js.New(conn)
	if err != nil {
		t.Fatal(err)
	}

	names := client.StreamNames(t.Context())

	var stream js.Stream

	for name := range names.Name() {
		if strings.HasPrefix(name, "FC_") {
			stream, err = client.Stream(t.Context(), name)
			if err != nil {
				t.Fatal(err)
			}

			break
		}
	}

	if stream == nil {
		t.Fatal("replicated stream missing")
	}

	publish := func(id string) {
		t.Helper()

		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			receipt, err := conduit.Publish(ctx, publisher, placed, order{ID: id}, conduit.MessageID(id))

			cancel()

			if err == nil {
				if !receipt.Persisted {
					t.Fatal("publication was not persisted")
				}

				return
			}

			time.Sleep(50 * time.Millisecond)
		}

		t.Fatal("publication failed after failover")
	}
	publish("before-failure")
	wait(t, func() bool { return handled.Load() == 1 })
	waitAcknowledged(t, consumer, 1)

	info, err := stream.Info(t.Context())
	if err != nil || info.Cluster == nil {
		t.Fatalf("cluster info=%+v %v", info, err)
	}

	leader := info.Cluster.Leader

	index := slices.IndexFunc(nodes, func(s *server.Server) bool { return s.Name() == leader })
	if index < 0 {
		t.Fatal("stream leader not found")
	}

	nodes[index].Shutdown()
	nodes[index].WaitForShutdown()
	wait(t, func() bool {
		info, err := stream.Info(t.Context())

		return err == nil && info.Cluster.Leader != "" && info.Cluster.Leader != leader
	})
	publish("after-failure")
	wait(t, func() bool { return handled.Load() == 2 })
	waitAcknowledged(t, consumer, 2)
	nodes[index] = clusterNode(t, options[index])
	wait(t, func() bool {
		info, err := stream.Info(t.Context())

		return err == nil && len(info.Cluster.Replicas) == 2 && slices.ContainsFunc(info.Cluster.Replicas, func(peer *js.PeerInfo) bool { return peer.Name == leader && peer.Current })
	})

	if err := consumer.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}

	cfg.Identity.InstanceID = "replacement"

	replacement := runtimeFor(t, cfg, provider())
	if err := conduit.Subscribe(replacement, placed, func(context.Context, conduit.Message[order]) error {
		handled.Add(1)

		return nil
	}, conduit.Consumer("process")); err != nil {
		t.Fatal(err)
	}

	start(t, replacement)
	publish("after-service-roll")
	wait(t, func() bool { return handled.Load() == 3 })
	waitAcknowledged(t, replacement, 1)

	for i := range nodes {
		nodes[i].Shutdown()
		nodes[i].WaitForShutdown()
		nodes[i] = clusterNode(t, options[i])
		wait(t, func() bool {
			info, err := stream.Info(t.Context())

			return err == nil && len(info.Cluster.Replicas) == 2 && slices.ContainsFunc(info.Cluster.Replicas, func(peer *js.PeerInfo) bool { return peer.Name == nodes[i].Name() && peer.Current }) || err == nil && info.Cluster.Leader == nodes[i].Name()
		})
		publish(fmt.Sprintf("roll-%d", i))
		wait(t, func() bool { return handled.Load() == int32(4+i) })
		waitAcknowledged(t, replacement, uint64(2+i))
	}

	info, err = stream.Info(t.Context())
	if err != nil || info.State.Msgs != 6 {
		t.Fatalf("retained after rolls=%+v %v", info, err)
	}
}

type cutProxy struct {
	mu          sync.Mutex
	listener    net.Listener
	cut         bool
	connections map[net.Conn]bool
	target      string
}

func newCutProxy(t *testing.T, target string) *cutProxy {
	t.Helper()

	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	p := &cutProxy{listener: listener, target: target, connections: map[net.Conn]bool{}}

	t.Cleanup(func() { p.setCut(true); _ = listener.Close() })

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}

			go p.forward(t.Context(), conn)
		}
	}()

	return p
}
func (p *cutProxy) forward(ctx context.Context, conn net.Conn) {
	upstream, err := (&net.Dialer{Timeout: time.Second}).DialContext(ctx, "tcp", p.target)
	if err != nil {
		_ = conn.Close()

		return
	}

	p.mu.Lock()
	if p.cut {
		p.mu.Unlock()

		_ = conn.Close()
		_ = upstream.Close()

		return
	}

	p.connections[conn], p.connections[upstream] = true, true
	p.mu.Unlock()

	defer func() {
		_ = conn.Close()
		_ = upstream.Close()

		p.mu.Lock()
		delete(p.connections, conn)
		delete(p.connections, upstream)
		p.mu.Unlock()
	}()

	go func() { _, _ = io.Copy(upstream, conn); _ = upstream.Close() }()

	_, _ = io.Copy(conn, upstream)
}
func (p *cutProxy) setCut(value bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.cut = value
	if value {
		for conn := range p.connections {
			_ = conn.Close()
		}
	}
}
func TestNetworkPartitionAndStablePublicationRetry(t *testing.T) {
	srv := brokerServer(t, t.TempDir(), -1)
	proxy := newCutProxy(t, srv.Addr().String())
	p := jetstream.New(jetstream.Options{URL: "nats://" + proxy.listener.Addr().String(), NATS: []nats.Option{nats.ReconnectWait(20 * time.Millisecond), nats.MaxReconnects(-1)}})
	r := runtimeFor(t, config("orders", "publisher", false, conduit.Competing), p)
	start(t, r)
	proxy.setCut(true)

	ctx, cancel := context.WithTimeout(t.Context(), 150*time.Millisecond)
	receipt, err := conduit.Publish(ctx, r, placed, order{ID: "partition"}, conduit.MessageID("partition"))

	cancel()

	if !errors.Is(err, conduit.ErrOutcomeUnknown) || receipt.Persisted {
		t.Fatalf("partition receipt=%+v %v", receipt, err)
	}

	proxy.setCut(false)
	wait(t, func() bool {
		ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
		defer cancel()

		return p.Health(ctx) == nil
	})

	receipt, err = conduit.Publish(t.Context(), r, placed, order{ID: "partition"}, conduit.MessageID("partition"))
	if err != nil || !receipt.Persisted {
		t.Fatalf("retry=%+v %v", receipt, err)
	}

	duplicate, err := conduit.Publish(t.Context(), r, placed, order{ID: "partition"}, conduit.MessageID("partition"))
	if err != nil || !duplicate.Duplicate || duplicate.Sequence != receipt.Sequence {
		t.Fatalf("retry dedupe=%+v %v", duplicate, err)
	}

	snapshot, err := r.Snapshot(t.Context())
	if err != nil || snapshot.Streams[0].Messages != 1 {
		t.Fatalf("retained duplicate=%+v %v", snapshot, err)
	}
}
func TestCrashedInstanceLeaseExpires(t *testing.T) {
	if testing.Short() {
		t.Skip("45-second crash lease integration runs in the mandatory integration job")
	}

	srv := brokerServer(t, t.TempDir(), -1)
	dead := jetstream.New(jetstream.Options{URL: srv.ClientURL()})

	peer := jetstream.New(jetstream.Options{URL: srv.ClientURL()})
	for _, p := range []*jetstream.Provider{dead, peer} {
		if err := p.Connect(t.Context()); err != nil {
			t.Fatal(err)
		}

		t.Cleanup(func() { _ = p.Close(t.Context()) })
	}

	identity := conduit.Identity{Namespace: "test", ServiceID: "billing", InstanceID: "crashed"}
	if err := dead.Register(t.Context(), conduit.Instance{Identity: identity, Ready: true}); err != nil {
		t.Fatal(err)
	}

	members, err := peer.Resolve(t.Context(), "test", "billing")
	if err != nil || len(members) != 1 {
		t.Fatal("instance was not discoverable")
	}

	if err := dead.Close(t.Context()); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(55 * time.Second)
	for time.Now().Before(deadline) {
		members, err := peer.Resolve(t.Context(), "test", "billing")
		if err != nil {
			t.Fatal(err)
		}

		if len(members) == 0 {
			return
		}

		time.Sleep(200 * time.Millisecond)
	}

	t.Fatal("crashed instance lease did not expire")
}

func waitAcknowledged(t *testing.T, r *conduit.Runtime, count uint64) {
	t.Helper()
	wait(t, func() bool {
		snapshot, err := r.Snapshot(t.Context())

		return err == nil && snapshot.Acknowledged >= count
	})
}
