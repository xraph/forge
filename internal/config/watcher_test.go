package config

import (
	"context"
	"testing"
	"time"
)

// =============================================================================
// WATCHER CREATION TESTS
// =============================================================================

func TestNewWatcher(t *testing.T) {
	registry := NewSourceRegistry()

	tests := []struct {
		name     string
		interval time.Duration
	}{
		{"default interval", 5 * time.Second},
		{"custom interval", 1 * time.Second},
		{"long interval", 1 * time.Minute},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			watcher := NewWatcher(registry, tt.interval)
			if watcher == nil {
				t.Fatal("NewWatcher() returned nil")
			}

			w, ok := watcher.(*Watcher)
			if !ok {
				t.Fatal("NewWatcher() did not return *Watcher")
			}

			if w.interval != tt.interval {
				t.Errorf("interval = %v, want %v", w.interval, tt.interval)
			}

			if w.callbacks == nil {
				t.Error("callbacks slice not initialized")
			}

			if w.lastChecksums == nil {
				t.Error("lastChecksums map not initialized")
			}
		})
	}
}

func TestNewWatcher_NilRegistry(t *testing.T) {
	watcher := NewWatcher(nil, 5*time.Second)
	if watcher == nil {
		t.Fatal("NewWatcher() with nil registry returned nil")
	}

	// Should handle nil registry gracefully
}

// =============================================================================
// CALLBACK REGISTRATION TESTS
// =============================================================================

func TestWatcher_Watch(t *testing.T) {
	registry := NewSourceRegistry()
	watcher := NewWatcher(registry, 100*time.Millisecond).(*Watcher)

	callbackCalled := false
	var receivedChange ConfigChange

	callback := func(change ConfigChange) {
		callbackCalled = true
		receivedChange = change
	}

	t.Run("register callback", func(t *testing.T) {
		watcher.Watch(callback)

		if len(watcher.callbacks) != 1 {
			t.Errorf("Expected 1 callback, got %d", len(watcher.callbacks))
		}
	})

	t.Run("register multiple callbacks", func(t *testing.T) {
		callback2 := func(change ConfigChange) {}
		watcher.Watch(callback2)

		if len(watcher.callbacks) != 2 {
			t.Errorf("Expected 2 callbacks, got %d", len(watcher.callbacks))
		}
	})

	t.Run("register nil callback", func(t *testing.T) {
		// Should not panic
		watcher.Watch(nil)
	})

	// Clean up for next test
	_ = callbackCalled
	_ = receivedChange
}

// =============================================================================
// START/STOP TESTS
// =============================================================================

func TestWatcher_Start(t *testing.T) {
	registry := NewSourceRegistry()
	watcher := NewWatcher(registry, 100*time.Millisecond).(*Watcher)

	ctx := context.Background()

	t.Run("start watcher", func(t *testing.T) {
		err := watcher.Start(ctx)
		if err != nil {
			t.Errorf("Start() error = %v, want nil", err)
		}

		if !watcher.running {
			t.Error("Watcher should be running after Start()")
		}
	})

	t.Run("start already running watcher", func(t *testing.T) {
		err := watcher.Start(ctx)
		if err != nil {
			// Starting an already running watcher should be idempotent
			t.Errorf("Start() on running watcher error = %v", err)
		}
	})

	watcher.Stop()
}

func TestWatcher_Stop(t *testing.T) {
	registry := NewSourceRegistry()
	watcher := NewWatcher(registry, 100*time.Millisecond).(*Watcher)

	ctx := context.Background()
	watcher.Start(ctx)

	t.Run("stop watcher", func(t *testing.T) {
		err := watcher.Stop()
		if err != nil {
			t.Errorf("Stop() error = %v, want nil", err)
		}

		if watcher.running {
			t.Error("Watcher should not be running after Stop()")
		}
	})

	t.Run("stop already stopped watcher", func(t *testing.T) {
		err := watcher.Stop()
		if err != nil {
			// Stopping an already stopped watcher should be idempotent
			t.Errorf("Stop() on stopped watcher error = %v", err)
		}
	})
}

// =============================================================================
// CHANGE DETECTION TESTS
// =============================================================================

func TestWatcher_DetectChanges(t *testing.T) {
	registry := NewSourceRegistry()
	watcher := NewWatcher(registry, 100*time.Millisecond).(*Watcher)

	// Add a mock source
	source := newMockSource("test", 1)
	source.loadData = map[string]interface{}{
		"key1": "value1",
		"key2": "value2",
	}
	registry.Register(source)

	ctx := context.Background()

	t.Run("initial load - no changes", func(t *testing.T) {
		watcher.Start(ctx)

		// Wait for initial check
		time.Sleep(200 * time.Millisecond)

		// Initial load shouldn't trigger changes
		// (depending on implementation)
	})

	t.Run("detect value change", func(t *testing.T) {
		callbackCalled := false
		var receivedChange ConfigChange

		watcher.Watch(func(change ConfigChange) {
			callbackCalled = true
			receivedChange = change
		})

		// Change source data
		source.loadData = map[string]interface{}{
			"key1": "new_value1",
			"key2": "value2",
		}

		// Wait for watcher to detect change
		time.Sleep(300 * time.Millisecond)

		if !callbackCalled {
			t.Error("Callback was not called for data change")
		}

		if callbackCalled && receivedChange.Key != "key1" {
			t.Logf("Expected change for key1, got %s", receivedChange.Key)
		}
	})

	watcher.Stop()
}

func TestWatcher_DetectAddedKeys(t *testing.T) {
	registry := NewSourceRegistry()
	watcher := NewWatcher(registry, 100*time.Millisecond).(*Watcher)

	source := newMockSource("test", 1)
	source.loadData = map[string]interface{}{
		"existing": "value",
	}
	registry.Register(source)

	ctx := context.Background()
	watcher.Start(ctx)

	// Wait for initial check
	time.Sleep(150 * time.Millisecond)

	callbackCalled := false
	var receivedChange ConfigChange

	watcher.Watch(func(change ConfigChange) {
		callbackCalled = true
		receivedChange = change
	})

	// Add a new key
	source.loadData["new_key"] = "new_value"

	// Wait for detection
	time.Sleep(300 * time.Millisecond)

	if !callbackCalled {
		t.Error("Callback was not called for added key")
	}

	if callbackCalled && receivedChange.Type != ChangeTypeAdded {
		t.Errorf("Change type = %v, want %v", receivedChange.Type, ChangeTypeAdded)
	}

	watcher.Stop()
}

func TestWatcher_DetectDeletedKeys(t *testing.T) {
	registry := NewSourceRegistry()
	watcher := NewWatcher(registry, 100*time.Millisecond).(*Watcher)

	source := newMockSource("test", 1)
	source.loadData = map[string]interface{}{
		"key1": "value1",
		"key2": "value2",
	}
	registry.Register(source)

	ctx := context.Background()
	watcher.Start(ctx)

	// Wait for initial check
	time.Sleep(150 * time.Millisecond)

	callbackCalled := false
	var receivedChange ConfigChange

	watcher.Watch(func(change ConfigChange) {
		callbackCalled = true
		receivedChange = change
	})

	// Delete a key
	delete(source.loadData, "key1")

	// Wait for detection
	time.Sleep(300 * time.Millisecond)

	if !callbackCalled {
		t.Error("Callback was not called for deleted key")
	}

	if callbackCalled && receivedChange.Type != ChangeTypeDeleted {
		t.Errorf("Change type = %v, want %v", receivedChange.Type, ChangeTypeDeleted)
	}

	watcher.Stop()
}

// =============================================================================
// MULTIPLE SOURCE TESTS
// =============================================================================

func TestWatcher_MultipleSources(t *testing.T) {
	registry := NewSourceRegistry()
	watcher := NewWatcher(registry, 100*time.Millisecond).(*Watcher)

	source1 := newMockSource("source1", 1)
	source1.loadData = map[string]interface{}{"key1": "value1"}

	source2 := newMockSource("source2", 2)
	source2.loadData = map[string]interface{}{"key2": "value2"}

	registry.Register(source1)
	registry.Register(source2)

	ctx := context.Background()
	watcher.Start(ctx)

	// Wait for initial check
	time.Sleep(150 * time.Millisecond)

	changeCount := 0
	watcher.Watch(func(change ConfigChange) {
		changeCount++
	})

	// Change both sources
	source1.loadData["key1"] = "new_value1"
	source2.loadData["key2"] = "new_value2"

	// Wait for detection
	time.Sleep(300 * time.Millisecond)

	if changeCount < 2 {
		t.Errorf("Expected at least 2 changes, got %d", changeCount)
	}

	watcher.Stop()
}

// =============================================================================
// WATCHABLE SOURCE TESTS
// =============================================================================

func TestWatcher_WatchableSource(t *testing.T) {
	registry := NewSourceRegistry()
	watcher := NewWatcher(registry, 100*time.Millisecond).(*Watcher)

	// Create watchable source
	source := newMockSource("watchable", 1)
	source.isWatchable = true
	source.metadata.IsWatchable = true
	source.loadData = map[string]interface{}{"key": "value"}

	registry.Register(source)

	ctx := context.Background()
	watcher.Start(ctx)

	// Watchable sources should be watched directly
	// (implementation dependent)

	watcher.Stop()
}

// =============================================================================
// CHECKSUM TESTS
// =============================================================================

func TestWatcher_Checksums(t *testing.T) {
	registry := NewSourceRegistry()
	watcher := NewWatcher(registry, 100*time.Millisecond).(*Watcher)

	source := newMockSource("test", 1)
	source.loadData = map[string]interface{}{
		"key": "value",
	}
	registry.Register(source)

	ctx := context.Background()
	watcher.Start(ctx)

	// Wait for initial check to establish checksums
	time.Sleep(150 * time.Millisecond)

	t.Run("same data produces same checksum", func(t *testing.T) {
		// No change
		time.Sleep(200 * time.Millisecond)

		// Should have checksum stored
		if len(watcher.lastChecksums) == 0 {
			t.Error("No checksums stored")
		}
	})

	t.Run("different data produces different checksum", func(t *testing.T) {
		oldChecksum := watcher.lastChecksums["test"]

		// Change data
		source.loadData["key"] = "different_value"

		// Wait for check
		time.Sleep(200 * time.Millisecond)

		newChecksum := watcher.lastChecksums["test"]

		if oldChecksum == newChecksum && oldChecksum != "" {
			t.Error("Checksum should change when data changes")
		}
	})

	watcher.Stop()
}

// =============================================================================
// CALLBACK NOTIFICATION TESTS
// =============================================================================

func TestWatcher_MultipleCallbacks(t *testing.T) {
	registry := NewSourceRegistry()
	watcher := NewWatcher(registry, 100*time.Millisecond).(*Watcher)

	source := newMockSource("test", 1)
	source.loadData = map[string]interface{}{"key": "value"}
	registry.Register(source)

	ctx := context.Background()
	watcher.Start(ctx)

	// Wait for initial check
	time.Sleep(150 * time.Millisecond)

	callback1Called := false
	callback2Called := false

	watcher.Watch(func(change ConfigChange) {
		callback1Called = true
	})

	watcher.Watch(func(change ConfigChange) {
		callback2Called = true
	})

	// Trigger change
	source.loadData["key"] = "new_value"

	// Wait for notification
	time.Sleep(300 * time.Millisecond)

	if !callback1Called {
		t.Error("Callback 1 was not called")
	}

	if !callback2Called {
		t.Error("Callback 2 was not called")
	}

	watcher.Stop()
}

// =============================================================================
// INTERVAL TESTS
// =============================================================================

func TestWatcher_Interval(t *testing.T) {
	registry := NewSourceRegistry()

	// Short interval for testing
	shortInterval := 50 * time.Millisecond
	watcher := NewWatcher(registry, shortInterval).(*Watcher)

	source := newMockSource("test", 1)
	source.loadData = map[string]interface{}{"key": "value"}
	registry.Register(source)

	ctx := context.Background()
	watcher.Start(ctx)

	// Wait for initial check
	time.Sleep(100 * time.Millisecond)

	checkCount := 0
	watcher.Watch(func(change ConfigChange) {
		checkCount++
	})

	// Make multiple changes with time between
	for i := 0; i < 3; i++ {
		source.loadData["key"] = string(rune('a' + i))
		time.Sleep(100 * time.Millisecond)
	}

	if checkCount < 2 {
		t.Errorf("Expected at least 2 checks, got %d", checkCount)
	}

	watcher.Stop()
}

// =============================================================================
// ERROR HANDLING TESTS
// =============================================================================

func TestWatcher_SourceLoadError(t *testing.T) {
	registry := NewSourceRegistry()
	watcher := NewWatcher(registry, 100*time.Millisecond).(*Watcher)

	source := newMockSource("test", 1)
	source.loadData = map[string]interface{}{"key": "value"}
	source.loadErr = ErrConfigError("load failed", nil)
	registry.Register(source)

	ctx := context.Background()

	// Should not panic when source returns error
	err := watcher.Start(ctx)
	if err != nil {
		t.Errorf("Start() error = %v, want nil (should handle source errors gracefully)", err)
	}

	// Wait a bit
	time.Sleep(200 * time.Millisecond)

	// Should still be running despite source error
	if !watcher.running {
		t.Error("Watcher should continue running despite source errors")
	}

	watcher.Stop()
}

// =============================================================================
// CONTEXT CANCELLATION TESTS
// =============================================================================

func TestWatcher_ContextCancellation(t *testing.T) {
	registry := NewSourceRegistry()
	watcher := NewWatcher(registry, 100*time.Millisecond).(*Watcher)

	source := newMockSource("test", 1)
	source.loadData = map[string]interface{}{"key": "value"}
	registry.Register(source)

	ctx, cancel := context.WithCancel(context.Background())

	watcher.Start(ctx)

	// Wait for watcher to start
	time.Sleep(150 * time.Millisecond)

	// Cancel context
	cancel()

	// Wait for watcher to stop
	time.Sleep(200 * time.Millisecond)

	// Watcher should stop gracefully
	if watcher.running {
		t.Error("Watcher should stop when context is cancelled")
	}
}

// =============================================================================
// DEFAULT CHANGE DETECTOR TESTS
// =============================================================================

func TestDefaultChangeDetector(t *testing.T) {
	detector := &DefaultChangeDetector{}

	oldData := map[string]interface{}{
		"key1": "value1",
		"key2": "value2",
	}

	t.Run("no changes", func(t *testing.T) {
		newData := map[string]interface{}{
			"key1": "value1",
			"key2": "value2",
		}

		changes := detector.DetectChanges("test", oldData, newData)
		if len(changes) != 0 {
			t.Errorf("Expected 0 changes, got %d", len(changes))
		}
	})

	t.Run("value changed", func(t *testing.T) {
		newData := map[string]interface{}{
			"key1": "new_value1",
			"key2": "value2",
		}

		changes := detector.DetectChanges("test", oldData, newData)
		if len(changes) != 1 {
			t.Errorf("Expected 1 change, got %d", len(changes))
		}

		if len(changes) > 0 {
			if changes[0].Type != ChangeTypeModified {
				t.Errorf("Change type = %v, want %v", changes[0].Type, ChangeTypeModified)
			}
			if changes[0].Key != "key1" {
				t.Errorf("Change key = %v, want %v", changes[0].Key, "key1")
			}
		}
	})

	t.Run("key added", func(t *testing.T) {
		newData := map[string]interface{}{
			"key1": "value1",
			"key2": "value2",
			"key3": "value3",
		}

		changes := detector.DetectChanges("test", oldData, newData)
		if len(changes) != 1 {
			t.Errorf("Expected 1 change, got %d", len(changes))
		}

		if len(changes) > 0 {
			if changes[0].Type != ChangeTypeAdded {
				t.Errorf("Change type = %v, want %v", changes[0].Type, ChangeTypeAdded)
			}
		}
	})

	t.Run("key deleted", func(t *testing.T) {
		newData := map[string]interface{}{
			"key1": "value1",
		}

		changes := detector.DetectChanges("test", oldData, newData)
		if len(changes) != 1 {
			t.Errorf("Expected 1 change, got %d", len(changes))
		}

		if len(changes) > 0 {
			if changes[0].Type != ChangeTypeDeleted {
				t.Errorf("Change type = %v, want %v", changes[0].Type, ChangeTypeDeleted)
			}
		}
	})

	t.Run("multiple changes", func(t *testing.T) {
		newData := map[string]interface{}{
			"key1": "new_value1", // modified
			"key3": "value3",     // added
			// key2 deleted
		}

		changes := detector.DetectChanges("test", oldData, newData)
		if len(changes) != 3 {
			t.Errorf("Expected 3 changes, got %d", len(changes))
		}
	})
}

// =============================================================================
// DEBOUNCE WATCHER TESTS
// =============================================================================

func TestDebounceWatcher(t *testing.T) {
	delay := 100 * time.Millisecond
	debouncer := NewDebounceWatcher(delay)

	callCount := 0
	var lastChange ConfigChange

	callback := func(change ConfigChange) {
		callCount++
		lastChange = change
	}

	debouncer.Watch(callback)

	t.Run("single change triggers callback", func(t *testing.T) {
		change := ConfigChange{
			Key:    "test",
			Type:   ChangeTypeModified,
			Source: "test_source",
		}

		debouncer.OnChange(change)

		// Wait for debounce delay
		time.Sleep(delay + 50*time.Millisecond)

		if callCount != 1 {
			t.Errorf("Callback called %d times, want 1", callCount)
		}

		if lastChange.Key != "test" {
			t.Errorf("Last change key = %v, want %v", lastChange.Key, "test")
		}
	})

	t.Run("rapid changes are debounced", func(t *testing.T) {
		initialCount := callCount

		// Fire multiple rapid changes
		for i := 0; i < 5; i++ {
			change := ConfigChange{
				Key:    "rapid",
				Type:   ChangeTypeModified,
				Source: "test_source",
			}
			debouncer.OnChange(change)
			time.Sleep(20 * time.Millisecond) // Less than debounce delay
		}

		// Wait for debounce
		time.Sleep(delay + 50*time.Millisecond)

		// Should only trigger once
		if callCount-initialCount != 1 {
			t.Errorf("Rapid changes triggered %d callbacks, want 1", callCount-initialCount)
		}
	})

	t.Run("changes after debounce period trigger separately", func(t *testing.T) {
		initialCount := callCount

		change := ConfigChange{
			Key:    "delayed",
			Type:   ChangeTypeModified,
			Source: "test_source",
		}

		debouncer.OnChange(change)
		time.Sleep(delay + 50*time.Millisecond)

		debouncer.OnChange(change)
		time.Sleep(delay + 50*time.Millisecond)

		if callCount-initialCount != 2 {
			t.Errorf("Separate changes triggered %d callbacks, want 2", callCount-initialCount)
		}
	})

	debouncer.Stop()
}

// =============================================================================
// CONCURRENCY TESTS
// =============================================================================

func TestWatcher_Concurrency(t *testing.T) {
	registry := NewSourceRegistry()
	watcher := NewWatcher(registry, 100*time.Millisecond).(*Watcher)

	source := newMockSource("test", 1)
	source.loadData = map[string]interface{}{"key": "value"}
	registry.Register(source)

	ctx := context.Background()
	watcher.Start(ctx)

	done := make(chan bool)

	// Concurrent callback registrations
	for i := 0; i < 10; i++ {
		go func() {
			watcher.Watch(func(change ConfigChange) {})
			done <- true
		}()
	}

	// Concurrent source modifications
	for i := 0; i < 10; i++ {
		go func(val int) {
			source.loadData["key"] = val
			done <- true
		}(i)
	}

	// Wait for all goroutines
	timeout := time.After(5 * time.Second)
	for i := 0; i < 20; i++ {
		select {
		case <-done:
		case <-timeout:
			t.Fatal("Timeout waiting for concurrent operations")
		}
	}

	watcher.Stop()
}

// =============================================================================
// EDGE CASE TESTS
// =============================================================================

func TestWatcher_EdgeCases(t *testing.T) {
	t.Run("watcher with zero interval", func(t *testing.T) {
		registry := NewSourceRegistry()
		watcher := NewWatcher(registry, 0)

		if watcher == nil {
			t.Fatal("NewWatcher() with zero interval returned nil")
		}

		// Should handle gracefully (might use default interval)
	})

	t.Run("watcher with negative interval", func(t *testing.T) {
		registry := NewSourceRegistry()
		watcher := NewWatcher(registry, -1*time.Second)

		if watcher == nil {
			t.Fatal("NewWatcher() with negative interval returned nil")
		}
	})

	t.Run("watcher with no sources", func(t *testing.T) {
		registry := NewSourceRegistry()
		watcher := NewWatcher(registry, 100*time.Millisecond).(*Watcher)

		ctx := context.Background()
		err := watcher.Start(ctx)
		if err != nil {
			t.Errorf("Start() with no sources error = %v, want nil", err)
		}

		time.Sleep(200 * time.Millisecond)

		watcher.Stop()
	})

	t.Run("watcher with stopped source", func(t *testing.T) {
		registry := NewSourceRegistry()
		watcher := NewWatcher(registry, 100*time.Millisecond).(*Watcher)

		source := newMockSource("test", 1)
		source.loadData = map[string]interface{}{"key": "value"}
		registry.Register(source)

		ctx := context.Background()
		watcher.Start(ctx)

		// Stop the source
		source.Stop()

		// Watcher should continue
		time.Sleep(200 * time.Millisecond)

		watcher.Stop()
	})

	t.Run("callback panics", func(t *testing.T) {
		registry := NewSourceRegistry()
		watcher := NewWatcher(registry, 100*time.Millisecond).(*Watcher)

		source := newMockSource("test", 1)
		source.loadData = map[string]interface{}{"key": "value"}
		registry.Register(source)

		// Register callback that panics
		watcher.Watch(func(change ConfigChange) {
			panic("test panic")
		})

		ctx := context.Background()
		watcher.Start(ctx)

		// Wait for initial check
		time.Sleep(150 * time.Millisecond)

		// Change data to trigger callback
		source.loadData["key"] = "new_value"

		// Wait for callback - should not crash watcher
		time.Sleep(200 * time.Millisecond)

		// Watcher should still be running
		if !watcher.running {
			t.Error("Watcher stopped after callback panic")
		}

		watcher.Stop()
	})
}

// =============================================================================
// SOURCE REMOVAL TESTS
// =============================================================================

func TestWatcher_SourceRemoval(t *testing.T) {
	registry := NewSourceRegistry()
	watcher := NewWatcher(registry, 100*time.Millisecond).(*Watcher)

	source := newMockSource("test", 1)
	source.loadData = map[string]interface{}{"key": "value"}
	registry.Register(source)

	ctx := context.Background()
	watcher.Start(ctx)

	// Wait for initial check
	time.Sleep(150 * time.Millisecond)

	// Remove source
	registry.Unregister("test")

	// Watcher should continue without error
	time.Sleep(200 * time.Millisecond)

	if !watcher.running {
		t.Error("Watcher stopped after source removal")
	}

	watcher.Stop()
}
