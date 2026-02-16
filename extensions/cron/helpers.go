package cron

import (
	"fmt"

	"github.com/xraph/forge"
)

// Helper functions for convenient cron service access from DI container.
// Provides lightweight wrappers around Forge's DI system to eliminate verbose boilerplate.

// GetScheduler retrieves the cron Scheduler from the container.
// Returns error if not found or type assertion fails.
func GetScheduler(c forge.Container) (Scheduler, error) {
	return forge.Inject[Scheduler](c)
}

// MustGetScheduler retrieves the cron Scheduler from the container.
// Panics if not found or type assertion fails.
func MustGetScheduler(c forge.Container) Scheduler {
	scheduler, err := GetScheduler(c)
	if err != nil {
		panic(fmt.Sprintf("failed to get cron scheduler: %v", err))
	}
	return scheduler
}

// GetRegistry retrieves the JobRegistry from the container.
// Returns error if not found or type assertion fails.
func GetRegistry(c forge.Container) (*JobRegistry, error) {
	return forge.Inject[*JobRegistry](c)
}

// MustGetRegistry retrieves the JobRegistry from the container.
// Panics if not found or type assertion fails.
func MustGetRegistry(c forge.Container) *JobRegistry {
	registry, err := GetRegistry(c)
	if err != nil {
		panic(fmt.Sprintf("failed to get cron registry: %v", err))
	}
	return registry
}

// GetExecutor retrieves the Executor from the container.
// Returns error if not found or type assertion fails.
func GetExecutor(c forge.Container) (*Executor, error) {
	return forge.Inject[*Executor](c)
}

// MustGetExecutor retrieves the Executor from the container.
// Panics if not found or type assertion fails.
func MustGetExecutor(c forge.Container) *Executor {
	executor, err := GetExecutor(c)
	if err != nil {
		panic(fmt.Sprintf("failed to get cron executor: %v", err))
	}
	return executor
}

// GetStorage retrieves the Storage from the container.
// Returns error if not found or type assertion fails.
func GetStorage(c forge.Container) (Storage, error) {
	return forge.Inject[Storage](c)
}

// MustGetStorage retrieves the Storage from the container.
// Panics if not found or type assertion fails.
func MustGetStorage(c forge.Container) Storage {
	storage, err := GetStorage(c)
	if err != nil {
		panic(fmt.Sprintf("failed to get cron storage: %v", err))
	}
	return storage
}

// GetSchedulerFromApp retrieves the cron Scheduler from the app.
// Returns error if not found or type assertion fails.
func GetSchedulerFromApp(app forge.App) (Scheduler, error) {
	if app == nil {
		return nil, fmt.Errorf("app is nil")
	}
	return GetScheduler(app.Container())
}

// MustGetSchedulerFromApp retrieves the cron Scheduler from the app.
// Panics if not found or type assertion fails.
func MustGetSchedulerFromApp(app forge.App) Scheduler {
	if app == nil {
		panic("app is nil")
	}
	return MustGetScheduler(app.Container())
}
