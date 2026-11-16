package backends

import (
	"context"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
)

// TLSEnabled must produce a TLS handshake, not plaintext h2c carrying the
// username and password.
func TestEtcdTLSEnabledSpeaksTLS(t *testing.T) {
	l, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	got := make(chan []byte, 1)

	go func() {
		c, err := l.Accept()
		if err != nil {
			return
		}
		defer c.Close()

		buf := make([]byte, 64)
		_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))

		n, _ := c.Read(buf)
		got <- buf[:n]
	}()

	// With credentials set the client authenticates while it is created, so
	// the handshake happens inside NewEtcdBackend. The fake server never
	// completes it, so the constructor's error is expected and ignored.
	done := make(chan struct{})

	go func() {
		defer close(done)

		b, err := NewEtcdBackend(EtcdConfig{
			Endpoints: []string{l.Addr().String()}, Username: "svc", Password: "s3cret",
			TLSEnabled: true, DialTimeout: 2 * time.Second,
		})
		if err == nil {
			_ = b.client.Close()
		}
	}()

	defer func() { <-done }()

	select {
	case first := <-got:
		// A TLS record starts with content type 0x16 (handshake) and version major 0x03.
		if len(first) < 2 || first[0] != 0x16 || first[1] != 0x03 {
			t.Fatalf("first bytes %q are not a TLS ClientHello", first)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("client never connected")
	}
}

func writeTestCA(t *testing.T) string {
	t.Helper()

	srv := httptest.NewTLSServer(http.NotFoundHandler())
	defer srv.Close()

	file := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(file, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw}), 0o600); err != nil {
		t.Fatal(err)
	}

	return file
}

func TestBuildEtcdTLSConfig(t *testing.T) {
	ca := writeTestCA(t)
	notPEM := filepath.Join(t.TempDir(), "bad.pem")

	if err := os.WriteFile(notPEM, []byte("nope"), 0o600); err != nil {
		t.Fatal(err)
	}

	if cfg, err := buildEtcdTLSConfig(EtcdConfig{}); err != nil || cfg != nil {
		t.Fatalf("disabled: cfg=%v err=%v, want nil, nil", cfg, err)
	}

	cfg, err := buildEtcdTLSConfig(EtcdConfig{TLSEnabled: true, TLSCAFile: ca})
	if err != nil {
		t.Fatal(err)
	}

	if cfg.RootCAs == nil {
		t.Fatal("RootCAs not set from tls_ca_file")
	}

	bad := map[string]EtcdConfig{
		"files without tls_enabled": {TLSCAFile: ca},
		"missing ca file":           {TLSEnabled: true, TLSCAFile: filepath.Join(t.TempDir(), "missing")},
		"ca without pem":            {TLSEnabled: true, TLSCAFile: notPEM},
		"cert without key":          {TLSEnabled: true, TLSCert: notPEM},
		"key without cert":          {TLSEnabled: true, TLSKey: notPEM},
		"invalid key pair":          {TLSEnabled: true, TLSCert: notPEM, TLSKey: notPEM},
	}

	for name, c := range bad {
		t.Run(name, func(t *testing.T) {
			if _, err := buildEtcdTLSConfig(c); err == nil {
				t.Fatal("expected error")
			}

			c.Endpoints = []string{"127.0.0.1:1"}
			if _, err := NewEtcdBackend(c); err == nil {
				t.Fatal("NewEtcdBackend accepted invalid TLS config")
			}
		})
	}
}

type recordedPut struct {
	key   string
	lease clientv3.LeaseID
}

type fakeEtcdOps struct {
	mu      sync.Mutex
	puts    []recordedPut
	deletes []string
	failPut bool
}

func (f *fakeEtcdOps) put(_ context.Context, key, _ string, lease clientv3.LeaseID) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.failPut {
		return errors.New("put failed")
	}

	f.puts = append(f.puts, recordedPut{key: key, lease: lease})

	return nil
}

func (f *fakeEtcdOps) delete(_ context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.deletes = append(f.deletes, key)

	return nil
}

func (f *fakeEtcdOps) reset() {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.puts, f.deletes = nil, nil
}

func newFakeEtcdBackend(t *testing.T) (*EtcdBackend, *fakeEtcdOps) {
	t.Helper()

	ops := &fakeEtcdOps{}
	b := newEtcdBackend(EtcdConfig{KeyPrefix: "/services"}, ops)
	t.Cleanup(b.cancel)

	return b, ops
}

func TestEtcdRegisterRequiresLease(t *testing.T) {
	b, ops := newFakeEtcdBackend(t)

	err := b.Register(t.Context(), &ServiceInstance{ID: "a", Name: "svc"})
	if !errors.Is(err, ErrEtcdNotInitialized) {
		t.Fatalf("err = %v, want ErrEtcdNotInitialized", err)
	}

	if len(ops.puts) != 0 {
		t.Fatal("instance written without a lease")
	}
}

// After the lease is replaced, every registered instance is written again
// under the new lease, and deregistered instances are not resurrected.
func TestEtcdLeaseRenewalReregistersInstances(t *testing.T) {
	b, ops := newFakeEtcdBackend(t)
	b.setLease(1)

	for _, id := range []string{"a", "b", "c"} {
		if err := b.Register(t.Context(), &ServiceInstance{ID: id, Name: "svc"}); err != nil {
			t.Fatal(err)
		}
	}

	if err := b.Deregister(t.Context(), "b"); err != nil {
		t.Fatal(err)
	}

	if len(ops.deletes) != 1 || ops.deletes[0] != "/services/svc/b" {
		t.Fatalf("deletes = %v", ops.deletes)
	}

	ops.reset()

	if err := b.reregisterAll(t.Context(), 2); err != nil {
		t.Fatal(err)
	}

	if b.currentLease() != 2 {
		t.Fatalf("lease = %d, want 2", b.currentLease())
	}

	got := map[string]clientv3.LeaseID{}
	for _, p := range ops.puts {
		got[p.key] = p.lease
	}

	want := map[string]clientv3.LeaseID{"/services/svc/a": 2, "/services/svc/c": 2}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("re-put = %v, want %v", got, want)
	}

	// New registrations use the new lease.
	ops.reset()

	if err := b.Register(t.Context(), &ServiceInstance{ID: "d", Name: "svc"}); err != nil {
		t.Fatal(err)
	}

	if ops.puts[0].lease != 2 {
		t.Fatalf("new registration lease = %d, want 2", ops.puts[0].lease)
	}
}

func TestEtcdReregisterReportsFailures(t *testing.T) {
	b, ops := newFakeEtcdBackend(t)
	b.setLease(1)

	if err := b.Register(t.Context(), &ServiceInstance{ID: "a", Name: "svc"}); err != nil {
		t.Fatal(err)
	}

	ops.failPut = true

	if err := b.reregisterAll(t.Context(), 2); err == nil {
		t.Fatal("failed re-put not reported")
	}
}

// Register and lease replacement touch the lease ID from different
// goroutines; run under -race to catch unsynchronised access.
func TestEtcdLeaseAccessIsRaceFree(t *testing.T) {
	b, _ := newFakeEtcdBackend(t)
	b.setLease(1)

	var wg sync.WaitGroup

	for i := range 8 {
		wg.Add(2)

		go func() {
			defer wg.Done()

			_ = b.Register(context.Background(), &ServiceInstance{ID: fmt.Sprintf("i-%d", i), Name: "svc"})
		}()

		go func() {
			defer wg.Done()

			_ = b.reregisterAll(context.Background(), clientv3.LeaseID(i+2))
			_ = b.currentLease()
		}()
	}

	wg.Wait()
}
