package kafka

import (
	"testing"
	"time"

	"github.com/IBM/sarama"
)

func TestKafkaInterface(t *testing.T) {
	// Test that our interface matches expectations
	var _ Kafka = (*kafkaClient)(nil)
}

func TestMessageHandlerType(t *testing.T) {
	handler := func(message *sarama.ConsumerMessage) error {
		return nil
	}
	
	if handler == nil {
		t.Error("handler should not be nil")
	}
}

func TestProducerMessage(t *testing.T) {
	msg := &ProducerMessage{
		Topic:     "test-topic",
		Key:       []byte("key"),
		Value:     []byte("value"),
		Partition: 0,
		Offset:    100,
		Timestamp: time.Now(),
	}

	if msg.Topic != "test-topic" {
		t.Errorf("expected topic 'test-topic', got %s", msg.Topic)
	}

	if string(msg.Key) != "key" {
		t.Errorf("expected key 'key', got %s", string(msg.Key))
	}

	if string(msg.Value) != "value" {
		t.Errorf("expected value 'value', got %s", string(msg.Value))
	}

	if msg.Partition != 0 {
		t.Errorf("expected partition 0, got %d", msg.Partition)
	}

	if msg.Offset != 100 {
		t.Errorf("expected offset 100, got %d", msg.Offset)
	}
}

func TestMessageHeader(t *testing.T) {
	header := MessageHeader{
		Key:   "Content-Type",
		Value: []byte("application/json"),
	}

	if header.Key != "Content-Type" {
		t.Errorf("expected key 'Content-Type', got %s", header.Key)
	}

	if string(header.Value) != "application/json" {
		t.Errorf("expected value 'application/json', got %s", string(header.Value))
	}
}

func TestTopicConfig(t *testing.T) {
	retention := "86400000"
	config := TopicConfig{
		NumPartitions:     3,
		ReplicationFactor: 2,
		ConfigEntries: map[string]*string{
			"retention.ms": &retention,
		},
	}

	if config.NumPartitions != 3 {
		t.Errorf("expected 3 partitions, got %d", config.NumPartitions)
	}

	if config.ReplicationFactor != 2 {
		t.Errorf("expected replication factor 2, got %d", config.ReplicationFactor)
	}

	if config.ConfigEntries["retention.ms"] == nil {
		t.Error("expected retention.ms to be set")
	}
}

func TestTopicMetadata(t *testing.T) {
	metadata := &TopicMetadata{
		Name: "test-topic",
		Partitions: []PartitionMetadata{
			{
				ID:       0,
				Leader:   1,
				Replicas: []int32{1, 2, 3},
				Isr:      []int32{1, 2},
			},
		},
		Config: map[string]string{
			"retention.ms": "86400000",
		},
	}

	if metadata.Name != "test-topic" {
		t.Errorf("expected name 'test-topic', got %s", metadata.Name)
	}

	if len(metadata.Partitions) != 1 {
		t.Errorf("expected 1 partition, got %d", len(metadata.Partitions))
	}

	if metadata.Partitions[0].ID != 0 {
		t.Errorf("expected partition ID 0, got %d", metadata.Partitions[0].ID)
	}

	if metadata.Partitions[0].Leader != 1 {
		t.Errorf("expected leader 1, got %d", metadata.Partitions[0].Leader)
	}

	if len(metadata.Partitions[0].Replicas) != 3 {
		t.Errorf("expected 3 replicas, got %d", len(metadata.Partitions[0].Replicas))
	}
}

func TestPartitionMetadata(t *testing.T) {
	partition := PartitionMetadata{
		ID:       0,
		Leader:   1,
		Replicas: []int32{1, 2, 3},
		Isr:      []int32{1, 2},
	}

	if partition.ID != 0 {
		t.Errorf("expected ID 0, got %d", partition.ID)
	}

	if partition.Leader != 1 {
		t.Errorf("expected leader 1, got %d", partition.Leader)
	}

	if len(partition.Replicas) != 3 {
		t.Errorf("expected 3 replicas, got %d", len(partition.Replicas))
	}

	if len(partition.Isr) != 2 {
		t.Errorf("expected 2 ISR, got %d", len(partition.Isr))
	}
}

func TestClientStats(t *testing.T) {
	stats := ClientStats{
		Connected:        true,
		ConnectTime:      time.Now(),
		MessagesSent:     100,
		MessagesReceived: 200,
		BytesSent:        10000,
		BytesReceived:    20000,
		Errors:           5,
		ActiveConsumers:  2,
		ActiveProducers:  1,
	}

	if !stats.Connected {
		t.Error("expected connected to be true")
	}

	if stats.MessagesSent != 100 {
		t.Errorf("expected 100 messages sent, got %d", stats.MessagesSent)
	}

	if stats.MessagesReceived != 200 {
		t.Errorf("expected 200 messages received, got %d", stats.MessagesReceived)
	}

	if stats.BytesSent != 10000 {
		t.Errorf("expected 10000 bytes sent, got %d", stats.BytesSent)
	}

	if stats.BytesReceived != 20000 {
		t.Errorf("expected 20000 bytes received, got %d", stats.BytesReceived)
	}

	if stats.Errors != 5 {
		t.Errorf("expected 5 errors, got %d", stats.Errors)
	}

	if stats.ActiveConsumers != 2 {
		t.Errorf("expected 2 active consumers, got %d", stats.ActiveConsumers)
	}

	if stats.ActiveProducers != 1 {
		t.Errorf("expected 1 active producer, got %d", stats.ActiveProducers)
	}
}

func TestConsumerGroupHandlerInterface(t *testing.T) {
	// Test that we can implement the interface
	var _ ConsumerGroupHandler = (*testConsumerGroupHandler)(nil)
}

type testConsumerGroupHandler struct{}

func (h *testConsumerGroupHandler) Setup(sarama.ConsumerGroupSession) error {
	return nil
}

func (h *testConsumerGroupHandler) Cleanup(sarama.ConsumerGroupSession) error {
	return nil
}

func (h *testConsumerGroupHandler) ConsumeClaim(sarama.ConsumerGroupSession, sarama.ConsumerGroupClaim) error {
	return nil
}
