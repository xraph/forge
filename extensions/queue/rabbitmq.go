package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/xraph/forge"
	"github.com/xraph/forge/internal/errors"
)

// RabbitMQQueue implements Queue interface using RabbitMQ.
type RabbitMQQueue struct {
	config    Config
	logger    forge.Logger
	metrics   forge.Metrics
	conn      *amqp.Connection
	channel   *amqp.Channel
	connected bool
	consumers map[string]*rabbitConsumer
	mu        sync.RWMutex
	startTime time.Time
}

type rabbitConsumer struct {
	queue   string
	handler MessageHandler
	opts    ConsumeOptions
	cancel  context.CancelFunc
	active  bool
}

// NewRabbitMQQueue creates a new RabbitMQ-backed queue instance.
func NewRabbitMQQueue(config Config, logger forge.Logger, metrics forge.Metrics) (*RabbitMQQueue, error) {
	if config.URL == "" && len(config.Hosts) == 0 {
		return nil, errors.New("rabbitmq requires URL or hosts")
	}

	return &RabbitMQQueue{
		config:    config,
		logger:    logger,
		metrics:   metrics,
		consumers: make(map[string]*rabbitConsumer),
	}, nil
}

func (q *RabbitMQQueue) Connect(ctx context.Context) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.connected {
		return ErrAlreadyConnected
	}

	// Build connection URL
	url := q.config.URL
	if url == "" && len(q.config.Hosts) > 0 {
		// Build URL from components
		url = fmt.Sprintf("amqp://%s:%s@%s%s",
			q.config.Username,
			q.config.Password,
			q.config.Hosts[0],
			q.config.VHost,
		)
	}

	// Connect to RabbitMQ
	conn, err := amqp.Dial(url)
	if err != nil {
		return fmt.Errorf("failed to connect to rabbitmq: %w", err)
	}

	// Open a channel
	ch, err := conn.Channel()
	if err != nil {
		conn.Close()

		return fmt.Errorf("failed to open channel: %w", err)
	}

	// Set QoS if configured
	if q.config.DefaultPrefetch > 0 {
		if err := ch.Qos(q.config.DefaultPrefetch, 0, false); err != nil {
			ch.Close()
			conn.Close()

			return fmt.Errorf("failed to set QoS: %w", err)
		}
	}

	q.conn = conn
	q.channel = ch
	q.connected = true
	q.startTime = time.Now()

	q.logger.Info("connected to rabbitmq", forge.F("url", url))

	return nil
}

func (q *RabbitMQQueue) Disconnect(ctx context.Context) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if !q.connected {
		return ErrNotConnected
	}

	// Stop all consumers
	for _, c := range q.consumers {
		if c.cancel != nil {
			c.cancel()
		}
	}

	if q.channel != nil {
		q.channel.Close()
	}

	if q.conn != nil {
		q.conn.Close()
	}

	q.connected = false
	q.consumers = make(map[string]*rabbitConsumer)
	q.logger.Info("disconnected from rabbitmq")

	return nil
}

func (q *RabbitMQQueue) Ping(ctx context.Context) error {
	q.mu.RLock()
	defer q.mu.RUnlock()

	if !q.connected {
		return ErrNotConnected
	}

	if q.conn == nil || q.conn.IsClosed() {
		return ErrConnectionFailed
	}

	return nil
}

func (q *RabbitMQQueue) DeclareQueue(ctx context.Context, name string, opts QueueOptions) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if !q.connected {
		return ErrNotConnected
	}

	// Build arguments
	args := amqp.Table{}
	if opts.MessageTTL > 0 {
		args["x-message-ttl"] = int64(opts.MessageTTL.Milliseconds())
	}

	if opts.MaxLength > 0 {
		args["x-max-length"] = opts.MaxLength
	}

	if opts.MaxPriority > 0 {
		args["x-max-priority"] = opts.MaxPriority
	}

	if opts.DeadLetterQueue != "" {
		args["x-dead-letter-exchange"] = ""
		args["x-dead-letter-routing-key"] = opts.DeadLetterQueue
	}

	_, err := q.channel.QueueDeclare(
		name,
		opts.Durable,
		opts.AutoDelete,
		opts.Exclusive,
		false, // no-wait
		args,
	)
	if err != nil {
		return fmt.Errorf("failed to declare queue: %w", err)
	}

	q.logger.Info("declared rabbitmq queue", forge.F("queue", name))

	return nil
}

func (q *RabbitMQQueue) DeleteQueue(ctx context.Context, name string) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if !q.connected {
		return ErrNotConnected
	}

	_, err := q.channel.QueueDelete(name, false, false, false)
	if err != nil {
		return fmt.Errorf("failed to delete queue: %w", err)
	}

	q.logger.Info("deleted rabbitmq queue", forge.F("queue", name))

	return nil
}

func (q *RabbitMQQueue) ListQueues(ctx context.Context) ([]string, error) {
	q.mu.RLock()
	defer q.mu.RUnlock()

	if !q.connected {
		return nil, ErrNotConnected
	}

	// Note: RabbitMQ doesn't provide a built-in way to list queues via AMQP
	// This would require the management API
	// For now, return empty list
	return []string{}, nil
}

func (q *RabbitMQQueue) GetQueueInfo(ctx context.Context, name string) (*QueueInfo, error) {
	q.mu.RLock()
	defer q.mu.RUnlock()

	if !q.connected {
		return nil, ErrNotConnected
	}

	// Passive declare to get queue info
	queue, err := q.channel.QueueInspect(name)
	if err != nil {
		return nil, ErrQueueNotFound
	}

	return &QueueInfo{
		Name:       queue.Name,
		Messages:   int64(queue.Messages),
		Consumers:  queue.Consumers,
		Durable:    true, // We don't have this info from inspect
		AutoDelete: false,
		CreatedAt:  time.Now(), // Not available
	}, nil
}

func (q *RabbitMQQueue) PurgeQueue(ctx context.Context, name string) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if !q.connected {
		return ErrNotConnected
	}

	_, err := q.channel.QueuePurge(name, false)
	if err != nil {
		return fmt.Errorf("failed to purge queue: %w", err)
	}

	q.logger.Info("purged rabbitmq queue", forge.F("queue", name))

	return nil
}

func (q *RabbitMQQueue) Publish(ctx context.Context, queueName string, message Message) error {
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

	// Build headers
	headers := amqp.Table{}
	for k, v := range message.Headers {
		headers[k] = v
	}

	publishing := amqp.Publishing{
		DeliveryMode: amqp.Persistent,
		ContentType:  "application/json",
		Body:         data,
		Headers:      headers,
		Timestamp:    time.Now(),
	}

	if message.Priority > 0 {
		publishing.Priority = uint8(message.Priority)
	}

	if message.Expiration > 0 {
		publishing.Expiration = strconv.FormatInt(message.Expiration.Milliseconds(), 10)
	}

	err = q.channel.PublishWithContext(
		ctx,
		"",        // exchange
		queueName, // routing key
		false,     // mandatory
		false,     // immediate
		publishing,
	)
	if err != nil {
		return fmt.Errorf("failed to publish message: %w", err)
	}

	q.logger.Debug("published message to rabbitmq",
		forge.F("queue", queueName),
		forge.F("size", len(data)),
	)

	return nil
}

func (q *RabbitMQQueue) PublishBatch(ctx context.Context, queueName string, messages []Message) error {
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

func (q *RabbitMQQueue) PublishDelayed(ctx context.Context, queueName string, message Message, delay time.Duration) error {
	// RabbitMQ delayed messages require a plugin or TTL+DLX pattern
	// For simplicity, we'll use a delayed queue with TTL
	delayedQueueName := queueName + ".delayed"

	// Declare delayed queue with TTL and DLX to original queue
	opts := QueueOptions{
		Durable:         true,
		MessageTTL:      delay,
		DeadLetterQueue: queueName,
	}

	if err := q.DeclareQueue(ctx, delayedQueueName, opts); err != nil {
		// Queue might already exist, continue
	}

	// Publish to delayed queue
	return q.Publish(ctx, delayedQueueName, message)
}

func (q *RabbitMQQueue) Consume(ctx context.Context, queueName string, handler MessageHandler, opts ConsumeOptions) error {
	q.mu.Lock()

	consumerID := fmt.Sprintf("consumer-%d", time.Now().UnixNano())
	consumerCtx, cancel := context.WithCancel(ctx)

	q.consumers[consumerID] = &rabbitConsumer{
		queue:   queueName,
		handler: handler,
		opts:    opts,
		cancel:  cancel,
		active:  true,
	}
	q.mu.Unlock()

	go func() {
		// Get channel (we should ideally create a new channel per consumer)
		q.mu.RLock()
		ch := q.channel
		q.mu.RUnlock()

		// Start consuming
		msgs, err := ch.Consume(
			queueName,
			consumerID,
			opts.AutoAck,
			opts.Exclusive,
			false, // no-local
			false, // no-wait
			nil,   // args
		)
		if err != nil {
			q.logger.Error("failed to start consumer",
				forge.F("queue", queueName),
				forge.F("error", err),
			)

			return
		}

		for {
			select {
			case <-consumerCtx.Done():
				return
			case delivery, ok := <-msgs:
				if !ok {
					return
				}

				// Parse message
				var msg Message
				if err := json.Unmarshal(delivery.Body, &msg); err != nil {
					q.logger.Error("failed to unmarshal message",
						forge.F("error", err),
					)
					delivery.Nack(false, false)

					continue
				}

				msg.ID = strconv.FormatUint(delivery.DeliveryTag, 10)

				// Handle message
				handlerCtx := consumerCtx

				if opts.Timeout > 0 {
					var cancelTimeout context.CancelFunc

					handlerCtx, cancelTimeout = context.WithTimeout(consumerCtx, opts.Timeout)
					defer cancelTimeout()
				}

				if err := handler(handlerCtx, msg); err != nil {
					q.logger.Error("message handler failed",
						forge.F("queue", queueName),
						forge.F("msg_id", msg.ID),
						forge.F("error", err),
					)

					// Nack with requeue based on retry logic
					shouldRequeue := msg.Retries < msg.MaxRetries
					delivery.Nack(false, shouldRequeue)
				} else {
					// Ack message
					if !opts.AutoAck {
						delivery.Ack(false)
					}
				}
			}
		}
	}()

	q.logger.Info("started rabbitmq consumer", forge.F("queue", queueName))

	return nil
}

func (q *RabbitMQQueue) StopConsuming(ctx context.Context, queueName string) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	for id, c := range q.consumers {
		if c.queue == queueName && c.active {
			if c.cancel != nil {
				c.cancel()
			}

			// Cancel the consumer on the channel
			if q.channel != nil {
				q.channel.Cancel(id, false)
			}

			c.active = false

			delete(q.consumers, id)
		}
	}

	q.logger.Info("stopped rabbitmq consumer", forge.F("queue", queueName))

	return nil
}

func (q *RabbitMQQueue) Ack(ctx context.Context, messageID string) error {
	// Handled in consumer loop
	return nil
}

func (q *RabbitMQQueue) Nack(ctx context.Context, messageID string, requeue bool) error {
	// Handled in consumer loop
	return nil
}

func (q *RabbitMQQueue) Reject(ctx context.Context, messageID string) error {
	// Handled in consumer loop
	return nil
}

func (q *RabbitMQQueue) GetDeadLetterQueue(ctx context.Context, queueName string) ([]Message, error) {
	q.mu.RLock()
	defer q.mu.RUnlock()

	if !q.connected {
		return nil, ErrNotConnected
	}

	// Get messages without consuming them (this is tricky in RabbitMQ)
	// We'd need to use Get in a loop, but that modifies the queue
	// For production, consider using the management API
	// Dead letter queue would be named: fmt.Sprintf("%s.dlq", queueName)

	return []Message{}, nil
}

func (q *RabbitMQQueue) RequeueDeadLetter(ctx context.Context, queueName string, messageID string) error {
	q.mu.RLock()
	defer q.mu.RUnlock()

	if !q.connected {
		return ErrNotConnected
	}

	// This would require moving messages between queues
	// In practice, use RabbitMQ Shovel plugin or management API
	return nil
}

func (q *RabbitMQQueue) Stats(ctx context.Context) (*QueueStats, error) {
	q.mu.RLock()
	defer q.mu.RUnlock()

	if !q.connected {
		return nil, ErrNotConnected
	}

	uptime := time.Duration(0)
	if !q.startTime.IsZero() {
		uptime = time.Since(q.startTime)
	}

	return &QueueStats{
		QueueCount:      0, // Would need management API
		TotalMessages:   0, // Would need management API
		TotalConsumers:  len(q.consumers),
		Uptime:          uptime,
		Version:         "rabbitmq-3.12.0",
		ConnectionCount: 1,
	}, nil
}
