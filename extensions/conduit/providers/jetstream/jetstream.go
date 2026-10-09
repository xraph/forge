// Package jetstream implements durable streams and stable pull consumers using NATS.
package jetstream

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	js "github.com/nats-io/nats.go/jetstream"
	"github.com/xraph/forge/extensions/conduit/core"
)

// Options allows native NATS authentication and TLS without copying secrets into snapshots.
type Options struct {
	URL                string
	NATS               []nats.Option
	DeadLetterReplicas int
}

// Provider owns one service instance's connection.
type Provider struct {
	mu      sync.RWMutex
	options Options
	conn    *nats.Conn
	js      js.JetStream
}

// New prepares a provider. Connect is managed by the Conduit runtime.
func New(options Options) *Provider {
	if options.DeadLetterReplicas == 0 {
		options.DeadLetterReplicas = 1
	}

	return &Provider{options: options}
}

// Name returns the provider type.
func (p *Provider) Name() string { return "nats-jetstream" }

// Capabilities reports persisted streams and recovery, with no key-ordering promise.
func (p *Provider) Capabilities() core.Capabilities {
	return core.Capabilities{Durable: true, Replay: true, DeadLetters: true, RPC: true, ConsumerControls: true, Backfill: true}
}

// Connect creates a dedicated connection and JetStream client.
func (p *Provider) Connect(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.conn != nil {
		return core.ErrConflict
	}

	options := append([]nats.Option{nats.Name("forge-conduit"), nats.Timeout(5 * time.Second)}, p.options.NATS...)

	conn, err := nats.Connect(p.options.URL, options...)
	if err != nil {
		return errors.New("conduit/jetstream: connection failed")
	}

	client, err := js.New(conn)
	if err != nil {
		conn.Close()

		return errors.New("conduit/jetstream: client initialization failed")
	}

	if err := conn.FlushWithContext(ctx); err != nil {
		conn.Close()

		return errors.New("conduit/jetstream: connection check failed")
	}

	p.conn, p.js = conn, client

	return nil
}

// Close disconnects this instance without deleting durable consumers.
func (p *Provider) Close(context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.conn != nil {
		p.conn.Close()
		p.conn = nil
		p.js = nil
	}

	return nil
}

func (p *Provider) client() (js.JetStream, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.conn == nil || p.conn.IsClosed() {
		return nil, core.ErrNotRunning
	}

	return p.js, nil
}

// Health checks the server over the active connection.
func (p *Provider) Health(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.conn == nil {
		return core.ErrNotRunning
	}

	return p.conn.FlushWithContext(ctx)
}

func hash(parts ...string) string {
	data, err := json.Marshal(parts)
	if err != nil {
		panic(err)
	}

	sum := sha256.Sum256(data)

	return hex.EncodeToString(sum[:16])
}

func streamName(namespace, name string) string { return "FC_" + hash(namespace, name) }
func prefix(namespace, stream string) string {
	return "fc." + hash(namespace) + "." + hash(stream) + "."
}
func letterKey(service, subscription, id string) string {
	return "l." + hash(service) + "." + hash(subscription) + "." + id
}

// EnsureStream creates topology or verifies that another instance declared the same settings.
func (p *Provider) EnsureStream(ctx context.Context, namespace string, cfg core.StreamConfig) error {
	client, err := p.client()
	if err != nil {
		return err
	}

	subjects := []string{prefix(namespace, cfg.Name) + "recovery.*"}
	for _, subject := range cfg.Subjects {
		subjects = append(subjects, prefix(namespace, cfg.Name)+"events."+subject)
	}

	slices.Sort(subjects)

	maxMessages := cfg.MaxMessages
	if maxMessages == 0 {
		maxMessages = -1
	}

	desired := js.StreamConfig{Name: streamName(namespace, cfg.Name), Subjects: subjects, Storage: js.FileStorage, Retention: js.LimitsPolicy, MaxMsgs: maxMessages, MaxAge: cfg.MaxAge, Replicas: cfg.Replicas, Metadata: map[string]string{"conduit.namespace": namespace, "conduit.stream": cfg.Name}}

	stream, err := client.Stream(ctx, desired.Name)
	if errors.Is(err, js.ErrStreamNotFound) {
		_, err = client.CreateStream(ctx, desired)
		if err != nil {
			// Another replica can create the same stream during startup.
			stream, err = client.Stream(ctx, desired.Name)
			if err != nil {
				return err
			}
		} else {
			_, err = p.bucket(ctx, namespace)

			return err
		}
	} else if err != nil {
		return err
	}

	info, err := stream.Info(ctx)
	if err != nil {
		return err
	}

	actualSubjects := slices.Clone(info.Config.Subjects)
	slices.Sort(actualSubjects)

	if !slices.Equal(subjects, actualSubjects) || info.Config.Storage != desired.Storage || info.Config.Retention != desired.Retention || info.Config.MaxMsgs != desired.MaxMsgs || info.Config.MaxAge != desired.MaxAge || info.Config.Replicas != desired.Replicas {
		return fmt.Errorf("%w: stream %s", core.ErrConflict, cfg.Name)
	}

	_, err = p.bucket(ctx, namespace)

	return err
}

func (p *Provider) publish(ctx context.Context, namespace string, cfg core.StreamConfig, msg core.Envelope, publishID string) (core.Receipt, error) {
	client, err := p.client()
	if err != nil {
		return core.Receipt{}, err
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return core.Receipt{}, err
	}

	subject := prefix(namespace, cfg.Name) + "events." + msg.Type
	if msg.TargetConsumer != "" {
		subject = prefix(namespace, cfg.Name) + "recovery." + msg.TargetConsumer
	}

	ack, err := client.Publish(ctx, subject, data, js.WithMsgID(publishID), js.WithExpectStream(streamName(namespace, cfg.Name)))
	if err != nil {
		var apiErr *js.APIError
		if errors.As(err, &apiErr) {
			return core.Receipt{}, errors.New("conduit/jetstream: publication rejected by broker")
		}

		return core.Receipt{}, core.ErrOutcomeUnknown
	}

	return core.Receipt{MessageID: msg.ID, Sequence: ack.Sequence, Persisted: true, Duplicate: ack.Duplicate}, nil
}

// Publish waits for the server's persisted publish acknowledgement.
func (p *Provider) Publish(ctx context.Context, namespace string, cfg core.StreamConfig, msg core.Envelope) (core.Receipt, error) {
	return p.publish(ctx, namespace, cfg, msg, msg.ID)
}

type subscription struct {
	provider *Provider
	binding  core.Binding
	consumer js.Consumer
	closed   chan struct{}
	once     sync.Once
}

// Subscribe binds every service replica to the same competing consumer.
func (p *Provider) Subscribe(ctx context.Context, binding core.Binding) (core.Subscription, error) {
	client, err := p.client()
	if err != nil {
		return nil, err
	}

	id := binding.ConsumerID()
	base := prefix(binding.Identity.Namespace, binding.Stream.Name) + "events." + binding.Subscription.MessageType

	cfg := js.ConsumerConfig{Name: id, AckPolicy: js.AckExplicitPolicy, AckWait: binding.Subscription.Timeout + 5*time.Second, MaxDeliver: -1, MaxAckPending: binding.Subscription.MaxInFlight, FilterSubjects: []string{base, prefix(binding.Identity.Namespace, binding.Stream.Name) + "recovery." + id}, Metadata: map[string]string{"conduit.service": binding.Identity.ServiceID, "conduit.subscription": binding.Subscription.ID, "conduit.delivery": string(binding.Subscription.Mode), "conduit.maxAttempts": strconv.Itoa(binding.Subscription.MaxAttempts), "conduit.retryDelay": binding.Subscription.RetryDelay.String()}}
	if binding.Subscription.Durable {
		cfg.Durable = id
	} else {
		cfg.InactiveThreshold = 5 * time.Minute
	}

	switch binding.Subscription.StartAt {
	case "new":
		cfg.DeliverPolicy = js.DeliverNewPolicy
	case "sequence":
		cfg.DeliverPolicy = js.DeliverByStartSequencePolicy
		cfg.OptStartSeq = binding.Subscription.StartSequence
	default:
		cfg.DeliverPolicy = js.DeliverAllPolicy
	}

	consumer, err := client.Consumer(ctx, streamName(binding.Identity.Namespace, binding.Stream.Name), id)
	if errors.Is(err, js.ErrConsumerNotFound) {
		consumer, err = client.CreateConsumer(ctx, streamName(binding.Identity.Namespace, binding.Stream.Name), cfg)
		if err != nil {
			consumer, err = client.Consumer(ctx, streamName(binding.Identity.Namespace, binding.Stream.Name), id)
		}
	}

	if err != nil {
		return nil, err
	}

	info, err := consumer.Info(ctx)
	if err != nil {
		return nil, err
	}

	actual := info.Config

	metadataMatches := true
	for key, value := range cfg.Metadata {
		metadataMatches = metadataMatches && actual.Metadata[key] == value
	}

	if actual.Durable != cfg.Durable || actual.AckPolicy != cfg.AckPolicy || actual.AckWait != cfg.AckWait || actual.MaxAckPending != cfg.MaxAckPending || actual.DeliverPolicy != cfg.DeliverPolicy || actual.OptStartSeq != cfg.OptStartSeq || !reflect.DeepEqual(actual.FilterSubjects, cfg.FilterSubjects) || !metadataMatches {
		return nil, fmt.Errorf("%w: consumer %s", core.ErrConflict, id)
	}

	return &subscription{provider: p, binding: binding, consumer: consumer, closed: make(chan struct{})}, nil
}

func (s *subscription) Next(ctx context.Context) (core.Delivery, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		select {
		case <-s.closed:
			return nil, context.Canceled
		default:
		}

		batch, err := s.consumer.Fetch(1, js.FetchMaxWait(250*time.Millisecond))
		if err != nil {
			return nil, err
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-s.closed:
			return nil, context.Canceled
		case msg, ok := <-batch.Messages():
			if !ok {
				if err := batch.Error(); err != nil && !errors.Is(err, nats.ErrTimeout) {
					return nil, err
				}

				continue
			}

			metadata, err := msg.Metadata()
			if err != nil {
				return nil, err
			}

			var envelope core.Envelope
			if err := json.Unmarshal(msg.Data(), &envelope); err != nil {
				payload, encodeErr := json.Marshal(map[string]string{"raw": base64.StdEncoding.EncodeToString(msg.Data())})
				if encodeErr != nil {
					return nil, encodeErr
				}

				envelope = core.Envelope{ID: fmt.Sprintf("malformed-%d", metadata.Sequence.Stream), Type: s.binding.Subscription.MessageType, Data: payload, Headers: map[string]string{"conduit.decodeError": "malformed envelope"}}
			}

			return &delivery{subscription: s, msg: msg, envelope: envelope, metadata: metadata}, nil
		}
	}
}

func (s *subscription) Close(ctx context.Context) error {
	s.once.Do(func() { close(s.closed) })

	if s.binding.Subscription.Mode == core.Broadcast && !s.binding.Subscription.Durable {
		client, err := s.provider.client()
		if err != nil {
			return err
		}

		return client.DeleteConsumer(ctx, streamName(s.binding.Identity.Namespace, s.binding.Stream.Name), s.binding.ConsumerID())
	}

	return nil
}

type delivery struct {
	subscription *subscription
	msg          js.Msg
	envelope     core.Envelope
	metadata     *js.MsgMetadata
}

func (d *delivery) Message() core.Envelope { return d.envelope.Clone() }
func (d *delivery) Info() core.DeliveryInfo {
	b := d.subscription.binding

	return core.DeliveryInfo{Stream: b.Stream.Name, ConsumerID: b.ConsumerID(), SubscriptionID: b.Subscription.ID, Destination: b.Identity, Mode: b.Subscription.Mode, Attempt: d.metadata.NumDelivered, Sequence: d.metadata.Sequence.Stream}
}
func (d *delivery) Ack(ctx context.Context) error { return d.msg.DoubleAck(ctx) }
func (d *delivery) Retry(ctx context.Context, delay time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	return d.msg.NakWithDelay(delay)
}
func (d *delivery) Reject(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	return d.msg.Term()
}

// Inspect returns broker counts for the configured namespace and stream.
func (p *Provider) Inspect(ctx context.Context, namespace string, cfg core.StreamConfig) (core.StreamInfo, error) {
	client, err := p.client()
	if err != nil {
		return core.StreamInfo{}, err
	}

	stream, err := client.Stream(ctx, streamName(namespace, cfg.Name))
	if err != nil {
		return core.StreamInfo{}, err
	}

	info, err := stream.Info(ctx)
	if err != nil {
		return core.StreamInfo{}, err
	}

	return core.StreamInfo{Config: cfg, Messages: info.State.Msgs, Bytes: info.State.Bytes, Consumers: info.State.Consumers}, nil
}

func (p *Provider) bucket(ctx context.Context, namespace string) (js.KeyValue, error) {
	client, err := p.client()
	if err != nil {
		return nil, err
	}

	return p.ensureBucket(ctx, client, js.KeyValueConfig{Bucket: "FC_DLQ_" + hash(namespace), Storage: js.FileStorage, Replicas: p.options.DeadLetterReplicas, History: 1})
}

func (p *Provider) ensureBucket(ctx context.Context, client js.JetStream, expected js.KeyValueConfig) (js.KeyValue, error) {
	bucket, err := client.KeyValue(ctx, expected.Bucket)
	if errors.Is(err, js.ErrBucketNotFound) {
		bucket, err = client.CreateKeyValue(ctx, expected)
		if err != nil {
			bucket, err = client.KeyValue(ctx, expected.Bucket)
		}
	}

	if err != nil {
		return nil, err
	}

	status, err := bucket.Status(ctx)
	if err != nil {
		return nil, err
	}

	actual := status.Config()
	if actual.Storage != expected.Storage || actual.Replicas != expected.Replicas || actual.TTL != expected.TTL || actual.History != expected.History {
		return nil, fmt.Errorf("%w: key value topology", core.ErrConflict)
	}

	return bucket, nil
}

type storedLetter struct {
	Letter  core.DeadLetter `json:"letter"`
	Claim   string          `json:"claim,omitempty"`
	ClaimAt time.Time       `json:"claimAt,omitzero"`
}

// StoreDeadLetter waits for persisted, idempotent failure acceptance.
func (p *Provider) StoreDeadLetter(ctx context.Context, namespace string, letter core.DeadLetter) error {
	bucket, err := p.bucket(ctx, namespace)
	if err != nil {
		return err
	}

	data, err := json.Marshal(storedLetter{Letter: letter})
	if err != nil {
		return err
	}

	_, err = bucket.Create(ctx, letterKey(letter.Delivery.Destination.ServiceID, letter.Delivery.SubscriptionID, letter.ID), data)
	if errors.Is(err, js.ErrKeyExists) {
		return nil
	}

	return err
}

// ListDeadLetters uses a service-filtered key scan and an explicit continuation cursor.
func (p *Provider) ListDeadLetters(ctx context.Context, identity core.Identity, subscriptionID, cursor string, limit int) ([]core.DeadLetter, string, error) {
	bucket, err := p.bucket(ctx, identity.Namespace)
	if err != nil {
		return nil, "", err
	}

	filter := "l." + hash(identity.ServiceID) + ".>"
	if subscriptionID != "" {
		filter = "l." + hash(identity.ServiceID) + "." + hash(subscriptionID) + ".*"
	}

	lister, err := bucket.ListKeysFiltered(ctx, filter)
	if errors.Is(err, js.ErrNoKeysFound) {
		return []core.DeadLetter{}, "", nil
	}

	if err != nil {
		return nil, "", err
	}

	defer func() { _ = lister.Stop() }()

	letters := make([]core.DeadLetter, 0, limit+1)

	for key := range lister.Keys() {
		entry, err := bucket.Get(ctx, key)
		if err != nil {
			return nil, "", err
		}

		var stored storedLetter
		if err := json.Unmarshal(entry.Value(), &stored); err != nil {
			return nil, "", err
		}

		if stored.Letter.ID > cursor {
			letters = append(letters, stored.Letter)
			slices.SortFunc(letters, func(a, b core.DeadLetter) int { return strings.Compare(a.ID, b.ID) })

			if len(letters) > limit+1 {
				letters = letters[:limit+1]
			}
		}
	}

	if err := ctx.Err(); err != nil {
		return nil, "", err
	}

	slices.SortFunc(letters, func(a, b core.DeadLetter) int { return strings.Compare(a.ID, b.ID) })

	next := ""

	if len(letters) > limit {
		letters = letters[:limit]
		next = letters[len(letters)-1].ID
	}

	return letters, next, nil
}

// ReplayDeadLetter claims a failure and publishes only to its original consumer.
func (p *Provider) ReplayDeadLetter(ctx context.Context, identity core.Identity, subscriptionID, id string) (core.Receipt, error) {
	bucket, err := p.bucket(ctx, identity.Namespace)
	if err != nil {
		return core.Receipt{}, err
	}

	key := letterKey(identity.ServiceID, subscriptionID, id)

	entry, err := bucket.Get(ctx, key)
	if errors.Is(err, js.ErrKeyNotFound) {
		return core.Receipt{}, core.ErrNotFound
	}

	if err != nil {
		return core.Receipt{}, err
	}

	var stored storedLetter
	if err := json.Unmarshal(entry.Value(), &stored); err != nil {
		return core.Receipt{}, err
	}

	if stored.Letter.Delivery.Destination.ServiceID != identity.ServiceID || stored.Letter.Delivery.SubscriptionID != subscriptionID {
		return core.Receipt{}, core.ErrNotFound
	}

	if stored.Letter.Replayed || stored.Claim != "" && time.Since(stored.ClaimAt) < time.Minute {
		return core.Receipt{}, core.ErrConflict
	}

	stored.Claim, stored.ClaimAt = core.NewID(), time.Now().UTC()

	data, err := json.Marshal(stored)
	if err != nil {
		return core.Receipt{}, err
	}

	revision, err := bucket.Update(ctx, key, data, entry.Revision())
	if err != nil {
		return core.Receipt{}, fmt.Errorf("%w: replay claim", core.ErrConflict)
	}

	msg := stored.Letter.Message.Clone()
	msg.TargetConsumer = stored.Letter.Delivery.ConsumerID

	receipt, err := p.publish(ctx, identity.Namespace, core.StreamConfig{Name: stored.Letter.Delivery.Stream}, msg, "replay-"+id)
	if err != nil {
		return receipt, err
	}

	stored.Letter.Replayed = true
	stored.Claim = ""

	data, err = json.Marshal(stored)
	if err != nil {
		return receipt, err
	}

	if _, err := bucket.Update(ctx, key, data, revision); err != nil {
		return receipt, core.ErrOutcomeUnknown
	}

	return receipt, nil
}

var _ core.Provider = (*Provider)(nil)
var _ core.Management = (*Provider)(nil)
