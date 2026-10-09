// Package core defines provider contracts and the service communication runtime.
package core

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"strings"
	"time"
)

var (
	ErrNotRunning     = errors.New("conduit: runtime not running")
	ErrUnsupported    = errors.New("conduit: provider does not support requested capability")
	ErrOutcomeUnknown = errors.New("conduit: publish outcome unknown")
	ErrNotFound       = errors.New("conduit: resource not found")
	ErrConflict       = errors.New("conduit: configuration conflict")
)

// Identity separates a logical service from a running replica.
type Identity struct {
	Namespace  string `json:"namespace"  yaml:"namespace"`
	ServiceID  string `json:"serviceID"  yaml:"service_id"`
	InstanceID string `json:"instanceID" yaml:"instance_id"`
}

// Validate checks that every identity component is present.
func (i Identity) Validate() error {
	for _, value := range []string{i.Namespace, i.ServiceID, i.InstanceID} {
		if strings.TrimSpace(value) != value || value == "" || len(value) > 128 {
			return errors.New("conduit: namespace, service ID and instance ID must contain 1 to 128 characters")
		}

		if strings.ContainsFunc(value, func(r rune) bool { return r < 32 || r == 127 }) {
			return errors.New("conduit: identity contains control characters")
		}
	}

	return nil
}

// ValidateMessageID accepts stable IDs that can also address persisted recovery records.
func ValidateMessageID(id string) error {
	if len(id) == 0 || len(id) > 128 || strings.Contains(id, ".") {
		return errors.New("conduit: message ID must contain 1 to 128 letters, digits, underscores or hyphens")
	}

	return ValidateTopic(id, false)
}

// NewID generates an identifier independent of broker sequence numbers.
func NewID() string {
	var id [16]byte

	_, _ = rand.Read(id[:])

	return hex.EncodeToString(id[:])
}

// Envelope is immutable after broker acceptance. ID survives redelivery.
type Envelope struct {
	ID            string            `json:"id"`
	Type          string            `json:"type"`
	Source        Identity          `json:"source"`
	ContentType   string            `json:"contentType"`
	Data          json.RawMessage   `json:"data"`
	CreatedAt     time.Time         `json:"createdAt"`
	Key           string            `json:"key,omitempty"`
	CorrelationID string            `json:"correlationID,omitempty"`
	CausationID   string            `json:"causationID,omitempty"`
	Headers       map[string]string `json:"headers,omitempty"`
	// TargetConsumer directs recovery to the original subscription.
	TargetConsumer string `json:"targetConsumer,omitempty"`
}

// Clone prevents providers and observer hooks from sharing mutable payloads.
func (e Envelope) Clone() Envelope {
	e.Data = append(json.RawMessage(nil), e.Data...)

	headers := make(map[string]string, len(e.Headers))
	maps.Copy(headers, e.Headers)

	e.Headers = headers

	return e
}

// DeliveryMode controls replica distribution for one subscription.
type DeliveryMode string

const (
	Competing DeliveryMode = "competing"
	Broadcast DeliveryMode = "broadcast"
)

// StreamConfig describes retained messages, independently of consumer progress.
type StreamConfig struct {
	Name        string        `json:"name"        yaml:"name"`
	Provider    string        `json:"provider"    yaml:"provider"`
	Subjects    []string      `json:"subjects"    yaml:"subjects"`
	MaxAge      time.Duration `json:"maxAge"      yaml:"max_age"`
	MaxMessages int64         `json:"maxMessages" yaml:"max_messages"`
	Replicas    int           `json:"replicas"    yaml:"replicas"`
}

// SubscriptionConfig belongs to a logical service, with per-replica worker limits.
type SubscriptionConfig struct {
	ID            string        `json:"id"                      yaml:"id"`
	Stream        string        `json:"stream"                  yaml:"stream"`
	MessageType   string        `json:"messageType"             yaml:"message_type"`
	Mode          DeliveryMode  `json:"mode"                    yaml:"delivery"`
	Durable       bool          `json:"durable"                 yaml:"durable"`
	Concurrency   int           `json:"concurrency"             yaml:"concurrency"`
	MaxInFlight   int           `json:"maxInFlight"             yaml:"max_in_flight"`
	Timeout       time.Duration `json:"timeout"                 yaml:"timeout"`
	MaxAttempts   int           `json:"maxAttempts"             yaml:"max_attempts"`
	RetryDelay    time.Duration `json:"retryDelay"              yaml:"retry_delay"`
	StartAt       string        `json:"startAt"                 yaml:"start_at"`
	StartSequence uint64        `json:"startSequence,omitempty" yaml:"start_sequence"`
	// BroadcastID pins a durable broadcast subscriber across process restarts.
	BroadcastID string `json:"broadcastID,omitempty" yaml:"broadcast_id"`
}

// Binding adds the running member without changing logical consumer identity.
type Binding struct {
	Identity     Identity
	Subscription SubscriptionConfig
	Stream       StreamConfig
}

// ConsumerID excludes the instance for competing consumers.
func (b Binding) ConsumerID() string {
	parts := []string{b.Identity.Namespace, b.Identity.ServiceID, b.Subscription.ID}
	if b.Subscription.Mode == Broadcast {
		member := b.Subscription.BroadcastID
		if member == "" {
			member = b.Identity.InstanceID
		}

		parts = append(parts, member)
	}

	value, err := json.Marshal(parts)
	if err != nil {
		panic(err)
	}

	sum := sha256.Sum256(value)

	return "c_" + hex.EncodeToString(sum[:16])
}

// Capabilities report guarantees implemented by a provider.
type Capabilities struct {
	ConsumerControls bool `json:"consumerControls"`
	Backfill         bool `json:"backfill"`
	RPC              bool `json:"rpc"`
	Durable          bool `json:"durable"`
	Replay           bool `json:"replay"`
	DeadLetters      bool `json:"deadLetters"`
	KeyOrdering      bool `json:"keyOrdering"`
}

// Receipt distinguishes broker acceptance from processing success.
type Receipt struct {
	MessageID string `json:"messageID"`
	Sequence  uint64 `json:"sequence"`
	Persisted bool   `json:"persisted"`
	Duplicate bool   `json:"duplicate"`
}

// DeliveryInfo identifies this attempt independently of its original message.
type DeliveryInfo struct {
	Stream         string       `json:"stream"`
	ConsumerID     string       `json:"consumerID"`
	SubscriptionID string       `json:"subscriptionID"`
	Destination    Identity     `json:"destination"`
	Mode           DeliveryMode `json:"mode"`
	Attempt        uint64       `json:"attempt"`
	Sequence       uint64       `json:"sequence"`
}

// Delivery owns settlement for one broker delivery.
type Delivery interface {
	Message() Envelope
	Info() DeliveryInfo
	Ack(ctx context.Context) error
	Retry(ctx context.Context, delay time.Duration) error
	Reject(ctx context.Context) error
}

// Subscription supplies deliveries to a bounded pool of workers.
type Subscription interface {
	Next(ctx context.Context) (Delivery, error)
	Close(ctx context.Context) error
}

// Provider implements retained messaging. Optional management has its own interface.
type Provider interface {
	Name() string
	Capabilities() Capabilities
	Connect(ctx context.Context) error
	Close(ctx context.Context) error
	Health(ctx context.Context) error
	EnsureStream(ctx context.Context, namespace string, config StreamConfig) error
	Publish(ctx context.Context, namespace string, config StreamConfig, message Envelope) (Receipt, error)
	Subscribe(ctx context.Context, binding Binding) (Subscription, error)
}

// DeadLetter retains the failure and the original message for controlled recovery.
type DeadLetter struct {
	ID       string       `json:"id"`
	Message  Envelope     `json:"message"`
	Delivery DeliveryInfo `json:"delivery"`
	FailedAt time.Time    `json:"failedAt"`
	Reason   string       `json:"reason"`
	Replayed bool         `json:"replayed"`
}

// StreamInfo is a provider's current retained-message state.
type StreamInfo struct {
	Config    StreamConfig `json:"config"`
	Messages  uint64       `json:"messages"`
	Bytes     uint64       `json:"bytes"`
	Consumers int          `json:"consumers"`
}

// Management exposes persisted failures and stream inspection without fake fallbacks.
type Management interface {
	Inspect(ctx context.Context, namespace string, config StreamConfig) (StreamInfo, error)
	StoreDeadLetter(ctx context.Context, namespace string, letter DeadLetter) error
	ListDeadLetters(ctx context.Context, identity Identity, subscription string, cursor string, limit int) ([]DeadLetter, string, error)
	ReplayDeadLetter(ctx context.Context, identity Identity, subscription string, id string) (Receipt, error)
}

// Handler processes one immutable envelope and its delivery context.
type Handler func(context.Context, Envelope, DeliveryInfo) error

// Middleware surrounds each processing attempt.
type Middleware func(Handler) Handler

type permanentError struct{ error }

// Permanent routes an invalid or terminal message to the dead letter store.
func Permanent(err error) error {
	if err == nil {
		return nil
	}

	return permanentError{err}
}

// IsPermanent reports whether an error explicitly ends retries.
func IsPermanent(err error) bool {
	var target permanentError

	return errors.As(err, &target)
}

// ValidateTopic accepts exact event types and a trailing wildcard in stream subjects.
func ValidateTopic(topic string, wildcard bool) error {
	if topic == "" || len(topic) > 200 {
		return errors.New("conduit: invalid event type")
	}

	parts := strings.Split(topic, ".")
	for index, part := range parts {
		if wildcard && (part == "*" || (part == ">" && index == len(parts)-1)) {
			continue
		}

		if part == "" {
			return fmt.Errorf("conduit: invalid event type %q", topic)
		}

		for _, ch := range part {
			if (ch < 'a' || ch > 'z') && (ch < 'A' || ch > 'Z') && (ch < '0' || ch > '9') && ch != '_' && ch != '-' {
				return fmt.Errorf("conduit: invalid event type %q", topic)
			}
		}
	}

	return nil
}

// Matches compares an event type with a stream subject pattern.
func Matches(pattern, topic string) bool {
	p, t := strings.Split(pattern, "."), strings.Split(topic, ".")
	for i, part := range p {
		if part == ">" {
			return i < len(t)
		}

		if i >= len(t) || (part != "*" && part != t[i]) {
			return false
		}
	}

	return len(p) == len(t)
}
