package storage

// DI container keys for storage services.
// These constants define the keys used to register and resolve storage services
// from the Forge DI container.
const (
	// ManagerKey is the DI key for the StorageManager singleton.
	// Use GetManager() or MustGetManager() to resolve it.
	ManagerKey = "storage.manager"

	// StorageKey is the DI key for the default Storage backend.
	// Use GetStorage() or MustGetStorage() to resolve it.
	StorageKey = "storage.default"
)

