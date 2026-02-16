package storage

import (
	"fmt"

	"github.com/xraph/forge"
	"github.com/xraph/forge/errors"
)

// Helper functions for convenient storage access from DI container.
//
// This file provides lightweight wrappers (~16ns, 0 allocs) around Forge's DI system
// to eliminate verbose storage resolution boilerplate.
//
// QUICK START:
//
//	// In a controller or service constructor:
//	func NewFileService(c forge.Container) *FileService {
//	    return &FileService{
//	        storage: storage.MustGetStorage(c, "s3"),  // Simple!
//	    }
//	}
//
//	// Or with error handling:
//	s, err := storage.GetStorage(c, "s3")
//	if err != nil {
//	    return err
//	}
//
// LIFECYCLE TIMING:
//
//	⚠️  IMPORTANT: Storage backends are ready immediately after Register()!
//
//	During Extension Register() phase:
//	  ✅ GetManager()     - Manager exists, safe to call
//	  ✅ GetStorage()     - Named backend exists and ready to use
//	  ✅ GetBackend()     - Named backends exist and ready to use
//	  ✅ GetDefault()     - Default backend exists and ready to use
//
//	During Extension Start() phase:
//	  ✅ All helpers work perfectly
//
//	Key Difference from Database Extension:
//	  Unlike the database extension where connections are opened in Start(),
//	  storage backends are fully initialized during Register(). This means
//	  you can safely resolve and use storage services immediately after
//	  the storage extension is registered.
//
// PATTERNS:
//   - Use Must* variants when storage is required (fail-fast)
//   - Use regular variants when storage is optional (explicit errors)
//   - Store storage refs in structs during construction
//   - Use Named* helpers for multi-backend scenarios
//
// EXAMPLES:
//
//	// Pattern 1: Resolve during construction
//	type FileService struct {
//	    storage Storage
//	}
//
//	func NewFileService(c forge.Container) *FileService {
//	    return &FileService{
//	        storage: storage.MustGetStorage(c, "s3"),
//	    }
//	}
//
//	// Pattern 2: Multi-backend usage
//	func NewBackupService(c forge.Container) *BackupService {
//	    return &BackupService{
//	        primary: storage.MustGetBackend(c, "s3"),
//	        backup:  storage.MustGetBackend(c, "gcs"),
//	    }
//	}
//
//	// Pattern 3: Using default backend
//	func NewCacheService(c forge.Container) (*CacheService, error) {
//	    s, err := storage.GetDefault(c)
//	    if err != nil {
//	        // Storage not configured, use in-memory cache
//	        return &CacheService{useMemory: true}, nil
//	    }
//	    return &CacheService{storage: s}, nil
//	}

// =============================================================================
// Container-based Helpers (Most flexible)
// =============================================================================

// GetManager retrieves the StorageManager from the container.
// Returns error if not found or type assertion fails.
//
// Example:
//
//	manager, err := storage.GetManager(c)
//	if err != nil {
//	    return fmt.Errorf("storage not available: %w", err)
//	}
//	s3 := manager.Backend("s3")
func GetManager(c forge.Container) (*StorageManager, error) {
	man, _ := forge.InjectType[*StorageManager](c)
	if man != nil {
		return man, nil
	}
	return forge.Inject[*StorageManager](c)
}

// MustGetManager retrieves the StorageManager from the container.
// Panics if not found or type assertion fails.
//
// Example:
//
//	manager := storage.MustGetManager(c)
//	backends := manager.backends
func MustGetManager(c forge.Container) *StorageManager {
	man, _ := forge.InjectType[*StorageManager](c)
	if man != nil {
		return man
	}
	return forge.MustInject[*StorageManager](c)
}

// GetDefault retrieves the default storage backend from the container using the StorageManager.
//
// Returns error if:
//   - Storage extension not registered
//   - No default backend configured
//   - Default backend not found
//
// This is useful when you want the Storage interface without knowing the specific backend type.
func GetDefault(c forge.Container) (Storage, error) {
	manager, err := GetManager(c)
	if err != nil {
		return nil, err
	}
	return manager.DefaultBackend()
}

// MustGetDefault retrieves the default storage backend from the container using the StorageManager.
// Panics if storage extension is not registered or no default backend is configured.
//
// This is useful when you want the Storage interface without knowing the specific backend type.
func MustGetDefault(c forge.Container) Storage {
	backend, err := GetDefault(c)
	if err != nil {
		panic(fmt.Sprintf("failed to get default storage backend: %v", err))
	}
	return backend
}

// GetStorage retrieves a named Storage backend from the container.
//
// Safe to call anytime - automatically ensures StorageManager is resolved first.
//
// Returns error if:
//   - Storage extension not registered
//   - Backend with given name not found
//
// Example:
//
//	s, err := storage.GetStorage(c, "s3")
//	if err != nil {
//	    return fmt.Errorf("failed to get storage: %w", err)
//	}
//	err = s.Upload(ctx, "file.pdf", reader)
func GetStorage(c forge.Container, name string) (Storage, error) {
	// Ensure manager is resolved first
	manager, err := GetManager(c)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve storage manager: %w", err)
	}

	backend := manager.Backend(name)
	if backend == nil {
		return nil, fmt.Errorf("backend %s not found", name)
	}

	return backend, nil
}

// MustGetStorage retrieves a named Storage backend from the container.
//
// Safe to call anytime - automatically ensures StorageManager is resolved first.
//
// Panics if:
//   - Storage extension not registered
//   - Backend with given name not found
//
// Example:
//
//	func NewFileService(c forge.Container) *FileService {
//	    return &FileService{
//	        storage: storage.MustGetStorage(c, "s3"),
//	    }
//	}
func MustGetStorage(c forge.Container, name string) Storage {
	// Ensure manager is resolved first
	backend, err := GetStorage(c, name)
	if err != nil {
		panic(fmt.Sprintf("failed to resolve storage backend %s: %v", name, err))
	}

	return backend
}

// GetBackend retrieves a named backend through the StorageManager.
//
// Safe to call anytime - automatically ensures StorageManager is resolved first.
//
// Returns error if:
//   - Storage extension not registered
//   - Backend with given name not found
//
// Example:
//
//	s3, err := storage.GetBackend(c, "s3")
//	if err != nil {
//	    return err
//	}
func GetBackend(c forge.Container, name string) (Storage, error) {
	// Ensure manager is resolved first
	manager, err := GetManager(c)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve storage manager: %w", err)
	}

	backend := manager.Backend(name)
	if backend == nil {
		return nil, fmt.Errorf("backend %s not found", name)
	}

	return backend, nil
}

// MustGetBackend retrieves a named backend through the StorageManager.
//
// Safe to call anytime - automatically ensures StorageManager is resolved first.
//
// Panics if:
//   - Storage extension not registered
//   - Backend with given name not found
//
// Example:
//
//	s3 := storage.MustGetBackend(c, "s3")
func MustGetBackend(c forge.Container, name string) Storage {
	// Ensure manager is resolved first
	backend, err := GetBackend(c, name)
	if err != nil {
		panic(fmt.Sprintf("failed to resolve backend %s: %v", name, err))
	}

	return backend
}

// =============================================================================
// App-based Helpers (Convenience wrappers)
// =============================================================================

// GetManagerFromApp retrieves the StorageManager from the app.
// Returns error if app is nil or manager not found.
//
// Example:
//
//	manager, err := storage.GetManagerFromApp(app)
//	if err != nil {
//	    return err
//	}
func GetManagerFromApp(app forge.App) (*StorageManager, error) {
	if app == nil {
		return nil, errors.New("app is nil")
	}

	return GetManager(app.Container())
}

// MustGetManagerFromApp retrieves the StorageManager from the app.
// Panics if app is nil or manager not found.
//
// Example:
//
//	manager := storage.MustGetManagerFromApp(app)
func MustGetManagerFromApp(app forge.App) *StorageManager {
	if app == nil {
		panic("app is nil")
	}

	return MustGetManager(app.Container())
}

// GetDefaultFromApp retrieves the default Storage backend from the app.
// Returns error if app is nil or backend not found.
//
// Example:
//
//	backend, err := storage.GetDefaultFromApp(app)
//	if err != nil {
//	    return err
//	}
func GetDefaultFromApp(app forge.App) (Storage, error) {
	if app == nil {
		return nil, errors.New("app is nil")
	}

	return GetDefault(app.Container())
}

// MustGetDefaultFromApp retrieves the default Storage backend from the app.
// Panics if app is nil or backend not found.
//
// Example:
//
//	backend := storage.MustGetDefaultFromApp(app)
func MustGetDefaultFromApp(app forge.App) Storage {
	if app == nil {
		panic("app is nil")
	}

	return MustGetDefault(app.Container())
}

// GetStorageFromApp retrieves a named Storage backend from the app.
// Returns error if app is nil or storage not found.
//
// Example:
//
//	s, err := storage.GetStorageFromApp(app, "s3")
//	if err != nil {
//	    return err
//	}
func GetStorageFromApp(app forge.App, name string) (Storage, error) {
	if app == nil {
		return nil, errors.New("app is nil")
	}

	return GetStorage(app.Container(), name)
}

// MustGetStorageFromApp retrieves a named Storage backend from the app.
// Panics if app is nil or storage not found.
//
// Example:
//
//	func setupRoutes(app forge.App) {
//	    s := storage.MustGetStorageFromApp(app, "s3")
//	    // Use storage
//	}
func MustGetStorageFromApp(app forge.App, name string) Storage {
	if app == nil {
		panic("app is nil")
	}

	return MustGetStorage(app.Container(), name)
}

// GetBackendFromApp retrieves a named backend from the app.
// Returns error if app is nil, manager not found, or backend not found.
//
// Example:
//
//	s3, err := storage.GetBackendFromApp(app, "s3")
//	if err != nil {
//	    return err
//	}
func GetBackendFromApp(app forge.App, name string) (Storage, error) {
	if app == nil {
		return nil, errors.New("app is nil")
	}

	return GetBackend(app.Container(), name)
}

// MustGetBackendFromApp retrieves a named backend from the app.
// Panics if app is nil, manager not found, or backend not found.
//
// Example:
//
//	s3 := storage.MustGetBackendFromApp(app, "s3")
func MustGetBackendFromApp(app forge.App, name string) Storage {
	if app == nil {
		panic("app is nil")
	}

	return MustGetBackend(app.Container(), name)
}

// =============================================================================
// Named Backend Helpers (Advanced usage)
// =============================================================================

// GetNamedBackend retrieves a named backend through the StorageManager.
// This is useful when you have multiple backends configured.
//
// Example:
//
//	// Use S3 for production files
//	s3, err := storage.GetNamedBackend(c, "s3")
//	if err != nil {
//	    return err
//	}
//
//	// Use local storage for temporary files
//	local, err := storage.GetNamedBackend(c, "local")
//	if err != nil {
//	    return err
//	}
func GetNamedBackend(c forge.Container, name string) (Storage, error) {
	manager, err := GetManager(c)
	if err != nil {
		return nil, fmt.Errorf("failed to get storage manager: %w", err)
	}

	backend := manager.Backend(name)
	if backend == nil {
		return nil, fmt.Errorf("backend %s not found", name)
	}

	return backend, nil
}

// MustGetNamedBackend retrieves a named backend through the StorageManager.
// Panics if manager not found or backend not found.
//
// Example:
//
//	type BackupService struct {
//	    primary Storage
//	    backup  Storage
//	}
//
//	func NewBackupService(c forge.Container) *BackupService {
//	    return &BackupService{
//	        primary: storage.MustGetNamedBackend(c, "s3"),
//	        backup:  storage.MustGetNamedBackend(c, "gcs"),
//	    }
//	}
func MustGetNamedBackend(c forge.Container, name string) Storage {
	manager := MustGetManager(c)

	backend := manager.Backend(name)
	if backend == nil {
		panic(fmt.Sprintf("failed to get backend %s", name))
	}

	return backend
}

// =============================================================================
// App-based Named Backend Helpers (Convenience)
// =============================================================================

// GetNamedBackendFromApp retrieves a named backend from the app.
// Returns error if app is nil, manager not found, or backend not found.
//
// Example:
//
//	s3, err := storage.GetNamedBackendFromApp(app, "s3")
//	if err != nil {
//	    return err
//	}
func GetNamedBackendFromApp(app forge.App, name string) (Storage, error) {
	if app == nil {
		return nil, errors.New("app is nil")
	}

	return GetNamedBackend(app.Container(), name)
}

// MustGetNamedBackendFromApp retrieves a named backend from the app.
// Panics if app is nil, manager not found, or backend not found.
//
// Example:
//
//	s3 := storage.MustGetNamedBackendFromApp(app, "s3")
func MustGetNamedBackendFromApp(app forge.App, name string) Storage {
	if app == nil {
		panic("app is nil")
	}

	return MustGetNamedBackend(app.Container(), name)
}
