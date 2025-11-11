package memory

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/xraph/farp"
)

func TestNewRegistry(t *testing.T) {
	registry := NewRegistry()
	defer registry.Close()

	if registry == nil {
		t.Fatal("expected non-nil registry")
	}

	ctx := context.Background()
	if err := registry.Health(ctx); err != nil {
		t.Errorf("expected healthy registry, got error: %v", err)
	}
}

func TestRegistry_RegisterManifest(t *testing.T) {
	registry := NewRegistry()
	defer registry.Close()

	manifest := farp.NewManifest("test-service", "v1.0.0", "instance-123")
	manifest.Endpoints.Health = "/health"
	manifest.AddSchema(farp.SchemaDescriptor{
		Type:        farp.SchemaTypeOpenAPI,
		SpecVersion: "3.1.0",
		Location: farp.SchemaLocation{
			Type: farp.LocationTypeHTTP,
			URL:  "http://test.com/openapi.json",
		},
		ContentType: "application/json",
		Hash:        "1234567890123456789012345678901234567890123456789012345678901234",
		Size:        1024,
	})
	manifest.UpdateChecksum()

	ctx := context.Background()

	err := registry.RegisterManifest(ctx, manifest)
	if err != nil {
		t.Fatalf("RegisterManifest failed: %v", err)
	}

	// Verify it was registered
	retrieved, err := registry.GetManifest(ctx, manifest.InstanceID)
	if err != nil {
		t.Fatalf("GetManifest failed: %v", err)
	}

	if retrieved.ServiceName != manifest.ServiceName {
		t.Errorf("expected service name '%s', got '%s'", manifest.ServiceName, retrieved.ServiceName)
	}
}

func TestRegistry_GetManifest_NotFound(t *testing.T) {
	registry := NewRegistry()
	defer registry.Close()

	ctx := context.Background()
	_, err := registry.GetManifest(ctx, "non-existent")

	if !errors.Is(err, farp.ErrManifestNotFound) {
		t.Errorf("expected ErrManifestNotFound, got: %v", err)
	}
}

func TestRegistry_UpdateManifest(t *testing.T) {
	registry := NewRegistry()
	defer registry.Close()

	manifest := farp.NewManifest("test-service", "v1.0.0", "instance-123")
	manifest.Endpoints.Health = "/health"
	manifest.AddSchema(farp.SchemaDescriptor{
		Type:        farp.SchemaTypeOpenAPI,
		SpecVersion: "3.1.0",
		Location:    farp.SchemaLocation{Type: farp.LocationTypeHTTP, URL: "http://test.com"},
		ContentType: "application/json",
		Hash:        "1234567890123456789012345678901234567890123456789012345678901234",
		Size:        1024,
	})
	manifest.UpdateChecksum()

	ctx := context.Background()
	registry.RegisterManifest(ctx, manifest)

	// Update manifest
	manifest.AddCapability("rest")
	manifest.UpdateChecksum()

	err := registry.UpdateManifest(ctx, manifest)
	if err != nil {
		t.Fatalf("UpdateManifest failed: %v", err)
	}

	// Verify update
	retrieved, _ := registry.GetManifest(ctx, manifest.InstanceID)
	if !retrieved.HasCapability("rest") {
		t.Error("expected manifest to have 'rest' capability")
	}
}

func TestRegistry_UpdateManifest_NotFound(t *testing.T) {
	registry := NewRegistry()
	defer registry.Close()

	manifest := farp.NewManifest("test-service", "v1.0.0", "instance-123")
	manifest.Endpoints.Health = "/health"

	ctx := context.Background()
	err := registry.UpdateManifest(ctx, manifest)

	if !errors.Is(err, farp.ErrManifestNotFound) {
		t.Errorf("expected ErrManifestNotFound, got: %v", err)
	}
}

func TestRegistry_DeleteManifest(t *testing.T) {
	registry := NewRegistry()
	defer registry.Close()

	manifest := farp.NewManifest("test-service", "v1.0.0", "instance-123")
	manifest.Endpoints.Health = "/health"

	ctx := context.Background()
	registry.RegisterManifest(ctx, manifest)

	err := registry.DeleteManifest(ctx, manifest.InstanceID)
	if err != nil {
		t.Fatalf("DeleteManifest failed: %v", err)
	}

	// Verify deletion
	_, err = registry.GetManifest(ctx, manifest.InstanceID)
	if !errors.Is(err, farp.ErrManifestNotFound) {
		t.Error("expected manifest to be deleted")
	}
}

func TestRegistry_ListManifests(t *testing.T) {
	registry := NewRegistry()
	defer registry.Close()

	ctx := context.Background()

	// Register multiple manifests
	for i := range 3 {
		manifest := farp.NewManifest("test-service", "v1.0.0", "instance-"+string(rune('0'+i)))
		manifest.Endpoints.Health = "/health"
		registry.RegisterManifest(ctx, manifest)
	}

	// Register manifest for different service
	otherManifest := farp.NewManifest("other-service", "v1.0.0", "instance-x")
	otherManifest.Endpoints.Health = "/health"
	registry.RegisterManifest(ctx, otherManifest)

	// List all for test-service
	manifests, err := registry.ListManifests(ctx, "test-service")
	if err != nil {
		t.Fatalf("ListManifests failed: %v", err)
	}

	if len(manifests) != 3 {
		t.Errorf("expected 3 manifests, got %d", len(manifests))
	}

	// List all services
	allManifests, err := registry.ListManifests(ctx, "")
	if err != nil {
		t.Fatalf("ListManifests failed: %v", err)
	}

	if len(allManifests) != 4 {
		t.Errorf("expected 4 manifests, got %d", len(allManifests))
	}
}

func TestRegistry_PublishAndFetchSchema(t *testing.T) {
	registry := NewRegistry()
	defer registry.Close()

	schema := map[string]any{
		"openapi": "3.1.0",
		"info": map[string]any{
			"title":   "Test API",
			"version": "1.0.0",
		},
	}

	ctx := context.Background()

	err := registry.PublishSchema(ctx, "/schemas/test/v1/openapi", schema)
	if err != nil {
		t.Fatalf("PublishSchema failed: %v", err)
	}

	// Fetch schema
	fetched, err := registry.FetchSchema(ctx, "/schemas/test/v1/openapi")
	if err != nil {
		t.Fatalf("FetchSchema failed: %v", err)
	}

	fetchedMap, ok := fetched.(map[string]any)
	if !ok {
		t.Fatal("expected schema to be map[string]interface{}")
	}

	if fetchedMap["openapi"] != "3.1.0" {
		t.Error("fetched schema has incorrect content")
	}
}

func TestRegistry_FetchSchema_NotFound(t *testing.T) {
	registry := NewRegistry()
	defer registry.Close()

	ctx := context.Background()
	_, err := registry.FetchSchema(ctx, "/non-existent")

	if !errors.Is(err, farp.ErrSchemaNotFound) {
		t.Errorf("expected ErrSchemaNotFound, got: %v", err)
	}
}

func TestRegistry_DeleteSchema(t *testing.T) {
	registry := NewRegistry()
	defer registry.Close()

	schema := map[string]any{"test": "data"}

	ctx := context.Background()
	registry.PublishSchema(ctx, "/schemas/test", schema)

	err := registry.DeleteSchema(ctx, "/schemas/test")
	if err != nil {
		t.Fatalf("DeleteSchema failed: %v", err)
	}

	// Verify deletion
	_, err = registry.FetchSchema(ctx, "/schemas/test")
	if !errors.Is(err, farp.ErrSchemaNotFound) {
		t.Error("expected schema to be deleted")
	}
}

func TestRegistry_WatchManifests(t *testing.T) {
	registry := NewRegistry()
	defer registry.Close()

	ctx := t.Context()

	eventChan := make(chan *farp.ManifestEvent, 10)

	// Start watching
	go func() {
		registry.WatchManifests(ctx, "test-service", func(event *farp.ManifestEvent) {
			eventChan <- event
		})
	}()

	// Wait for watcher to initialize
	time.Sleep(50 * time.Millisecond)

	// Register a manifest
	manifest := farp.NewManifest("test-service", "v1.0.0", "instance-123")
	manifest.Endpoints.Health = "/health"
	registry.RegisterManifest(context.Background(), manifest)

	// Wait for event
	select {
	case event := <-eventChan:
		if event.Type != farp.EventTypeAdded {
			t.Errorf("expected EventTypeAdded, got %s", event.Type)
		}

		if event.Manifest.InstanceID != manifest.InstanceID {
			t.Error("event has wrong manifest")
		}
	case <-time.After(1 * time.Second):
		t.Error("timeout waiting for event")
	}

	// Update manifest
	manifest.AddCapability("rest")
	registry.UpdateManifest(context.Background(), manifest)

	// Wait for update event
	select {
	case event := <-eventChan:
		if event.Type != farp.EventTypeUpdated {
			t.Errorf("expected EventTypeUpdated, got %s", event.Type)
		}
	case <-time.After(1 * time.Second):
		t.Error("timeout waiting for update event")
	}

	// Delete manifest
	registry.DeleteManifest(context.Background(), manifest.InstanceID)

	// Wait for delete event
	select {
	case event := <-eventChan:
		if event.Type != farp.EventTypeRemoved {
			t.Errorf("expected EventTypeRemoved, got %s", event.Type)
		}
	case <-time.After(1 * time.Second):
		t.Error("timeout waiting for delete event")
	}
}

func TestRegistry_WatchManifests_GlobalWatch(t *testing.T) {
	registry := NewRegistry()
	defer registry.Close()

	ctx := t.Context()

	eventChan := make(chan *farp.ManifestEvent, 10)

	// Start watching all services (empty service name)
	go func() {
		registry.WatchManifests(ctx, "", func(event *farp.ManifestEvent) {
			eventChan <- event
		})
	}()

	time.Sleep(50 * time.Millisecond)

	// Register manifests for different services
	manifest1 := farp.NewManifest("service-1", "v1.0.0", "instance-1")
	manifest1.Endpoints.Health = "/health"
	registry.RegisterManifest(context.Background(), manifest1)

	manifest2 := farp.NewManifest("service-2", "v1.0.0", "instance-2")
	manifest2.Endpoints.Health = "/health"
	registry.RegisterManifest(context.Background(), manifest2)

	// Should receive events for both services
	eventsReceived := 0
	timeout := time.After(1 * time.Second)

	for eventsReceived < 2 {
		select {
		case <-eventChan:
			eventsReceived++
		case <-timeout:
			t.Fatalf("timeout waiting for events, received %d/2", eventsReceived)
		}
	}
}

func TestRegistry_Close(t *testing.T) {
	registry := NewRegistry()

	err := registry.Close()
	if err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Operations after close should fail
	ctx := context.Background()
	manifest := farp.NewManifest("test", "v1", "id")
	manifest.Endpoints.Health = "/health"

	err = registry.RegisterManifest(ctx, manifest)
	if !errors.Is(err, farp.ErrBackendUnavailable) {
		t.Errorf("expected ErrBackendUnavailable after close, got: %v", err)
	}

	err = registry.Health(ctx)
	if !errors.Is(err, farp.ErrBackendUnavailable) {
		t.Errorf("expected ErrBackendUnavailable after close, got: %v", err)
	}
}

func TestRegistry_Clear(t *testing.T) {
	registry := NewRegistry()
	defer registry.Close()

	ctx := context.Background()

	// Add some data
	manifest := farp.NewManifest("test-service", "v1.0.0", "instance-123")
	manifest.Endpoints.Health = "/health"
	registry.RegisterManifest(ctx, manifest)

	schema := map[string]any{"test": "data"}
	registry.PublishSchema(ctx, "/test", schema)

	// Clear
	registry.Clear()

	// Verify everything is cleared
	_, err := registry.GetManifest(ctx, manifest.InstanceID)
	if !errors.Is(err, farp.ErrManifestNotFound) {
		t.Error("expected manifests to be cleared")
	}

	_, err = registry.FetchSchema(ctx, "/test")
	if !errors.Is(err, farp.ErrSchemaNotFound) {
		t.Error("expected schemas to be cleared")
	}
}

func BenchmarkRegistry_RegisterManifest(b *testing.B) {
	registry := NewRegistry()
	defer registry.Close()

	ctx := context.Background()

	for i := 0; b.Loop(); i++ {
		manifest := farp.NewManifest("test-service", "v1.0.0", "instance-"+string(rune(i)))
		manifest.Endpoints.Health = "/health"
		registry.RegisterManifest(ctx, manifest)
	}
}

func BenchmarkRegistry_GetManifest(b *testing.B) {
	registry := NewRegistry()
	defer registry.Close()

	ctx := context.Background()
	manifest := farp.NewManifest("test-service", "v1.0.0", "instance-123")
	manifest.Endpoints.Health = "/health"
	registry.RegisterManifest(ctx, manifest)

	for b.Loop() {
		registry.GetManifest(ctx, manifest.InstanceID)
	}
}
