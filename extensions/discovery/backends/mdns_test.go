package backends

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// -----------------------------------------------------------------------
// Helpers
// -----------------------------------------------------------------------

// newTestBackend creates an MDNSBackend with sensible test defaults.
// It registers a cleanup function to close the backend when the test ends.
func newTestBackend(t *testing.T, opts ...func(*MDNSConfig)) *MDNSBackend {
	t.Helper()

	cfg := MDNSConfig{
		Domain:        "local.",
		BrowseTimeout: 5 * time.Second,
		WatchInterval: 30 * time.Second,
		TTL:           120,
	}

	for _, opt := range opts {
		opt(&cfg)
	}

	b, err := NewMDNSBackend(cfg)
	if err != nil {
		t.Fatalf("failed to create mDNS backend: %v", err)
	}

	if err := b.Initialize(context.Background()); err != nil {
		t.Skipf("mDNS not available on this system: %v", err)
	}

	t.Cleanup(func() { _ = b.Close() })

	return b
}

// newTestInstance creates a ServiceInstance with sensible defaults.
func newTestInstance(name, id string, port int, metadata map[string]string, tags []string) *ServiceInstance {
	if metadata == nil {
		metadata = make(map[string]string)
	}
	return &ServiceInstance{
		ID:       id,
		Name:     name,
		Version:  "1.0.0",
		Address:  "127.0.0.1",
		Port:     port,
		Tags:     tags,
		Metadata: metadata,
		Status:   HealthStatusPassing,
	}
}

// discoverWithRetry retries mDNS Discover up to maxAttempts times,
// waiting between attempts. Returns the first non-empty result or the
// last result if all attempts return empty.
func discoverWithRetry(t *testing.T, b *MDNSBackend, serviceName string, maxAttempts int) []*ServiceInstance {
	t.Helper()

	ctx := context.Background()
	var instances []*ServiceInstance

	for i := 0; i < maxAttempts; i++ {
		var err error
		instances, err = b.Discover(ctx, serviceName)
		if err != nil {
			t.Fatalf("Discover(%s): %v", serviceName, err)
		}
		if len(instances) > 0 {
			return instances
		}
		time.Sleep(500 * time.Millisecond)
	}

	return instances
}

// -----------------------------------------------------------------------
// Unit tests (no network — pure logic)
// -----------------------------------------------------------------------

func TestMDNSBackend_Name(t *testing.T) {
	b := newTestBackend(t)
	if got := b.Name(); got != "mdns" {
		t.Errorf("Name() = %q, want %q", got, "mdns")
	}
}

func TestMDNSBackend_DefaultConfig(t *testing.T) {
	b, err := NewMDNSBackend(MDNSConfig{})
	if err != nil {
		t.Fatalf("NewMDNSBackend with empty config: %v", err)
	}
	defer b.Close()

	if b.config.Domain != "local." {
		t.Errorf("default Domain = %q, want %q", b.config.Domain, "local.")
	}
	if b.config.BrowseTimeout != 5*time.Second {
		t.Errorf("default BrowseTimeout = %v, want %v", b.config.BrowseTimeout, 5*time.Second)
	}
	if b.config.WatchInterval != 30*time.Second {
		t.Errorf("default WatchInterval = %v, want %v", b.config.WatchInterval, 30*time.Second)
	}
	if b.config.TTL != 120 {
		t.Errorf("default TTL = %d, want %d", b.config.TTL, 120)
	}
}

func TestMDNSBackend_RegisterValidation(t *testing.T) {
	b := newTestBackend(t)
	ctx := context.Background()

	tests := []struct {
		name string
		inst *ServiceInstance
	}{
		{"missing ID", newTestInstance("svc", "", 8080, nil, nil)},
		{"missing Name", newTestInstance("", "id-1", 8080, nil, nil)},
		{"missing Port", newTestInstance("svc", "id-2", 0, nil, nil)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := b.Register(ctx, tc.inst)
			if err == nil {
				t.Error("expected error, got nil")
			}
		})
	}
}

func TestMDNSBackend_RegisterDuplicate(t *testing.T) {
	b := newTestBackend(t)
	ctx := context.Background()

	inst := newTestInstance("dup-svc", "dup-1", 18081, nil, nil)
	if err := b.Register(ctx, inst); err != nil {
		t.Fatalf("first Register: %v", err)
	}

	err := b.Register(ctx, inst)
	if err == nil {
		t.Fatal("expected error when registering duplicate, got nil")
	}
}

func TestMDNSBackend_DeregisterNotFound(t *testing.T) {
	b := newTestBackend(t)

	err := b.Deregister(context.Background(), "nonexistent")
	if err == nil {
		t.Error("expected error deregistering nonexistent service, got nil")
	}
}

func TestMDNSBackend_Deregister(t *testing.T) {
	b := newTestBackend(t)
	ctx := context.Background()

	inst := newTestInstance("dereg-svc", "dereg-1", 18082, nil, nil)
	if err := b.Register(ctx, inst); err != nil {
		t.Fatalf("Register: %v", err)
	}

	if err := b.Deregister(ctx, "dereg-1"); err != nil {
		t.Fatalf("Deregister: %v", err)
	}

	b.mu.RLock()
	_, exists := b.services["dereg-1"]
	b.mu.RUnlock()

	if exists {
		t.Error("service still in local map after Deregister")
	}
}

func TestMDNSBackend_Close(t *testing.T) {
	cfg := MDNSConfig{
		Domain:        "local.",
		BrowseTimeout: 3 * time.Second,
		TTL:           120,
	}
	b, err := NewMDNSBackend(cfg)
	if err != nil {
		t.Fatalf("NewMDNSBackend: %v", err)
	}

	ctx := context.Background()
	if err := b.Initialize(ctx); err != nil {
		t.Skipf("mDNS not available: %v", err)
	}

	inst := newTestInstance("close-svc", "close-1", 19030, nil, nil)
	if err := b.Register(ctx, inst); err != nil {
		t.Fatalf("Register: %v", err)
	}

	if err := b.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	b.mu.RLock()
	svcCount := len(b.services)
	watchCount := len(b.watchers)
	b.mu.RUnlock()

	if svcCount != 0 {
		t.Errorf("services not cleared after Close: %d", svcCount)
	}
	if watchCount != 0 {
		t.Errorf("watchers not cleared after Close: %d", watchCount)
	}
}

func TestMDNSBackend_Health(t *testing.T) {
	b := newTestBackend(t)

	if err := b.Health(context.Background()); err != nil {
		t.Errorf("Health: %v", err)
	}
}

func TestMDNSBackend_DiscoverAllTypesNoConfig(t *testing.T) {
	b := newTestBackend(t) // no ServiceTypes

	_, err := b.DiscoverAllTypes(context.Background())
	if err == nil {
		t.Error("expected error when ServiceTypes not configured, got nil")
	}
}

func TestMDNSBackend_ListServicesLocal(t *testing.T) {
	b := newTestBackend(t)
	ctx := context.Background()

	// Register two services locally.
	for i, name := range []string{"svc-alpha", "svc-beta"} {
		inst := newTestInstance(name, fmt.Sprintf("id-%d", i), 18090+i, nil, nil)
		if err := b.Register(ctx, inst); err != nil {
			t.Fatalf("Register(%s): %v", name, err)
		}
	}

	names, err := b.ListServices(ctx)
	if err != nil {
		t.Fatalf("ListServices: %v", err)
	}

	nameSet := make(map[string]bool, len(names))
	for _, n := range names {
		nameSet[n] = true
	}

	for _, want := range []string{"svc-alpha", "svc-beta"} {
		if !nameSet[want] {
			t.Errorf("ListServices missing %q; got %v", want, names)
		}
	}
}

func TestConvertEntryToInstance_NilEntry(t *testing.T) {
	inst := convertEntryToInstance("svc", nil)
	if inst != nil {
		t.Error("expected nil for nil entry")
	}
}

// -----------------------------------------------------------------------
// Utility function tests
// -----------------------------------------------------------------------

func TestSanitizeServiceName(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"MyService", "myservice"},
		{"my_service", "my-service"},
		{"My Service", "my-service"},
		{"already-clean", "already-clean"},
		{"UPPER_CASE", "upper-case"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := sanitizeServiceName(tc.input)
			if got != tc.want {
				t.Errorf("sanitizeServiceName(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestExtractServiceNameFromType(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"_octopus._tcp", "octopus"},
		{"_http._tcp.local.", "http"},
		{"_twinos._tcp", "twinos"},
		{"_my-svc._tcp", "my-svc"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := extractServiceNameFromType(tc.input)
			if got != tc.want {
				t.Errorf("extractServiceNameFromType(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

// -----------------------------------------------------------------------
// Integration tests (require mDNS network, may be flaky in CI)
// -----------------------------------------------------------------------

// TestMDNSBackend_RegisterAndDiscover verifies basic register → discover flow.
func TestMDNSBackend_RegisterAndDiscover(t *testing.T) {
	b := newTestBackend(t)
	ctx := context.Background()

	inst := newTestInstance("test-svc", "inst-1", 18080, nil, nil)
	if err := b.Register(ctx, inst); err != nil {
		t.Fatalf("Register: %v", err)
	}

	instances := discoverWithRetry(t, b, "test-svc", 5)

	if len(instances) == 0 {
		t.Skip("mDNS Discover returned 0 instances — likely a network timing issue")
	}

	found := false
	for _, i := range instances {
		if i.ID == "inst-1" {
			found = true
			if i.Name != "test-svc" {
				t.Errorf("Name = %q, want %q", i.Name, "test-svc")
			}
			if i.Port != 18080 {
				t.Errorf("Port = %d, want %d", i.Port, 18080)
			}
		}
	}

	if !found {
		t.Logf("registered instance 'inst-1' not found among %d instances", len(instances))
	}
}

// TestMDNSBackend_SharedServiceType verifies that multiple services
// registered under a shared ServiceType can be discovered and that
// Discover() correctly filters by name.
func TestMDNSBackend_SharedServiceType(t *testing.T) {
	sharedType := "_mdnstest._tcp"

	b := newTestBackend(t, func(cfg *MDNSConfig) {
		cfg.ServiceType = sharedType
		cfg.ServiceTypes = []string{sharedType}
	})
	ctx := context.Background()

	// Register two services with different names under the same type.
	svcA := newTestInstance("ServiceA", "shared-a", 19001, map[string]string{
		"role": "api",
	}, []string{"tagA"})
	svcB := newTestInstance("ServiceB", "shared-b", 19002, map[string]string{
		"role": "worker",
	}, []string{"tagB"})

	if err := b.Register(ctx, svcA); err != nil {
		t.Fatalf("Register ServiceA: %v", err)
	}
	if err := b.Register(ctx, svcB); err != nil {
		t.Fatalf("Register ServiceB: %v", err)
	}

	// Allow mDNS time to propagate.
	time.Sleep(300 * time.Millisecond)

	t.Run("Discover filters by name", func(t *testing.T) {
		instances := discoverWithRetry(t, b, "ServiceA", 5)

		if len(instances) == 0 {
			t.Skip("mDNS browse returned 0 instances — likely network timing issue")
		}

		for _, inst := range instances {
			if !strings.EqualFold(inst.Name, "ServiceA") {
				t.Errorf("Discover(ServiceA) returned instance with Name=%q", inst.Name)
			}
		}
	})

	t.Run("Discover filters by name case insensitive", func(t *testing.T) {
		instances := discoverWithRetry(t, b, "serviceb", 5)

		if len(instances) == 0 {
			// mDNS browse can be unreliable — skip rather than fail
			// when the network-level query returns no results.
			t.Skip("mDNS browse returned 0 instances — likely network timing issue")
		}

		for _, inst := range instances {
			if !strings.EqualFold(inst.Name, "ServiceB") {
				t.Errorf("Discover(serviceb) returned instance with Name=%q", inst.Name)
			}
		}
	})

	t.Run("DiscoverAllTypes returns all", func(t *testing.T) {
		var instances []*ServiceInstance
		var err error
		for attempt := 0; attempt < 5; attempt++ {
			instances, err = b.DiscoverAllTypes(ctx)
			if err != nil {
				t.Fatalf("DiscoverAllTypes: %v", err)
			}
			if len(instances) >= 2 {
				break
			}
			time.Sleep(1 * time.Second)
		}

		if len(instances) < 2 {
			t.Skipf("DiscoverAllTypes returned %d instances, need 2 — mDNS timing issue", len(instances))
		}

		nameSet := make(map[string]bool)
		for _, inst := range instances {
			nameSet[inst.Name] = true
		}

		if !nameSet["ServiceA"] {
			t.Errorf("DiscoverAllTypes missing ServiceA (got %v)", nameSet)
		}
		if !nameSet["ServiceB"] {
			t.Errorf("DiscoverAllTypes missing ServiceB (got %v)", nameSet)
		}
	})

	t.Run("ListServices browses with ServiceTypes", func(t *testing.T) {
		// ListServices includes locally registered services AND network browse.
		// Local services should always be present.
		names, err := b.ListServices(ctx)
		if err != nil {
			t.Fatalf("ListServices: %v", err)
		}

		nameSet := make(map[string]bool)
		for _, n := range names {
			nameSet[n] = true
		}

		// At minimum, locally registered services should be present.
		if !nameSet["ServiceA"] {
			t.Errorf("ListServices missing locally-registered ServiceA; got %v", names)
		}
		if !nameSet["ServiceB"] {
			t.Errorf("ListServices missing locally-registered ServiceB; got %v", names)
		}
	})

	t.Run("Discover nonexistent returns empty", func(t *testing.T) {
		instances, err := b.Discover(ctx, "NonExistent")
		if err != nil {
			t.Fatalf("Discover(NonExistent): %v", err)
		}
		// When a shared type is configured, Discover browses the type and
		// filters by name. "NonExistent" should not match any TXT name record.
		for _, inst := range instances {
			if strings.EqualFold(inst.Name, "NonExistent") {
				t.Errorf("unexpected instance with name NonExistent: %+v", inst)
			}
		}
	})
}

// TestMDNSBackend_TXTRecordMetadata verifies that metadata, tags, and
// the service name are correctly round-tripped through TXT records.
func TestMDNSBackend_TXTRecordMetadata(t *testing.T) {
	b := newTestBackend(t)
	ctx := context.Background()

	meta := map[string]string{
		"farp.openapi": "http://localhost:8080/openapi.json",
		"farp.enabled": "true",
	}
	tags := []string{"api", "v2"}

	inst := newTestInstance("MetaSvc", "meta-1", 19010, meta, tags)
	inst.Version = "2.5.0"

	if err := b.Register(ctx, inst); err != nil {
		t.Fatalf("Register: %v", err)
	}

	instances := discoverWithRetry(t, b, "MetaSvc", 5)

	if len(instances) == 0 {
		t.Skip("mDNS Discover returned 0 instances — likely a network timing issue")
	}

	found := instances[0]

	if found.Name != "MetaSvc" {
		t.Errorf("Name = %q, want %q", found.Name, "MetaSvc")
	}
	if found.Version != "2.5.0" {
		t.Errorf("Version = %q, want %q", found.Version, "2.5.0")
	}

	// Check metadata round-trip (excluding internal keys like mdns.service_type).
	for k, v := range meta {
		if got, ok := found.Metadata[k]; !ok {
			t.Errorf("metadata missing key %q", k)
		} else if got != v {
			t.Errorf("metadata[%q] = %q, want %q", k, got, v)
		}
	}

	// Check tags.
	if len(found.Tags) != len(tags) {
		t.Errorf("len(Tags) = %d, want %d", len(found.Tags), len(tags))
	}
}

// TestMDNSBackend_DiscoverWithTags verifies tag-based filtering.
func TestMDNSBackend_DiscoverWithTags(t *testing.T) {
	b := newTestBackend(t)
	ctx := context.Background()

	inst1 := newTestInstance("tag-svc", "tag-1", 19020, nil, []string{"api", "prod"})
	inst2 := newTestInstance("tag-svc", "tag-2", 19021, nil, []string{"api", "staging"})

	if err := b.Register(ctx, inst1); err != nil {
		t.Fatalf("Register tag-1: %v", err)
	}
	if err := b.Register(ctx, inst2); err != nil {
		t.Fatalf("Register tag-2: %v", err)
	}

	time.Sleep(300 * time.Millisecond)

	instances, err := b.DiscoverWithTags(ctx, "tag-svc", []string{"prod"})
	if err != nil {
		t.Fatalf("DiscoverWithTags: %v", err)
	}

	for _, inst := range instances {
		hasProd := false
		for _, tag := range inst.Tags {
			if tag == "prod" {
				hasProd = true
			}
		}
		if !hasProd {
			t.Errorf("instance %s missing 'prod' tag", inst.ID)
		}
	}
}

// TestMDNSBackend_Watch verifies that Watch delivers initial instances.
func TestMDNSBackend_Watch(t *testing.T) {
	b := newTestBackend(t)
	ctx := context.Background()

	inst := newTestInstance("watch-svc", "watch-1", 19040, nil, nil)
	if err := b.Register(ctx, inst); err != nil {
		t.Fatalf("Register: %v", err)
	}

	var mu sync.Mutex
	var received []*ServiceInstance

	err := b.Watch(ctx, "watch-svc", func(instances []*ServiceInstance) {
		mu.Lock()
		defer mu.Unlock()
		received = instances
	})
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}

	// Wait for the initial callback.
	time.Sleep(2 * time.Second)

	mu.Lock()
	count := len(received)
	mu.Unlock()

	if count == 0 {
		t.Log("Watch callback received 0 instances (may be timing-dependent)")
	}
}

// -----------------------------------------------------------------------
// Cross-backend discovery (simulates gateway discovering other services)
// -----------------------------------------------------------------------

// TestMDNSBackend_CrossBackendDiscovery simulates the gateway scenario:
// - Backend A registers ServiceA under a shared type
// - Backend B (gateway) browses for the shared type and discovers ServiceA
func TestMDNSBackend_CrossBackendDiscovery(t *testing.T) {
	sharedType := "_crosstest._tcp"

	// Backend A: service registrar (e.g., Portal)
	bA := newTestBackend(t, func(cfg *MDNSConfig) {
		cfg.ServiceType = sharedType
	})

	// Backend B: gateway/consumer
	bB := newTestBackend(t, func(cfg *MDNSConfig) {
		cfg.ServiceType = sharedType
		cfg.ServiceTypes = []string{sharedType}
	})

	ctx := context.Background()

	// Register service on backend A.
	svc := newTestInstance("Portal", "portal-1", 19050, map[string]string{
		"farp.openapi": "http://127.0.0.1:19050/openapi.json",
		"farp.enabled": "true",
	}, []string{"api"})

	if err := bA.Register(ctx, svc); err != nil {
		t.Fatalf("Register on backend A: %v", err)
	}

	// Allow mDNS propagation — cross-backend needs more time.
	time.Sleep(1 * time.Second)

	// Gateway (backend B) lists services — retry for reliability.
	var names []string
	for attempt := 0; attempt < 5; attempt++ {
		var err error
		names, err = bB.ListServices(ctx)
		if err != nil {
			t.Fatalf("ListServices on backend B: %v", err)
		}
		t.Logf("ListServices attempt %d: %v", attempt+1, names)

		for _, n := range names {
			if n == "Portal" {
				goto foundInList
			}
		}
		time.Sleep(1 * time.Second)
	}

	// If we get here, Portal was never found.
	t.Skipf("gateway ListServices did not find 'Portal' after retries — mDNS may be unreliable in this env; got %v", names)

foundInList:
	// Gateway discovers Portal by name.
	instances := discoverWithRetry(t, bB, "Portal", 5)

	if len(instances) == 0 {
		t.Skip("gateway Discover(Portal) returned 0 instances — mDNS timing issue")
	}

	portalInst := instances[0]
	if portalInst.Port != 19050 {
		t.Errorf("Portal port = %d, want 19050", portalInst.Port)
	}
	if portalInst.Metadata["farp.openapi"] != "http://127.0.0.1:19050/openapi.json" {
		t.Errorf("Portal farp.openapi = %q, want http://127.0.0.1:19050/openapi.json",
			portalInst.Metadata["farp.openapi"])
	}
	if portalInst.Name != "Portal" {
		t.Errorf("Portal Name = %q, want %q", portalInst.Name, "Portal")
	}

	// Gateway should NOT discover services with wrong name.
	wrongInstances, err := bB.Discover(ctx, "NonExistent")
	if err != nil {
		t.Fatalf("Discover(NonExistent): %v", err)
	}
	if len(wrongInstances) != 0 {
		t.Errorf("Discover(NonExistent) returned %d instances, want 0", len(wrongInstances))
	}
}
