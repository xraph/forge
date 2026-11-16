package gateway

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xraph/farp"

	"github.com/xraph/forge/farp/registry/memory"
)

func inlineManifest(t *testing.T, svc, inst string, schema map[string]any, hash string) *farp.SchemaManifest {
	t.Helper()

	if hash == "" {
		h, err := farp.CalculateSchemaChecksum(schema)
		if err != nil {
			t.Fatal(err)
		}

		hash = h
	}

	return &farp.SchemaManifest{
		Version: farp.ProtocolVersion, ServiceName: svc, InstanceID: inst,
		Endpoints: farp.SchemaEndpoints{Health: "/health"},
		Schemas: []farp.SchemaDescriptor{{
			Type: farp.SchemaTypeOpenAPI, SpecVersion: "3.1.0", ContentType: "application/json",
			Location:     farp.SchemaLocation{Type: farp.LocationTypeInline},
			InlineSchema: schema, Hash: hash,
		}},
	}
}

func openAPI(paths ...string) map[string]any {
	p := map[string]any{}
	for _, path := range paths {
		p[path] = map[string]any{"get": map[string]any{}}
	}

	return map[string]any{"paths": p}
}

// An attacker that registers first and claims the victim's schema hash must
// not be able to plant its own routes under the victim's service.
func TestConvertToRoutesRejectsSpoofedSchemaHash(t *testing.T) {
	reg := memory.NewRegistry()
	ctx := context.Background()

	victimSchema := openAPI("/charge")

	victimHash, err := farp.CalculateSchemaChecksum(victimSchema)
	if err != nil {
		t.Fatal(err)
	}

	evil := inlineManifest(t, "attacker", "evil-1", openAPI("/admin/export"), victimHash)
	if err := reg.RegisterManifest(ctx, evil); err != nil {
		t.Fatal(err)
	}

	victim := inlineManifest(t, "payments", "pay-1", victimSchema, "")
	if err := reg.RegisterManifest(ctx, victim); err != nil {
		t.Fatal(err)
	}

	c := NewClient(reg)
	routes := c.ConvertToRoutes([]*farp.SchemaManifest{evil, victim})

	var paymentPaths []string

	for _, r := range routes {
		if r.Path == "/admin/export" {
			t.Fatalf("spoofed schema produced route %+v", r)
		}

		if r.ServiceName == "payments" {
			paymentPaths = append(paymentPaths, r.Path)
		}
	}

	if len(paymentPaths) != 1 || paymentPaths[0] != "/charge" {
		t.Fatalf("payments routes = %v, want [/charge]", paymentPaths)
	}
}

func TestVerifySchemaHash(t *testing.T) {
	schema := openAPI("/a")

	good, err := farp.CalculateSchemaChecksum(schema)
	if err != nil {
		t.Fatal(err)
	}

	if err := VerifySchemaHash(schema, good); err != nil {
		t.Fatalf("matching hash rejected: %v", err)
	}

	if err := VerifySchemaHash(schema, strings.Repeat("a", 64)); !errors.Is(err, ErrSchemaHashMismatch) {
		t.Fatalf("mismatched hash: err = %v, want ErrSchemaHashMismatch", err)
	}

	if err := VerifySchemaHash(schema, ""); !errors.Is(err, ErrSchemaHashMismatch) {
		t.Fatalf("empty hash: err = %v, want ErrSchemaHashMismatch", err)
	}
}

// Two instances presenting identical content get separate cache entries, so
// neither can influence what the other resolves to.
func TestSchemaCacheKeyedByInstance(t *testing.T) {
	c := NewClient(memory.NewRegistry())
	schema := openAPI("/x")

	a := inlineManifest(t, "svc", "a", schema, "")
	b := inlineManifest(t, "svc", "b", schema, "")
	c.ConvertToRoutes([]*farp.SchemaManifest{a, b})

	if got := c.schemaCache.len(); got != 2 {
		t.Fatalf("cache entries = %d, want 2", got)
	}
}

func TestSchemaCacheIsBounded(t *testing.T) {
	c := NewClient(memory.NewRegistry(), WithSchemaCacheSize(4))

	var manifests []*farp.SchemaManifest
	for i := range 20 {
		manifests = append(manifests, inlineManifest(t, "svc", fmt.Sprintf("i-%d", i), openAPI(fmt.Sprintf("/p%d", i)), ""))
	}

	routes := c.ConvertToRoutes(manifests)
	if len(routes) != 20 {
		t.Fatalf("routes = %d, want 20", len(routes))
	}

	if got := c.schemaCache.len(); got != 4 {
		t.Fatalf("cache entries = %d, want 4", got)
	}

	c.ClearCache()

	if got := c.schemaCache.len(); got != 0 {
		t.Fatalf("cache entries after clear = %d, want 0", got)
	}
}

// Closing the registry under an active WatchServices must not panic or spin.
func TestWatchServicesSurvivesRegistryClose(t *testing.T) {
	reg := memory.NewRegistry()
	c := NewClient(reg)

	ctx := t.Context()

	var (
		mu    sync.Mutex
		calls int
	)

	err := c.WatchServices(ctx, "svc", func([]ServiceRoute) {
		mu.Lock()
		calls++
		mu.Unlock()
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := reg.Close(); err != nil {
		t.Fatal(err)
	}

	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	// Only the initial load should have been reported.
	if calls != 1 {
		t.Fatalf("onChange calls = %d, want 1", calls)
	}
}

// The first watch event must be applied on top of the initial manifest set,
// not replace it.
func TestWatchServicesKeepsInitialManifests(t *testing.T) {
	reg := memory.NewRegistry()
	ctx := t.Context()

	if err := reg.RegisterManifest(ctx, inlineManifest(t, "svc", "a", openAPI("/a"), "")); err != nil {
		t.Fatal(err)
	}

	got := make(chan []ServiceRoute, 4)

	if err := NewClient(reg).WatchServices(ctx, "svc", func(r []ServiceRoute) { got <- r }); err != nil {
		t.Fatal(err)
	}

	<-got // initial load

	if err := reg.RegisterManifest(ctx, inlineManifest(t, "svc", "b", openAPI("/b"), "")); err != nil {
		t.Fatal(err)
	}

	select {
	case routes := <-got:
		if len(routes) != 2 {
			t.Fatalf("routes after second registration = %d, want 2", len(routes))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no watch event")
	}
}
