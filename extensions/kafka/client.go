package kafka

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/IBM/sarama"
	"github.com/xdg-go/scram"
	"go.opentelemetry.io/otel/trace"

	"github.com/xraph/forge"
	"github.com/xraph/forge/messaging"
)

// kafkaClient implements Kafka interface.
type kafkaClient struct {
	config        Config
	logger        forge.Logger
	metrics       forge.Metrics
	telemetry     *messaging.Telemetry
	client        sarama.Client
	producer      sarama.SyncProducer
	asyncProducer sarama.AsyncProducer
	consumer      sarama.Consumer
	consumerGroup sarama.ConsumerGroup
	stats         ClientStats
	mu            sync.RWMutex
	cancelConsume context.CancelFunc
}

// NewKafkaClient creates a new Kafka client.
func NewKafkaClient(config Config, logger forge.Logger, metrics forge.Metrics) (Kafka, error) {
	saramaConfig, err := config.ToSaramaConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to create sarama config: %w", err)
	}

	// Setup TLS if enabled
	if config.EnableTLS {
		tlsConfig, err := buildTLSConfig(config)
		if err != nil {
			return nil, fmt.Errorf("failed to build TLS config: %w", err)
		}

		saramaConfig.Net.TLS.Enable = true
		saramaConfig.Net.TLS.Config = tlsConfig
	}

	// Setup SASL if enabled
	if config.EnableSASL {
		saramaConfig.Net.SASL.Enable = true
		saramaConfig.Net.SASL.User = config.SASLUsername
		saramaConfig.Net.SASL.Password = config.SASLPassword

		switch config.SASLMechanism {
		case "PLAIN":
			saramaConfig.Net.SASL.Mechanism = sarama.SASLTypePlaintext
		case "SCRAM-SHA-256":
			saramaConfig.Net.SASL.Mechanism = sarama.SASLTypeSCRAMSHA256
			saramaConfig.Net.SASL.SCRAMClientGeneratorFunc = func() sarama.SCRAMClient {
				return &XDGSCRAMClient{HashGeneratorFcn: scram.HashGeneratorFcn(SHA256)}
			}
		case "SCRAM-SHA-512":
			saramaConfig.Net.SASL.Mechanism = sarama.SASLTypeSCRAMSHA512
			saramaConfig.Net.SASL.SCRAMClientGeneratorFunc = func() sarama.SCRAMClient {
				return &XDGSCRAMClient{HashGeneratorFcn: scram.HashGeneratorFcn(SHA512)}
			}
		}
	}

	// Create client
	client, err := sarama.NewClient(config.Brokers, saramaConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create kafka client: %w", err)
	}

	kc := &kafkaClient{
		config:    config,
		logger:    logger,
		metrics:   metrics,
		telemetry: newTelemetry(config, metrics),
		client:    client,
		stats: ClientStats{
			Connected:   true,
			ConnectTime: time.Now(),
		},
	}

	// Create producer if enabled
	if config.ProducerEnabled {
		producer, err := sarama.NewSyncProducerFromClient(client)
		if err != nil {
			client.Close()

			return nil, fmt.Errorf("failed to create producer: %w", err)
		}

		kc.producer = producer
		kc.stats.ActiveProducers++

		asyncProducer, err := sarama.NewAsyncProducerFromClient(client)
		if err != nil {
			producer.Close()
			client.Close()

			return nil, fmt.Errorf("failed to create async producer: %w", err)
		}

		kc.asyncProducer = asyncProducer

		// Handle async producer errors
		go kc.handleAsyncProducerErrors()
	}

	// Create consumer if enabled
	if config.ConsumerEnabled {
		consumer, err := sarama.NewConsumerFromClient(client)
		if err != nil {
			if kc.producer != nil {
				kc.producer.Close()
			}

			if kc.asyncProducer != nil {
				kc.asyncProducer.Close()
			}

			client.Close()

			return nil, fmt.Errorf("failed to create consumer: %w", err)
		}

		kc.consumer = consumer
		kc.stats.ActiveConsumers++
	}

	logger.Info("kafka client created",
		forge.F("brokers", config.Brokers),
		forge.F("client_id", config.ClientID),
	)

	return kc, nil
}

func (c *kafkaClient) SendMessage(topic string, key, value []byte) error {
	return c.SendMessageContext(context.Background(), topic, key, value)
}

// SendMessageContext sends one message and waits for the broker to acknowledge it.
func (c *kafkaClient) SendMessageContext(ctx context.Context, topic string, key, value []byte) error {
	if c.producer == nil {
		return errors.New("producer not enabled")
	}

	msg := &sarama.ProducerMessage{
		Topic: topic,
		Key:   sarama.ByteEncoder(key),
		Value: sarama.ByteEncoder(value),
	}

	ctx, op := c.telemetry.StartSend(ctx, topic, "send")
	messaging.Inject(ctx, ProducerHeaderCarrier(msg))

	partition, offset, err := c.producer.SendMessage(msg)
	op.End(err)

	if err != nil {
		c.mu.Lock()
		c.stats.Errors++
		c.stats.LastError = err
		c.stats.LastErrorTime = time.Now()
		c.mu.Unlock()

		if c.config.EnableLogging {
			c.logger.Error("kafka send message failed", forge.F("topic", topic), forge.F("error", err))
		}

		return fmt.Errorf("send message failed: %w", err)
	}

	c.mu.Lock()
	c.stats.MessagesSent++
	c.stats.BytesSent += int64(len(value))
	c.mu.Unlock()

	if c.config.EnableMetrics {
		c.recordSendMetric(topic, len(value))
	}

	if c.config.EnableLogging {
		c.logger.Debug("kafka message sent",
			forge.F("topic", topic),
			forge.F("partition", partition),
			forge.F("offset", offset),
		)
	}

	return nil
}

func (c *kafkaClient) SendMessageAsync(topic string, key, value []byte) error {
	return c.SendMessageAsyncContext(context.Background(), topic, key, value)
}

// SendMessageAsyncContext queues one message on the async producer.
func (c *kafkaClient) SendMessageAsyncContext(ctx context.Context, topic string, key, value []byte) error {
	if c.asyncProducer == nil {
		return errors.New("async producer not enabled")
	}

	msg := &sarama.ProducerMessage{
		Topic: topic,
		Key:   sarama.ByteEncoder(key),
		Value: sarama.ByteEncoder(value),
	}

	ctx, op := c.telemetry.StartSend(ctx, topic, "send")
	messaging.Inject(ctx, ProducerHeaderCarrier(msg))

	c.asyncProducer.Input() <- msg
	// The span covers the hand-off to the producer only. Delivery errors arrive
	// on the async producer's own error channel, after this call has returned.
	op.End(nil)

	c.mu.Lock()
	c.stats.MessagesSent++
	c.stats.BytesSent += int64(len(value))
	c.mu.Unlock()

	if c.config.EnableMetrics {
		c.recordSendMetric(topic, len(value))
	}

	return nil
}

func (c *kafkaClient) SendMessages(messages []*ProducerMessage) error {
	return c.SendMessagesContext(context.Background(), messages)
}

// SendMessagesContext sends a batch of messages, including their headers and timestamps.
func (c *kafkaClient) SendMessagesContext(ctx context.Context, messages []*ProducerMessage) error {
	if c.producer == nil {
		return errors.New("producer not enabled")
	}

	saramaMessages := make([]*sarama.ProducerMessage, len(messages))
	links := make([]trace.Link, 0, len(messages))

	for i, msg := range messages {
		saramaMessages[i] = &sarama.ProducerMessage{
			Topic:     msg.Topic,
			Key:       sarama.ByteEncoder(msg.Key),
			Value:     sarama.ByteEncoder(msg.Value),
			Headers:   toRecordHeaders(msg.Headers),
			Partition: msg.Partition,
			Timestamp: msg.Timestamp,
		}

		// Each record gets its own create span, so a consumer's trace parents
		// on the record it read and not on the whole batch.
		createCtx, create := c.telemetry.StartCreate(ctx, msg.Topic)
		messaging.Inject(createCtx, ProducerHeaderCarrier(saramaMessages[i]))
		create.End(nil)

		if sc := trace.SpanContextFromContext(createCtx); sc.IsValid() && sc.SpanID() != trace.SpanContextFromContext(ctx).SpanID() {
			links = append(links, trace.Link{SpanContext: sc})
		}
	}

	// An empty batch sends nothing, so it gets no span and no observation. A nil
	// operation is safe to End.
	var op *messaging.Operation

	if len(messages) > 0 {
		_, op = c.telemetry.StartSend(ctx, batchDestination(messages), "send",
			messaging.WithBatchSize(len(messages)), messaging.WithLinks(links...))
	}

	err := c.producer.SendMessages(saramaMessages)
	op.End(err)

	if err != nil {
		c.mu.Lock()
		c.stats.Errors++
		c.stats.LastError = err
		c.stats.LastErrorTime = time.Now()
		c.mu.Unlock()

		return fmt.Errorf("send messages failed: %w", err)
	}

	c.mu.Lock()
	c.stats.MessagesSent += int64(len(messages))
	c.mu.Unlock()

	return nil
}

func (c *kafkaClient) Consume(ctx context.Context, topics []string, handler MessageHandler) error {
	return c.ConsumeContext(ctx, topics, dropContext(handler))
}

func (c *kafkaClient) ConsumePartition(ctx context.Context, topic string, partition int32, offset int64, handler MessageHandler) error {
	return c.ConsumePartitionContext(ctx, topic, partition, offset, dropContext(handler))
}

// dropContext adapts a handler that takes no context.
func dropContext(handler MessageHandler) ContextMessageHandler {
	return func(_ context.Context, msg *sarama.ConsumerMessage) error { return handler(msg) }
}

// toRecordHeaders converts headers to sarama's form. nil stays nil.
func toRecordHeaders(h []MessageHeader) []sarama.RecordHeader {
	if len(h) == 0 {
		return nil
	}

	out := make([]sarama.RecordHeader, len(h))
	for i, header := range h {
		out[i] = sarama.RecordHeader{Key: []byte(header.Key), Value: header.Value}
	}

	return out
}

// ConsumeContext starts a consumer on every partition of the topics and hands each
// message to handler with a context derived from ctx.
func (c *kafkaClient) ConsumeContext(ctx context.Context, topics []string, handler ContextMessageHandler) error {
	if c.consumer == nil {
		return errors.New("consumer not enabled")
	}

	ctx, cancel := context.WithCancel(ctx)

	c.mu.Lock()
	c.cancelConsume = cancel
	c.mu.Unlock()

	for _, topic := range topics {
		partitions, err := c.consumer.Partitions(topic)
		if err != nil {
			return fmt.Errorf("failed to get partitions for topic %s: %w", topic, err)
		}

		for _, partition := range partitions {
			pc, err := c.consumer.ConsumePartition(topic, partition, sarama.OffsetNewest)
			if err != nil {
				return fmt.Errorf("failed to start consumer for partition %d: %w", partition, err)
			}

			go c.consumePartition(ctx, pc, handler)
		}
	}

	c.logger.Info("kafka consumer started", forge.F("topics", topics))

	return nil
}

// ConsumePartitionContext starts a consumer on one partition and hands each message
// to handler with a context derived from ctx.
func (c *kafkaClient) ConsumePartitionContext(ctx context.Context, topic string, partition int32, offset int64, handler ContextMessageHandler) error {
	if c.consumer == nil {
		return errors.New("consumer not enabled")
	}

	pc, err := c.consumer.ConsumePartition(topic, partition, offset)
	if err != nil {
		return fmt.Errorf("failed to start partition consumer: %w", err)
	}

	go c.consumePartition(ctx, pc, handler)

	c.logger.Info("kafka partition consumer started",
		forge.F("topic", topic),
		forge.F("partition", partition),
	)

	return nil
}

func (c *kafkaClient) consumePartition(ctx context.Context, pc sarama.PartitionConsumer, handler ContextMessageHandler) {
	defer pc.Close()

	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-pc.Messages():
			if msg != nil {
				c.mu.Lock()
				c.stats.MessagesReceived++
				c.stats.BytesReceived += int64(len(msg.Value))
				c.mu.Unlock()

				if c.config.EnableMetrics {
					c.recordReceiveMetric(msg.Topic, len(msg.Value))
				}

				err := c.telemetry.Process(ctx, ConsumerHeaderCarrier(msg), msg.Topic, "process",
					func(ctx context.Context) error { return handler(ctx, msg) })
				if err != nil {
					c.logger.Error("kafka message handler error",
						forge.F("topic", msg.Topic),
						forge.F("partition", msg.Partition),
						forge.F("offset", msg.Offset),
						forge.F("error", err),
					)
				}
			}
		case err := <-pc.Errors():
			if err != nil {
				c.mu.Lock()
				c.stats.Errors++
				c.stats.LastError = err
				c.stats.LastErrorTime = time.Now()
				c.mu.Unlock()

				c.logger.Error("kafka consumer error", forge.F("error", err))
			}
		}
	}
}

func (c *kafkaClient) StopConsume() error {
	c.mu.Lock()
	cancel := c.cancelConsume
	c.cancelConsume = nil
	c.mu.Unlock()

	if cancel != nil {
		cancel()
	}

	c.logger.Info("kafka consumer stopped")

	return nil
}

func (c *kafkaClient) JoinConsumerGroup(ctx context.Context, groupID string, topics []string, handler ConsumerGroupHandler) error {
	if c.consumerGroup != nil {
		return errors.New("already in consumer group")
	}

	saramaConfig, err := c.config.ToSaramaConfig()
	if err != nil {
		return fmt.Errorf("failed to create config: %w", err)
	}

	cg, err := sarama.NewConsumerGroup(c.config.Brokers, groupID, saramaConfig)
	if err != nil {
		return fmt.Errorf("failed to create consumer group: %w", err)
	}

	c.mu.Lock()
	c.consumerGroup = cg
	c.mu.Unlock()

	go func() {
		for {
			if err := cg.Consume(ctx, topics, handler); err != nil {
				c.logger.Error("kafka consumer group error", forge.F("error", err))

				return
			}

			if ctx.Err() != nil {
				return
			}
		}
	}()

	c.logger.Info("kafka consumer group joined",
		forge.F("group_id", groupID),
		forge.F("topics", topics),
	)

	return nil
}

func (c *kafkaClient) LeaveConsumerGroup(ctx context.Context) error {
	c.mu.Lock()
	cg := c.consumerGroup
	c.consumerGroup = nil
	c.mu.Unlock()

	if cg == nil {
		return errors.New("not in consumer group")
	}

	if err := cg.Close(); err != nil {
		return fmt.Errorf("failed to close consumer group: %w", err)
	}

	c.logger.Info("kafka consumer group left")

	return nil
}

func (c *kafkaClient) CreateTopic(topic string, config TopicConfig) error {
	broker := c.client.Brokers()[0]
	if err := broker.Open(c.client.Config()); err != nil {
		return fmt.Errorf("failed to connect to broker: %w", err)
	}
	defer broker.Close()

	request := &sarama.CreateTopicsRequest{
		TopicDetails: map[string]*sarama.TopicDetail{
			topic: {
				NumPartitions:     config.NumPartitions,
				ReplicationFactor: config.ReplicationFactor,
				ConfigEntries:     config.ConfigEntries,
			},
		},
	}

	response, err := broker.CreateTopics(request)
	if err != nil {
		return fmt.Errorf("failed to create topic: %w", err)
	}

	if topicErr := response.TopicErrors[topic]; topicErr != nil && topicErr.Err != sarama.ErrNoError {
		return fmt.Errorf("topic creation error: %w", topicErr.Err)
	}

	c.logger.Info("kafka topic created", forge.F("topic", topic))

	return nil
}

func (c *kafkaClient) DeleteTopic(topic string) error {
	broker := c.client.Brokers()[0]
	if err := broker.Open(c.client.Config()); err != nil {
		return fmt.Errorf("failed to connect to broker: %w", err)
	}
	defer broker.Close()

	request := &sarama.DeleteTopicsRequest{
		Topics:  []string{topic},
		Timeout: 5 * time.Second,
	}

	response, err := broker.DeleteTopics(request)
	if err != nil {
		return fmt.Errorf("failed to delete topic: %w", err)
	}

	if topicErr := response.TopicErrorCodes[topic]; topicErr != sarama.ErrNoError {
		return fmt.Errorf("topic deletion error: %w", topicErr)
	}

	c.logger.Info("kafka topic deleted", forge.F("topic", topic))

	return nil
}

func (c *kafkaClient) ListTopics() ([]string, error) {
	topics, err := c.client.Topics()
	if err != nil {
		return nil, fmt.Errorf("failed to list topics: %w", err)
	}

	return topics, nil
}

func (c *kafkaClient) DescribeTopic(topic string) (*TopicMetadata, error) {
	partitions, err := c.client.Partitions(topic)
	if err != nil {
		return nil, fmt.Errorf("failed to get partitions: %w", err)
	}

	metadata := &TopicMetadata{
		Name:       topic,
		Partitions: make([]PartitionMetadata, len(partitions)),
	}

	for i, partition := range partitions {
		replicas, err := c.client.Replicas(topic, partition)
		if err != nil {
			return nil, fmt.Errorf("failed to get replicas: %w", err)
		}

		leader, err := c.client.Leader(topic, partition)
		if err != nil {
			return nil, fmt.Errorf("failed to get leader: %w", err)
		}

		metadata.Partitions[i] = PartitionMetadata{
			ID:       partition,
			Leader:   leader.ID(),
			Replicas: replicas,
		}
	}

	return metadata, nil
}

func (c *kafkaClient) GetPartitions(topic string) ([]int32, error) {
	return c.client.Partitions(topic)
}

func (c *kafkaClient) GetOffset(topic string, partition int32, time int64) (int64, error) {
	return c.client.GetOffset(topic, partition, time)
}

func (c *kafkaClient) GetProducer() sarama.SyncProducer {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.producer
}

func (c *kafkaClient) GetAsyncProducer() sarama.AsyncProducer {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.asyncProducer
}

func (c *kafkaClient) GetConsumer() sarama.Consumer {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.consumer
}

func (c *kafkaClient) GetConsumerGroup() sarama.ConsumerGroup {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.consumerGroup
}

func (c *kafkaClient) GetClient() sarama.Client {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.client
}

func (c *kafkaClient) GetStats() ClientStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.stats
}

func (c *kafkaClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	var errors []error

	if c.producer != nil {
		if err := c.producer.Close(); err != nil {
			errors = append(errors, err)
		}
	}

	if c.asyncProducer != nil {
		if err := c.asyncProducer.Close(); err != nil {
			errors = append(errors, err)
		}
	}

	if c.consumer != nil {
		if err := c.consumer.Close(); err != nil {
			errors = append(errors, err)
		}
	}

	if c.consumerGroup != nil {
		if err := c.consumerGroup.Close(); err != nil {
			errors = append(errors, err)
		}
	}

	if c.client != nil {
		if err := c.client.Close(); err != nil {
			errors = append(errors, err)
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("errors closing kafka client: %v", errors)
	}

	c.logger.Info("kafka client closed")

	return nil
}

func (c *kafkaClient) Ping(ctx context.Context) error {
	if c.client.Closed() {
		return errors.New("client is closed")
	}

	topics, err := c.client.Topics()
	if err != nil {
		return fmt.Errorf("ping failed: %w", err)
	}

	if topics == nil {
		return errors.New("no topics available")
	}

	return nil
}

func (c *kafkaClient) handleAsyncProducerErrors() {
	for err := range c.asyncProducer.Errors() {
		c.mu.Lock()
		c.stats.Errors++
		c.stats.LastError = err
		c.stats.LastErrorTime = time.Now()
		c.mu.Unlock()

		c.logger.Error("kafka async producer error", forge.F("error", err))
	}
}

// Metrics recording methods.
func (c *kafkaClient) recordSendMetric(topic string, size int) {
	if c.metrics == nil {
		return
	}

	c.metrics.Gauge("kafka.bytes.sent", forge.WithLabel("topic", topic)).Set(float64(size))
}

func (c *kafkaClient) recordReceiveMetric(topic string, size int) {
	if c.metrics == nil {
		return
	}

	c.metrics.Gauge("kafka.bytes.received", forge.WithLabel("topic", topic)).Set(float64(size))
}

// buildTLSConfig creates TLS configuration.
func buildTLSConfig(config Config) (*tls.Config, error) {
	tlsConfig := &tls.Config{
		InsecureSkipVerify: config.TLSSkipVerify, //nolint:gosec // G402: opt-in through the tls_skip_verify config option, off by default
	}

	if config.TLSCertFile != "" && config.TLSKeyFile != "" {
		cert, err := tls.LoadX509KeyPair(config.TLSCertFile, config.TLSKeyFile)
		if err != nil {
			return nil, fmt.Errorf("failed to load client cert: %w", err)
		}

		tlsConfig.Certificates = []tls.Certificate{cert}
	}

	if config.TLSCAFile != "" {
		caCert, err := os.ReadFile(config.TLSCAFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read CA cert: %w", err)
		}

		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(caCert) {
			return nil, errors.New("failed to append CA cert")
		}

		tlsConfig.RootCAs = caCertPool
	}

	return tlsConfig, nil
}
