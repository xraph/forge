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
//	        storage: storage.MustGetStorage(c),  // Simple!
//	    }
//	}
//
//	// Or with error handling:
//	s, err := storage.GetStorage(c)
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
//	  ✅ GetStorage()     - Default backend exists and ready to use
//	  ✅ GetNamedBackend()- Named backends exist and ready to use
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
//	        storage: storage.MustGetStorage(c),
//	    }
//	}
//
//	// Pattern 2: Multi-backend usage
//	func NewBackupService(c forge.Container) *BackupService {
//	    return &BackupService{
//	        primary: storage.MustGetNamedBackend(c, "s3"),
//	        backup:  storage.MustGetNamedBackend(c, "gcs"),
//	    }
//	}
//
//	// Pattern 3: Optional storage
//	func NewCacheService(c forge.Container) (*CacheService, error) {
//	    s, err := storage.GetStorage(c)
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
	return forge.Resolve[*StorageManager](c, ManagerKey)
}

// MustGetManager retrieves the StorageManager from the container.
// Panics if not found or type assertion fails.
//
// Example:
//
//	manager := storage.MustGetManager(c)
//	backends := manager.backends
func MustGetManager(c forge.Container) *StorageManager {
	return forge.Must[*StorageManager](c, ManagerKey)
}

// GetStorage retrieves the default Storage backend from the container.
// Returns error if not found or type assertion fails.
//
// This is the most common helper for accessing storage services.
// It returns the default backend configured in the extension.
//
// Example:
//
//	s, err := storage.GetStorage(c)
//	if err != nil {
//	    return fmt.Errorf("failed to get storage: %w", err)
//	}
//	err = s.Upload(ctx, "file.pdf", reader)
func GetStorage(c forge.Container) (Storage, error) {
	return forge.Resolve[Storage](c, StorageKey)
}

// MustGetStorage retrieves the default Storage backend from the container.
// Panics if not found or type assertion fails.
//
// This is the recommended helper for services that require storage.
//
// Example:
//
//	func NewFileService(c forge.Container) *FileService {
//	    return &FileService{
//	        storage: storage.MustGetStorage(c),
//	    }
//	}
func MustGetStorage(c forge.Container) Storage {
	return forge.Must[Storage](c, StorageKey)
}

// GetBackend retrieves a named backend through the StorageManager.
// This is an alias for GetNamedBackend for consistency with the manager's Backend() method.
//
// Example:
//
//	s3, err := storage.GetBackend(c, "s3")
//	if err != nil {
//	    return err
//	}
func GetBackend(c forge.Container, name string) (Storage, error) {
	return GetNamedBackend(c, name)
}

// MustGetBackend retrieves a named backend through the StorageManager.
// This is an alias for MustGetNamedBackend for consistency with the manager's Backend() method.
// Panics if manager not found or backend not found.
//
// Example:
//
//	s3 := storage.MustGetBackend(c, "s3")
func MustGetBackend(c forge.Container, name string) Storage {
	return MustGetNamedBackend(c, name)
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

// GetStorageFromApp retrieves the default Storage backend from the app.
// Returns error if app is nil or storage not found.
//
// Example:
//
//	s, err := storage.GetStorageFromApp(app)
//	if err != nil {
//	    return err
//	}
func GetStorageFromApp(app forge.App) (Storage, error) {
	if app == nil {
		return nil, errors.New("app is nil")
	}

	return GetStorage(app.Container())
}

// MustGetStorageFromApp retrieves the default Storage backend from the app.
// Panics if app is nil or storage not found.
//
// Example:
//
//	func setupRoutes(app forge.App) {
//	    s := storage.MustGetStorageFromApp(app)
//	    // Use storage
//	}
func MustGetStorageFromApp(app forge.App) Storage {
	if app == nil {
		panic("app is nil")
	}

	return MustGetStorage(app.Container())
}

// GetBackendFromApp retrieves a named backend from the app.
// This is an alias for GetNamedBackendFromApp.
// Returns error if app is nil, manager not found, or backend not found.
//
// Example:
//
//	s3, err := storage.GetBackendFromApp(app, "s3")
//	if err != nil {
//	    return err
//	}
func GetBackendFromApp(app forge.App, name string) (Storage, error) {
	return GetNamedBackendFromApp(app, name)
}

// MustGetBackendFromApp retrieves a named backend from the app.
// This is an alias for MustGetNamedBackendFromApp.
// Panics if app is nil, manager not found, or backend not found.
//
// Example:
//
//	s3 := storage.MustGetBackendFromApp(app, "s3")
func MustGetBackendFromApp(app forge.App, name string) Storage {
	return MustGetNamedBackendFromApp(app, name)
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
		panic(fmt.Sprintf("backend %s not found", name))
	}

	return backend
}

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

