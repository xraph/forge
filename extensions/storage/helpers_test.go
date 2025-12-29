package storage

import (
	"context"
	"testing"

	"github.com/xraph/forge"
	"github.com/xraph/forge/internal/logger"
)

func TestHelpers_Manager(t *testing.T) {
	// Create test app with storage extension
	app := forge.New(
		forge.WithAppName("test-helpers"),
		forge.WithAppVersion("1.0.0"),
		forge.WithAppLogger(logger.NewNoopLogger()),
		forge.WithConfig(forge.DefaultAppConfig()),
	)

	ext := NewExtension(
		WithDefault("local"),
		WithLocalBackend("local", t.TempDir(), "http://localhost:8080/files"),
	)

	if err := ext.Register(app); err != nil {
		t.Fatalf("failed to register extension: %v", err)
	}

	if err := ext.Start(context.Background()); err != nil {
		t.Fatalf("failed to start extension: %v", err)
	}

	t.Run("GetManager", func(t *testing.T) {
		manager, err := GetManager(app.Container())
		if err != nil {
			t.Fatalf("GetManager failed: %v", err)
		}

		if manager == nil {
			t.Fatal("manager is nil")
		}
	})

	t.Run("MustGetManager", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("MustGetManager panicked: %v", r)
			}
		}()

		manager := MustGetManager(app.Container())
		if manager == nil {
			t.Fatal("manager is nil")
		}
	})

	t.Run("GetManagerFromApp", func(t *testing.T) {
		manager, err := GetManagerFromApp(app)
		if err != nil {
			t.Fatalf("GetManagerFromApp failed: %v", err)
		}

		if manager == nil {
			t.Fatal("manager is nil")
		}
	})

	t.Run("MustGetManagerFromApp", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("MustGetManagerFromApp panicked: %v", r)
			}
		}()

		manager := MustGetManagerFromApp(app)
		if manager == nil {
			t.Fatal("manager is nil")
		}
	})

	t.Run("GetManagerFromApp_NilApp", func(t *testing.T) {
		_, err := GetManagerFromApp(nil)
		if err == nil {
			t.Fatal("expected error for nil app")
		}
	})

	t.Run("MustGetManagerFromApp_NilApp", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("expected panic for nil app")
			}
		}()

		MustGetManagerFromApp(nil)
	})
}

func TestHelpers_Storage(t *testing.T) {
	app := forge.New(
		forge.WithAppName("test-helpers-storage"),
		forge.WithAppVersion("1.0.0"),
		forge.WithAppLogger(logger.NewNoopLogger()),
		forge.WithConfig(forge.DefaultAppConfig()),
	)

	ext := NewExtension(
		WithDefault("local"),
		WithLocalBackend("local", t.TempDir(), "http://localhost:8080/files"),
	)

	if err := ext.Register(app); err != nil {
		t.Fatalf("failed to register extension: %v", err)
	}

	if err := ext.Start(context.Background()); err != nil {
		t.Fatalf("failed to start extension: %v", err)
	}

	t.Run("GetStorage", func(t *testing.T) {
		s, err := GetStorage(app.Container())
		if err != nil {
			t.Fatalf("GetStorage failed: %v", err)
		}

		if s == nil {
			t.Fatal("storage is nil")
		}
	})

	t.Run("MustGetStorage", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("MustGetStorage panicked: %v", r)
			}
		}()

		s := MustGetStorage(app.Container())
		if s == nil {
			t.Fatal("storage is nil")
		}
	})

	t.Run("GetStorageFromApp", func(t *testing.T) {
		s, err := GetStorageFromApp(app)
		if err != nil {
			t.Fatalf("GetStorageFromApp failed: %v", err)
		}

		if s == nil {
			t.Fatal("storage is nil")
		}
	})

	t.Run("MustGetStorageFromApp", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("MustGetStorageFromApp panicked: %v", r)
			}
		}()

		s := MustGetStorageFromApp(app)
		if s == nil {
			t.Fatal("storage is nil")
		}
	})

	t.Run("GetStorageFromApp_NilApp", func(t *testing.T) {
		_, err := GetStorageFromApp(nil)
		if err == nil {
			t.Fatal("expected error for nil app")
		}
	})

	t.Run("MustGetStorageFromApp_NilApp", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("expected panic for nil app")
			}
		}()

		MustGetStorageFromApp(nil)
	})
}

func TestHelpers_NamedBackends(t *testing.T) {
	app := forge.New(
		forge.WithAppName("test-helpers-named"),
		forge.WithAppVersion("1.0.0"),
		forge.WithAppLogger(logger.NewNoopLogger()),
		forge.WithConfig(forge.DefaultAppConfig()),
	)

	// Create extension with local backend using variadic options
	ext := NewExtension(
		WithDefault("local"),
		WithLocalBackend("local", t.TempDir(), "http://localhost:8080/files"),
	)

	if err := ext.Register(app); err != nil {
		t.Fatalf("failed to register extension: %v", err)
	}

	if err := ext.Start(context.Background()); err != nil {
		t.Fatalf("failed to start extension: %v", err)
	}

	// Test with the "local" backend that exists in default config
	t.Run("GetNamedBackend", func(t *testing.T) {
		s, err := GetNamedBackend(app.Container(), "local")
		if err != nil {
			t.Fatalf("GetNamedBackend failed: %v", err)
		}

		if s == nil {
			t.Fatal("backend is nil")
		}
	})

	t.Run("MustGetNamedBackend", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("MustGetNamedBackend panicked: %v", r)
			}
		}()

		s := MustGetNamedBackend(app.Container(), "local")
		if s == nil {
			t.Fatal("backend is nil")
		}
	})

	t.Run("GetBackend", func(t *testing.T) {
		s, err := GetBackend(app.Container(), "local")
		if err != nil {
			t.Fatalf("GetBackend failed: %v", err)
		}

		if s == nil {
			t.Fatal("backend is nil")
		}
	})

	t.Run("MustGetBackend", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("MustGetBackend panicked: %v", r)
			}
		}()

		s := MustGetBackend(app.Container(), "local")
		if s == nil {
			t.Fatal("backend is nil")
		}
	})

	t.Run("GetNamedBackendFromApp", func(t *testing.T) {
		s, err := GetNamedBackendFromApp(app, "local")
		if err != nil {
			t.Fatalf("GetNamedBackendFromApp failed: %v", err)
		}

		if s == nil {
			t.Fatal("backend is nil")
		}
	})

	t.Run("MustGetNamedBackendFromApp", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("MustGetNamedBackendFromApp panicked: %v", r)
			}
		}()

		s := MustGetNamedBackendFromApp(app, "local")
		if s == nil {
			t.Fatal("backend is nil")
		}
	})

	t.Run("GetBackendFromApp", func(t *testing.T) {
		s, err := GetBackendFromApp(app, "local")
		if err != nil {
			t.Fatalf("GetBackendFromApp failed: %v", err)
		}

		if s == nil {
			t.Fatal("backend is nil")
		}
	})

	t.Run("MustGetBackendFromApp", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("MustGetBackendFromApp panicked: %v", r)
			}
		}()

		s := MustGetBackendFromApp(app, "local")
		if s == nil {
			t.Fatal("backend is nil")
		}
	})

	t.Run("GetNamedBackend_NotFound", func(t *testing.T) {
		_, err := GetNamedBackend(app.Container(), "nonexistent")
		if err == nil {
			t.Fatal("expected error for non-existent backend")
		}
	})

	t.Run("MustGetNamedBackend_NotFound", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("expected panic for non-existent backend")
			}
		}()

		MustGetNamedBackend(app.Container(), "nonexistent")
	})

	t.Run("GetBackend_NotFound", func(t *testing.T) {
		_, err := GetBackend(app.Container(), "nonexistent")
		if err == nil {
			t.Fatal("expected error for non-existent backend")
		}
	})

	t.Run("MustGetBackend_NotFound", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("expected panic for non-existent backend")
			}
		}()

		MustGetBackend(app.Container(), "nonexistent")
	})
}

func TestHelpers_NotRegistered(t *testing.T) {
	// Create app without storage extension
	app := forge.New(
		forge.WithAppName("test-not-registered"),
		forge.WithAppVersion("1.0.0"),
		forge.WithAppLogger(logger.NewNoopLogger()),
		forge.WithConfig(forge.DefaultAppConfig()),
	)

	t.Run("GetManager_NotRegistered", func(t *testing.T) {
		_, err := GetManager(app.Container())
		if err == nil {
			t.Fatal("expected error when storage extension not registered")
		}
	})

	t.Run("MustGetManager_NotRegistered", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("expected panic when storage extension not registered")
			}
		}()

		MustGetManager(app.Container())
	})

	t.Run("GetStorage_NotRegistered", func(t *testing.T) {
		_, err := GetStorage(app.Container())
		if err == nil {
			t.Fatal("expected error when storage extension not registered")
		}
	})

	t.Run("MustGetStorage_NotRegistered", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("expected panic when storage extension not registered")
			}
		}()

		MustGetStorage(app.Container())
	})

	t.Run("GetNamedBackend_NotRegistered", func(t *testing.T) {
		_, err := GetNamedBackend(app.Container(), "any")
		if err == nil {
			t.Fatal("expected error when storage extension not registered")
		}
	})

	t.Run("MustGetNamedBackend_NotRegistered", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("expected panic when storage extension not registered")
			}
		}()

		MustGetNamedBackend(app.Container(), "any")
	})
}

// Note: Skipping TestHelpers_NoDefaultBackend because the extension currently
// always loads DefaultConfig() which sets a default backend. This test would
// require changes to the extension's config loading behavior.

// BenchmarkHelpers tests the performance of helper functions.
func BenchmarkHelpers(b *testing.B) {
	app := forge.New(
		forge.WithAppName("bench-helpers"),
		forge.WithAppVersion("1.0.0"),
		forge.WithAppLogger(logger.NewNoopLogger()),
		forge.WithConfig(forge.DefaultAppConfig()),
	)

	ext := NewExtension(
		WithDefault("local"),
		WithLocalBackend("local", b.TempDir(), "http://localhost:8080/files"),
	)

	if err := ext.Register(app); err != nil {
		b.Fatalf("failed to register extension: %v", err)
	}

	if err := ext.Start(context.Background()); err != nil {
		b.Fatalf("failed to start extension: %v", err)
	}

	b.Run("GetManager", func(b *testing.B) {
		for range b.N {
			_, _ = GetManager(app.Container())
		}
	})

	b.Run("MustGetManager", func(b *testing.B) {
		for range b.N {
			_ = MustGetManager(app.Container())
		}
	})

	b.Run("GetStorage", func(b *testing.B) {
		for range b.N {
			_, _ = GetStorage(app.Container())
		}
	})

	b.Run("MustGetStorage", func(b *testing.B) {
		for range b.N {
			_ = MustGetStorage(app.Container())
		}
	})

	b.Run("GetNamedBackend", func(b *testing.B) {
		for range b.N {
			_, _ = GetNamedBackend(app.Container(), "local")
		}
	})

	b.Run("MustGetNamedBackend", func(b *testing.B) {
		for range b.N {
			_ = MustGetNamedBackend(app.Container(), "local")
		}
	})
}
