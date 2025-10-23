package kafka

import (
	"context"
	"time"

	"github.com/IBM/sarama"
)

// Kafka represents a unified Kafka client interface
type Kafka interface {
	// Producer operations
	SendMessage(topic string, key, value []byte) error
	SendMessageAsync(topic string, key, value []byte) error
	SendMessages(messages []*ProducerMessage) error

	// Consumer operations
	Consume(ctx context.Context, topics []string, handler MessageHandler) error
	ConsumePartition(ctx context.Context, topic string, partition int32, offset int64, handler MessageHandler) error
	StopConsume() error

	// Consumer group operations
	JoinConsumerGroup(ctx context.Context, groupID string, topics []string, handler ConsumerGroupHandler) error
	LeaveConsumerGroup(ctx context.Context) error

	// Admin operations
	CreateTopic(topic string, config TopicConfig) error
	DeleteTopic(topic string) error
	ListTopics() ([]string, error)
	DescribeTopic(topic string) (*TopicMetadata, error)
	GetPartitions(topic string) ([]int32, error)
	GetOffset(topic string, partition int32, time int64) (int64, error)

	// Client info
	GetProducer() sarama.SyncProducer
	GetAsyncProducer() sarama.AsyncProducer
	GetConsumer() sarama.Consumer
	GetConsumerGroup() sarama.ConsumerGroup
	GetClient() sarama.Client
	GetStats() ClientStats

	// Lifecycle
	Close() error

	// Health
	Ping(ctx context.Context) error
}

// MessageHandler processes incoming Kafka messages
type MessageHandler func(message *sarama.ConsumerMessage) error

// ConsumerGroupHandler handles consumer group messages
type ConsumerGroupHandler interface {
	Setup(sarama.ConsumerGroupSession) error
	Cleanup(sarama.ConsumerGroupSession) error
	ConsumeClaim(sarama.ConsumerGroupSession, sarama.ConsumerGroupClaim) error
}

// ProducerMessage represents a message to be produced
type ProducerMessage struct {
	Topic     string
	Key       []byte
	Value     []byte
	Headers   []MessageHeader
	Partition int32
	Offset    int64
	Timestamp time.Time
}

// MessageHeader represents a Kafka message header
type MessageHeader struct {
	Key   string
	Value []byte
}

// TopicConfig contains topic configuration
type TopicConfig struct {
	NumPartitions     int32
	ReplicationFactor int16
	ConfigEntries     map[string]*string
}

// TopicMetadata contains topic metadata
type TopicMetadata struct {
	Name       string
	Partitions []PartitionMetadata
	Config     map[string]string
}

// PartitionMetadata contains partition metadata
type PartitionMetadata struct {
	ID       int32
	Leader   int32
	Replicas []int32
	Isr      []int32
}

// ClientStats contains client statistics
type ClientStats struct {
	Connected        bool
	ConnectTime      time.Time
	MessagesSent     int64
	MessagesReceived int64
	BytesSent        int64
	BytesReceived    int64
	Errors           int64
	LastError        error
	LastErrorTime    time.Time
	ActiveConsumers  int
	ActiveProducers  int
}
