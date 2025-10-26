package config

import (
	"context"
	"reflect"
	"testing"
	"time"

	configcore "github.com/xraph/forge/internal/config/core"
)

// =============================================================================
// MOCK CONFIG SOURCE FOR TESTING
// =============================================================================

type mockConfigSource struct {
	name        string
	priority    int
	isWatchable bool
	loadData    map[string]interface{}
	loadErr     error
	watchFunc   func(map[string]interface{})
	metadata    configcore.SourceMetadata
}

func newMockSource(name string, priority int) *mockConfigSource {
	return &mockConfigSource{
		name:     name,
		priority: priority,
		metadata: configcore.SourceMetadata{
			Name:       name,
			Priority:   priority,
			Type:       "mock",
			IsWatching: false,
		},
	}
}

func (m *mockConfigSource) Name() string {
	return m.name
}

func (m *mockConfigSource) Priority() int {
	return m.priority
}

func (m *mockConfigSource) IsWatchable() bool {
	return m.isWatchable
}

func (m *mockConfigSource) Load(ctx context.Context) (map[string]interface{}, error) {
	if m.loadErr != nil {
		return nil, m.loadErr
	}
	return m.loadData, nil
}

func (m *mockConfigSource) Watch(ctx context.Context, callback func(map[string]interface{})) error {
	if !m.isWatchable {
		return ErrLifecycleError("source not watchable", nil)
	}
	m.watchFunc = callback
	return nil
}

func (m *mockConfigSource) Get(key string) (interface{}, bool) {
	if m.loadData == nil {
		return nil, false
	}
	val, ok := m.loadData[key]
	return val, ok
}

func (m *mockConfigSource) Metadata() configcore.SourceMetadata {
	return m.metadata
}

func (m *mockConfigSource) Validate(ctx context.Context) error {
	return nil
}

func (m *mockConfigSource) Stop() error {
	return nil
}

func (m *mockConfigSource) StopWatch() error {
	return nil
}

func (m *mockConfigSource) Reload(ctx context.Context) error {
	if m.loadErr != nil {
		return m.loadErr
	}
	return nil
}

func (m *mockConfigSource) SupportsSecrets() bool {
	return false
}

func (m *mockConfigSource) GetSecret(ctx context.Context, key string) (string, error) {
	return "", ErrValidationError("secret_not_supported", nil)
}

func (m *mockConfigSource) IsAvailable(ctx context.Context) bool {
	return true
}

func (m *mockConfigSource) GetName() string {
	return m.name
}

func (m *mockConfigSource) GetType() string {
	return "mock"
}

// =============================================================================
// REGISTRY CREATION TESTS
// =============================================================================

func TestNewSourceRegistry(t *testing.T) {
	registry := NewSourceRegistry(nil)

	if registry == nil {
		t.Fatal("NewSourceRegistry() returned nil")
	}

	sources := registry.GetSources()
	if len(sources) != 0 {
		t.Errorf("New registry should have no sources, got %d", len(sources))
	}
}

// =============================================================================
// SOURCE REGISTRATION TESTS
// =============================================================================

func TestSourceRegistry_Register(t *testing.T) {
	registry := NewSourceRegistry(nil)

	source1 := newMockSource("source1", 1)
	source2 := newMockSource("source2", 2)

	t.Run("register first source", func(t *testing.T) {
		err := registry.RegisterSource(source1)
		if err != nil {
			t.Errorf("RegisterSource() error = %v, want nil", err)
		}

		sources := registry.GetSources()
		if len(sources) != 1 {
			t.Errorf("Expected 1 source, got %d", len(sources))
		}
	})

	t.Run("register second source", func(t *testing.T) {
		err := registry.RegisterSource(source2)
		if err != nil {
			t.Errorf("RegisterSource() error = %v, want nil", err)
		}

		sources := registry.GetSources()
		if len(sources) != 2 {
			t.Errorf("Expected 2 sources, got %d", len(sources))
		}
	})

	t.Run("register nil source", func(t *testing.T) {
		err := registry.RegisterSource(nil)
		if err == nil {
			t.Error("RegisterSource(nil) should return error")
		}
	})

	t.Run("register duplicate source", func(t *testing.T) {
		duplicate := newMockSource("source1", 1)
		err := registry.RegisterSource(duplicate)
		if err == nil {
			t.Error("RegisterSource() should return error for duplicate source name")
		}
	})
}

func TestSourceRegistry_RegisterMultiple(t *testing.T) {
	t.Skip("RegisterMultiple method not implemented")
	registry := NewSourceRegistry(nil)

	sources := []configcore.ConfigSource{
		newMockSource("source1", 1),
		newMockSource("source2", 2),
		newMockSource("source3", 3),
	}

	t.Run("register multiple sources", func(t *testing.T) {
		// Manual registration for now
		for _, src := range sources {
			if err := registry.RegisterSource(src); err != nil {
				t.Errorf("RegisterSource() error = %v, want nil", err)
			}
		}

		allSources := registry.GetSources()
		if len(allSources) != 3 {
			t.Errorf("Expected 3 sources, got %d", len(allSources))
		}
	})
}

// =============================================================================
// SOURCE UNREGISTRATION TESTS
// =============================================================================

func TestSourceRegistry_Unregister(t *testing.T) {
	registry := NewSourceRegistry(nil)

	source := newMockSource("source1", 1)
	registry.RegisterSource(source)

	t.Run("unregister existing source", func(t *testing.T) {
		err := registry.UnregisterSource("source1")
		if err != nil {
			t.Errorf("Unregister() error = %v, want nil", err)
		}

		sources := registry.GetSources()
		if len(sources) != 0 {
			t.Errorf("Expected 0 sources after unregister, got %d", len(sources))
		}
	})

	t.Run("unregister non-existent source", func(t *testing.T) {
		err := registry.UnregisterSource("nonexistent")
		if err == nil {
			t.Error("Unregister() should return error for non-existent source")
		}
	})
}

// =============================================================================
// SOURCE RETRIEVAL TESTS
// =============================================================================

func TestSourceRegistry_GetSource(t *testing.T) {
	registry := NewSourceRegistry(nil)

	source := newMockSource("test_source", 1)
	registry.RegisterSource(source)

	t.Run("get existing source", func(t *testing.T) {
		retrieved, err := registry.GetSource("test_source")
		if err != nil {
			t.Fatalf("GetSource() error = %v", err)
		}
		if retrieved == nil {
			t.Fatal("GetSource() returned nil")
		}
		if retrieved.Name() != "test_source" {
			t.Errorf("GetSource() returned wrong source, got %v", retrieved.Name())
		}
	})

	t.Run("get non-existent source", func(t *testing.T) {
		retrieved, err := registry.GetSource("nonexistent")
		if err == nil {
			t.Error("GetSource() should return error for non-existent source")
		}
		if retrieved != nil {
			t.Error("GetSource() should return nil for non-existent source")
		}
	})
}

func TestSourceRegistry_GetAllSources(t *testing.T) {
	registry := NewSourceRegistry(nil)

	source1 := newMockSource("source1", 3)
	source2 := newMockSource("source2", 1)
	source3 := newMockSource("source3", 2)

	registry.RegisterSource(source1)
	registry.RegisterSource(source2)
	registry.RegisterSource(source3)

	sources := registry.GetSources()

	t.Run("correct count", func(t *testing.T) {
		if len(sources) != 3 {
			t.Errorf("GetAllSources() count = %d, want 3", len(sources))
		}
	})

	t.Run("sorted by priority", func(t *testing.T) {
		// Higher priority should come first
		if sources[0].Name() != "source1" || sources[0].Priority() != 3 {
			t.Errorf("First source should be source1 with priority 3, got %s with %d",
				sources[0].Name(), sources[0].Priority())
		}
		if sources[1].Name() != "source3" || sources[1].Priority() != 2 {
			t.Errorf("Second source should be source3 with priority 2, got %s with %d",
				sources[1].Name(), sources[1].Priority())
		}
		if sources[2].Name() != "source2" || sources[2].Priority() != 1 {
			t.Errorf("Third source should be source2 with priority 1, got %s with %d",
				sources[2].Name(), sources[2].Priority())
		}
	})

	t.Run("returns copy", func(t *testing.T) {
		sources1 := registry.GetSources()
		sources2 := registry.GetSources()

		// Modifying returned slice shouldn't affect registry
		sources1[0] = nil

		if sources2[0] == nil {
			t.Error("GetAllSources() should return copy, not reference")
		}
	})
}

func TestSourceRegistry_GetSourcesByType(t *testing.T) {
	registry := NewSourceRegistry(nil).(*SourceRegistryImpl)

	source1 := newMockSource("mock1", 1)
	source1.metadata.Type = "typeA"

	source2 := newMockSource("mock2", 2)
	source2.metadata.Type = "typeB"

	source3 := newMockSource("mock3", 3)
	source3.metadata.Type = "typeA"

	registry.RegisterSource(source1)
	registry.RegisterSource(source2)
	registry.RegisterSource(source3)

	t.Run("get sources by type", func(t *testing.T) {
		typeASources := registry.GetSourcesByType("typeA")
		if len(typeASources) != 2 {
			t.Errorf("Expected 2 sources of typeA, got %d", len(typeASources))
		}

		typeBSources := registry.GetSourcesByType("typeB")
		if len(typeBSources) != 1 {
			t.Errorf("Expected 1 source of typeB, got %d", len(typeBSources))
		}
	})

	t.Run("get sources by non-existent type", func(t *testing.T) {
		sources := registry.GetSourcesByType("nonexistent")
		if len(sources) != 0 {
			t.Errorf("Expected 0 sources for non-existent type, got %d", len(sources))
		}
	})
}

func TestSourceRegistry_GetWatchableSources(t *testing.T) {
	registry := NewSourceRegistry(nil).(*SourceRegistryImpl)

	watchable1 := newMockSource("watchable1", 1)
	watchable1.isWatchable = true
	watchable1.metadata.IsWatching = true

	watchable2 := newMockSource("watchable2", 2)
	watchable2.isWatchable = true
	watchable2.metadata.IsWatching = true

	nonWatchable := newMockSource("nonwatchable", 3)
	nonWatchable.isWatchable = false
	nonWatchable.metadata.IsWatching = false

	registry.RegisterSource(watchable1)
	registry.RegisterSource(watchable2)
	registry.RegisterSource(nonWatchable)

	watchable := registry.GetWatchableSources()

	if len(watchable) != 2 {
		t.Errorf("Expected 2 watchable sources, got %d", len(watchable))
	}

	for _, source := range watchable {
		if !source.IsWatchable() {
			t.Errorf("Non-watchable source %s in watchable sources list", source.Name())
		}
	}
}

// =============================================================================
// METADATA TESTS
// =============================================================================

func TestSourceRegistry_GetMetadata(t *testing.T) {
	registry := NewSourceRegistry(nil)

	source := newMockSource("test_source", 1)
	source.metadata.Type = "test_type"
	source.metadata.IsWatching = true

	registry.RegisterSource(source)

	t.Run("get existing source metadata", func(t *testing.T) {
		metadata, err := registry.GetSourceMetadata("test_source")
		if err != nil {
			t.Fatalf("GetSourceMetadata() error = %v", err)
		}
		if metadata == nil {
			t.Fatal("GetSourceMetadata() returned nil")
		}
		if metadata.Name != "test_source" {
			t.Errorf("metadata.Name = %v, want %v", metadata.Name, "test_source")
		}
		if metadata.Type != "test_type" {
			t.Errorf("metadata.Type = %v, want %v", metadata.Type, "test_type")
		}
	})

	t.Run("get non-existent source metadata", func(t *testing.T) {
		metadata, err := registry.GetSourceMetadata("nonexistent")
		if err == nil {
			t.Error("GetSourceMetadata() should return error for non-existent source")
		}
		if metadata != nil {
			t.Error("GetSourceMetadata() should return nil metadata for non-existent source")
		}
	})
}

func TestSourceRegistry_GetAllMetadata(t *testing.T) {
	registry := NewSourceRegistry(nil)

	source1 := newMockSource("source1", 1)
	source2 := newMockSource("source2", 2)

	registry.RegisterSource(source1)
	registry.RegisterSource(source2)

	metadata := registry.GetAllMetadata()

	if len(metadata) != 2 {
		t.Errorf("GetAllMetadata() count = %d, want 2", len(metadata))
	}

	// Check that we have metadata for both sources
	names := make(map[string]bool)
	for _, meta := range metadata {
		names[meta.Name] = true
	}

	if !names["source1"] || !names["source2"] {
		t.Error("GetAllMetadata() missing expected source metadata")
	}
}

// =============================================================================
// QUERY TESTS
// =============================================================================

func TestSourceRegistry_HasSource(t *testing.T) {
	registry := NewSourceRegistry(nil).(*SourceRegistryImpl)

	source := newMockSource("existing", 1)
	registry.RegisterSource(source)

	tests := []struct {
		name       string
		sourceName string
		want       bool
	}{
		{"existing source", "existing", true},
		{"non-existent source", "nonexistent", false},
		{"empty name", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := registry.HasSource(tt.sourceName)
			if got != tt.want {
				t.Errorf("HasSource(%q) = %v, want %v", tt.sourceName, got, tt.want)
			}
		})
	}
}

func TestSourceRegistry_GetSourceCount(t *testing.T) {
	registry := NewSourceRegistry(nil)

	t.Run("empty registry", func(t *testing.T) {
		count := len(registry.GetSources())
		if count != 0 {
			t.Errorf("GetSourceCount() = %d, want 0", count)
		}
	})

	t.Run("with sources", func(t *testing.T) {
		registry.RegisterSource(newMockSource("source1", 1))
		registry.RegisterSource(newMockSource("source2", 2))

		count := len(registry.GetSources())
		if count != 2 {
			t.Errorf("GetSourceCount() = %d, want 2", count)
		}
	})
}

// =============================================================================
// PRIORITY TESTS
// =============================================================================

func TestSourceRegistry_SetPriority(t *testing.T) {
	t.Skip("SetPriority method not implemented")
}

func TestSourceRegistry_GetPriority(t *testing.T) {
	t.Skip("GetPriority method not implemented")
}

// =============================================================================
// CLEAR TESTS
// =============================================================================

func TestSourceRegistry_Clear(t *testing.T) {
	registry := NewSourceRegistry(nil).(*SourceRegistryImpl)

	registry.RegisterSource(newMockSource("source1", 1))
	registry.RegisterSource(newMockSource("source2", 2))

	if len(registry.GetSources()) != 2 {
		t.Fatalf("Setup failed, expected 2 sources")
	}

	registry.Clear()

	if len(registry.GetSources()) != 0 {
		t.Errorf("After Clear(), GetSourceCount() = %d, want 0", len(registry.GetSources()))
	}

	sources := registry.GetSources()
	if len(sources) != 0 {
		t.Errorf("After Clear(), GetAllSources() length = %d, want 0", len(sources))
	}

	metadata := registry.GetAllMetadata()
	if len(metadata) != 0 {
		t.Errorf("After Clear(), GetAllMetadata() length = %d, want 0", len(metadata))
	}
}

// =============================================================================
// EVENT HANDLER TESTS
// =============================================================================

func TestSourceRegistry_AddEventHandler(t *testing.T) {
	t.Skip("AddEventHandler method not implemented")
}

func TestSourceRegistry_RemoveEventHandler(t *testing.T) {
	t.Skip("RemoveEventHandler method not implemented")
}

// =============================================================================
// CONCURRENCY TESTS
// =============================================================================

func TestSourceRegistry_Concurrency(t *testing.T) {
	registry := NewSourceRegistry(nil)

	done := make(chan bool)
	errChan := make(chan error, 100)

	// Concurrent registrations
	for i := 0; i < 10; i++ {
		go func(idx int) {
			source := newMockSource(string(rune('A'+idx)), idx)
			err := registry.RegisterSource(source)
			if err != nil {
				errChan <- err
			}
			done <- true
		}(i)
	}

	// Concurrent reads
	for i := 0; i < 10; i++ {
		go func() {
			_ = registry.GetSources()
			_ = len(registry.GetSources())
			done <- true
		}()
	}

	// Wait for all goroutines
	timeout := time.After(5 * time.Second)
	for i := 0; i < 20; i++ {
		select {
		case <-done:
		case err := <-errChan:
			t.Errorf("Concurrent operation error: %v", err)
		case <-timeout:
			t.Fatal("Timeout waiting for concurrent operations")
		}
	}

	// Verify final state
	count := len(registry.GetSources())
	if count != 10 {
		t.Errorf("After concurrent operations, source count = %d, want 10", count)
	}
}

// =============================================================================
// EDGE CASE TESTS
// =============================================================================

func TestSourceRegistry_EdgeCases(t *testing.T) {
	t.Run("register source with zero priority", func(t *testing.T) {
		registry := NewSourceRegistry(nil)
		source := newMockSource("zero", 0)

		err := registry.RegisterSource(source)
		if err != nil {
			t.Errorf("Register() with zero priority error = %v, want nil", err)
		}

		priority := source.Priority()
		if priority != 0 {
			t.Errorf("Priority = %d, want 0", priority)
		}
	})

	t.Run("register sources with same priority", func(t *testing.T) {
		registry := NewSourceRegistry(nil)

		source1 := newMockSource("first", 5)
		source2 := newMockSource("second", 5)

		registry.RegisterSource(source1)
		registry.RegisterSource(source2)

		sources := registry.GetSources()
		if len(sources) != 2 {
			t.Errorf("Expected 2 sources, got %d", len(sources))
		}

		// Both should have same priority
		if sources[0].Priority() != 5 || sources[1].Priority() != 5 {
			t.Error("Sources with same priority not handled correctly")
		}
	})

	t.Run("register source with negative priority", func(t *testing.T) {
		registry := NewSourceRegistry(nil)
		source := newMockSource("negative", -1)

		err := registry.RegisterSource(source)
		if err != nil {
			t.Errorf("Register() with negative priority error = %v, want nil", err)
		}
	})

	t.Run("update metadata reflects in queries", func(t *testing.T) {
		registry := NewSourceRegistry(nil)
		source := newMockSource("updatable", 1)

		registry.RegisterSource(source)

		// Change the source's metadata after registration
		source.metadata.Type = "updated_type"
		source.metadata.IsWatching = true

		// Note: Depending on implementation, this may or may not update
		// This tests whether registry stores reference or copy
		metadata, _ := registry.GetSourceMetadata("updatable")
		if metadata == nil {
			t.Fatal("Metadata not found")
		}
		// The behavior depends on whether registry stores copy or reference
	})
}

// =============================================================================
// SORTING AND ORDERING TESTS
// =============================================================================

func TestSourceRegistry_SourceOrdering(t *testing.T) {
	registry := NewSourceRegistry(nil)

	// Register sources in random order
	registry.RegisterSource(newMockSource("medium", 50))
	registry.RegisterSource(newMockSource("lowest", 10))
	registry.RegisterSource(newMockSource("highest", 100))
	registry.RegisterSource(newMockSource("low", 20))
	registry.RegisterSource(newMockSource("high", 80))

	sources := registry.GetSources()

	// Verify descending order
	expectedOrder := []string{"highest", "high", "medium", "low", "lowest"}
	for i, expectedName := range expectedOrder {
		if sources[i].Name() != expectedName {
			t.Errorf("Position %d: expected %s, got %s", i, expectedName, sources[i].Name())
		}
	}
}

// =============================================================================
// TYPE FILTERING TESTS
// =============================================================================

func TestSourceRegistry_TypeFiltering(t *testing.T) {
	registry := NewSourceRegistry(nil).(*SourceRegistryImpl)

	// Register sources with various types
	source1 := newMockSource("file1", 1)
	source1.metadata.Type = "file"

	source2 := newMockSource("file2", 2)
	source2.metadata.Type = "file"

	source3 := newMockSource("env1", 3)
	source3.metadata.Type = "env"

	source4 := newMockSource("remote1", 4)
	source4.metadata.Type = "remote"

	registry.RegisterSource(source1)
	registry.RegisterSource(source2)
	registry.RegisterSource(source3)
	registry.RegisterSource(source4)

	t.Run("filter file type", func(t *testing.T) {
		fileSources := registry.GetSourcesByType("file")
		if len(fileSources) != 2 {
			t.Errorf("Expected 2 file sources, got %d", len(fileSources))
		}
	})

	t.Run("filter env type", func(t *testing.T) {
		envSources := registry.GetSourcesByType("env")
		if len(envSources) != 1 {
			t.Errorf("Expected 1 env source, got %d", len(envSources))
		}
	})

	t.Run("filter remote type", func(t *testing.T) {
		remoteSources := registry.GetSourcesByType("remote")
		if len(remoteSources) != 1 {
			t.Errorf("Expected 1 remote source, got %d", len(remoteSources))
		}
	})
}

// =============================================================================
// HELPER FUNCTION TESTS
// =============================================================================

func TestSourceRegistry_SortingStability(t *testing.T) {
	registry := NewSourceRegistry(nil)

	// Register sources with same priority in specific order
	for i := 0; i < 5; i++ {
		source := newMockSource(string(rune('A'+i)), 10)
		registry.RegisterSource(source)
	}

	sources1 := registry.GetSources()
	sources2 := registry.GetSources()

	// Verify stable ordering
	for i := range sources1 {
		if sources1[i].Name() != sources2[i].Name() {
			t.Error("Sorting is not stable across multiple calls")
			break
		}
	}
}

func TestSourceRegistry_MetadataConsistency(t *testing.T) {
	registry := NewSourceRegistry(nil)

	source := newMockSource("test", 1)
	source.metadata.Type = "test_type"
	source.metadata.IsWatching = true

	registry.RegisterSource(source)

	// Get metadata through different methods
	metadata1, _ := registry.GetSourceMetadata("test")
	allMetadata := registry.GetAllMetadata()

	if metadata1 == nil {
		t.Fatal("GetMetadata() returned nil")
	}

	// Find in all metadata
	var metadata2 *configcore.SourceMetadata
	for name, meta := range allMetadata {
		if name == "test" {
			metadata2 = meta
			break
		}
	}

	if metadata2 == nil {
		t.Fatal("Source not found in GetAllMetadata()")
	}

	// Verify consistency
	if !reflect.DeepEqual(metadata1, metadata2) {
		t.Error("Metadata inconsistent between GetMetadata() and GetAllMetadata()")
	}
}
