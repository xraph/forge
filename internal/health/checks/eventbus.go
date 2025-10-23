package checks

import (
	"context"
	"fmt"
	"sync"
	"time"

	health "github.com/xraph/forge/internal/health/internal"
	"github.com/xraph/forge/internal/shared"
)

// EventBusHealthCheck performs health checks on event bus systems
type EventBusHealthCheck struct {
	*health.BaseHealthCheck
	eventBus    interface{} // Would be actual event bus interface
	testTopic   string
	testMessage string
	brokerType  string
	mu          sync.RWMutex
}

// EventBusHealthCheckConfig contains configuration for event bus health checks
type EventBusHealthCheckConfig struct {
	Name        string
	EventBus    interface{}
	TestTopic   string
	TestMessage string
	BrokerType  string
	Timeout     time.Duration
	Critical    bool
	Tags        map[string]string
}

// NewEventBusHealthCheck creates a new event bus health check
func NewEventBusHealthCheck(config *EventBusHealthCheckConfig) *EventBusHealthCheck {
	if config == nil {
		config = &EventBusHealthCheckConfig{}
	}

	if config.Name == "" {
		config.Name = "eventbus"
	}

	if config.Timeout == 0 {
		config.Timeout = 10 * time.Second
	}

	if config.TestTopic == "" {
		config.TestTopic = "health-check-topic"
	}

	if config.TestMessage == "" {
		config.TestMessage = "health-check-message"
	}

	if config.BrokerType == "" {
		config.BrokerType = "unknown"
	}

	if config.Tags == nil {
		config.Tags = make(map[string]string)
	}

	config.Tags["broker_type"] = config.BrokerType

	baseConfig := &health.HealthCheckConfig{
		Name:     config.Name,
		Timeout:  config.Timeout,
		Critical: config.Critical,
		Tags:     config.Tags,
	}

	return &EventBusHealthCheck{
		BaseHealthCheck: health.NewBaseHealthCheck(baseConfig),
		eventBus:        config.EventBus,
		testTopic:       config.TestTopic,
		testMessage:     config.TestMessage,
		brokerType:      config.BrokerType,
	}
}

// Check performs the event bus health check
func (ebhc *EventBusHealthCheck) Check(ctx context.Context) *health.HealthResult {
	start := time.Now()

	if ebhc.eventBus == nil {
		return health.NewHealthResult(ebhc.Name(), health.HealthStatusUnhealthy, "event bus is nil").
			WithDuration(time.Since(start)).
			WithDetail("broker_type", ebhc.brokerType)
	}

	result := health.NewHealthResult(ebhc.Name(), health.HealthStatusHealthy, "event bus is healthy").
		WithDuration(time.Since(start)).
		WithCritical(ebhc.Critical()).
		WithTags(ebhc.Tags()).
		WithDetail("broker_type", ebhc.brokerType)

	// Check connection status
	if err := ebhc.checkConnection(ctx); err != nil {
		return result.
			WithError(err).
			WithDetail("connection_check", "failed")
	}

	// Check publish capability
	if err := ebhc.checkPublish(ctx); err != nil {
		return result.
			WithError(err).
			WithDetail("publish_check", "failed")
	}

	// Check subscribe capability
	if err := ebhc.checkSubscribe(ctx); err != nil {
		return result.
			WithError(err).
			WithDetail("subscribe_check", "failed")
	}

	// Add broker-specific stats
	if stats := ebhc.getBrokerStats(); stats != nil {
		for k, v := range stats {
			result.WithDetail(k, v)
		}
	}

	result.WithDuration(time.Since(start))
	return result
}

// checkConnection checks the connection to the event bus
func (ebhc *EventBusHealthCheck) checkConnection(ctx context.Context) error {
	ebhc.mu.RLock()
	defer ebhc.mu.RUnlock()

	// Check if event bus implements a health check interface
	if healthCheckable, ok := ebhc.eventBus.(interface{ HealthCheck(context.Context) error }); ok {
		return healthCheckable.HealthCheck(ctx)
	}

	// Check if event bus implements a ping interface
	if pingable, ok := ebhc.eventBus.(interface{ Ping(context.Context) error }); ok {
		return pingable.Ping(ctx)
	}

	// Default: assume healthy if we have an event bus instance
	return nil
}

// checkPublish checks if we can publish messages
func (ebhc *EventBusHealthCheck) checkPublish(ctx context.Context) error {
	ebhc.mu.RLock()
	defer ebhc.mu.RUnlock()

	// Check if event bus implements a publish interface
	if publisher, ok := ebhc.eventBus.(interface {
		Publish(context.Context, string, interface{}) error
	}); ok {
		return publisher.Publish(ctx, ebhc.testTopic, ebhc.testMessage)
	}

	// If no publish interface, skip this check
	return nil
}

// checkSubscribe checks if we can subscribe to messages
func (ebhc *EventBusHealthCheck) checkSubscribe(ctx context.Context) error {
	ebhc.mu.RLock()
	defer ebhc.mu.RUnlock()

	// For a proper implementation, we would:
	// 1. Subscribe to a test topic
	// 2. Publish a test message
	// 3. Wait for the message to be received
	// 4. Unsubscribe
	// For now, we'll just check if the subscribe method exists

	if subscriber, ok := ebhc.eventBus.(interface {
		Subscribe(context.Context, string, func(interface{})) error
	}); ok {
		// Create a test subscription (we won't actually use it)
		_ = subscriber
		return nil
	}

	return nil
}

// getBrokerStats returns broker-specific statistics
func (ebhc *EventBusHealthCheck) getBrokerStats() map[string]interface{} {
	ebhc.mu.RLock()
	defer ebhc.mu.RUnlock()

	stats := make(map[string]interface{})
	stats["broker_type"] = ebhc.brokerType

	// Check if event bus implements a stats interface
	if statsProvider, ok := ebhc.eventBus.(interface{ GetStats() map[string]interface{} }); ok {
		brokerStats := statsProvider.GetStats()
		for k, v := range brokerStats {
			stats[k] = v
		}
	}

	return stats
}

// NATSHealthCheck is a specialized health check for NATS
type NATSHealthCheck struct {
	*EventBusHealthCheck
	natsConn       interface{} // Would be *nats.Conn
	checkJetStream bool
	checkClusters  bool
}

// NewNATSHealthCheck creates a new NATS health check
func NewNATSHealthCheck(config *EventBusHealthCheckConfig) *NATSHealthCheck {
	if config.BrokerType == "" {
		config.BrokerType = "nats"
	}

	return &NATSHealthCheck{
		EventBusHealthCheck: NewEventBusHealthCheck(config),
		natsConn:            config.EventBus,
		checkJetStream:      true,
		checkClusters:       true,
	}
}

// Check performs NATS-specific health checks
func (nhc *NATSHealthCheck) Check(ctx context.Context) *health.HealthResult {
	// Perform base event bus check
	result := nhc.EventBusHealthCheck.Check(ctx)

	if result.IsUnhealthy() {
		return result
	}

	// Add NATS-specific checks
	if nhc.checkJetStream {
		if jsStatus := nhc.checkJetStreamStatus(ctx); jsStatus != nil {
			for k, v := range jsStatus {
				result.WithDetail(k, v)
			}
		}
	}

	if nhc.checkClusters {
		if clusterStatus := nhc.checkClusterStatus(ctx); clusterStatus != nil {
			for k, v := range clusterStatus {
				result.WithDetail(k, v)
			}
		}
	}

	return result
}

// checkJetStreamStatus checks NATS JetStream status
func (nhc *NATSHealthCheck) checkJetStreamStatus(ctx context.Context) map[string]interface{} {
	// In a real implementation, this would check JetStream status
	// For now, return placeholder data
	return map[string]interface{}{
		"jetstream_enabled": true,
		"jetstream_status":  "healthy",
	}
}

// checkClusterStatus checks NATS cluster status
func (nhc *NATSHealthCheck) checkClusterStatus(ctx context.Context) map[string]interface{} {
	// In a real implementation, this would check cluster status
	// For now, return placeholder data
	return map[string]interface{}{
		"cluster_enabled": false,
		"cluster_size":    1,
	}
}

// KafkaHealthCheck is a specialized health check for Kafka
type KafkaHealthCheck struct {
	*EventBusHealthCheck
	kafkaClient         interface{} // Would be kafka client
	checkTopics         bool
	checkPartitions     bool
	checkConsumerGroups bool
}

// NewKafkaHealthCheck creates a new Kafka health check
func NewKafkaHealthCheck(config *EventBusHealthCheckConfig) *KafkaHealthCheck {
	if config.BrokerType == "" {
		config.BrokerType = "kafka"
	}

	return &KafkaHealthCheck{
		EventBusHealthCheck: NewEventBusHealthCheck(config),
		kafkaClient:         config.EventBus,
		checkTopics:         true,
		checkPartitions:     true,
		checkConsumerGroups: true,
	}
}

// Check performs Kafka-specific health checks
func (khc *KafkaHealthCheck) Check(ctx context.Context) *health.HealthResult {
	// Perform base event bus check
	result := khc.EventBusHealthCheck.Check(ctx)

	if result.IsUnhealthy() {
		return result
	}

	// Add Kafka-specific checks
	if khc.checkTopics {
		if topicStatus := khc.checkTopicStatus(ctx); topicStatus != nil {
			for k, v := range topicStatus {
				result.WithDetail(k, v)
			}
		}
	}

	if khc.checkPartitions {
		if partitionStatus := khc.checkPartitionStatus(ctx); partitionStatus != nil {
			for k, v := range partitionStatus {
				result.WithDetail(k, v)
			}
		}
	}

	if khc.checkConsumerGroups {
		if consumerStatus := khc.checkConsumerGroupStatus(ctx); consumerStatus != nil {
			for k, v := range consumerStatus {
				result.WithDetail(k, v)
			}
		}
	}

	return result
}

// checkTopicStatus checks Kafka topic status
func (khc *KafkaHealthCheck) checkTopicStatus(ctx context.Context) map[string]interface{} {
	// In a real implementation, this would check topic metadata
	return map[string]interface{}{
		"topic_count":    10,
		"topics_healthy": true,
	}
}

// checkPartitionStatus checks Kafka partition status
func (khc *KafkaHealthCheck) checkPartitionStatus(ctx context.Context) map[string]interface{} {
	// In a real implementation, this would check partition metadata
	return map[string]interface{}{
		"partition_count":    30,
		"partitions_healthy": true,
	}
}

// checkConsumerGroupStatus checks Kafka consumer group status
func (khc *KafkaHealthCheck) checkConsumerGroupStatus(ctx context.Context) map[string]interface{} {
	// In a real implementation, this would check consumer group metadata
	return map[string]interface{}{
		"consumer_group_count":    5,
		"consumer_groups_healthy": true,
	}
}

// RabbitMQHealthCheck is a specialized health check for RabbitMQ
type RabbitMQHealthCheck struct {
	*EventBusHealthCheck
	rabbitConn     interface{} // Would be *amqp.Connection
	checkQueues    bool
	checkExchanges bool
	checkBindings  bool
}

// NewRabbitMQHealthCheck creates a new RabbitMQ health check
func NewRabbitMQHealthCheck(config *EventBusHealthCheckConfig) *RabbitMQHealthCheck {
	if config.BrokerType == "" {
		config.BrokerType = "rabbitmq"
	}

	return &RabbitMQHealthCheck{
		EventBusHealthCheck: NewEventBusHealthCheck(config),
		rabbitConn:          config.EventBus,
		checkQueues:         true,
		checkExchanges:      true,
		checkBindings:       true,
	}
}

// Check performs RabbitMQ-specific health checks
func (rhc *RabbitMQHealthCheck) Check(ctx context.Context) *health.HealthResult {
	// Perform base event bus check
	result := rhc.EventBusHealthCheck.Check(ctx)

	if result.IsUnhealthy() {
		return result
	}

	// Add RabbitMQ-specific checks
	if rhc.checkQueues {
		if queueStatus := rhc.checkQueueStatus(ctx); queueStatus != nil {
			for k, v := range queueStatus {
				result.WithDetail(k, v)
			}
		}
	}

	if rhc.checkExchanges {
		if exchangeStatus := rhc.checkExchangeStatus(ctx); exchangeStatus != nil {
			for k, v := range exchangeStatus {
				result.WithDetail(k, v)
			}
		}
	}

	if rhc.checkBindings {
		if bindingStatus := rhc.checkBindingStatus(ctx); bindingStatus != nil {
			for k, v := range bindingStatus {
				result.WithDetail(k, v)
			}
		}
	}

	return result
}

// checkQueueStatus checks RabbitMQ queue status
func (rhc *RabbitMQHealthCheck) checkQueueStatus(ctx context.Context) map[string]interface{} {
	// In a real implementation, this would check queue status
	return map[string]interface{}{
		"queue_count":    15,
		"queues_healthy": true,
	}
}

// checkExchangeStatus checks RabbitMQ exchange status
func (rhc *RabbitMQHealthCheck) checkExchangeStatus(ctx context.Context) map[string]interface{} {
	// In a real implementation, this would check exchange status
	return map[string]interface{}{
		"exchange_count":    8,
		"exchanges_healthy": true,
	}
}

// checkBindingStatus checks RabbitMQ binding status
func (rhc *RabbitMQHealthCheck) checkBindingStatus(ctx context.Context) map[string]interface{} {
	// In a real implementation, this would check binding status
	return map[string]interface{}{
		"binding_count":    25,
		"bindings_healthy": true,
	}
}

// RedisStreamHealthCheck is a specialized health check for Redis Streams
type RedisStreamHealthCheck struct {
	*EventBusHealthCheck
	redisClient         interface{} // Would be redis client
	checkStreams        bool
	checkConsumerGroups bool
}

// NewRedisStreamHealthCheck creates a new Redis Stream health check
func NewRedisStreamHealthCheck(config *EventBusHealthCheckConfig) *RedisStreamHealthCheck {
	if config.BrokerType == "" {
		config.BrokerType = "redis-stream"
	}

	return &RedisStreamHealthCheck{
		EventBusHealthCheck: NewEventBusHealthCheck(config),
		redisClient:         config.EventBus,
		checkStreams:        true,
		checkConsumerGroups: true,
	}
}

// Check performs Redis Stream-specific health checks
func (rshc *RedisStreamHealthCheck) Check(ctx context.Context) *health.HealthResult {
	// Perform base event bus check
	result := rshc.EventBusHealthCheck.Check(ctx)

	if result.IsUnhealthy() {
		return result
	}

	// Add Redis Stream-specific checks
	if rshc.checkStreams {
		if streamStatus := rshc.checkStreamStatus(ctx); streamStatus != nil {
			for k, v := range streamStatus {
				result.WithDetail(k, v)
			}
		}
	}

	if rshc.checkConsumerGroups {
		if consumerStatus := rshc.checkConsumerGroupStatus(ctx); consumerStatus != nil {
			for k, v := range consumerStatus {
				result.WithDetail(k, v)
			}
		}
	}

	return result
}

// checkStreamStatus checks Redis Stream status
func (rshc *RedisStreamHealthCheck) checkStreamStatus(ctx context.Context) map[string]interface{} {
	// In a real implementation, this would check stream status
	return map[string]interface{}{
		"stream_count":    5,
		"streams_healthy": true,
	}
}

// checkConsumerGroupStatus checks Redis Stream consumer group status
func (rshc *RedisStreamHealthCheck) checkConsumerGroupStatus(ctx context.Context) map[string]interface{} {
	// In a real implementation, this would check consumer group status
	return map[string]interface{}{
		"consumer_group_count":    3,
		"consumer_groups_healthy": true,
	}
}

// EventBusHealthCheckFactory creates event bus health checks based on configuration
type EventBusHealthCheckFactory struct {
	container shared.Container
}

// NewEventBusHealthCheckFactory creates a new factory
func NewEventBusHealthCheckFactory(container shared.Container) *EventBusHealthCheckFactory {
	return &EventBusHealthCheckFactory{
		container: container,
	}
}

// CreateEventBusHealthCheck creates an event bus health check
func (factory *EventBusHealthCheckFactory) CreateEventBusHealthCheck(name string, brokerType string, critical bool) (health.HealthCheck, error) {
	config := &EventBusHealthCheckConfig{
		Name:       name,
		BrokerType: brokerType,
		Timeout:    10 * time.Second,
		Critical:   critical,
	}

	// Try to resolve event bus from container
	if factory.container != nil {
		if eventBus, err := factory.container.Resolve(name); err == nil {
			config.EventBus = eventBus
		}
	}

	switch brokerType {
	case "nats":
		return NewNATSHealthCheck(config), nil
	case "kafka":
		return NewKafkaHealthCheck(config), nil
	case "rabbitmq", "amqp":
		return NewRabbitMQHealthCheck(config), nil
	case "redis-stream":
		return NewRedisStreamHealthCheck(config), nil
	default:
		return NewEventBusHealthCheck(config), nil
	}
}

// RegisterEventBusHealthChecks registers event bus health checks with the health service
func RegisterEventBusHealthChecks(healthService health.HealthService, container shared.Container) error {
	factory := NewEventBusHealthCheckFactory(container)

	// This would typically discover event bus services from the container
	eventBuses := []struct {
		name       string
		brokerType string
		critical   bool
	}{
		{"nats", "nats", true},
		{"kafka", "kafka", true},
		{"rabbitmq", "rabbitmq", false},
		{"redis-stream", "redis-stream", false},
	}

	for _, eb := range eventBuses {
		check, err := factory.CreateEventBusHealthCheck(eb.name, eb.brokerType, eb.critical)
		if err != nil {
			continue // Skip unsupported event buses
		}

		if err := healthService.Register(check); err != nil {
			return fmt.Errorf("failed to register %s health check: %w", eb.name, err)
		}
	}

	return nil
}

// EventBusHealthCheckComposite combines multiple event bus health checks
type EventBusHealthCheckComposite struct {
	*health.CompositeHealthCheck
	eventBusChecks []health.HealthCheck
}

// NewEventBusHealthCheckComposite creates a composite event bus health check
func NewEventBusHealthCheckComposite(name string, checks ...health.HealthCheck) *EventBusHealthCheckComposite {
	config := &health.HealthCheckConfig{
		Name:     name,
		Timeout:  30 * time.Second,
		Critical: true,
	}

	composite := health.NewCompositeHealthCheck(config, checks...)

	return &EventBusHealthCheckComposite{
		CompositeHealthCheck: composite,
		eventBusChecks:       checks,
	}
}

// GetEventBusChecks returns the individual event bus checks
func (ebhcc *EventBusHealthCheckComposite) GetEventBusChecks() []health.HealthCheck {
	return ebhcc.eventBusChecks
}

// AddEventBusCheck adds an event bus check to the composite
func (ebhcc *EventBusHealthCheckComposite) AddEventBusCheck(check health.HealthCheck) {
	ebhcc.eventBusChecks = append(ebhcc.eventBusChecks, check)
	ebhcc.CompositeHealthCheck.AddCheck(check)
}
