package kafka

import (
	"context"
	"fmt"

	"github.com/IBM/sarama"
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
func (s *KafkaService) Stop(_ context.Context) error {
	s.logger.Info("stopping kafka service")

	if s.client != nil {
		if err := s.client.Close(); err != nil {
			s.logger.Error("failed to close kafka client", forge.F("error", err))
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

func (s *KafkaService) SendMessage(topic string, key, value []byte) error {
	return s.client.SendMessage(topic, key, value)
}

func (s *KafkaService) SendMessageAsync(topic string, key, value []byte) error {
	return s.client.SendMessageAsync(topic, key, value)
}

func (s *KafkaService) SendMessages(messages []*ProducerMessage) error {
	return s.client.SendMessages(messages)
}

func (s *KafkaService) Consume(ctx context.Context, topics []string, handler MessageHandler) error {
	return s.client.Consume(ctx, topics, handler)
}

func (s *KafkaService) ConsumePartition(ctx context.Context, topic string, partition int32, offset int64, handler MessageHandler) error {
	return s.client.ConsumePartition(ctx, topic, partition, offset, handler)
}

func (s *KafkaService) StopConsume() error {
	return s.client.StopConsume()
}

func (s *KafkaService) JoinConsumerGroup(ctx context.Context, groupID string, topics []string, handler ConsumerGroupHandler) error {
	return s.client.JoinConsumerGroup(ctx, groupID, topics, handler)
}

func (s *KafkaService) LeaveConsumerGroup(ctx context.Context) error {
	return s.client.LeaveConsumerGroup(ctx)
}

func (s *KafkaService) CreateTopic(topic string, config TopicConfig) error {
	return s.client.CreateTopic(topic, config)
}

func (s *KafkaService) DeleteTopic(topic string) error {
	return s.client.DeleteTopic(topic)
}

func (s *KafkaService) ListTopics() ([]string, error) {
	return s.client.ListTopics()
}

func (s *KafkaService) DescribeTopic(topic string) (*TopicMetadata, error) {
	return s.client.DescribeTopic(topic)
}

func (s *KafkaService) GetPartitions(topic string) ([]int32, error) {
	return s.client.GetPartitions(topic)
}

func (s *KafkaService) GetOffset(topic string, partition int32, time int64) (int64, error) {
	return s.client.GetOffset(topic, partition, time)
}

func (s *KafkaService) GetProducer() sarama.SyncProducer {
	return s.client.GetProducer()
}

func (s *KafkaService) GetAsyncProducer() sarama.AsyncProducer {
	return s.client.GetAsyncProducer()
}

func (s *KafkaService) GetConsumer() sarama.Consumer {
	return s.client.GetConsumer()
}

func (s *KafkaService) GetConsumerGroup() sarama.ConsumerGroup {
	return s.client.GetConsumerGroup()
}

func (s *KafkaService) GetClient() sarama.Client {
	return s.client.GetClient()
}

func (s *KafkaService) GetStats() ClientStats {
	return s.client.GetStats()
}

func (s *KafkaService) Ping(ctx context.Context) error {
	return s.client.Ping(ctx)
}

func (s *KafkaService) Close() error {
	return s.client.Close()
}
