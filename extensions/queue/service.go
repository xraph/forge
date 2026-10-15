package queue

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/messaging"
)

// QueueService wraps a Queue implementation and provides lifecycle management.
// It implements vessel's di.Service interface so Vessel can manage its lifecycle.
type QueueService struct {
	config  Config
	queue   Queue
	logger  forge.Logger
	metrics forge.Metrics

	telemetry *messaging.Telemetry
}

// NewQueueService creates a new queue service with the given configuration.
// This is the constructor that will be registered with the DI container.
func NewQueueService(config Config, queue Queue, logger forge.Logger, metrics forge.Metrics) *QueueService {
	return &QueueService{
		config:  config,
		queue:   queue,
		logger:  logger,
		metrics: metrics,

		telemetry: newTelemetry(config, metrics),
	}
}

// Name returns the service name for Vessel's lifecycle management.
func (s *QueueService) Name() string {
	return "queue-service"
}

// Start starts the queue service by connecting to the backend.
// This is called automatically by Vessel during container.Start().
func (s *QueueService) Start(ctx context.Context) error {
	s.logger.Info("starting queue service",
		forge.F("driver", s.config.Driver),
	)

	if err := s.queue.Connect(ctx); err != nil {
		return fmt.Errorf("failed to connect to queue: %w", err)
	}

	s.logger.Info("queue service started",
		forge.F("driver", s.config.Driver),
	)

	return nil
}

// Stop stops the queue service by disconnecting from the backend.
// This is called automatically by Vessel during container.Stop().
func (s *QueueService) Stop(ctx context.Context) error {
	s.logger.Info("stopping queue service")

	if s.queue != nil {
		if err := s.queue.Disconnect(ctx); err != nil {
			s.logger.Error("failed to disconnect queue",
				forge.F("error", err),
			)
			// Don't return error, log and continue
		}
	}

	s.logger.Info("queue service stopped")

	return nil
}

// Health checks if the queue service is healthy.
func (s *QueueService) Health(ctx context.Context) error {
	if s.queue == nil {
		return errors.New("queue not initialized")
	}

	if err := s.queue.Ping(ctx); err != nil {
		return fmt.Errorf("queue health check failed: %w", err)
	}

	return nil
}

// Queue returns the underlying queue implementation.
func (s *QueueService) Queue() Queue {
	return s.queue
}

// Ensure QueueService implements Queue interface for convenience

func (s *QueueService) Connect(ctx context.Context) error {
	return s.queue.Connect(ctx)
}

func (s *QueueService) Disconnect(ctx context.Context) error {
	return s.queue.Disconnect(ctx)
}

func (s *QueueService) Ping(ctx context.Context) error {
	return s.queue.Ping(ctx)
}

func (s *QueueService) DeclareQueue(ctx context.Context, name string, opts QueueOptions) error {
	return s.queue.DeclareQueue(ctx, name, opts)
}

func (s *QueueService) DeleteQueue(ctx context.Context, name string) error {
	return s.queue.DeleteQueue(ctx, name)
}

func (s *QueueService) ListQueues(ctx context.Context) ([]string, error) {
	return s.queue.ListQueues(ctx)
}

func (s *QueueService) GetQueueInfo(ctx context.Context, name string) (*QueueInfo, error) {
	return s.queue.GetQueueInfo(ctx, name)
}

func (s *QueueService) PurgeQueue(ctx context.Context, name string) error {
	return s.queue.PurgeQueue(ctx, name)
}

// Publish sends a message inside a producer span and writes the trace context into a copy of
// the message headers, so the caller's map is never modified.
func (s *QueueService) Publish(ctx context.Context, queue string, message Message) error {
	ctx, op := s.telemetry.StartSend(ctx, queue, "publish")
	message.Headers = messaging.WithTraceContext(ctx, message.Headers)

	err := s.queue.Publish(ctx, queue, message)
	op.End(err)

	return err
}

// PublishBatch publishes each message in turn, so each gets its own send span.
func (s *QueueService) PublishBatch(ctx context.Context, queue string, messages []Message) error {
	if len(messages) == 0 {
		// Keep the backend's own answer for an empty batch, such as ErrNotConnected.
		return s.queue.PublishBatch(ctx, queue, messages)
	}

	for _, msg := range messages {
		if err := s.Publish(ctx, queue, msg); err != nil {
			return err
		}
	}

	return nil
}

func (s *QueueService) PublishDelayed(ctx context.Context, queue string, message Message, delay time.Duration) error {
	ctx, op := s.telemetry.StartSend(ctx, queue, "publish")
	message.Headers = messaging.WithTraceContext(ctx, message.Headers)

	err := s.queue.PublishDelayed(ctx, queue, message, delay)
	op.End(err)

	return err
}

// Consume runs every delivery inside a consumer span that continues the publisher's trace.
func (s *QueueService) Consume(ctx context.Context, queue string, handler MessageHandler, opts ConsumeOptions) error {
	return s.queue.Consume(ctx, queue, s.traceHandler(queue, handler), opts)
}

func (s *QueueService) StopConsuming(ctx context.Context, queue string) error {
	return s.queue.StopConsuming(ctx, queue)
}

func (s *QueueService) Ack(ctx context.Context, messageID string) error {
	return s.queue.Ack(ctx, messageID)
}

func (s *QueueService) Nack(ctx context.Context, messageID string, requeue bool) error {
	return s.queue.Nack(ctx, messageID, requeue)
}

func (s *QueueService) Reject(ctx context.Context, messageID string) error {
	return s.queue.Reject(ctx, messageID)
}

func (s *QueueService) GetDeadLetterQueue(ctx context.Context, queue string) ([]Message, error) {
	return s.queue.GetDeadLetterQueue(ctx, queue)
}

func (s *QueueService) RequeueDeadLetter(ctx context.Context, queue string, messageID string) error {
	return s.queue.RequeueDeadLetter(ctx, queue, messageID)
}

func (s *QueueService) Stats(ctx context.Context) (*QueueStats, error) {
	return s.queue.Stats(ctx)
}
