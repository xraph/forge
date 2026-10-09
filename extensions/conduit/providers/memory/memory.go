// Package memory provides a shared development broker without disk durability.
package memory

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/xraph/forge/extensions/conduit/core"
)

type record struct {
	sequence   uint64
	message    core.Envelope
	acceptedAt time.Time
}
type stream struct {
	config    core.StreamConfig
	records   []record
	sequence  uint64
	consumers map[string]*consumer
}
type pending struct {
	attempt   uint64
	available time.Time
	token     string
}
type consumer struct {
	binding core.Binding
	cursor  uint64
	pending map[uint64]*pending
}

// Broker can be shared by several development instances.
type Broker struct {
	mu      sync.Mutex
	streams map[string]*stream
	letters map[string]core.DeadLetter
}

// New creates a broker whose state lasts for this process only.
func New() *Broker {
	return &Broker{streams: make(map[string]*stream), letters: make(map[string]core.DeadLetter)}
}

// Name returns the provider type.
func (b *Broker) Name() string { return "memory" }

// Capabilities explicitly exclude disk durability and key ordering.
func (b *Broker) Capabilities() core.Capabilities {
	return core.Capabilities{Replay: true, DeadLetters: true}
}

// Connect needs no external connection.
func (b *Broker) Connect(context.Context) error { return nil }

// Close leaves the shared development broker available to other instances.
func (b *Broker) Close(context.Context) error { return nil }

// Health checks the local broker.
func (b *Broker) Health(context.Context) error { return nil }

func streamKey(namespace, name string) string { return namespace + "\x00" + name }

// EnsureStream refuses incompatible declarations by different instances.
func (b *Broker) EnsureStream(_ context.Context, namespace string, cfg core.StreamConfig) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	key := streamKey(namespace, cfg.Name)
	if existing := b.streams[key]; existing != nil {
		if !reflect.DeepEqual(existing.config, cfg) {
			return fmt.Errorf("%w: stream %s", core.ErrConflict, cfg.Name)
		}

		return nil
	}

	if cfg.Replicas > 1 {
		return fmt.Errorf("%w: memory replication", core.ErrUnsupported)
	}

	b.streams[key] = &stream{config: cfg, consumers: make(map[string]*consumer)}

	return nil
}

func (b *Broker) publish(namespace string, cfg core.StreamConfig, msg core.Envelope) (core.Receipt, error) {
	s := b.streams[streamKey(namespace, cfg.Name)]
	if s == nil {
		return core.Receipt{}, core.ErrNotFound
	}

	s.sequence++

	s.records = append(s.records, record{sequence: s.sequence, message: msg.Clone(), acceptedAt: time.Now()})
	s.prune(time.Now())

	return core.Receipt{MessageID: msg.ID, Sequence: s.sequence}, nil
}

func (s *stream) prune(now time.Time) {
	start := 0
	if s.config.MaxAge > 0 {
		for start < len(s.records) && now.Sub(s.records[start].acceptedAt) >= s.config.MaxAge {
			start++
		}
	}

	if s.config.MaxMessages > 0 && int64(len(s.records)-start) > s.config.MaxMessages {
		start = len(s.records) - int(s.config.MaxMessages)
	}

	if start == 0 {
		return
	}

	s.records = slices.Clone(s.records[start:])

	first := s.sequence + 1
	if len(s.records) > 0 {
		first = s.records[0].sequence
	}

	for _, consumer := range s.consumers {
		for sequence := range consumer.pending {
			if sequence < first {
				delete(consumer.pending, sequence)
			}
		}
	}
}

// Publish accepts a message into process memory.
func (b *Broker) Publish(ctx context.Context, namespace string, cfg core.StreamConfig, msg core.Envelope) (core.Receipt, error) {
	if err := ctx.Err(); err != nil {
		return core.Receipt{}, err
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	return b.publish(namespace, cfg, msg)
}

type subscription struct {
	broker  *Broker
	binding core.Binding
	closed  chan struct{}
	once    sync.Once
}

// Subscribe attaches a worker to the logical consumer, or creates a broadcast cursor.
func (b *Broker) Subscribe(_ context.Context, binding core.Binding) (core.Subscription, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	s := b.streams[streamKey(binding.Identity.Namespace, binding.Stream.Name)]
	if s == nil {
		return nil, core.ErrNotFound
	}

	id := binding.ConsumerID()
	if existing := s.consumers[id]; existing != nil {
		shared, other := existing.binding.Subscription, binding.Subscription

		shared.Concurrency, other.Concurrency = 0, 0
		if shared.Mode == core.Competing {
			shared.BroadcastID, other.BroadcastID = "", ""
		}

		if !reflect.DeepEqual(shared, other) {
			return nil, fmt.Errorf("%w: consumer %s", core.ErrConflict, id)
		}
	} else {
		cursor := uint64(1)
		if binding.Subscription.StartAt == "new" {
			cursor = s.sequence + 1
		}

		if binding.Subscription.StartAt == "sequence" {
			cursor = binding.Subscription.StartSequence
		}

		s.consumers[id] = &consumer{binding: binding, cursor: cursor, pending: make(map[uint64]*pending)}
	}

	return &subscription{broker: b, binding: binding, closed: make(chan struct{})}, nil
}

func (s *subscription) Next(ctx context.Context) (core.Delivery, error) {
	for {
		select {
		case <-s.closed:
			return nil, context.Canceled
		default:
		}

		if err := ctx.Err(); err != nil {
			return nil, err
		}

		s.broker.mu.Lock()
		st := s.broker.streams[streamKey(s.binding.Identity.Namespace, s.binding.Stream.Name)]
		c := st.consumers[s.binding.ConsumerID()]
		now := time.Now()
		st.prune(now)

		for _, row := range st.records {
			p := c.pending[row.sequence]

			if p == nil && row.sequence < c.cursor {
				continue
			}

			if p != nil && p.available.After(now) {
				continue
			}

			if p == nil {
				c.cursor = row.sequence + 1
				if row.message.Type != s.binding.Subscription.MessageType || (row.message.TargetConsumer != "" && row.message.TargetConsumer != s.binding.ConsumerID()) {
					continue
				}

				if len(c.pending) >= s.binding.Subscription.MaxInFlight {
					c.cursor = row.sequence

					break
				}

				p = &pending{}
				c.pending[row.sequence] = p
			}

			p.attempt++
			p.token = core.NewID()
			p.available = now.Add(s.binding.Subscription.Timeout)
			d := &delivery{subscription: s, record: row, token: p.token, attempt: p.attempt}
			s.broker.mu.Unlock()

			return d, nil
		}
		s.broker.mu.Unlock()

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-s.closed:
			return nil, context.Canceled
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func (s *subscription) Close(context.Context) error {
	s.once.Do(func() { close(s.closed) })

	return nil
}

type delivery struct {
	subscription *subscription
	record       record
	token        string
	attempt      uint64
}

func (d *delivery) Message() core.Envelope { return d.record.message.Clone() }
func (d *delivery) Info() core.DeliveryInfo {
	b := d.subscription.binding

	return core.DeliveryInfo{Stream: b.Stream.Name, ConsumerID: b.ConsumerID(), SubscriptionID: b.Subscription.ID, Destination: b.Identity, Mode: b.Subscription.Mode, Attempt: d.attempt, Sequence: d.record.sequence}
}

func (d *delivery) settle(ctx context.Context, retry *time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	b := d.subscription.broker
	b.mu.Lock()
	defer b.mu.Unlock()

	c := b.streams[streamKey(d.subscription.binding.Identity.Namespace, d.subscription.binding.Stream.Name)].consumers[d.subscription.binding.ConsumerID()]

	p := c.pending[d.record.sequence]
	if p == nil || p.token != d.token {
		return fmt.Errorf("%w: stale delivery", core.ErrConflict)
	}

	if retry != nil {
		p.available = time.Now().Add(*retry)
		p.token = ""
	} else {
		delete(c.pending, d.record.sequence)
	}

	return nil
}

func (d *delivery) Ack(ctx context.Context) error    { return d.settle(ctx, nil) }
func (d *delivery) Reject(ctx context.Context) error { return d.settle(ctx, nil) }
func (d *delivery) Retry(ctx context.Context, delay time.Duration) error {
	return d.settle(ctx, &delay)
}

// Inspect reports current process-memory stream state.
func (b *Broker) Inspect(_ context.Context, namespace string, cfg core.StreamConfig) (core.StreamInfo, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	s := b.streams[streamKey(namespace, cfg.Name)]
	if s == nil {
		return core.StreamInfo{}, core.ErrNotFound
	}

	s.prune(time.Now())

	var bytes uint64
	for _, row := range s.records {
		bytes += uint64(len(row.message.Data))
	}

	return core.StreamInfo{Config: cfg, Messages: uint64(len(s.records)), Bytes: bytes, Consumers: len(s.consumers)}, nil
}

func cloneLetter(letter core.DeadLetter) core.DeadLetter {
	letter.Message = letter.Message.Clone()

	return letter
}

// StoreDeadLetter retains a failure before its original delivery is settled.
func (b *Broker) StoreDeadLetter(_ context.Context, namespace string, letter core.DeadLetter) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	key := namespace + "\x00" + letter.ID
	if _, exists := b.letters[key]; !exists {
		b.letters[key] = cloneLetter(letter)
	}

	return nil
}

// ListDeadLetters filters by service identity before cursor paging.
func (b *Broker) ListDeadLetters(_ context.Context, identity core.Identity, subscriptionID, cursor string, limit int) ([]core.DeadLetter, string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	letters := make([]core.DeadLetter, 0)
	for key, letter := range b.letters {
		if letter.ID > cursor && strings.HasPrefix(key, identity.Namespace+"\x00") && letter.Delivery.Destination.ServiceID == identity.ServiceID && (subscriptionID == "" || letter.Delivery.SubscriptionID == subscriptionID) {
			letters = append(letters, cloneLetter(letter))
		}
	}

	slices.SortFunc(letters, func(a, b core.DeadLetter) int { return strings.Compare(a.ID, b.ID) })

	next := ""

	if len(letters) > limit {
		letters = letters[:limit]
		next = letters[len(letters)-1].ID
	}

	return letters, next, nil
}

// ReplayDeadLetter atomically queues recovery for the original consumer in memory.
func (b *Broker) ReplayDeadLetter(_ context.Context, identity core.Identity, subscriptionID, id string) (core.Receipt, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	key := identity.Namespace + "\x00" + id

	letter, ok := b.letters[key]
	if !ok || letter.Delivery.Destination.ServiceID != identity.ServiceID || letter.Delivery.SubscriptionID != subscriptionID {
		return core.Receipt{}, core.ErrNotFound
	}

	if letter.Replayed {
		return core.Receipt{}, core.ErrConflict
	}

	var target *stream

	for _, st := range b.streams {
		if st.consumers[letter.Delivery.ConsumerID] != nil {
			target = st

			break
		}
	}

	if target == nil {
		return core.Receipt{}, core.ErrNotFound
	}

	msg := letter.Message.Clone()
	msg.TargetConsumer = letter.Delivery.ConsumerID

	receipt, err := b.publish(identity.Namespace, target.config, msg)
	if err == nil {
		letter.Replayed = true
		b.letters[key] = letter
	}

	return receipt, err
}

var _ core.Provider = (*Broker)(nil)
var _ core.Management = (*Broker)(nil)
