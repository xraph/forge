package invalidation

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	cachecore "github.com/xraph/forge/pkg/cache/core"
	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/events"
	"github.com/xraph/forge/pkg/logger"
)

// BasePublisher provides common functionality for invalidation publishers
type BasePublisher struct {
	name    string
	logger  common.Logger
	metrics common.Metrics
	config  PublisherConfig
	stats   PublisherStats
}

// PublisherConfig contains configuration for invalidation publishers
type PublisherConfig struct {
	Name          string                 `yaml:"name" json:"name"`
	Type          string                 `yaml:"type" json:"type"`
	Enabled       bool                   `yaml:"enabled" json:"enabled"`
	BatchSize     int                    `yaml:"batch_size" json:"batch_size"`
	FlushInterval time.Duration          `yaml:"flush_interval" json:"flush_interval"`
	RetryAttempts int                    `yaml:"retry_attempts" json:"retry_attempts"`
	RetryDelay    time.Duration          `yaml:"retry_delay" json:"retry_delay"`
	Timeout       time.Duration          `yaml:"timeout" json:"timeout"`
	Compression   bool                   `yaml:"compression" json:"compression"`
	Config        map[string]interface{} `yaml:"config" json:"config"`
}

// PublisherStats contains statistics for invalidation publishers
type PublisherStats struct {
	EventsPublished  int64         `json:"events_published"`
	BatchesPublished int64         `json:"batches_published"`
	EventsFailed     int64         `json:"events_failed"`
	AverageLatency   time.Duration `json:"average_latency"`
	LastPublish      time.Time     `json:"last_publish"`
	Errors           int64         `json:"errors"`
	Retries          int64         `json:"retries"`
}

// EventPublisher publishes events to external systems
type EventPublisher struct {
	*BasePublisher
	eventBus events.EventBus
	topic    string
}

// WebhookPublisher publishes events via HTTP webhooks
type WebhookPublisher struct {
	*BasePublisher
	endpoint string
	headers  map[string]string
	client   HTTPClient
}

// HTTPClient interface for HTTP operations
type HTTPClient interface {
	Post(url string, contentType string, body []byte, headers map[string]string) error
}

// NewBasePublisher creates a new base publisher
func NewBasePublisher(config PublisherConfig, logger common.Logger, metrics common.Metrics) *BasePublisher {
	return &BasePublisher{
		name:    config.Name,
		logger:  logger,
		metrics: metrics,
		config:  config,
		stats:   PublisherStats{},
	}
}

// NewEventPublisher creates a new event publisher
func NewEventPublisher(config PublisherConfig, eventBus events.EventBus, logger common.Logger, metrics common.Metrics) *EventPublisher {
	topic := "cache.invalidation"
	if t, ok := config.Config["topic"].(string); ok {
		topic = t
	}

	return &EventPublisher{
		BasePublisher: NewBasePublisher(config, logger, metrics),
		eventBus:      eventBus,
		topic:         topic,
	}
}

// NewWebhookPublisher creates a new webhook publisher
func NewWebhookPublisher(config PublisherConfig, client HTTPClient, logger common.Logger, metrics common.Metrics) *WebhookPublisher {
	endpoint := ""
	if e, ok := config.Config["endpoint"].(string); ok {
		endpoint = e
	}

	headers := make(map[string]string)
	if h, ok := config.Config["headers"].(map[string]interface{}); ok {
		for k, v := range h {
			if str, ok := v.(string); ok {
				headers[k] = str
			}
		}
	}

	return &WebhookPublisher{
		BasePublisher: NewBasePublisher(config, logger, metrics),
		endpoint:      endpoint,
		headers:       headers,
		client:        client,
	}
}

// Publish publishes a single invalidation event
func (bp *BasePublisher) Publish(ctx context.Context, event cachecore.InvalidationEvent) error {
	start := time.Now()
	defer func() {
		bp.stats.AverageLatency = time.Since(start)
		bp.stats.LastPublish = time.Now()
	}()

	if !bp.config.Enabled {
		return fmt.Errorf("publisher %s is disabled", bp.name)
	}

	// Add timeout to context
	if bp.config.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, bp.config.Timeout)
		defer cancel()
	}

	if err := bp.publishEvent(ctx, event); err != nil {
		bp.stats.EventsFailed++
		bp.stats.Errors++

		if bp.metrics != nil {
			bp.metrics.Counter("forge.cache.invalidation.publisher.failed", "publisher", bp.name).Inc()
		}

		// Retry logic
		if bp.config.RetryAttempts > 0 {
			return bp.retryPublish(ctx, event)
		}

		return err
	}

	bp.stats.EventsPublished++

	if bp.metrics != nil {
		bp.metrics.Counter("forge.cache.invalidation.publisher.success", "publisher", bp.name).Inc()
		bp.metrics.Histogram("forge.cache.invalidation.publisher.latency", "publisher", bp.name).Observe(time.Since(start).Seconds())
	}

	return nil
}

// PublishBatch publishes a batch of invalidation events
func (bp *BasePublisher) PublishBatch(ctx context.Context, events []cachecore.InvalidationEvent) error {
	start := time.Now()
	defer func() {
		bp.stats.AverageLatency = time.Since(start)
		bp.stats.LastPublish = time.Now()
	}()

	if !bp.config.Enabled {
		return fmt.Errorf("publisher %s is disabled", bp.name)
	}

	if len(events) == 0 {
		return nil
	}

	// Add timeout to context
	if bp.config.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, bp.config.Timeout)
		defer cancel()
	}

	// Process in batches
	batchSize := bp.config.BatchSize
	if batchSize <= 0 {
		batchSize = 100
	}

	for i := 0; i < len(events); i += batchSize {
		end := i + batchSize
		if end > len(events) {
			end = len(events)
		}

		batch := events[i:end]
		if err := bp.publishBatch(ctx, batch); err != nil {
			bp.stats.EventsFailed += int64(len(batch))
			bp.stats.Errors++

			if bp.metrics != nil {
				bp.metrics.Counter("forge.cache.invalidation.publisher.batch.failed", "publisher", bp.name).Inc()
			}

			return err
		}

		bp.stats.EventsPublished += int64(len(batch))
		bp.stats.BatchesPublished++
	}

	if bp.metrics != nil {
		bp.metrics.Counter("forge.cache.invalidation.publisher.batch.success", "publisher", bp.name).Inc()
		bp.metrics.Histogram("forge.cache.invalidation.publisher.batch.latency", "publisher", bp.name).Observe(time.Since(start).Seconds())
	}

	return nil
}

// retryPublish retries publishing an event with exponential backoff
func (bp *BasePublisher) retryPublish(ctx context.Context, event cachecore.InvalidationEvent) error {
	var lastErr error
	delay := bp.config.RetryDelay
	if delay == 0 {
		delay = time.Second
	}

	for attempt := 0; attempt < bp.config.RetryAttempts; attempt++ {
		// Wait before retry (except first attempt)
		if attempt > 0 {
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return ctx.Err()
			}
			delay *= 2 // Exponential backoff
		}

		if err := bp.publishEvent(ctx, event); err != nil {
			lastErr = err
			bp.stats.Retries++

			bp.logger.Warn("publish retry failed",
				logger.String("publisher", bp.name),
				logger.Int("attempt", attempt+1),
				logger.Error(err),
			)

			continue
		}

		// Success
		bp.stats.EventsPublished++

		if bp.metrics != nil {
			bp.metrics.Counter("forge.cache.invalidation.publisher.retry.success", "publisher", bp.name).Inc()
		}

		return nil
	}

	return lastErr
}

// publishEvent is implemented by specific publisher types
func (bp *BasePublisher) publishEvent(ctx context.Context, event cachecore.InvalidationEvent) error {
	return fmt.Errorf("publishEvent not implemented")
}

// publishBatch is implemented by specific publisher types
func (bp *BasePublisher) publishBatch(ctx context.Context, events []cachecore.InvalidationEvent) error {
	// Default implementation: publish events individually
	for _, event := range events {
		if err := bp.publishEvent(ctx, event); err != nil {
			return err
		}
	}
	return nil
}

// GetStats returns publisher statistics
func (bp *BasePublisher) GetStats() PublisherStats {
	return bp.stats
}

// Close closes the publisher
func (bp *BasePublisher) Close() error {
	bp.logger.Info("closing publisher", logger.String("name", bp.name))
	return nil
}

// EventPublisher implementation

// Publish publishes a single invalidation event to the event bus
func (ep *EventPublisher) Publish(ctx context.Context, event cachecore.InvalidationEvent) error {
	return ep.BasePublisher.Publish(ctx, event)
}

// PublishBatch publishes a batch of invalidation events to the event bus
func (ep *EventPublisher) PublishBatch(ctx context.Context, events []cachecore.InvalidationEvent) error {
	return ep.BasePublisher.PublishBatch(ctx, events)
}

// publishEvent publishes an event to the event bus
func (ep *EventPublisher) publishEvent(ctx context.Context, event cachecore.InvalidationEvent) error {
	busEvent := &events.Event{
		ID:          fmt.Sprintf("invalidation-%d", time.Now().UnixNano()),
		Type:        ep.topic,
		AggregateID: event.CacheName,
		Data:        event,
		Metadata: map[string]interface{}{
			"publisher": ep.name,
			"source":    "invalidation_publisher",
		},
		Timestamp: time.Now(),
		Version:   1,
	}

	return ep.eventBus.Publish(ctx, busEvent)
}

// Close closes the event publisher
func (ep *EventPublisher) Close() error {
	return ep.BasePublisher.Close()
}

// WebhookPublisher implementation

// Publish publishes a single invalidation event via webhook
func (wp *WebhookPublisher) Publish(ctx context.Context, event cachecore.InvalidationEvent) error {
	return wp.BasePublisher.Publish(ctx, event)
}

// PublishBatch publishes a batch of invalidation events via webhook
func (wp *WebhookPublisher) PublishBatch(ctx context.Context, events []cachecore.InvalidationEvent) error {
	return wp.BasePublisher.PublishBatch(ctx, events)
}

// publishEvent publishes an event via HTTP webhook
func (wp *WebhookPublisher) publishEvent(ctx context.Context, event cachecore.InvalidationEvent) error {
	if wp.endpoint == "" {
		return fmt.Errorf("webhook endpoint not configured")
	}

	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	headers := make(map[string]string)
	for k, v := range wp.headers {
		headers[k] = v
	}
	headers["Content-Type"] = "application/json"

	return wp.client.Post(wp.endpoint, "application/json", data, headers)
}

// publishBatch publishes a batch of events via HTTP webhook
func (wp *WebhookPublisher) publishBatch(ctx context.Context, events []cachecore.InvalidationEvent) error {
	if wp.endpoint == "" {
		return fmt.Errorf("webhook endpoint not configured")
	}

	payload := map[string]interface{}{
		"events":    events,
		"count":     len(events),
		"timestamp": time.Now(),
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal batch: %w", err)
	}

	headers := make(map[string]string)
	for k, v := range wp.headers {
		headers[k] = v
	}
	headers["Content-Type"] = "application/json"

	return wp.client.Post(wp.endpoint, "application/json", data, headers)
}

// Close closes the webhook publisher
func (wp *WebhookPublisher) Close() error {
	return wp.BasePublisher.Close()
}

// PublisherFactory creates invalidation publishers
type PublisherFactory struct {
	logger   common.Logger
	metrics  common.Metrics
	eventBus events.EventBus
	client   HTTPClient
}

// NewPublisherFactory creates a new publisher factory
func NewPublisherFactory(logger common.Logger, metrics common.Metrics, eventBus events.EventBus, client HTTPClient) *PublisherFactory {
	return &PublisherFactory{
		logger:   logger,
		metrics:  metrics,
		eventBus: eventBus,
		client:   client,
	}
}

// CreatePublisher creates a publisher based on configuration
func (pf *PublisherFactory) CreatePublisher(config PublisherConfig) (cachecore.InvalidationPublisher, error) {
	switch config.Type {
	case "event":
		if pf.eventBus == nil {
			return nil, fmt.Errorf("event bus not available for event publisher")
		}
		return NewEventPublisher(config, pf.eventBus, pf.logger, pf.metrics), nil
	case "webhook":
		if pf.client == nil {
			return nil, fmt.Errorf("HTTP client not available for webhook publisher")
		}
		return NewWebhookPublisher(config, pf.client, pf.logger, pf.metrics), nil
	default:
		return nil, fmt.Errorf("unknown publisher type: %s", config.Type)
	}
}
