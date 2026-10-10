// Package kafka implements retained events with a single partition per Conduit stream.
package kafka

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"sync"
	"time"

	broker "github.com/segmentio/kafka-go"
	"github.com/xraph/forge/extensions/conduit/core"
)

// Options accepts native transport and dialer configuration for TLS and SASL.
// Every stream uses one partition, so only one competing replica is active at a time.
type Options struct {
	Brokers            []string
	Transport          *broker.Transport
	Dialer             *broker.Dialer
	DeadLetterReplicas int
}

// Provider owns a Kafka client and its consumer group memberships.
type Provider struct {
	mu            sync.RWMutex
	options       Options
	client        *broker.Client
	subscriptions map[*subscription]struct{}
	configs       map[string]core.StreamConfig
}

// New prepares a provider without starting connections.
func New(options Options) *Provider {
	if options.DeadLetterReplicas == 0 {
		options.DeadLetterReplicas = 1
	}

	return &Provider{options: options, subscriptions: map[*subscription]struct{}{}, configs: map[string]core.StreamConfig{}}
}

// Name returns the broker type.
func (p *Provider) Name() string { return "kafka" }

// Capabilities reports retained replay and dead letter recovery without key ordering or native RPC.
func (p *Provider) Capabilities() core.Capabilities {
	return core.Capabilities{Durable: true, Replay: true, DeadLetters: true}
}
func hash(parts ...string) string {
	data, err := json.Marshal(parts)
	if err != nil {
		panic(err)
	}

	sum := sha256.Sum256(data)

	return hex.EncodeToString(sum[:16])
}
func topicName(ns, stream string) string { return "fc_" + hash(ns, stream) }

// Connect checks broker metadata and keeps connection errors free of credentials.
func (p *Provider) Connect(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.client != nil {
		return core.ErrConflict
	}

	if len(p.options.Brokers) == 0 {
		return core.ErrConflict
	}

	transport := p.options.Transport
	if transport == nil {
		transport = &broker.Transport{ClientID: "forge-conduit"}
		if p.options.Dialer != nil {
			transport.TLS = p.options.Dialer.TLS
			transport.SASL = p.options.Dialer.SASLMechanism
		}
	}

	if p.options.Dialer == nil {
		p.options.Dialer = &broker.Dialer{Timeout: 5 * time.Second, TLS: transport.TLS, SASLMechanism: transport.SASL}
	}

	p.client = &broker.Client{Addr: broker.TCP(p.options.Brokers...), Transport: transport, Timeout: 5 * time.Second}
	if _, err := p.client.Metadata(ctx, &broker.MetadataRequest{}); err != nil {
		transport.CloseIdleConnections()

		p.client = nil

		return errors.New("conduit/kafka: connection failed")
	}

	return nil
}
func (p *Provider) getClient() (*broker.Client, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.client == nil {
		return nil, core.ErrNotRunning
	}

	return p.client, nil
}

// Close releases all memberships and idle broker connections.
func (p *Provider) Close(ctx context.Context) error {
	p.mu.Lock()
	client := p.client
	p.client = nil

	subs := make([]*subscription, 0, len(p.subscriptions))
	for s := range p.subscriptions {
		subs = append(subs, s)
	}
	p.mu.Unlock()

	var result error
	for _, s := range subs {
		result = errors.Join(result, s.Close(ctx))
	}

	if client != nil {
		if transport, ok := client.Transport.(*broker.Transport); ok {
			transport.CloseIdleConnections()
		}
	}

	return result
}

// Health fetches live cluster metadata.
func (p *Provider) Health(ctx context.Context) error {
	c, err := p.getClient()
	if err != nil {
		return err
	}

	_, err = c.Metadata(ctx, &broker.MetadataRequest{})
	if err != nil {
		return errors.New("conduit/kafka: health check failed")
	}

	return nil
}

func (p *Provider) createTopic(ctx context.Context, name string, replicas int, age time.Duration) (bool, error) {
	c, err := p.getClient()
	if err != nil {
		return false, err
	}

	retention := "-1"
	if age > 0 {
		retention = strconv.FormatInt(max(1, age.Milliseconds()), 10)
	}

	result, err := c.CreateTopics(ctx, &broker.CreateTopicsRequest{Topics: []broker.TopicConfig{{Topic: name, NumPartitions: 1, ReplicationFactor: replicas, ConfigEntries: []broker.ConfigEntry{{ConfigName: "cleanup.policy", ConfigValue: "delete"}, {ConfigName: "retention.ms", ConfigValue: retention}, {ConfigName: "retention.bytes", ConfigValue: "-1"}, {ConfigName: "min.insync.replicas", ConfigValue: strconv.Itoa(replicas)}}}}})
	if err != nil {
		return false, errors.New("conduit/kafka: topic creation failed")
	}

	if err := result.Errors[name]; err != nil && !errors.Is(err, broker.TopicAlreadyExists) {
		return false, errors.New("conduit/kafka: topic creation rejected")
	}

	created := result.Errors[name] == nil

	metadata, err := c.Metadata(ctx, &broker.MetadataRequest{Topics: []string{name}})
	if err != nil {
		return false, err
	}

	if len(metadata.Topics) != 1 || metadata.Topics[0].Error != nil || len(metadata.Topics[0].Partitions) != 1 || len(metadata.Topics[0].Partitions[0].Replicas) != replicas {
		return false, core.ErrConflict
	}

	config, err := c.DescribeConfigs(ctx, &broker.DescribeConfigsRequest{Resources: []broker.DescribeConfigRequestResource{{ResourceType: broker.ResourceTypeTopic, ResourceName: name}}})
	if err != nil {
		return false, err
	}

	if len(config.Resources) != 1 || config.Resources[0].Error != nil {
		return false, core.ErrConflict
	}

	expected := map[string]string{"cleanup.policy": "delete", "retention.ms": retention, "retention.bytes": "-1", "min.insync.replicas": strconv.Itoa(replicas)}
	for _, entry := range config.Resources[0].ConfigEntries {
		if want, ok := expected[entry.ConfigName]; ok {
			if want != entry.ConfigValue {
				return false, fmt.Errorf("%w: %s wanted %s got %s", core.ErrConflict, entry.ConfigName, want, entry.ConfigValue)
			}

			delete(expected, entry.ConfigName)
		}
	}

	if len(expected) != 0 {
		return false, fmt.Errorf("%w: missing %v", core.ErrConflict, expected)
	}

	return created, nil
}

func (p *Provider) produce(ctx context.Context, topic string, data []byte) (uint64, error) {
	c, err := p.getClient()
	if err != nil {
		return 0, err
	}

	result, err := c.Produce(ctx, &broker.ProduceRequest{Topic: topic, Partition: 0, RequiredAcks: broker.RequireAll, Records: broker.NewRecordReader(broker.Record{Value: broker.NewBytes(data), Time: time.Now()})})
	if err != nil {
		return 0, core.ErrOutcomeUnknown
	}

	if result.Error != nil {
		return 0, errors.New("conduit/kafka: publication rejected")
	}

	if result.BaseOffset < 0 {
		return 0, core.ErrOutcomeUnknown
	}

	return uint64(result.BaseOffset) + 1, nil
}
func (p *Provider) reader(topic string) *broker.Reader {
	return broker.NewReader(broker.ReaderConfig{Brokers: p.options.Brokers, Topic: topic, Partition: 0, Dialer: p.options.Dialer, MinBytes: 1, MaxBytes: 16 << 20, MaxWait: 250 * time.Millisecond, ReadBackoffMin: 50 * time.Millisecond, ReadBackoffMax: 250 * time.Millisecond})
}

// declaration uses the topic creator as the sole writer of immutable topology.
// If that creator crashes before publishing, initialization times out and fails closed.
func (p *Provider) declaration(ctx context.Context, name string, replicas int, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}

	created, err := p.createTopic(ctx, name, replicas, 0)
	if err != nil {
		return err
	}

	if created {
		_, err := p.produce(ctx, name, data)

		return err
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	reader := p.reader(name)
	defer func() { _ = reader.Close() }()

	msg, err := reader.ReadMessage(ctx)
	if err != nil {
		return errors.New("conduit/kafka: topology declaration unavailable")
	}

	if string(msg.Value) != string(data) {
		return core.ErrConflict
	}

	return nil
}

// EnsureStream verifies immutable declarations and topic replication/retention.
// Kafka retains by age and bytes; message-count retention is explicitly unsupported.
func (p *Provider) EnsureStream(ctx context.Context, ns string, cfg core.StreamConfig) error {
	if cfg.MaxMessages != 0 {
		return core.ErrUnsupported
	}

	cfg.Subjects = slices.Clone(cfg.Subjects)
	slices.Sort(cfg.Subjects)
	cfg.Provider = ""

	topic := topicName(ns, cfg.Name)
	if err := p.declaration(ctx, topic+"_config", cfg.Replicas, cfg); err != nil {
		return err
	}

	if _, err := p.createTopic(ctx, topic, cfg.Replicas, cfg.MaxAge); err != nil {
		return err
	}

	p.mu.Lock()
	p.configs[topic] = cfg
	p.mu.Unlock()

	return nil
}

// Publish waits for all configured in-sync replicas. Stable IDs need inbox deduplication.
func (p *Provider) Publish(ctx context.Context, ns string, cfg core.StreamConfig, msg core.Envelope) (core.Receipt, error) {
	data, err := json.Marshal(msg)
	if err != nil {
		return core.Receipt{}, err
	}

	sequence, err := p.produce(ctx, topicName(ns, cfg.Name), data)
	if err != nil {
		return core.Receipt{}, err
	}

	return core.Receipt{MessageID: msg.ID, Sequence: sequence, Persisted: true}, nil
}

// Subscription context owns group membership independently of intake cancellation.
type subscription struct {
	provider   *Provider
	binding    core.Binding
	topic      string
	group      *broker.ConsumerGroup
	cancel     context.CancelFunc
	ctx        context.Context //nolint:containedctx // Owns the consumer membership lifecycle.
	deliveries chan *delivery
	errors     chan error
	done       chan struct{}
	ready      chan struct{}
	readyOnce  sync.Once
	once       sync.Once
}

// Subscribe joins a logical service group; broadcast identities get independent groups.
func (p *Provider) Subscribe(ctx context.Context, b core.Binding) (core.Subscription, error) {
	policy := b.Subscription

	policy.Concurrency = 0
	if policy.Mode == core.Competing {
		policy.BroadcastID = ""
	}

	topic := topicName(b.Identity.Namespace, b.Stream.Name)
	if err := p.declaration(ctx, topic+"_"+b.ConsumerID()+"_config", b.Stream.Replicas, policy); err != nil {
		return nil, err
	}

	start := broker.FirstOffset
	if b.Subscription.StartAt == "new" {
		start = broker.LastOffset
	}

	group, err := broker.NewConsumerGroup(broker.ConsumerGroupConfig{ID: hash(b.Identity.Namespace, b.Stream.Name, b.ConsumerID()), Brokers: p.options.Brokers, Topics: []string{topic}, Dialer: p.options.Dialer, StartOffset: start, WatchPartitionChanges: true, SessionTimeout: 6 * time.Second, HeartbeatInterval: time.Second, RebalanceTimeout: 6 * time.Second, JoinGroupBackoff: 250 * time.Millisecond})
	if err != nil {
		return nil, errors.New("conduit/kafka: invalid consumer configuration")
	}

	subctx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	s := &subscription{provider: p, binding: b, topic: topic, group: group, cancel: cancel, ctx: subctx, deliveries: make(chan *delivery), errors: make(chan error, 1), done: make(chan struct{}), ready: make(chan struct{})}
	p.mu.Lock()
	if p.client == nil {
		p.mu.Unlock()
		cancel()

		_ = group.Close()

		return nil, core.ErrNotRunning
	}

	p.subscriptions[s] = struct{}{}
	p.mu.Unlock()

	go s.run()

	timer := time.NewTimer(20 * time.Second)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		_ = s.Close(context.WithoutCancel(ctx))

		return nil, ctx.Err()
	case err := <-s.errors:
		_ = s.Close(context.WithoutCancel(ctx))

		return nil, err
	case <-timer.C:
		_ = s.Close(context.WithoutCancel(ctx))

		return nil, errors.New("conduit/kafka: group startup timed out")
	case <-s.ready:
	}

	return s, nil
}
func (s *subscription) run() {
	defer close(s.done)

	for {
		sgen, err := s.group.Next(s.ctx)
		if err != nil {
			if s.ctx.Err() != nil || errors.Is(err, broker.ErrGroupClosed) {
				return
			}

			if errors.Is(err, broker.GroupAuthorizationFailed) || errors.Is(err, broker.TopicAuthorizationFailed) {
				s.report(errors.New("conduit/kafka: consumer authorization failed"))

				return
			}
			// The native group keeps rejoining after coordinator changes and rebalances.
			wait := time.NewTimer(250 * time.Millisecond)
			select {
			case <-s.ctx.Done():
				wait.Stop()

				return
			case <-wait.C:
			}

			continue
		}

		sgen.Start(func(ctx context.Context) {
			assignments := sgen.Assignments[s.topic]
			if len(assignments) == 0 {
				s.readyOnce.Do(func() { close(s.ready) })

				select {
				case <-ctx.Done():
				case <-s.ctx.Done():
				}

				return
			}

			reader := s.provider.reader(s.topic)
			defer func() { _ = reader.Close() }()

			offset := assignments[0].Offset
			if offset < 0 {
				client, err := s.provider.getClient()
				if err != nil {
					s.report(err)

					return
				}

				offsets, err := client.ListOffsets(ctx, &broker.ListOffsetsRequest{Topics: map[string][]broker.OffsetRequest{s.topic: {{Partition: 0, Timestamp: offset}}}})
				if err != nil || len(offsets.Topics[s.topic]) == 0 {
					s.report(errors.New("conduit/kafka: initial offset unavailable"))

					return
				}

				partition := offsets.Topics[s.topic][0]
				if partition.Error != nil {
					s.report(partition.Error)

					return
				}

				if offset == broker.LastOffset {
					offset = partition.LastOffset
				} else {
					offset = partition.FirstOffset
				}
			}

			if err := reader.SetOffset(offset); err != nil {
				s.report(err)

				return
			}

			s.readyOnce.Do(func() { close(s.ready) })

			for {
				// The generation's context prevents a stale member from committing after a rebalance.
				readctx, cancel := context.WithCancel(ctx)
				stop := context.AfterFunc(s.ctx, cancel)
				message, err := reader.ReadMessage(readctx)

				stop()
				cancel()

				if err != nil {
					return
				}

				var envelope core.Envelope
				if err := json.Unmarshal(message.Value, &envelope); err != nil {
					s.report(errors.New("conduit/kafka: invalid retained envelope"))

					return
				}

				filtered := envelope.Type != s.binding.Subscription.MessageType || envelope.TargetConsumer != "" && envelope.TargetConsumer != s.binding.ConsumerID() || s.binding.Stream.MaxAge > 0 && time.Since(message.Time) >= s.binding.Stream.MaxAge || s.binding.Subscription.StartAt == "sequence" && uint64(max(0, message.Offset))+1 < s.binding.Subscription.StartSequence
				if filtered {
					if err := sgen.CommitOffsets(map[string]map[int]int64{s.topic: {0: message.Offset + 1}}); err != nil {
						return
					}

					continue
				}

				attempt := uint64(1)
				for {
					d := &delivery{sub: s, envelope: envelope, message: message, generation: sgen, ctx: ctx, attempt: attempt, settle: make(chan settlement), completed: make(chan error, 1), expired: make(chan struct{})}
					select {
					case <-ctx.Done():
						return
					case <-s.ctx.Done():
						return
					case s.deliveries <- d:
					}
					// Never fetch or commit a later offset while this delivery remains unsettled.
					timer := time.NewTimer(s.binding.Subscription.Timeout + 5*time.Second)

					var outcome settlement
					select {
					case <-ctx.Done():
						timer.Stop()

						return
					case <-s.ctx.Done():
						timer.Stop()

						return
					case <-timer.C:
						close(d.expired)

						outcome.retry = true
					case outcome = <-d.settle:
						timer.Stop()
					}

					if outcome.retry {
						select {
						case d.completed <- nil:
						default:
						}

						wait := time.NewTimer(outcome.delay)
						select {
						case <-ctx.Done():
							wait.Stop()

							return
						case <-s.ctx.Done():
							wait.Stop()

							return
						case <-wait.C:
						}

						attempt++

						continue
					}

					err := sgen.CommitOffsets(map[string]map[int]int64{s.topic: {0: message.Offset + 1}})
					d.completed <- err

					if err != nil {
						return
					}

					break
				}
			}
		})
	}
}
func (s *subscription) report(err error) {
	select {
	case s.errors <- err:
	default:
	}
}
func (s *subscription) Next(ctx context.Context) (core.Delivery, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.ctx.Done():
		return nil, context.Canceled
	case err := <-s.errors:
		return nil, err
	case d := <-s.deliveries:
		return d, nil
	}
}
func (s *subscription) Close(ctx context.Context) error {
	s.once.Do(func() { s.cancel(); go func() { _ = s.group.Close() }() })

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-s.done:
	}

	s.provider.mu.Lock()
	delete(s.provider.subscriptions, s)
	s.provider.mu.Unlock()

	return nil
}

type settlement struct {
	retry bool
	delay time.Duration
}
type delivery struct {
	sub        *subscription
	envelope   core.Envelope
	message    broker.Message
	generation *broker.Generation
	ctx        context.Context //nolint:containedctx // Owns the consumer membership lifecycle.
	attempt    uint64
	settle     chan settlement
	completed  chan error
	expired    chan struct{}
	once       sync.Once
}

func (d *delivery) Message() core.Envelope { return d.envelope.Clone() }
func (d *delivery) Info() core.DeliveryInfo {
	b := d.sub.binding

	return core.DeliveryInfo{Stream: b.Stream.Name, ConsumerID: b.ConsumerID(), SubscriptionID: b.Subscription.ID, Destination: b.Identity, Mode: b.Subscription.Mode, Attempt: d.attempt, Sequence: uint64(max(0, d.message.Offset)) + 1}
}
func (d *delivery) settlement(ctx context.Context, value settlement) error {
	sent := false

	d.once.Do(func() {
		select {
		case <-ctx.Done():
		case <-d.ctx.Done():
		case <-d.expired:
		case <-d.sub.ctx.Done():
		case d.settle <- value:
			sent = true
		}
	})

	if !sent {
		return core.ErrConflict
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-d.ctx.Done():
		return core.ErrConflict
	case <-d.expired:
		return core.ErrConflict
	case <-d.sub.ctx.Done():
		return core.ErrNotRunning
	case err := <-d.completed:
		return err
	}
}
func (d *delivery) Ack(ctx context.Context) error    { return d.settlement(ctx, settlement{}) }
func (d *delivery) Reject(ctx context.Context) error { return d.Ack(ctx) }
func (d *delivery) Retry(ctx context.Context, delay time.Duration) error {
	return d.settlement(ctx, settlement{retry: true, delay: delay})
}

// Inspect reports the retained partition offset range. Compacted offsets are not used.
func (p *Provider) Inspect(ctx context.Context, ns string, cfg core.StreamConfig) (core.StreamInfo, error) {
	c, err := p.getClient()
	if err != nil {
		return core.StreamInfo{}, err
	}

	topic := topicName(ns, cfg.Name)

	offsets, err := c.ListOffsets(ctx, &broker.ListOffsetsRequest{Topics: map[string][]broker.OffsetRequest{topic: {{Partition: 0, Timestamp: broker.FirstOffset}, {Partition: 0, Timestamp: broker.LastOffset}}}})
	if err != nil {
		return core.StreamInfo{}, err
	}

	values := offsets.Topics[topic]
	if len(values) == 0 {
		return core.StreamInfo{}, core.ErrNotFound
	}

	var first, last int64

	for _, offset := range values {
		if offset.Error != nil {
			return core.StreamInfo{}, offset.Error
		}

		first = offset.FirstOffset
		last = offset.LastOffset
	}

	return core.StreamInfo{Config: cfg, Messages: uint64(max(0, last-first))}, nil
}

type storedLetter struct {
	Letter core.DeadLetter   `json:"letter"`
	Config core.StreamConfig `json:"config"`
}

func letterTopic(ns, service string) string { return "fc_dlq_" + hash(ns, service) }

// StoreDeadLetter appends a durable scoped failure before the original offset is committed.
func (p *Provider) StoreDeadLetter(ctx context.Context, ns string, l core.DeadLetter) error {
	p.mu.RLock()
	cfg, ok := p.configs[topicName(ns, l.Delivery.Stream)]
	p.mu.RUnlock()

	if !ok {
		return core.ErrNotFound
	}

	topic := letterTopic(ns, l.Delivery.Destination.ServiceID)
	if _, err := p.createTopic(ctx, topic, p.options.DeadLetterReplicas, 0); err != nil {
		return err
	}

	data, err := json.Marshal(storedLetter{Letter: l, Config: cfg})
	if err != nil {
		return err
	}

	_, err = p.produce(ctx, topic, data)

	return err
}
func (p *Provider) letters(ctx context.Context, id core.Identity) (map[string]storedLetter, error) {
	c, err := p.getClient()
	if err != nil {
		return nil, err
	}

	topic := letterTopic(id.Namespace, id.ServiceID)

	metadata, err := c.Metadata(ctx, &broker.MetadataRequest{Topics: []string{topic}})
	if err != nil {
		return nil, err
	}

	if len(metadata.Topics) == 0 || errors.Is(metadata.Topics[0].Error, broker.UnknownTopicOrPartition) {
		return map[string]storedLetter{}, nil
	}

	if metadata.Topics[0].Error != nil {
		return nil, metadata.Topics[0].Error
	}

	offsets, err := c.ListOffsets(ctx, &broker.ListOffsetsRequest{Topics: map[string][]broker.OffsetRequest{topic: {{Partition: 0, Timestamp: broker.LastOffset}}}})
	if err != nil {
		return nil, err
	}

	if len(offsets.Topics[topic]) == 0 {
		return nil, core.ErrNotFound
	}

	last := offsets.Topics[topic][0].LastOffset

	result := map[string]storedLetter{}
	if last == 0 {
		return result, nil
	}

	reader := p.reader(topic)
	defer func() { _ = reader.Close() }()

	for {
		message, err := reader.ReadMessage(ctx)
		if err != nil {
			return nil, err
		}

		var stored storedLetter
		if err := json.Unmarshal(message.Value, &stored); err != nil {
			return nil, err
		}

		l := stored.Letter
		if l.Delivery.Destination.Namespace == id.Namespace && l.Delivery.Destination.ServiceID == id.ServiceID {
			key := l.Delivery.SubscriptionID + ":" + l.ID

			previous := result[key]
			if !previous.Letter.Replayed {
				result[key] = stored
			}
		}

		if message.Offset+1 >= last {
			break
		}
	}

	return result, nil
}

// ListDeadLetters folds the retained failure log and returns a stable ID cursor.
func (p *Provider) ListDeadLetters(ctx context.Context, id core.Identity, sub, cursor string, limit int) ([]core.DeadLetter, string, error) {
	if limit < 1 || limit > 100 {
		return nil, "", core.ErrConflict
	}

	values, err := p.letters(ctx, id)
	if err != nil {
		return nil, "", err
	}

	result := make([]core.DeadLetter, 0, limit+1)

	for _, stored := range values {
		l := stored.Letter
		if l.ID > cursor && (sub == "" || l.Delivery.SubscriptionID == sub) {
			result = append(result, l)
		}
	}

	slices.SortFunc(result, func(a, b core.DeadLetter) int {
		if a.ID < b.ID {
			return -1
		}

		if a.ID > b.ID {
			return 1
		}

		return 0
	})

	next := ""

	if len(result) > limit {
		result = result[:limit]
		next = result[len(result)-1].ID
	}

	return result, next, nil
}

// ReplayDeadLetter preserves the original event ID and targets only its original group.
// Concurrent commands can publish duplicates; transactional inboxes protect business effects.
func (p *Provider) ReplayDeadLetter(ctx context.Context, id core.Identity, sub, messageID string) (core.Receipt, error) {
	values, err := p.letters(ctx, id)
	if err != nil {
		return core.Receipt{}, err
	}

	stored, ok := values[sub+":"+messageID]
	if !ok {
		return core.Receipt{}, core.ErrNotFound
	}

	if stored.Letter.Replayed {
		return core.Receipt{}, core.ErrConflict
	}

	stored.Letter.Message.TargetConsumer = stored.Letter.Delivery.ConsumerID

	receipt, err := p.Publish(ctx, id.Namespace, stored.Config, stored.Letter.Message)
	if err != nil {
		return core.Receipt{}, err
	}

	stored.Letter.Replayed = true

	data, err := json.Marshal(stored)
	if err != nil {
		return core.Receipt{}, err
	}

	_, err = p.produce(ctx, letterTopic(id.Namespace, id.ServiceID), data)
	if err != nil {
		return core.Receipt{}, err
	}

	return receipt, nil
}

var _ core.Provider = (*Provider)(nil)
var _ core.Management = (*Provider)(nil)
