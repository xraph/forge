package core

import (
	"errors"
	"maps"
	"slices"
)

// ConnectionConfig is private connection configuration, never dashboard metadata.
type ConnectionConfig struct {
	Type               string `json:"type"               yaml:"type"`
	URL                string `json:"url"                yaml:"url"`
	DeadLetterReplicas int    `json:"deadLetterReplicas" yaml:"dead_letter_replicas"`
}

// WithConfig supplies explicit Forge configuration, overriding loaded non-zero values.
func WithConfig(cfg Config) Option {
	return func(r *Runtime) error {
		if !r.deferred {
			return ErrConflict
		}

		r.config = cloneConfig(cfg)

		return nil
	}
}

// NewDeferred allows handler binding before Forge loads application configuration.
func NewDeferred(options ...Option) (*Runtime, error) {
	r := &Runtime{deferred: true, providers: map[string]Provider{}, registrations: map[string]registration{}}

	for _, option := range options {
		if option == nil {
			return nil, errors.New("conduit: nil option")
		}

		if err := option(r); err != nil {
			return nil, err
		}
	}

	return r, nil
}

func cloneConfig(cfg Config) Config {
	cfg.Endpoints = slices.Clone(cfg.Endpoints)
	cfg.Providers = maps.Clone(cfg.Providers)

	cfg.Streams = maps.Clone(cfg.Streams)
	for name, stream := range cfg.Streams {
		stream.Subjects = slices.Clone(stream.Subjects)
		cfg.Streams[name] = stream
	}

	cfg.Subscriptions = maps.Clone(cfg.Subscriptions)

	return cfg
}

// Configuration returns a copy for Forge initialization. It may contain credentials.
func (r *Runtime) Configuration() Config {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return cloneConfig(r.config)
}

// Providers returns registered provider instances for initialization adapters.
func (r *Runtime) Providers() map[string]Provider {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return maps.Clone(r.providers)
}

// SetRegistry fills the discovery adapter before startup.
func (r *Runtime) SetRegistry(registry Registry) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.running || r.closed {
		return ErrConflict
	}

	return WithRegistry(registry)(r)
}

// Configure validates loaded topology and applies deferred registrations atomically.
func (r *Runtime) Configure(cfg Config, extra ...Option) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.deferred || r.running || r.closed {
		return ErrConflict
	}

	opts := make([]Option, 0, len(r.providers)+len(extra)+2)
	for name, p := range r.providers {
		opts = append(opts, WithProvider(name, p))
	}

	opts = append(opts, WithHooks(r.hooks...))
	if r.registry != nil {
		opts = append(opts, WithRegistry(r.registry))
	}

	opts = append(opts, extra...)

	ready, err := New(cfg, opts...)
	if err != nil {
		return err
	}

	for id, reg := range r.registrations {
		if err := ready.Bind(id, reg.config.MessageType, reg.handler); err != nil {
			return err
		}
	}

	r.config, r.providers, r.registrations, r.hooks, r.registry = ready.config, ready.providers, ready.registrations, ready.hooks, ready.registry
	r.deferred = false

	return nil
}

func wrapHandler(handler Handler, middlewares []Middleware) (Handler, error) {
	err := protect(func() error {
		for _, middleware := range slices.Backward(middlewares) {
			if middleware == nil {
				return errors.New("conduit: nil middleware")
			}

			handler = middleware(handler)
			if handler == nil {
				return errors.New("conduit: middleware returned nil handler")
			}
		}

		return nil
	})

	return handler, err
}
