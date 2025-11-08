package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/xraph/forge/internal/errors"

	nats "github.com/nats-io/nats.go"
	"github.com/xraph/forge"
)

// NATSQueue implements Queue interface using NATS JetStream.
type NATSQueue struct {
	config    Config
	logger    forge.Logger
	metrics   forge.Metrics
	conn      *nats.Conn
	js        nats.JetStreamContext
	connected bool
	consumers map[string]*natsConsumer
	mu        sync.RWMutex
	startTime time.Time
}

type natsConsumer struct {
	queue   string
	handler MessageHandler
	opts    ConsumeOptions
	sub     *nats.Subscription
	cancel  context.CancelFunc
	active  bool
}

// NewNATSQueue creates a new NATS-backed queue instance.
func NewNATSQueue(config Config, logger forge.Logger, metrics forge.Metrics) (*NATSQueue, error) {
	if config.URL == "" && len(config.Hosts) == 0 {
		return nil, errors.New("nats requires URL or hosts")
	}

	return &NATSQueue{
		config:    config,
		logger:    logger,
		metrics:   metrics,
		consumers: make(map[string]*natsConsumer),
	}, nil
}

func (q *NATSQueue) Connect(ctx context.Context) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.connected {
		return ErrAlreadyConnected
	}

	// Build connection URL
	url := q.config.URL
	if url == "" && len(q.config.Hosts) > 0 {
		url = "nats://" + q.config.Hosts[0]
	}

	// Build NATS options
	opts := []nats.Option{
		nats.Name("forge-queue"),
		nats.MaxReconnects(q.config.MaxRetries),
		nats.ReconnectWait(time.Second),
		nats.Timeout(q.config.ConnectTimeout),
	}

	if q.config.Username != "" && q.config.Password != "" {
		opts = append(opts, nats.UserInfo(q.config.Username, q.config.Password))
	}

	// Connect to NATS
	conn, err := nats.Connect(url, opts...)
	if err != nil {
		return fmt.Errorf("failed to connect to nats: %w", err)
	}

	// Create JetStream context
	js, err := conn.JetStream()
	if err != nil {
		conn.Close()

		return fmt.Errorf("failed to create jetstream context: %w", err)
	}

	q.conn = conn
	q.js = js
	q.connected = true
	q.startTime = time.Now()

	q.logger.Info("connected to nats jetstream", forge.F("url", url))

	return nil
}

func (q *NATSQueue) Disconnect(ctx context.Context) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if !q.connected {
		return ErrNotConnected
	}

	// Stop all consumers
	for _, c := range q.consumers {
		if c.sub != nil {
			c.sub.Unsubscribe()
		}

		if c.cancel != nil {
			c.cancel()
		}
	}

	if q.conn != nil {
		q.conn.Close()
	}

	q.connected = false
	q.consumers = make(map[string]*natsConsumer)
	q.logger.Info("disconnected from nats")

	return nil
}

func (q *NATSQueue) Ping(ctx context.Context) error {
	q.mu.RLock()
	defer q.mu.RUnlock()

	if !q.connected {
		return ErrNotConnected
	}

	if q.conn == nil || !q.conn.IsConnected() {
		return ErrConnectionFailed
	}

	return nil
}

func (q *NATSQueue) DeclareQueue(ctx context.Context, name string, opts QueueOptions) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if !q.connected {
		return ErrNotConnected
	}

	// Create stream (like a queue in NATS)
	streamConfig := &nats.StreamConfig{
		Name:     name,
		Subjects: []string{name + ".*"},
		Storage:  nats.FileStorage,
	}

	if opts.MaxLength > 0 {
		streamConfig.MaxMsgs = int64(opts.MaxLength)
	}

	if opts.MessageTTL > 0 {
		streamConfig.MaxAge = opts.MessageTTL
	}

	// Check if stream already exists
	_, err := q.js.StreamInfo(name)
	if err == nil {
		return ErrQueueAlreadyExists
	}

	_, err = q.js.AddStream(streamConfig)
	if err != nil {
		return fmt.Errorf("failed to declare queue: %w", err)
	}

	q.logger.Info("declared nats stream", forge.F("stream", name))

	return nil
}

func (q *NATSQueue) DeleteQueue(ctx context.Context, name string) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if !q.connected {
		return ErrNotConnected
	}

	if err := q.js.DeleteStream(name); err != nil {
		return fmt.Errorf("failed to delete queue: %w", err)
	}

	q.logger.Info("deleted nats stream", forge.F("stream", name))

	return nil
}

func (q *NATSQueue) ListQueues(ctx context.Context) ([]string, error) {
	q.mu.RLock()
	defer q.mu.RUnlock()

	if !q.connected {
		return nil, ErrNotConnected
	}

	var streams []string
	for info := range q.js.Streams() {
		streams = append(streams, info.Config.Name)
	}

	return streams, nil
}

func (q *NATSQueue) GetQueueInfo(ctx context.Context, name string) (*QueueInfo, error) {
	q.mu.RLock()
	defer q.mu.RUnlock()

	if !q.connected {
		return nil, ErrNotConnected
	}

	info, err := q.js.StreamInfo(name)
	if err != nil {
		return nil, ErrQueueNotFound
	}

	return &QueueInfo{
		Name:       info.Config.Name,
		Messages:   int64(info.State.Msgs),
		Consumers:  info.State.Consumers,
		Durable:    info.Config.Storage == nats.FileStorage,
		AutoDelete: false,
		CreatedAt:  info.Created,
	}, nil
}

func (q *NATSQueue) PurgeQueue(ctx context.Context, name string) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if !q.connected {
		return ErrNotConnected
	}

	if err := q.js.PurgeStream(name); err != nil {
		return fmt.Errorf("failed to purge queue: %w", err)
	}

	q.logger.Info("purged nats stream", forge.F("stream", name))

	return nil
}

func (q *NATSQueue) Publish(ctx context.Context, queueName string, message Message) error {
	q.mu.RLock()
	defer q.mu.RUnlock()

	if !q.connected {
		return ErrNotConnected
	}

	// Serialize message
	data, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	subject := queueName + ".messages"

	// Build NATS message options
	var publishOpts []nats.PubOpt

	if message.ID != "" {
		publishOpts = append(publishOpts, nats.MsgId(message.ID))
	}

	// Publish to stream
	_, err = q.js.Publish(subject, data, publishOpts...)
	if err != nil {
		return fmt.Errorf("failed to publish message: %w", err)
	}

	q.logger.Debug("published message to nats",
		forge.F("stream", queueName),
		forge.F("size", len(data)),
	)

	return nil
}

func (q *NATSQueue) PublishBatch(ctx context.Context, queueName string, messages []Message) error {
	q.mu.RLock()
	connected := q.connected
	q.mu.RUnlock()

	if !connected {
		return ErrNotConnected
	}

	for _, msg := range messages {
		if err := q.Publish(ctx, queueName, msg); err != nil {
			return err
		}
	}

	return nil
}

func (q *NATSQueue) PublishDelayed(ctx context.Context, queueName string, message Message, delay time.Duration) error {
	q.mu.RLock()
	connected := q.connected
	q.mu.RUnlock()

	if !connected {
		return ErrNotConnected
	}

	// NATS doesn't have native delayed messages
	// We'll need to use a separate goroutine to delay publishing
	go func() {
		time.Sleep(delay)

		if err := q.Publish(context.Background(), queueName, message); err != nil {
			q.logger.Error("failed to publish delayed message",
				forge.F("queue", queueName),
				forge.F("error", err),
			)
		}
	}()

	return nil
}

func (q *NATSQueue) Consume(ctx context.Context, queueName string, handler MessageHandler, opts ConsumeOptions) error {
	q.mu.Lock()

	subject := queueName + ".messages"
	consumerName := fmt.Sprintf("consumer-%d", time.Now().UnixNano())

	// Create durable consumer
	consumerConfig := &nats.ConsumerConfig{
		Durable:       consumerName,
		AckPolicy:     nats.AckExplicitPolicy,
		MaxAckPending: opts.PrefetchCount,
	}

	if opts.Timeout > 0 {
		consumerConfig.AckWait = opts.Timeout
	}

	_, err := q.js.AddConsumer(queueName, consumerConfig)
	if err != nil {
		q.mu.Unlock()

		return fmt.Errorf("failed to add consumer: %w", err)
	}

	consumerCtx, cancel := context.WithCancel(ctx)

	// Subscribe to the stream
	sub, err := q.js.Subscribe(subject, func(msg *nats.Msg) {
		// Parse message
		var queueMsg Message
		if err := json.Unmarshal(msg.Data, &queueMsg); err != nil {
			q.logger.Error("failed to unmarshal message",
				forge.F("error", err),
			)
			msg.Nak()

			return
		}

		// Get message metadata
		meta, err := msg.Metadata()
		if err == nil {
			queueMsg.ID = strconv.FormatUint(meta.Sequence.Stream, 10)
		}

		// Handle message
		handlerCtx := consumerCtx

		if opts.Timeout > 0 {
			var cancelTimeout context.CancelFunc

			handlerCtx, cancelTimeout = context.WithTimeout(consumerCtx, opts.Timeout)
			defer cancelTimeout()
		}

		if err := handler(handlerCtx, queueMsg); err != nil {
			q.logger.Error("message handler failed",
				forge.F("stream", queueName),
				forge.F("msg_id", queueMsg.ID),
				forge.F("error", err),
			)

			// Nack with requeue based on retry logic
			if queueMsg.Retries < queueMsg.MaxRetries {
				msg.NakWithDelay(time.Second * time.Duration(queueMsg.Retries+1))
			} else {
				msg.Term() // Terminate message (move to dead letter if configured)
			}
		} else {
			// Ack message
			if !opts.AutoAck {
				msg.Ack()
			}
		}
	}, nats.Durable(consumerName), nats.ManualAck())
	if err != nil {
		cancel()
		q.mu.Unlock()

		return fmt.Errorf("failed to subscribe: %w", err)
	}

	q.consumers[consumerName] = &natsConsumer{
		queue:   queueName,
		handler: handler,
		opts:    opts,
		sub:     sub,
		cancel:  cancel,
		active:  true,
	}
	q.mu.Unlock()

	q.logger.Info("started nats consumer", forge.F("stream", queueName))

	return nil
}

func (q *NATSQueue) StopConsuming(ctx context.Context, queueName string) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	for id, c := range q.consumers {
		if c.queue == queueName && c.active {
			if c.sub != nil {
				c.sub.Unsubscribe()
			}

			if c.cancel != nil {
				c.cancel()
			}

			c.active = false

			delete(q.consumers, id)
		}
	}

	q.logger.Info("stopped nats consumer", forge.F("stream", queueName))

	return nil
}

func (q *NATSQueue) Ack(ctx context.Context, messageID string) error {
	// Handled in consumer callback
	return nil
}

func (q *NATSQueue) Nack(ctx context.Context, messageID string, requeue bool) error {
	// Handled in consumer callback
	return nil
}

func (q *NATSQueue) Reject(ctx context.Context, messageID string) error {
	// Handled in consumer callback
	return nil
}

func (q *NATSQueue) GetDeadLetterQueue(ctx context.Context, queueName string) ([]Message, error) {
	q.mu.RLock()
	defer q.mu.RUnlock()

	if !q.connected {
		return nil, ErrNotConnected
	}

	// NATS doesn't have a built-in DLQ concept in the same way
	// We'd need to create a separate stream for dead letters
	dlqName := queueName + "-dlq"

	info, err := q.js.StreamInfo(dlqName)
	if err != nil {
		return []Message{}, nil
	}

	messages := make([]Message, 0, info.State.Msgs)

	// This would require fetching messages which is complex
	// For production, consider using a separate consumer pattern

	return messages, nil
}

func (q *NATSQueue) RequeueDeadLetter(ctx context.Context, queueName string, messageID string) error {
	q.mu.RLock()
	defer q.mu.RUnlock()

	if !q.connected {
		return ErrNotConnected
	}

	// Would require moving messages between streams
	// Implementation depends on DLQ strategy
	return nil
}

func (q *NATSQueue) Stats(ctx context.Context) (*QueueStats, error) {
	q.mu.RLock()
	defer q.mu.RUnlock()

	if !q.connected {
		return nil, ErrNotConnected
	}

	streams, _ := q.ListQueues(ctx)

	var totalMessages int64

	for _, streamName := range streams {
		info, err := q.js.StreamInfo(streamName)
		if err != nil {
			continue
		}

		totalMessages += int64(info.State.Msgs)
	}

	uptime := time.Duration(0)
	if !q.startTime.IsZero() {
		uptime = time.Since(q.startTime)
	}

	return &QueueStats{
		QueueCount:      int64(len(streams)),
		TotalMessages:   totalMessages,
		TotalConsumers:  len(q.consumers),
		Uptime:          uptime,
		Version:         "nats-2.10.0",
		ConnectionCount: 1,
	}, nil
}
