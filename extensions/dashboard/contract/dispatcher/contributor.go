package dispatcher

import "fmt"

// RegisterContributor walks a Contributor's Handlers() and Subscriptions() maps
// and registers each one. Atomic: if any registration fails, all preceding
// registrations from this call are rolled back.
func (d *Dispatcher) RegisterContributor(c Contributor) error {
	if c == nil {
		return fmt.Errorf("dispatcher: nil contributor")
	}
	name := c.Name()
	if name == "" {
		return fmt.Errorf("dispatcher: contributor name is empty")
	}

	// Snapshot what we register so we can roll back on failure.
	var registeredHandlers []handlerKey
	var registeredSubs []handlerKey

	rollback := func() {
		d.mu.Lock()
		defer d.mu.Unlock()
		for _, k := range registeredHandlers {
			delete(d.handlers, k)
		}
		for _, k := range registeredSubs {
			delete(d.subscriptions, k)
		}
	}

	for ref, h := range c.Handlers() {
		if err := d.Register(name, ref.Intent, ref.Version, h); err != nil {
			rollback()
			return fmt.Errorf("contributor %q: %w", name, err)
		}
		registeredHandlers = append(registeredHandlers, handlerKey{name, ref.Intent, ref.Version})
	}
	for ref, h := range c.Subscriptions() {
		if err := d.RegisterSubscription(name, ref.Intent, ref.Version, h); err != nil {
			rollback()
			return fmt.Errorf("contributor %q: %w", name, err)
		}
		registeredSubs = append(registeredSubs, handlerKey{name, ref.Intent, ref.Version})
	}
	return nil
}
