// Package conduit provides typed service communication with pluggable brokers.
package conduit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/xraph/forge/extensions/conduit/core"
	"github.com/xraph/forge/extensions/conduit/transport"
)

type (
	Identity           = core.Identity
	Config             = core.Config
	ConnectionConfig   = core.ConnectionConfig
	Runtime            = core.Runtime
	Option             = core.Option
	Envelope           = core.Envelope
	Receipt            = core.Receipt
	StreamConfig       = core.StreamConfig
	SubscriptionConfig = core.SubscriptionConfig
	DeliveryMode       = core.DeliveryMode
	DeliveryInfo       = core.DeliveryInfo
	Provider           = core.Provider
	Capabilities       = core.Capabilities
	Hook               = core.Hook
	HookFuncs          = core.HookFuncs
	HookEvent          = core.HookEvent
	Stage              = core.Stage
	Middleware         = core.Middleware
	Snapshot           = core.Snapshot
	DeadLetter         = core.DeadLetter
	Endpoint           = core.Endpoint
	Instance           = core.Instance
	Registry           = core.Registry
)

const (
	Competing           = core.Competing
	Broadcast           = core.Broadcast
	Starting            = core.Starting
	Ready               = core.Ready
	Draining            = core.Draining
	Stopped             = core.Stopped
	ProviderConnected   = core.ProviderConnected
	SubscriptionStarted = core.SubscriptionStarted
	SubscriptionFailed  = core.SubscriptionFailed
	Publishing          = core.Publishing
	Published           = core.Published
	PublishFailed       = core.PublishFailed
	Received            = core.Received
	Handling            = core.Handling
	Handled             = core.Handled
	HandleFailed        = core.HandleFailed
	Acknowledged        = core.Acknowledged
	SettlementFailed    = core.SettlementFailed
	RetryScheduled      = core.RetryScheduled
	RetryExhausted      = core.RetryExhausted
	DeadLettered        = core.DeadLettered
	Replayed            = core.Replayed
	Duplicate           = core.Duplicate
	RPCCalling          = core.RPCCalling
	RPCReceived         = core.RPCReceived
	RPCHandling         = core.RPCHandling
	RPCHandled          = core.RPCHandled
	RPCReturned         = core.RPCReturned
	RPCFailed           = core.RPCFailed
)

// Errors let callers distinguish rejected requests from unknown broker outcomes.
var (
	ErrNotRunning     = core.ErrNotRunning
	ErrNotFound       = core.ErrNotFound
	ErrConflict       = core.ErrConflict
	ErrUnsupported    = core.ErrUnsupported
	ErrOutcomeUnknown = core.ErrOutcomeUnknown
)

// NewID creates a stable publication ID you can retain for retries.
func NewID() string { return core.NewID() }

// New constructs a runtime independent of Forge for tests and standalone services.
func New(cfg Config, opts ...Option) (*Runtime, error) { return core.New(cfg, opts...) }

// WithConfig supplies optional explicit Forge configuration.
func WithConfig(cfg Config) Option { return core.WithConfig(cfg) }

// WithProvider registers a named broker connection.
func WithProvider(name string, provider Provider) Option { return core.WithProvider(name, provider) }

// WithHooks registers ordered control and observation callbacks.
func WithHooks(hooks ...Hook) Option { return core.WithHooks(hooks...) }

// WithRegistry enables leased service registration and named clients.
func WithRegistry(registry Registry) Option { return core.WithRegistry(registry) }

// Clients binds HTTP and generated gRPC clients to the runtime's discovery provider.
func Clients(r *Runtime) *transport.Services {
	return &transport.Services{ResolverFunc: r.Resolver, IdentityFunc: r.Identity}
}

// Permanent rejects processing without retrying a terminal error.
func Permanent(err error) error { return core.Permanent(err) }

// EventType carries a typed contract independently of its broker binding.
type EventType[T any] struct {
	Name     string
	Validate func(T) error
}

// Event declares a versioned event name. Include the version in the contract name.
func Event[T any](name string) EventType[T] { return EventType[T]{Name: name} }

// Message gives a handler both typed data and the original delivery identity.
type Message[T any] struct {
	Data     T
	Envelope Envelope
	Delivery DeliveryInfo
}

// PublishOption enriches a draft. Source identity is always set by the runtime.
type PublishOption func(*Envelope)

// MessageID lets a producer retry an unknown publish outcome with the same ID.
func MessageID(id string) PublishOption { return func(e *Envelope) { e.ID = id } }

// Key adds an application routing key without implying ordered processing.
func Key(key string) PublishOption { return func(e *Envelope) { e.Key = key } }

// Headers supplies correlation, tracing and application metadata.
func Headers(headers map[string]string) PublishOption {
	return func(e *Envelope) { e.Headers = headers }
}

// Correlation links messages to a request or operation.
func Correlation(id string) PublishOption { return func(e *Envelope) { e.CorrelationID = id } }

// Causation links a derived message to its triggering event.
func Causation(id string) PublishOption { return func(e *Envelope) { e.CausationID = id } }

// Publication is a validated envelope and its selected stream.
type Publication struct {
	Stream   string
	Envelope Envelope
}

// Prepare enriches and validates typed data for publication or a transactional outbox.
func Prepare[T any](ctx context.Context, r *Runtime, event EventType[T], data T, opts ...PublishOption) (Publication, error) {
	stream, err := r.StreamFor(event.Name)
	if err != nil {
		return Publication{}, err
	}

	payload, err := json.Marshal(data)
	if err != nil {
		return Publication{}, err
	}

	msg := Envelope{Type: event.Name, ContentType: "application/json", Data: payload}
	for _, opt := range opts {
		opt(&msg)
	}

	prepared, err := r.Prepare(ctx, stream.Name, msg, func(envelope Envelope) error {
		if envelope.ContentType != "application/json" {
			return errors.New("conduit: typed events require JSON content type")
		}

		var typed T
		if err := json.Unmarshal(envelope.Data, &typed); err != nil {
			return fmt.Errorf("conduit: invalid payload: %w", err)
		}

		if event.Validate != nil {
			return event.Validate(typed)
		}

		return nil
	})
	if err != nil {
		return Publication{}, err
	}

	return Publication{Stream: stream.Name, Envelope: prepared}, nil
}

// Publish waits for the routed broker's acceptance of typed data.
func Publish[T any](ctx context.Context, r *Runtime, event EventType[T], data T, opts ...PublishOption) (Receipt, error) {
	publication, err := Prepare(ctx, r, event, data, opts...)
	if err != nil {
		return Receipt{}, err
	}

	return r.Send(ctx, publication.Stream, publication.Envelope)
}

type subscribeOptions struct {
	consumer   string
	middleware []Middleware
}

// SubscribeOption binds a typed handler to a configured logical subscription.
type SubscribeOption func(*subscribeOptions)

// Consumer selects the stable subscription ID, shared by service replicas.
func Consumer(id string) SubscribeOption {
	return func(options *subscribeOptions) { options.consumer = id }
}

// AroundHandle installs middleware in outermost-first order.
func AroundHandle(middleware ...Middleware) SubscribeOption {
	return func(options *subscribeOptions) { options.middleware = append(options.middleware, middleware...) }
}

// Subscribe decodes and validates every attempt before calling the typed handler.
func Subscribe[T any](r *Runtime, event EventType[T], handler func(context.Context, Message[T]) error, opts ...SubscribeOption) error {
	if handler == nil {
		return errors.New("conduit: handler is required")
	}

	options := subscribeOptions{}
	for _, opt := range opts {
		opt(&options)
	}

	if options.consumer == "" {
		return errors.New("conduit: Consumer subscription ID is required")
	}

	return r.Bind(options.consumer, event.Name, func(ctx context.Context, envelope Envelope, info DeliveryInfo) error {
		if envelope.Headers["conduit.decodeError"] != "" {
			return core.Permanent(errors.New("conduit: malformed broker envelope"))
		}

		if envelope.ContentType != "application/json" {
			return core.Permanent(errors.New("conduit: unsupported content type"))
		}

		var data T
		if err := json.Unmarshal(envelope.Data, &data); err != nil {
			return core.Permanent(fmt.Errorf("conduit: decode payload: %w", err))
		}

		if event.Validate != nil {
			if err := event.Validate(data); err != nil {
				return core.Permanent(err)
			}
		}

		return handler(ctx, Message[T]{Data: data, Envelope: envelope, Delivery: info})
	}, options.middleware...)
}
