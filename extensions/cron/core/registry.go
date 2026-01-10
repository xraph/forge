package core

import (
	"fmt"
	"sync"
)

// SchedulerDeps holds dependencies needed to create a scheduler.
// The actual types (Storage, Executor, etc.) are from the parent cron package,
// but we use interface{} here to avoid import cycles.
type SchedulerDeps struct {
	Storage  any // cron.Storage
	Executor any // *cron.Executor
	Registry any // *cron.JobRegistry
	Logger   any // forge.Logger
}

// SchedulerFactory is a function that creates a Scheduler instance.
type SchedulerFactory func(config any, deps *SchedulerDeps) (Scheduler, error)

// StorageFactory is a function that creates a Storage instance.
type StorageFactory func(config any) (Storage, error)

// schedulerRegistry holds registered scheduler factories.
var (
	schedulerFactories = make(map[string]SchedulerFactory)
	storageFactories   = make(map[string]StorageFactory)
	registryMutex      sync.RWMutex
)

// RegisterSchedulerFactory registers a scheduler factory with a given name.
func RegisterSchedulerFactory(name string, factory SchedulerFactory) {
	registryMutex.Lock()
	defer registryMutex.Unlock()

	schedulerFactories[name] = factory
}

// CreateScheduler creates a scheduler instance using the registered factory.
func CreateScheduler(name string, config any, deps *SchedulerDeps) (Scheduler, error) {
	registryMutex.RLock()

	factory, exists := schedulerFactories[name]

	registryMutex.RUnlock()

	if !exists {
		return nil, fmt.Errorf("scheduler '%s' not registered", name)
	}

	return factory(config, deps)
}

// ListSchedulers returns the names of all registered schedulers.
func ListSchedulers() []string {
	registryMutex.RLock()
	defer registryMutex.RUnlock()

	names := make([]string, 0, len(schedulerFactories))
	for name := range schedulerFactories {
		names = append(names, name)
	}

	return names
}

// RegisterStorageFactory registers a storage factory with a given name.
func RegisterStorageFactory(name string, factory StorageFactory) {
	registryMutex.Lock()
	defer registryMutex.Unlock()

	storageFactories[name] = factory
}

// CreateStorage creates a storage instance using the registered factory.
func CreateStorage(name string, config any) (Storage, error) {
	registryMutex.RLock()

	factory, exists := storageFactories[name]

	registryMutex.RUnlock()

	if !exists {
		return nil, fmt.Errorf("storage '%s' not registered", name)
	}

	return factory(config)
}

// ListStorages returns the names of all registered storage backends.
func ListStorages() []string {
	registryMutex.RLock()
	defer registryMutex.RUnlock()

	names := make([]string, 0, len(storageFactories))
	for name := range storageFactories {
		names = append(names, name)
	}

	return names
}
