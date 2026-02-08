package storage

// DI container keys for storage services.
// These constants define the keys used to register and resolve storage services
// from the Forge DI container.
const (
	// ManagerKey is the DI key for the StorageManager singleton.
	// Use GetManager() or MustGetManager() to resolve it.
	ManagerKey = "storage.manager"

	// StorageKey is the DI key for the default Storage backend.
	// Use GetDefault() or MustGetDefault() to resolve the default backend.
	// Use GetStorage(c, name) or MustGetStorage(c, name) to resolve named backends.
	StorageKey = "storage.default"
)
