package di

import (
	"fmt"
	"sync"

	errors2 "github.com/xraph/forge/internal/errors"
	"github.com/xraph/forge/internal/shared"
)

// scope implements Scope.
type scope struct {
	parent    *containerImpl
	instances map[string]any
	mu        sync.RWMutex
	ended     bool
}

// newScope creates a new scope.
func newScope(parent *containerImpl) *scope {
	return &scope{
		parent:    parent,
		instances: make(map[string]any),
	}
}

// Resolve returns a service by name from this scope.
func (s *scope) Resolve(name string) (any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.ended {
		return nil, errors2.ErrScopeEnded
	}

	// Get registration from parent
	s.parent.mu.RLock()
	reg, exists := s.parent.services[name]
	s.parent.mu.RUnlock()

	if !exists {
		return nil, errors2.ErrServiceNotFound(name)
	}

	// Singleton services: resolve from parent
	if reg.singleton {
		return s.parent.Resolve(name)
	}

	// Scoped services: cache in this scope
	if reg.scoped {
		if instance, ok := s.instances[name]; ok {
			return instance, nil
		}

		// Create new instance for this scope
		instance, err := reg.factory(s.parent)
		if err != nil {
			return nil, errors2.NewServiceError(name, "resolve", err)
		}

		s.instances[name] = instance

		return instance, nil
	}

	// Transient services: always create new
	instance, err := reg.factory(s.parent)
	if err != nil {
		return nil, errors2.NewServiceError(name, "resolve", err)
	}

	return instance, nil
}

// End cleans up all scoped services in this scope.
func (s *scope) End() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.ended {
		return errors2.ErrScopeEnded
	}

	// Dispose of scoped instances in reverse order
	var errs []error

	for name, instance := range s.instances {
		if disposable, ok := instance.(shared.Disposable); ok {
			if err := disposable.Dispose(); err != nil {
				errs = append(errs, fmt.Errorf("failed to dispose %s: %w", name, err))
			}
		}
	}

	s.instances = nil
	s.ended = true

	if len(errs) > 0 {
		return fmt.Errorf("scope cleanup errors: %v", errs)
	}

	return nil
}
