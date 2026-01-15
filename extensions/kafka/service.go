package kafka

import (
	"context"
	"fmt"

	"github.com/xraph/forge"
)

// KafkaService wraps a Kafka client and provides lifecycle management.
// It implements vessel's di.Service interface so Vessel can manage its lifecycle.
type KafkaService struct {
	config  Config
	client  Kafka
	logger  forge.Logger
	metrics forge.Metrics
}

// NewKafkaService creates a new Kafka service with the given configuration.
// This is the constructor that will be registered with the DI container.
func NewKafkaService(config Config, logger forge.Logger, metrics forge.Metrics) (*KafkaService, error) {
	// Validate config
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid kafka config: %w", err)
	}

	// Create Kafka client
	client, err := NewKafkaClient(config, logger, metrics)
	if err != nil {
		return nil, fmt.Errorf("failed to create kafka client: %w", err)
	}

	return &KafkaService{
		config:  config,
		client:  client,
		logger:  logger,
		metrics: metrics,
	}, nil
}

// Name returns the service name for Vessel's lifecycle management.
func (s *KafkaService) Name() string {
	return "kafka-service"
}

// Start starts the Kafka service by verifying connection.
// This is called automatically by Vessel during container.Start().
func (s *KafkaService) Start(ctx context.Context) error {
	s.logger.Info("starting kafka service",
		forge.F("brokers", s.config.Brokers),
		forge.F("client_id", s.config.ClientID),
	)

	// Verify client is healthy
	if err := s.client.Ping(ctx); err != nil {
		return fmt.Errorf("kafka client not healthy: %w", err)
	}

	s.logger.Info("kafka service started")
	return nil
}

// Stop stops the Kafka service by closing the client.
// This is called automatically by Vessel during container.Stop().
func (s *KafkaService) Stop(ctx context.Context) error {
	s.logger.Info("stopping kafka service")

	if s.client != nil {
		if err := s.client.Close(); err != nil {
			s.logger.Error("failed to close kafka client", forge.F("error", err))
			// Don't return error, log and continue
		}
	}

	s.logger.Info("kafka service stopped")
	return nil
}

// Health checks if the Kafka service is healthy.
func (s *KafkaService) Health(ctx context.Context) error {
	if s.client == nil {
		return fmt.Errorf("kafka client not initialized")
	}

	if err := s.client.Ping(ctx); err != nil {
		return fmt.Errorf("kafka health check failed: %w", err)
	}

	return nil
}

// Client returns the underlying Kafka client.
func (s *KafkaService) Client() Kafka {
	return s.client
}

// Delegate Kafka interface methods to client

func (s *KafkaService) Produce(ctx context.Context, message Message) error {
	return s.client.Produce(ctx, message)
}

func (s *KafkaService) ProduceBatch(ctx context.Context, messages []Message) error {
	return s.client.ProduceBatch(ctx, messages)
}

func (s *KafkaService) Consume(ctx context.Context, topics []string, handler MessageHandler, opts ...ConsumeOption) error {
	return s.client.Consume(ctx, topics, handler, opts...)
}

func (s *KafkaService) StopConsuming() error {
	return s.client.StopConsuming()
}

func (s *KafkaService) CreateTopic(ctx context.Context, topic string, partitions int, replicationFactor int16) error {
	return s.client.CreateTopic(ctx, topic, partitions, replicationFactor)
}

func (s *KafkaService) DeleteTopic(ctx context.Context, topic string) error {
	return s.client.DeleteTopic(ctx, topic)
}

func (s *KafkaService) ListTopics(ctx context.Context) ([]string, error) {
	return s.client.ListTopics(ctx)
}

func (s *KafkaService) GetTopicMetadata(ctx context.Context, topic string) (*TopicMetadata, error) {
	return s.client.GetTopicMetadata(ctx, topic)
}

func (s *KafkaService) Ping(ctx context.Context) error {
	return s.client.Ping(ctx)
}

func (s *KafkaService) Close() error {
	return s.client.Close()
}
