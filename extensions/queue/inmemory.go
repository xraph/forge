package queue

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge"
)

// InMemoryQueue implements Queue interface with an in-memory store
type InMemoryQueue struct {
	config    Config
	logger    forge.Logger
	metrics   forge.Metrics
	connected bool
	queues    map[string]*memoryQueue
	consumers map[string]*consumer
	mu        sync.RWMutex
	startTime time.Time
}

type memoryQueue struct {
	name       string
	opts       QueueOptions
	messages   []Message
	dlq        []Message
	inflight   map[string]Message // Track messages being processed
	createdAt  time.Time
	mu         sync.RWMutex
}

type consumer struct {
	queue   string
	handler MessageHandler
	opts    ConsumeOptions
	cancel  context.CancelFunc
	active  bool
}

// NewInMemoryQueue creates a new in-memory queue instance
func NewInMemoryQueue(config Config, logger forge.Logger, metrics forge.Metrics) *InMemoryQueue {
	return &InMemoryQueue{
		config:    config,
		logger:    logger,
		metrics:   metrics,
		queues:    make(map[string]*memoryQueue),
		consumers: make(map[string]*consumer),
	}
}

func (q *InMemoryQueue) Connect(ctx context.Context) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.connected {
		return ErrAlreadyConnected
	}

	q.connected = true
	q.startTime = time.Now()
	q.logger.Info("connected to in-memory queue")
	return nil
}

func (q *InMemoryQueue) Disconnect(ctx context.Context) error {
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

	q.connected = false
	q.queues = make(map[string]*memoryQueue)
	q.consumers = make(map[string]*consumer)
	q.logger.Info("disconnected from in-memory queue")
	return nil
}

func (q *InMemoryQueue) Ping(ctx context.Context) error {
	q.mu.RLock()
	defer q.mu.RUnlock()

	if !q.connected {
		return ErrNotConnected
	}
	return nil
}

func (q *InMemoryQueue) DeclareQueue(ctx context.Context, name string, opts QueueOptions) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if !q.connected {
		return ErrNotConnected
	}

	if _, exists := q.queues[name]; exists {
		return ErrQueueAlreadyExists
	}

	q.queues[name] = &memoryQueue{
		name:      name,
		opts:      opts,
		messages:  make([]Message, 0),
		dlq:       make([]Message, 0),
		inflight:  make(map[string]Message),
		createdAt: time.Now(),
	}

	q.logger.Info("declared queue", forge.F("queue", name))
	return nil
}

func (q *InMemoryQueue) DeleteQueue(ctx context.Context, name string) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if !q.connected {
		return ErrNotConnected
	}

	if _, exists := q.queues[name]; !exists {
		return ErrQueueNotFound
	}

	delete(q.queues, name)
	q.logger.Info("deleted queue", forge.F("queue", name))
	return nil
}

func (q *InMemoryQueue) ListQueues(ctx context.Context) ([]string, error) {
	q.mu.RLock()
	defer q.mu.RUnlock()

	if !q.connected {
		return nil, ErrNotConnected
	}

	names := make([]string, 0, len(q.queues))
	for name := range q.queues {
		names = append(names, name)
	}
	return names, nil
}

func (q *InMemoryQueue) GetQueueInfo(ctx context.Context, name string) (*QueueInfo, error) {
	q.mu.RLock()
	mq, exists := q.queues[name]
	q.mu.RUnlock()

	if !q.connected {
		return nil, ErrNotConnected
	}

	if !exists {
		return nil, ErrQueueNotFound
	}

	mq.mu.RLock()
	defer mq.mu.RUnlock()

	return &QueueInfo{
		Name:       name,
		Messages:   int64(len(mq.messages)),
		Consumers:  0,
		Durable:    mq.opts.Durable,
		AutoDelete: mq.opts.AutoDelete,
		CreatedAt:  mq.createdAt,
	}, nil
}

func (q *InMemoryQueue) PurgeQueue(ctx context.Context, name string) error {
	q.mu.RLock()
	mq, exists := q.queues[name]
	q.mu.RUnlock()

	if !q.connected {
		return ErrNotConnected
	}

	if !exists {
		return ErrQueueNotFound
	}

	mq.mu.Lock()
	mq.messages = make([]Message, 0)
	mq.mu.Unlock()

	q.logger.Info("purged queue", forge.F("queue", name))
	return nil
}

func (q *InMemoryQueue) Publish(ctx context.Context, queueName string, message Message) error {
	q.mu.RLock()
	mq, exists := q.queues[queueName]
	q.mu.RUnlock()

	if !q.connected {
		return ErrNotConnected
	}

	if !exists {
		return ErrQueueNotFound
	}

	message.ID = fmt.Sprintf("%d", time.Now().UnixNano())
	message.Queue = queueName
	message.PublishedAt = time.Now()

	mq.mu.Lock()
	mq.messages = append(mq.messages, message)
	mq.mu.Unlock()

	q.logger.Debug("published message", forge.F("queue", queueName), forge.F("msg_id", message.ID))
	return nil
}

func (q *InMemoryQueue) PublishBatch(ctx context.Context, queueName string, messages []Message) error {
	for _, msg := range messages {
		if err := q.Publish(ctx, queueName, msg); err != nil {
			return err
		}
	}
	return nil
}

func (q *InMemoryQueue) PublishDelayed(ctx context.Context, queueName string, message Message, delay time.Duration) error {
	// Simple implementation: just publish after delay
	time.Sleep(delay)
	return q.Publish(ctx, queueName, message)
}

func (q *InMemoryQueue) Consume(ctx context.Context, queueName string, handler MessageHandler, opts ConsumeOptions) error {
	q.mu.RLock()
	mq, exists := q.queues[queueName]
	q.mu.RUnlock()

	if !q.connected {
		return ErrNotConnected
	}

	if !exists {
		return ErrQueueNotFound
	}

	consumerCtx, cancel := context.WithCancel(ctx)
	consumerID := fmt.Sprintf("%s-%d", queueName, time.Now().UnixNano())

	q.mu.Lock()
	q.consumers[consumerID] = &consumer{
		queue:   queueName,
		handler: handler,
		opts:    opts,
		cancel:  cancel,
		active:  true,
	}
	q.mu.Unlock()

	// Start consumer goroutines based on concurrency setting
	concurrency := opts.Concurrency
	if concurrency == 0 {
		concurrency = 1
	}

	for i := 0; i < concurrency; i++ {
		go func() {
			for {
				select {
				case <-consumerCtx.Done():
					return
				default:
					// Try to get a message
					mq.mu.Lock()
					if len(mq.messages) == 0 {
						mq.mu.Unlock()
						// No messages, wait a bit before checking again
						time.Sleep(10 * time.Millisecond)
						continue
					}

					msg := mq.messages[0]
					mq.messages = mq.messages[1:]
					
					// Track message in flight
					if !opts.AutoAck {
						mq.inflight[msg.ID] = msg
					}
					mq.mu.Unlock()

					// Process message
					if err := handler(consumerCtx, msg); err != nil {
						q.logger.Error("message handler failed",
							forge.F("queue", queueName),
							forge.F("msg_id", msg.ID),
							forge.F("error", err),
						)
						
						mq.mu.Lock()
						// Remove from inflight if AutoAck
						if opts.AutoAck {
							// Move to DLQ on error
							if q.config.EnableDeadLetter {
								mq.dlq = append(mq.dlq, msg)
							}
						} else {
							// Remove from inflight and move to DLQ
							delete(mq.inflight, msg.ID)
							if q.config.EnableDeadLetter {
								mq.dlq = append(mq.dlq, msg)
							}
						}
						mq.mu.Unlock()
					} else {
						// Success - auto-ack if enabled
						if opts.AutoAck {
							// Message successfully processed, no further action needed
						}
						// If not AutoAck, message stays in inflight for manual Ack/Nack
					}
				}
			}
		}()
	}

	q.logger.Info("started consumer", forge.F("queue", queueName))
	return nil
}

func (q *InMemoryQueue) StopConsuming(ctx context.Context, queueName string) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	for id, c := range q.consumers {
		if c.queue == queueName && c.active {
			if c.cancel != nil {
				c.cancel()
			}
			c.active = false
			delete(q.consumers, id)
		}
	}

	q.logger.Info("stopped consumer", forge.F("queue", queueName))
	return nil
}

func (q *InMemoryQueue) Ack(ctx context.Context, messageID string) error {
	q.mu.RLock()
	defer q.mu.RUnlock()

	// Find which queue has this message in flight
	for _, mq := range q.queues {
		mq.mu.Lock()
		if _, exists := mq.inflight[messageID]; exists {
			delete(mq.inflight, messageID)
			mq.mu.Unlock()
			return nil
		}
		mq.mu.Unlock()
	}

	return ErrMessageNotFound
}

func (q *InMemoryQueue) Nack(ctx context.Context, messageID string, requeue bool) error {
	q.mu.RLock()
	defer q.mu.RUnlock()

	// Find which queue has this message in flight
	for _, mq := range q.queues {
		mq.mu.Lock()
		msg, exists := mq.inflight[messageID]
		if exists {
			delete(mq.inflight, messageID)
			if requeue {
				// Put back at the front of the queue
				mq.messages = append([]Message{msg}, mq.messages...)
			} else {
				// Move to DLQ
				mq.dlq = append(mq.dlq, msg)
			}
			mq.mu.Unlock()
			return nil
		}
		mq.mu.Unlock()
	}

	return ErrMessageNotFound
}

func (q *InMemoryQueue) Reject(ctx context.Context, messageID string) error {
	q.mu.RLock()
	defer q.mu.RUnlock()

	// Find which queue has this message in flight
	for _, mq := range q.queues {
		mq.mu.Lock()
		msg, exists := mq.inflight[messageID]
		if exists {
			delete(mq.inflight, messageID)
			// Move to DLQ
			mq.dlq = append(mq.dlq, msg)
			mq.mu.Unlock()
			return nil
		}
		mq.mu.Unlock()
	}

	return ErrMessageNotFound
}

func (q *InMemoryQueue) GetDeadLetterQueue(ctx context.Context, queueName string) ([]Message, error) {
	q.mu.RLock()
	mq, exists := q.queues[queueName]
	q.mu.RUnlock()

	if !q.connected {
		return nil, ErrNotConnected
	}

	if !exists {
		return nil, ErrQueueNotFound
	}

	mq.mu.RLock()
	defer mq.mu.RUnlock()

	return mq.dlq, nil
}

func (q *InMemoryQueue) RequeueDeadLetter(ctx context.Context, queueName string, messageID string) error {
	q.mu.RLock()
	mq, exists := q.queues[queueName]
	q.mu.RUnlock()

	if !q.connected {
		return ErrNotConnected
	}

	if !exists {
		return ErrQueueNotFound
	}

	mq.mu.Lock()
	defer mq.mu.Unlock()

	for i, msg := range mq.dlq {
		if msg.ID == messageID {
			mq.messages = append(mq.messages, msg)
			mq.dlq = append(mq.dlq[:i], mq.dlq[i+1:]...)
			return nil
		}
	}

	return ErrMessageNotFound
}

func (q *InMemoryQueue) Stats(ctx context.Context) (*QueueStats, error) {
	q.mu.RLock()
	defer q.mu.RUnlock()

	if !q.connected {
		return nil, ErrNotConnected
	}

	totalMessages := int64(0)
	for _, mq := range q.queues {
		mq.mu.RLock()
		totalMessages += int64(len(mq.messages))
		mq.mu.RUnlock()
	}

	uptime := time.Duration(0)
	if !q.startTime.IsZero() {
		uptime = time.Since(q.startTime)
	}

	return &QueueStats{
		QueueCount:      int64(len(q.queues)),
		TotalMessages:   totalMessages,
		TotalConsumers:  len(q.consumers),
		Uptime:          uptime,
		Version:         "inmemory-1.0.0",
		ConnectionCount: 1,
	}, nil
}
