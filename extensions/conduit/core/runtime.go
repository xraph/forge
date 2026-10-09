package core

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Config separates stream topology, logical subscriptions and replica settings.
type Config struct {
	Identity        Identity                      `json:"identity"        yaml:"identity"`
	Streams         map[string]StreamConfig       `json:"streams"         yaml:"streams"`
	Subscriptions   map[string]SubscriptionConfig `json:"subscriptions"   yaml:"subscriptions"`
	ObserverBuffer  int                           `json:"observerBuffer"  yaml:"observer_buffer"`
	MaxPayloadBytes int                           `json:"maxPayloadBytes" yaml:"max_payload_bytes"`
	Version         string                        `json:"version"         yaml:"version"`
	Endpoints       []Endpoint                    `json:"endpoints"       yaml:"endpoints"`
}

// Option supplies a provider or an extension point.
type Option func(*Runtime) error

// WithProvider registers a named connection independently of message routing.
func WithProvider(name string, provider Provider) Option {
	return func(r *Runtime) error {
		if name == "" || provider == nil {
			return errors.New("conduit: provider name and implementation are required")
		}

		if _, exists := r.providers[name]; exists {
			return fmt.Errorf("%w: duplicate provider %s", ErrConflict, name)
		}

		r.providers[name] = provider

		return nil
	}
}

// WithHooks registers optional callbacks in deterministic registration order.
func WithHooks(hooks ...Hook) Option {
	return func(r *Runtime) error {
		for _, hook := range hooks {
			if hook == nil || hook.Name() == "" {
				return errors.New("conduit: hook name is required")
			}

			for _, existing := range r.hooks {
				if existing.Name() == hook.Name() {
					return fmt.Errorf("%w: duplicate hook %s", ErrConflict, hook.Name())
				}
			}

			r.hooks = append(r.hooks, hook)
		}

		return nil
	}
}

// RegisterHook adds an extension hook before startup.
func (r *Runtime) RegisterHook(hook Hook) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.running || r.closed {
		return ErrConflict
	}

	return WithHooks(hook)(r)
}

type registration struct {
	config  SubscriptionConfig
	handler Handler
}

// Runtime owns one instance's connections and workers, never the service identity.
type Runtime struct {
	mu              sync.RWMutex
	config          Config
	providers       map[string]Provider
	registrations   map[string]registration
	hooks           []Hook
	observers       *observerPool
	cancel          context.CancelFunc
	observerCancel  context.CancelFunc
	subscriptions   []Subscription
	workers         sync.WaitGroup
	observerWorkers sync.WaitGroup
	running         bool
	startedAt       time.Time
	published       atomic.Uint64
	handled         atomic.Uint64
	failed          atomic.Uint64
	retried         atomic.Uint64
	deadLettered    atomic.Uint64
	acknowledged    atomic.Uint64
	historyMu       sync.Mutex
	history         []HookEvent
	registry        Registry
	closed          bool
	stopping        bool
}

// WithRegistry enables registration, discovery and instance inspection.
func WithRegistry(registry Registry) Option {
	return func(r *Runtime) error {
		if registry == nil {
			return errors.New("conduit: registry is required")
		}

		r.registry = registry

		return nil
	}
}

// Resolver exposes the configured service discovery provider.
func (r *Runtime) Resolver() Resolver { return r.registry }

// Instances lists visible members inside this runtime's namespace.
func (r *Runtime) Instances(ctx context.Context) ([]Instance, error) {
	lister, ok := r.registry.(InstanceLister)
	if !ok {
		return nil, ErrUnsupported
	}

	instances, err := lister.List(ctx, r.config.Identity.Namespace)
	if err != nil {
		return nil, err
	}

	visible := make([]Instance, 0, len(instances))
	for _, instance := range instances {
		if instance.Identity.Namespace != r.config.Identity.Namespace {
			continue
		}

		if err := instance.Validate(); err != nil {
			return nil, errors.New("conduit: invalid discovery record")
		}

		instance.Endpoints = slices.Clone(instance.Endpoints)
		visible = append(visible, instance)
	}

	return visible, nil
}

// New constructs a validated runtime. Instance ID can be generated per process.
func New(cfg Config, options ...Option) (*Runtime, error) {
	if cfg.Identity.Namespace == "" {
		cfg.Identity.Namespace = "default"
	}

	if cfg.Identity.InstanceID == "" {
		cfg.Identity.InstanceID = NewID()
	}

	if err := cfg.Identity.Validate(); err != nil {
		return nil, err
	}

	if cfg.ObserverBuffer <= 0 {
		cfg.ObserverBuffer = 256
	}

	if cfg.MaxPayloadBytes <= 0 {
		cfg.MaxPayloadBytes = 1 << 20
	}

	if cfg.ObserverBuffer > 65536 {
		return nil, errors.New("conduit: observer buffer exceeds 65536")
	}

	r := &Runtime{config: cfg, providers: make(map[string]Provider), registrations: make(map[string]registration)}

	r.config.Endpoints = slices.Clone(cfg.Endpoints)
	for _, endpoint := range cfg.Endpoints {
		if err := endpoint.Validate(); err != nil {
			return nil, err
		}
	}

	r.config.Streams = make(map[string]StreamConfig, len(cfg.Streams))

	r.config.Subscriptions = make(map[string]SubscriptionConfig, len(cfg.Subscriptions))
	for name, stream := range cfg.Streams {
		stream.Name = name

		stream.Subjects = slices.Clone(stream.Subjects)
		if stream.Replicas == 0 {
			stream.Replicas = 1
		}

		if err := ValidateTopic(name, false); err != nil {
			return nil, err
		}

		if len(stream.Subjects) == 0 || stream.MaxAge < 0 || stream.MaxMessages < 0 || stream.Replicas < 1 || stream.Replicas > 5 {
			return nil, fmt.Errorf("conduit: invalid stream %s", name)
		}

		for _, subject := range stream.Subjects {
			if err := ValidateTopic(subject, true); err != nil {
				return nil, err
			}
		}

		r.config.Streams[name] = stream
	}

	for id, sub := range cfg.Subscriptions {
		sub.ID = id
		r.config.Subscriptions[id] = defaults(sub)
	}

	for _, option := range options {
		if err := option(r); err != nil {
			return nil, err
		}
	}

	for name, stream := range r.config.Streams {
		if r.providers[stream.Provider] == nil {
			return nil, fmt.Errorf("conduit: stream %s references missing provider %s", name, stream.Provider)
		}
	}

	return r, nil
}

func defaults(sub SubscriptionConfig) SubscriptionConfig {
	if sub.Mode == "" {
		sub.Mode = Competing
	}

	if sub.Concurrency == 0 {
		sub.Concurrency = 1
	}

	if sub.MaxInFlight == 0 {
		sub.MaxInFlight = sub.Concurrency
	}

	if sub.Timeout == 0 {
		sub.Timeout = 30 * time.Second
	}

	if sub.MaxAttempts == 0 {
		sub.MaxAttempts = 5
	}

	if sub.RetryDelay == 0 {
		sub.RetryDelay = time.Second
	}

	if sub.StartAt == "" {
		sub.StartAt = "all"
		if sub.Mode == Broadcast && !sub.Durable {
			sub.StartAt = "new"
		}
	}

	return sub
}

// Identity returns the service and replica identity for this runtime.
func (r *Runtime) Identity() Identity { return r.config.Identity }

// DurableStream checks a provider before transactional publication.
func (r *Runtime) DurableStream(name string) bool {
	stream, ok := r.config.Streams[name]

	return ok && r.providers[stream.Provider].Capabilities().Durable
}

// StreamFor returns an unambiguous message route. Overlapping routes require an explicit stream.
func (r *Runtime) StreamFor(messageType string) (StreamConfig, error) {
	var found *StreamConfig

	for _, stream := range r.config.Streams {
		for _, subject := range stream.Subjects {
			if Matches(subject, messageType) {
				if found != nil && found.Name != stream.Name {
					return StreamConfig{}, fmt.Errorf("%w: ambiguous stream for %s", ErrConflict, messageType)
				}

				copyStream := stream
				copyStream.Subjects = slices.Clone(stream.Subjects)
				found = &copyStream

				break
			}
		}
	}

	if found == nil {
		return StreamConfig{}, fmt.Errorf("%w: no stream for %s", ErrNotFound, messageType)
	}

	return *found, nil
}

// Bind installs one logical handler before startup.
func (r *Runtime) Bind(id, messageType string, handler Handler, middlewares ...Middleware) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.running {
		return fmt.Errorf("%w: subscriptions must be registered before startup", ErrConflict)
	}

	if handler == nil {
		return errors.New("conduit: handler is required")
	}

	if _, exists := r.registrations[id]; exists {
		return fmt.Errorf("%w: duplicate subscription %s", ErrConflict, id)
	}

	sub, ok := r.config.Subscriptions[id]
	if !ok {
		return fmt.Errorf("%w: subscription %s is not configured", ErrNotFound, id)
	}

	if sub.MessageType != "" && sub.MessageType != messageType {
		return fmt.Errorf("%w: subscription message type differs", ErrConflict)
	}

	sub.MessageType = messageType
	if err := ValidateTopic(messageType, false); err != nil {
		return err
	}

	stream, ok := r.config.Streams[sub.Stream]
	if !ok {
		return fmt.Errorf("%w: stream %s", ErrNotFound, sub.Stream)
	}

	if err := validateSubscription(sub, r.providers[stream.Provider].Capabilities()); err != nil {
		return err
	}

	if _, ok := r.providers[stream.Provider].(Management); !ok {
		return ErrUnsupported
	}

	matched := false
	for _, subject := range stream.Subjects {
		matched = matched || Matches(subject, messageType)
	}

	if !matched {
		return fmt.Errorf("conduit: subscription %s does not match stream subjects", id)
	}

	if err := protect(func() error {
		for _, v := range slices.Backward(middlewares) {
			if v == nil {
				return errors.New("conduit: nil middleware")
			}

			handler = v(handler)
			if handler == nil {
				return errors.New("conduit: middleware returned nil handler")
			}
		}

		return nil
	}); err != nil {
		return err
	}

	r.config.Subscriptions[id] = sub
	r.registrations[id] = registration{config: sub, handler: handler}

	return nil
}

func validateSubscription(sub SubscriptionConfig, caps Capabilities) error {
	if sub.ID == "" || sub.Mode != Competing && sub.Mode != Broadcast || sub.Concurrency < 1 || sub.Concurrency > 1024 || sub.MaxInFlight < sub.Concurrency || sub.MaxInFlight > 65536 || sub.Timeout <= 0 || sub.MaxAttempts < 1 || sub.RetryDelay < 0 {
		return fmt.Errorf("conduit: invalid subscription %s", sub.ID)
	}

	if sub.Timeout > 24*time.Hour || sub.RetryDelay > 24*time.Hour || sub.MaxAttempts > 1000000 {
		return errors.New("conduit: subscription timeout, delay or attempt limit is too large")
	}

	if sub.Durable && !caps.Durable {
		return fmt.Errorf("%w: durable subscription %s", ErrUnsupported, sub.ID)
	}

	if sub.Mode == Broadcast && sub.Durable && sub.BroadcastID == "" {
		return errors.New("conduit: durable broadcast requires a stable broadcast_id")
	}

	if sub.StartAt != "all" && sub.StartAt != "new" && sub.StartAt != "sequence" {
		return errors.New("conduit: start_at must be all, new or sequence")
	}

	if sub.StartAt == "sequence" && (sub.StartSequence == 0 || !caps.Replay) {
		return fmt.Errorf("%w: replay sequence", ErrUnsupported)
	}

	if !caps.DeadLetters {
		return fmt.Errorf("%w: persisted dead letters", ErrUnsupported)
	}

	return nil
}

// Start connects providers, reconciles topology and then launches handlers.
func (r *Runtime) Start(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.running || r.stopping || r.closed {
		return fmt.Errorf("%w: runtime already started", ErrConflict)
	}

	life, cancel := context.WithCancel(context.WithoutCancel(ctx))
	observeCtx, observerCancel := context.WithCancel(context.WithoutCancel(ctx))
	r.cancel, r.observerCancel = cancel, observerCancel
	r.observers = &observerPool{queue: make(chan HookEvent, r.config.ObserverBuffer)}

	r.observerWorkers.Add(1)
	go r.observe(observeCtx)

	r.emit(ctx, HookEvent{Stage: Starting})

	var connected []Provider

	rollback := func() {
		cancel()

		for _, sub := range r.subscriptions {
			_ = sub.Close(ctx)
		}

		for _, provider := range connected {
			_ = provider.Close(ctx)
		}

		r.subscriptions = nil

		observerCancel()

		r.closed = true
	}

	for name, provider := range r.providers {
		if err := provider.Connect(ctx); err != nil {
			rollback()

			return fmt.Errorf("conduit: connect %s: %w", name, err)
		}

		connected = append(connected, provider)

		r.emit(ctx, HookEvent{Stage: ProviderConnected, Provider: name})
	}

	for _, stream := range r.config.Streams {
		if err := r.providers[stream.Provider].EnsureStream(ctx, r.config.Identity.Namespace, stream); err != nil {
			rollback()

			return err
		}
	}

	ids := make([]string, 0, len(r.registrations))
	for id := range r.registrations {
		ids = append(ids, id)
	}

	slices.Sort(ids)

	for _, id := range ids {
		reg := r.registrations[id]
		stream := r.config.Streams[reg.config.Stream]
		binding := Binding{Identity: r.config.Identity, Subscription: reg.config, Stream: stream}

		sub, err := r.providers[stream.Provider].Subscribe(ctx, binding)
		if err != nil {
			rollback()

			return fmt.Errorf("conduit: subscribe %s: %w", id, err)
		}

		r.subscriptions = append(r.subscriptions, sub)
		info := DeliveryInfo{ConsumerID: binding.ConsumerID(), SubscriptionID: id, Destination: binding.Identity, Stream: stream.Name, Mode: reg.config.Mode}
		r.emit(ctx, HookEvent{Stage: SubscriptionStarted, Delivery: &info})
	}

	if r.registry != nil {
		if err := r.registry.Register(ctx, Instance{Identity: r.config.Identity, Version: r.config.Version, Endpoints: r.config.Endpoints, Ready: true}); err != nil {
			rollback()

			return err
		}

		r.workers.Add(1)
		go r.renewRegistration(life)
	}

	r.running = true
	r.startedAt = time.Now().UTC()
	// Create workers only after every provider and subscription is ready.
	index := 0

	for _, id := range ids {
		reg := r.registrations[id]
		sub := r.subscriptions[index]
		index++

		for range reg.config.Concurrency {
			r.workers.Add(1)
			go r.consume(life, sub, reg)
		}
	}

	r.emit(ctx, HookEvent{Stage: Ready})

	return nil
}

// Stop cancels intake and waits for handlers before disconnecting providers.
func (r *Runtime) Stop(ctx context.Context) error {
	r.mu.Lock()
	if !r.running && !r.stopping {
		r.mu.Unlock()

		return nil
	}

	r.running = false
	r.stopping = true
	r.emit(ctx, HookEvent{Stage: Draining})
	r.cancel()
	subs := slices.Clone(r.subscriptions)
	r.mu.Unlock()

	for _, sub := range subs {
		_ = sub.Close(ctx)
	}

	done := make(chan struct{})

	go func() { r.workers.Wait(); close(done) }()

	select {
	case <-done:
	case <-ctx.Done():
		return ctx.Err()
	}

	var stopErr error
	if r.registry != nil {
		stopErr = r.registry.Deregister(ctx, r.config.Identity)
	}

	for _, provider := range r.providers {
		stopErr = errors.Join(stopErr, provider.Close(ctx))
	}

	r.emit(ctx, HookEvent{Stage: Stopped})
	r.observerCancel()

	observerDone := make(chan struct{})

	go func() { r.observerWorkers.Wait(); close(observerDone) }()

	select {
	case <-observerDone:
	case <-ctx.Done():
		return ctx.Err()
	}

	r.mu.Lock()
	r.closed, r.stopping = true, false
	r.mu.Unlock()

	return stopErr
}

func (r *Runtime) renewRegistration(ctx context.Context) {
	defer r.workers.Done()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			updateCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			err := r.registry.Register(updateCtx, Instance{Identity: r.config.Identity, Version: r.config.Version, Endpoints: r.config.Endpoints, Ready: true})

			cancel()

			if err != nil {
				r.emit(ctx, HookEvent{Stage: SubscriptionFailed, Error: "service registration renewal failed"})
			}
		}
	}
}

// Health checks provider availability and startup state.
func (r *Runtime) Health(ctx context.Context) error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if !r.running {
		return ErrNotRunning
	}

	for name, provider := range r.providers {
		if err := provider.Health(ctx); err != nil {
			return fmt.Errorf("conduit: provider %s unavailable: %w", name, err)
		}
	}

	return nil
}

// Prepare enriches and validates an immutable draft before publication or transactional enqueue.
func (r *Runtime) Prepare(ctx context.Context, streamName string, draft Envelope, validate func(Envelope) error) (Envelope, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if !r.running {
		return Envelope{}, ErrNotRunning
	}

	stream, ok := r.config.Streams[streamName]
	if !ok {
		return Envelope{}, fmt.Errorf("%w: stream %s", ErrNotFound, streamName)
	}

	msg := draft.Clone()
	if msg.TargetConsumer != "" {
		return Envelope{}, errors.New("conduit: recovery targeting requires ReplayDeadLetter")
	}

	if msg.ID == "" {
		msg.ID = NewID()
	}

	msg.Source = r.config.Identity
	if msg.CreatedAt.IsZero() {
		msg.CreatedAt = time.Now().UTC()
	}

	id, source, kind := msg.ID, msg.Source, msg.Type

	for _, hook := range r.hooks {
		if control, exists := hook.(PublishHook); exists {
			if err := protect(func() error { return control.BeforePublish(ctx, &msg) }); err != nil {
				return Envelope{}, err
			}
		}
	}

	if msg.ID != id || msg.Source != source || msg.Type != kind || msg.TargetConsumer != "" {
		return Envelope{}, errors.New("conduit: hooks cannot change message or source identity")
	}

	if err := ValidateMessageID(msg.ID); err != nil {
		return Envelope{}, err
	}

	if len(msg.Data) > r.config.MaxPayloadBytes {
		return Envelope{}, errors.New("conduit: payload exceeds configured limit")
	}

	if err := ValidateTopic(msg.Type, false); err != nil {
		return Envelope{}, err
	}

	matched := false
	for _, subject := range stream.Subjects {
		matched = matched || Matches(subject, msg.Type)
	}

	if !matched {
		return Envelope{}, errors.New("conduit: message does not match stream subjects")
	}

	if validate != nil {
		if err := validate(msg); err != nil {
			return Envelope{}, err
		}
	}

	return msg, nil
}

// Publish prepares a draft and waits for provider acceptance.
func (r *Runtime) Publish(ctx context.Context, streamName string, draft Envelope, validate func(Envelope) error) (Receipt, error) {
	msg, err := r.Prepare(ctx, streamName, draft, validate)
	if err != nil {
		return Receipt{}, err
	}

	return r.Send(ctx, streamName, msg)
}

// Send publishes an already prepared envelope, preserving its ID across outbox retries.
func (r *Runtime) Send(ctx context.Context, streamName string, msg Envelope) (Receipt, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if !r.running {
		return Receipt{}, ErrNotRunning
	}

	stream, ok := r.config.Streams[streamName]
	if !ok {
		return Receipt{}, ErrNotFound
	}

	if msg.ID == "" || msg.Source.Namespace != r.config.Identity.Namespace || msg.Source.ServiceID != r.config.Identity.ServiceID || msg.TargetConsumer != "" {
		return Receipt{}, ErrConflict
	}

	if err := ValidateMessageID(msg.ID); err != nil {
		return Receipt{}, err
	}

	if err := ValidateTopic(msg.Type, false); err != nil {
		return Receipt{}, err
	}

	matched := false
	for _, subject := range stream.Subjects {
		matched = matched || Matches(subject, msg.Type)
	}

	if !matched || len(msg.Data) > r.config.MaxPayloadBytes {
		return Receipt{}, ErrConflict
	}

	source := msg.Source
	r.emit(ctx, HookEvent{Stage: Publishing, Provider: stream.Provider, Message: &msg})

	receipt, err := r.providers[stream.Provider].Publish(ctx, source.Namespace, stream, msg)
	if err != nil {
		r.emit(ctx, HookEvent{Stage: PublishFailed, Provider: stream.Provider, Message: &msg, Error: err.Error()})

		return receipt, err
	}

	r.published.Add(1)
	r.emit(ctx, HookEvent{Stage: Published, Provider: stream.Provider, Message: &msg, Receipt: &receipt})

	if receipt.Duplicate {
		r.emit(ctx, HookEvent{Stage: Duplicate, Provider: stream.Provider, Message: &msg, Receipt: &receipt})
	}

	return receipt, nil
}

func (r *Runtime) consume(ctx context.Context, sub Subscription, reg registration) {
	defer r.workers.Done()

	handler := reg.handler

	for {
		delivery, err := sub.Next(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}

			r.emit(ctx, HookEvent{Stage: SubscriptionFailed, Error: err.Error()})

			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
				continue
			}
		}

		r.process(ctx, delivery, reg.config, handler)
	}
}

func (r *Runtime) process(ctx context.Context, delivery Delivery, cfg SubscriptionConfig, handler Handler) {
	msg, info := delivery.Message(), delivery.Info()
	r.emit(ctx, HookEvent{Stage: Received, Message: &msg, Delivery: &info})

	handleCtx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()

	err := protect(func() error {
		if msg.Type != cfg.MessageType {
			return Permanent(errors.New("conduit: envelope type differs from subscription"))
		}

		for _, hook := range r.hooks {
			if control, ok := hook.(HandleHook); ok {
				if hookErr := control.BeforeHandle(handleCtx, msg.Clone(), info); hookErr != nil {
					return hookErr
				}
			}
		}

		r.emit(handleCtx, HookEvent{Stage: Handling, Message: &msg, Delivery: &info})

		return handler(handleCtx, msg.Clone(), info)
	})
	if err == nil && handleCtx.Err() != nil {
		err = handleCtx.Err()
	}

	if err == nil {
		r.handled.Add(1)
		r.emit(ctx, HookEvent{Stage: Handled, Message: &msg, Delivery: &info})

		if ackErr := delivery.Ack(ctx); ackErr != nil {
			r.emit(ctx, HookEvent{Stage: SettlementFailed, Message: &msg, Delivery: &info, Error: ackErr.Error()})
		} else {
			r.acknowledged.Add(1)
			r.emit(ctx, HookEvent{Stage: Acknowledged, Message: &msg, Delivery: &info})
		}

		return
	}

	r.failed.Add(1)
	r.emit(ctx, HookEvent{Stage: HandleFailed, Message: &msg, Delivery: &info, Error: err.Error()})

	if ctx.Err() != nil {
		return
	}

	if !IsPermanent(err) && info.Attempt < uint64(cfg.MaxAttempts) { //nolint:gosec // MaxAttempts is validated positive.
		delay := cfg.RetryDelay * time.Duration(min(info.Attempt, 60)) //nolint:gosec // Capped at 60.
		if retryErr := delivery.Retry(ctx, delay); retryErr != nil {
			r.emit(ctx, HookEvent{Stage: SettlementFailed, Delivery: &info, Error: retryErr.Error()})

			return
		}

		r.retried.Add(1)
		r.emit(ctx, HookEvent{Stage: RetryScheduled, Delivery: &info, Message: &msg})

		return
	}

	r.emit(ctx, HookEvent{Stage: RetryExhausted, Delivery: &info, Message: &msg})
	stream := r.config.Streams[cfg.Stream]

	management, ok := r.providers[stream.Provider].(Management)
	if !ok {
		r.emit(ctx, HookEvent{Stage: SettlementFailed, Error: ErrUnsupported.Error()})

		return
	}

	letter := DeadLetter{ID: info.ConsumerID + "_" + msg.ID, Message: msg, Delivery: info, FailedAt: time.Now().UTC(), Reason: err.Error()}
	if storeErr := management.StoreDeadLetter(ctx, r.config.Identity.Namespace, letter); storeErr != nil {
		r.emit(ctx, HookEvent{Stage: SettlementFailed, Message: &msg, Delivery: &info, Error: storeErr.Error()})
		_ = delivery.Retry(ctx, cfg.RetryDelay)

		return
	}

	r.deadLettered.Add(1)
	r.emit(ctx, HookEvent{Stage: DeadLettered, Message: &msg, Delivery: &info})

	if rejectErr := delivery.Reject(ctx); rejectErr != nil {
		r.emit(ctx, HookEvent{Stage: SettlementFailed, Error: rejectErr.Error(), Delivery: &info})
	}
}

// ProviderInfo omits connection strings and credentials from dashboard responses.
type ProviderInfo struct {
	Name         string       `json:"name"`
	Type         string       `json:"type"`
	Capabilities Capabilities `json:"capabilities"`
	Healthy      bool         `json:"healthy"`
}

// Snapshot contains per-instance counters and broker stream state.
type Snapshot struct {
	Identity      Identity             `json:"identity"`
	Running       bool                 `json:"running"`
	StartedAt     time.Time            `json:"startedAt"`
	Providers     []ProviderInfo       `json:"providers"`
	Streams       []StreamInfo         `json:"streams"`
	Subscriptions []SubscriptionConfig `json:"subscriptions"`
	Published     uint64               `json:"published"`
	Handled       uint64               `json:"handled"`
	Acknowledged  uint64               `json:"acknowledged"`
	Failed        uint64               `json:"failed"`
	Retried       uint64               `json:"retried"`
	DeadLettered  uint64               `json:"deadLettered"`
	ObserverDrops uint64               `json:"observerDrops"`
}

// Snapshot reports unavailable inspection as an error, never an empty healthy list.
func (r *Runtime) Snapshot(ctx context.Context) (Snapshot, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	snapshot := Snapshot{Identity: r.config.Identity, Running: r.running, StartedAt: r.startedAt, Providers: []ProviderInfo{}, Streams: []StreamInfo{}, Subscriptions: []SubscriptionConfig{}, Published: r.published.Load(), Handled: r.handled.Load(), Acknowledged: r.acknowledged.Load(), Failed: r.failed.Load(), Retried: r.retried.Load(), DeadLettered: r.deadLettered.Load()}
	if r.observers != nil {
		snapshot.ObserverDrops = r.observers.dropped.Load()
	}

	for name, provider := range r.providers {
		snapshot.Providers = append(snapshot.Providers, ProviderInfo{Name: name, Type: provider.Name(), Capabilities: provider.Capabilities(), Healthy: r.running && provider.Health(ctx) == nil})
	}

	for _, stream := range r.config.Streams {
		management, ok := r.providers[stream.Provider].(Management)
		if !ok {
			return Snapshot{}, ErrUnsupported
		}

		if !r.running {
			stream.Subjects = slices.Clone(stream.Subjects)
			snapshot.Streams = append(snapshot.Streams, StreamInfo{Config: stream})

			continue
		}

		info, err := management.Inspect(ctx, r.config.Identity.Namespace, stream)
		if err != nil {
			return Snapshot{}, err
		}

		info.Config.Subjects = slices.Clone(info.Config.Subjects)
		snapshot.Streams = append(snapshot.Streams, info)
	}

	for _, sub := range r.config.Subscriptions {
		snapshot.Subscriptions = append(snapshot.Subscriptions, sub)
	}

	slices.SortFunc(snapshot.Providers, func(a, b ProviderInfo) int { return strings.Compare(a.Name, b.Name) })
	slices.SortFunc(snapshot.Streams, func(a, b StreamInfo) int { return strings.Compare(a.Config.Name, b.Config.Name) })
	slices.SortFunc(snapshot.Subscriptions, func(a, b SubscriptionConfig) int { return strings.Compare(a.ID, b.ID) })

	return snapshot, nil
}

// DeadLetters reads only failures owned by this logical service.
func (r *Runtime) DeadLetters(ctx context.Context, providerName, subscription, cursor string, limit int) ([]DeadLetter, string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if !r.running {
		return nil, "", ErrNotRunning
	}

	provider := r.providers[providerName]

	management, ok := provider.(Management)
	if !ok {
		return nil, "", ErrUnsupported
	}

	return management.ListDeadLetters(ctx, r.config.Identity, subscription, cursor, min(max(limit, 1), 100))
}

// ReplayDeadLetter republishes one retained failure without changing its message ID.
func (r *Runtime) ReplayDeadLetter(ctx context.Context, providerName, subscription, id string) (Receipt, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if !r.running {
		return Receipt{}, ErrNotRunning
	}

	management, ok := r.providers[providerName].(Management)
	if !ok {
		return Receipt{}, ErrUnsupported
	}

	receipt, err := management.ReplayDeadLetter(ctx, r.config.Identity, subscription, id)
	if err == nil {
		r.emit(ctx, HookEvent{Stage: Replayed, Provider: providerName, Receipt: &receipt})
	}

	return receipt, err
}
