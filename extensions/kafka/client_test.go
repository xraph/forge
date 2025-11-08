package kafka

import (
	"context"
	"testing"
	"time"

	"github.com/IBM/sarama"
	"github.com/xraph/forge"
	"github.com/xraph/forge/internal/logger"
)

func TestNewKafkaClientInvalidConfig(t *testing.T) {
	config := DefaultConfig()
	config.Version = "invalid"
	log := logger.NewNoopLogger()
	met := forge.NewNoOpMetrics()

	_, err := NewKafkaClient(config, log, met)
	if err == nil {
		t.Error("expected error with invalid version")
	}
}

func TestBuildTLSConfigSkipVerify(t *testing.T) {
	config := DefaultConfig()
	config.EnableTLS = true
	config.TLSSkipVerify = true

	tlsConfig, err := buildTLSConfig(config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !tlsConfig.InsecureSkipVerify {
		t.Error("expected InsecureSkipVerify to be true")
	}
}

func TestBuildTLSConfigWithCerts(t *testing.T) {
	config := DefaultConfig()
	config.EnableTLS = true
	config.TLSCertFile = "/nonexistent/cert.pem"
	config.TLSKeyFile = "/nonexistent/key.pem"
	config.TLSSkipVerify = false

	_, err := buildTLSConfig(config)
	if err == nil {
		t.Error("expected error with nonexistent cert files")
	}
}

func TestBuildTLSConfigWithCA(t *testing.T) {
	config := DefaultConfig()
	config.EnableTLS = true
	config.TLSSkipVerify = true
	config.TLSCAFile = "/nonexistent/ca.pem"

	_, err := buildTLSConfig(config)
	if err == nil {
		t.Error("expected error with nonexistent CA file")
	}
}

func TestSendMessageNotEnabled(t *testing.T) {
	config := DefaultConfig()
	config.ProducerEnabled = false
	log := logger.NewNoopLogger()
	met := forge.NewNoOpMetrics()

	// Create mock client without producer
	client := &kafkaClient{
		config:  config,
		logger:  log,
		metrics: met,
		stats:   ClientStats{},
	}

	err := client.SendMessage("test-topic", []byte("key"), []byte("value"))
	if err == nil {
		t.Error("expected error when producer not enabled")
	}
}

func TestSendMessageAsyncNotEnabled(t *testing.T) {
	config := DefaultConfig()
	config.ProducerEnabled = false
	log := logger.NewNoopLogger()
	met := forge.NewNoOpMetrics()

	client := &kafkaClient{
		config:  config,
		logger:  log,
		metrics: met,
		stats:   ClientStats{},
	}

	err := client.SendMessageAsync("test-topic", []byte("key"), []byte("value"))
	if err == nil {
		t.Error("expected error when async producer not enabled")
	}
}

func TestSendMessagesNotEnabled(t *testing.T) {
	config := DefaultConfig()
	config.ProducerEnabled = false
	log := logger.NewNoopLogger()
	met := forge.NewNoOpMetrics()

	client := &kafkaClient{
		config:  config,
		logger:  log,
		metrics: met,
		stats:   ClientStats{},
	}

	messages := []*ProducerMessage{
		{Topic: "test", Key: []byte("k"), Value: []byte("v")},
	}

	err := client.SendMessages(messages)
	if err == nil {
		t.Error("expected error when producer not enabled")
	}
}

func TestConsumeNotEnabled(t *testing.T) {
	config := DefaultConfig()
	config.ConsumerEnabled = false
	log := logger.NewNoopLogger()
	met := forge.NewNoOpMetrics()

	client := &kafkaClient{
		config:  config,
		logger:  log,
		metrics: met,
		stats:   ClientStats{},
	}

	ctx := context.Background()
	err := client.Consume(ctx, []string{"test-topic"}, nil)
	if err == nil {
		t.Error("expected error when consumer not enabled")
	}
}

func TestConsumePartitionNotEnabled(t *testing.T) {
	config := DefaultConfig()
	config.ConsumerEnabled = false
	log := logger.NewNoopLogger()
	met := forge.NewNoOpMetrics()

	client := &kafkaClient{
		config:  config,
		logger:  log,
		metrics: met,
		stats:   ClientStats{},
	}

	ctx := context.Background()
	err := client.ConsumePartition(ctx, "test-topic", 0, 0, nil)
	if err == nil {
		t.Error("expected error when consumer not enabled")
	}
}

func TestStopConsume(t *testing.T) {
	config := DefaultConfig()
	log := logger.NewNoopLogger()
	met := forge.NewNoOpMetrics()

	client := &kafkaClient{
		config:  config,
		logger:  log,
		metrics: met,
		stats:   ClientStats{},
	}

	// Should not error even when not consuming
	err := client.StopConsume()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestStopConsumeWithCancel(t *testing.T) {
	config := DefaultConfig()
	log := logger.NewNoopLogger()
	met := forge.NewNoOpMetrics()

	client := &kafkaClient{
		config:  config,
		logger:  log,
		metrics: met,
		stats:   ClientStats{},
	}

	// Set a cancel function
	ctx, cancel := context.WithCancel(context.Background())
	client.cancelConsume = cancel

	err := client.StopConsume()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Verify context is canceled
	select {
	case <-ctx.Done():
		// Expected
	case <-time.After(100 * time.Millisecond):
		t.Error("expected context to be canceled")
	}
}

func TestJoinConsumerGroupAlreadyInGroup(t *testing.T) {
	config := DefaultConfig()
	log := logger.NewNoopLogger()
	met := forge.NewNoOpMetrics()

	client := &kafkaClient{
		config:        config,
		logger:        log,
		metrics:       met,
		stats:         ClientStats{},
		consumerGroup: &mockConsumerGroup{},
	}

	ctx := context.Background()
	err := client.JoinConsumerGroup(ctx, "group", []string{"topic"}, &testConsumerGroupHandler{})
	if err == nil {
		t.Error("expected error when already in consumer group")
	}
}

func TestLeaveConsumerGroupNotInGroup(t *testing.T) {
	config := DefaultConfig()
	log := logger.NewNoopLogger()
	met := forge.NewNoOpMetrics()

	client := &kafkaClient{
		config:  config,
		logger:  log,
		metrics: met,
		stats:   ClientStats{},
	}

	ctx := context.Background()
	err := client.LeaveConsumerGroup(ctx)
	if err == nil {
		t.Error("expected error when not in consumer group")
	}
}

func TestGetProducer(t *testing.T) {
	config := DefaultConfig()
	log := logger.NewNoopLogger()
	met := forge.NewNoOpMetrics()

	client := &kafkaClient{
		config:  config,
		logger:  log,
		metrics: met,
		stats:   ClientStats{},
	}

	producer := client.GetProducer()
	if producer != nil {
		t.Error("expected producer to be nil")
	}
}

func TestGetAsyncProducer(t *testing.T) {
	config := DefaultConfig()
	log := logger.NewNoopLogger()
	met := forge.NewNoOpMetrics()

	client := &kafkaClient{
		config:  config,
		logger:  log,
		metrics: met,
		stats:   ClientStats{},
	}

	producer := client.GetAsyncProducer()
	if producer != nil {
		t.Error("expected async producer to be nil")
	}
}

func TestGetConsumer(t *testing.T) {
	config := DefaultConfig()
	log := logger.NewNoopLogger()
	met := forge.NewNoOpMetrics()

	client := &kafkaClient{
		config:  config,
		logger:  log,
		metrics: met,
		stats:   ClientStats{},
	}

	consumer := client.GetConsumer()
	if consumer != nil {
		t.Error("expected consumer to be nil")
	}
}

func TestGetConsumerGroup(t *testing.T) {
	config := DefaultConfig()
	log := logger.NewNoopLogger()
	met := forge.NewNoOpMetrics()

	client := &kafkaClient{
		config:  config,
		logger:  log,
		metrics: met,
		stats:   ClientStats{},
	}

	consumerGroup := client.GetConsumerGroup()
	if consumerGroup != nil {
		t.Error("expected consumer group to be nil")
	}
}

func TestGetClient(t *testing.T) {
	config := DefaultConfig()
	log := logger.NewNoopLogger()
	met := forge.NewNoOpMetrics()

	client := &kafkaClient{
		config:  config,
		logger:  log,
		metrics: met,
		stats:   ClientStats{},
	}

	kafkaClient := client.GetClient()
	if kafkaClient != nil {
		t.Error("expected client to be nil")
	}
}

func TestGetStats(t *testing.T) {
	config := DefaultConfig()
	log := logger.NewNoopLogger()
	met := forge.NewNoOpMetrics()

	client := &kafkaClient{
		config:  config,
		logger:  log,
		metrics: met,
		stats: ClientStats{
			Connected:        true,
			MessagesSent:     100,
			MessagesReceived: 200,
		},
	}

	stats := client.GetStats()

	if !stats.Connected {
		t.Error("expected connected to be true")
	}

	if stats.MessagesSent != 100 {
		t.Errorf("expected 100 messages sent, got %d", stats.MessagesSent)
	}

	if stats.MessagesReceived != 200 {
		t.Errorf("expected 200 messages received, got %d", stats.MessagesReceived)
	}
}

func TestClose(t *testing.T) {
	config := DefaultConfig()
	log := logger.NewNoopLogger()
	met := forge.NewNoOpMetrics()

	client := &kafkaClient{
		config:  config,
		logger:  log,
		metrics: met,
		stats:   ClientStats{},
	}

	err := client.Close()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestMetricRecording(t *testing.T) {
	config := DefaultConfig()
	config.EnableMetrics = true
	log := logger.NewNoopLogger()
	met := forge.NewNoOpMetrics()

	client := &kafkaClient{
		config:  config,
		logger:  log,
		metrics: met,
		stats:   ClientStats{},
	}

	// Test metric recording methods (should not panic)
	client.recordSendMetric("test-topic", 100)
	client.recordReceiveMetric("test-topic", 200)
}

func TestMetricRecordingDisabled(t *testing.T) {
	config := DefaultConfig()
	config.EnableMetrics = false
	log := logger.NewNoopLogger()

	client := &kafkaClient{
		config:  config,
		logger:  log,
		metrics: nil,
		stats:   ClientStats{},
	}

	// Test metric recording with nil metrics (should not panic)
	client.recordSendMetric("test-topic", 100)
	client.recordReceiveMetric("test-topic", 200)
}

// Mock types for testing
type mockConsumerGroup struct{}

func (m *mockConsumerGroup) Close() error {
	return nil
}

func (m *mockConsumerGroup) Consume(ctx context.Context, topics []string, handler sarama.ConsumerGroupHandler) error {
	return nil
}

func (m *mockConsumerGroup) Errors() <-chan error {
	return make(<-chan error)
}

func (m *mockConsumerGroup) Pause(partitions map[string][]int32) {
}

func (m *mockConsumerGroup) Resume(partitions map[string][]int32) {
}

func (m *mockConsumerGroup) PauseAll() {
}

func (m *mockConsumerGroup) ResumeAll() {
}
