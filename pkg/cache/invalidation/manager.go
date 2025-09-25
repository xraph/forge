package invalidation

import (
	"context"
	"fmt"
	"sync"
	"time"

	cachecore "github.com/xraph/forge/pkg/cache/core"
	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
)

// InvalidationManager manages cache invalidation across multiple cache backends
type InvalidationManager struct {
	publishers    map[string]cachecore.InvalidationPublisher
	subscribers   map[string]cachecore.InvalidationSubscriber
	patterns      []cachecore.InvalidationPattern
	callbacks     []cachecore.InvalidationCallback
	buffer        chan cachecore.InvalidationEvent
	bufferSize    int
	batchSize     int
	flushInterval time.Duration
	logger        common.Logger
	metrics       common.Metrics
	started       bool
	shutdown      chan struct{}
	wg            sync.WaitGroup
	mu            sync.RWMutex
}

// NewInvalidationManager creates a new InvalidationManager
func NewInvalidationManager(config cachecore.InvalidationConfig, logger common.Logger, metrics common.Metrics) *InvalidationManager {
	return &InvalidationManager{
		publishers:    make(map[string]cachecore.InvalidationPublisher),
		subscribers:   make(map[string]cachecore.InvalidationSubscriber),
		patterns:      make([]cachecore.InvalidationPattern, 0),
		callbacks:     make([]cachecore.InvalidationCallback, 0),
		bufferSize:    config.BufferSize,
		batchSize:     config.BatchSize,
		flushInterval: config.FlushInterval,
		logger:        logger,
		metrics:       metrics,
		shutdown:      make(chan struct{}),
	}
}

// Start starts the invalidation manager
func (im *InvalidationManager) Start(ctx context.Context) error {
	im.mu.Lock()
	defer im.mu.Unlock()

	if im.started {
		return fmt.Errorf("invalidation manager already started")
	}

	// Initialize buffer
	im.buffer = make(chan cachecore.InvalidationEvent, im.bufferSize)

	// OnStart background processors
	im.wg.Add(1)
	go im.eventProcessor()

	im.wg.Add(1)
	go im.batchProcessor()

	im.started = true

	im.logger.Info("invalidation manager started",
		logger.Int("buffer_size", im.bufferSize),
		logger.Int("batch_size", im.batchSize),
		logger.Duration("flush_interval", im.flushInterval),
	)

	if im.metrics != nil {
		im.metrics.Counter("forge.cache.invalidation.manager.started").Inc()
	}

	return nil
}

// Stop stops the invalidation manager
func (im *InvalidationManager) Stop(ctx context.Context) error {
	im.mu.Lock()
	defer im.mu.Unlock()

	if !im.started {
		return fmt.Errorf("invalidation manager not started")
	}

	im.logger.Info("stopping invalidation manager")

	// Signal shutdown
	close(im.shutdown)

	// Wait for processors to finish
	im.wg.Wait()

	// Close publishers
	for name, publisher := range im.publishers {
		if err := publisher.Close(); err != nil {
			im.logger.Error("failed to close publisher",
				logger.String("name", name),
				logger.Error(err),
			)
		}
	}

	// Close subscribers
	for name, subscriber := range im.subscribers {
		if err := subscriber.Close(); err != nil {
			im.logger.Error("failed to close subscriber",
				logger.String("name", name),
				logger.Error(err),
			)
		}
	}

	im.started = false

	im.logger.Info("invalidation manager stopped")

	if im.metrics != nil {
		im.metrics.Counter("forge.cache.invalidation.manager.stopped").Inc()
	}

	return nil
}

// HealthCheck performs health check
func (im *InvalidationManager) HealthCheck(ctx context.Context) error {
	im.mu.RLock()
	defer im.mu.RUnlock()

	if !im.started {
		return fmt.Errorf("invalidation manager not started")
	}

	// Check buffer capacity
	bufferUsage := float64(len(im.buffer)) / float64(im.bufferSize)
	if bufferUsage > 0.9 {
		return fmt.Errorf("invalidation buffer nearly full: %.2f%%", bufferUsage*100)
	}

	return nil
}

// RegisterPublisher registers an invalidation publisher
func (im *InvalidationManager) RegisterPublisher(name string, publisher cachecore.InvalidationPublisher) error {
	im.mu.Lock()
	defer im.mu.Unlock()

	im.publishers[name] = publisher

	im.logger.Info("invalidation publisher registered",
		logger.String("name", name),
	)

	if im.metrics != nil {
		im.metrics.Counter("forge.cache.invalidation.publishers.registered").Inc()
	}

	return nil
}

// RegisterSubscriber registers an invalidation subscriber
func (im *InvalidationManager) RegisterSubscriber(name string, subscriber cachecore.InvalidationSubscriber) error {
	im.mu.Lock()
	defer im.mu.Unlock()

	im.subscribers[name] = subscriber

	im.logger.Info("invalidation subscriber registered",
		logger.String("name", name),
	)

	if im.metrics != nil {
		im.metrics.Counter("forge.cache.invalidation.subscribers.registered").Inc()
	}

	return nil
}

// RegisterCallback registers an invalidation callback
func (im *InvalidationManager) RegisterCallback(callback cachecore.InvalidationCallback) {
	im.mu.Lock()
	defer im.mu.Unlock()

	im.callbacks = append(im.callbacks, callback)
}

// AddPattern adds an invalidation pattern
func (im *InvalidationManager) AddPattern(pattern cachecore.InvalidationPattern) {
	im.mu.Lock()
	defer im.mu.Unlock()

	im.patterns = append(im.patterns, pattern)

	im.logger.Info("invalidation pattern added",
		logger.String("name", pattern.Name),
		logger.String("pattern", pattern.Pattern),
		logger.Bool("enabled", pattern.Enabled),
	)
}

// InvalidateKey invalidates a specific key
func (im *InvalidationManager) InvalidateKey(ctx context.Context, cacheName, key string) error {
	event := cachecore.InvalidationEvent{
		Type:      cachecore.InvalidationTypeKey,
		CacheName: cacheName,
		Key:       key,
		Source:    "manual",
		Timestamp: time.Now(),
		Metadata:  make(map[string]interface{}),
	}

	return im.publishEvent(ctx, event)
}

// InvalidatePattern invalidates keys matching a pattern
func (im *InvalidationManager) InvalidatePattern(ctx context.Context, cacheName, pattern string) error {
	event := cachecore.InvalidationEvent{
		Type:      cachecore.InvalidationTypePattern,
		CacheName: cacheName,
		Pattern:   pattern,
		Source:    "manual",
		Timestamp: time.Now(),
		Metadata:  make(map[string]interface{}),
	}

	return im.publishEvent(ctx, event)
}

// InvalidateTag invalidates keys with a specific tag
func (im *InvalidationManager) InvalidateTag(ctx context.Context, cacheName, tag string) error {
	event := cachecore.InvalidationEvent{
		Type:      cachecore.InvalidationTypeTag,
		CacheName: cacheName,
		Tag:       tag,
		Source:    "manual",
		Timestamp: time.Now(),
		Metadata:  make(map[string]interface{}),
	}

	return im.publishEvent(ctx, event)
}

// InvalidateByTags invalidates keys with any of the specified tags
func (im *InvalidationManager) InvalidateByTags(ctx context.Context, cacheName string, tags []string) error {
	event := cachecore.InvalidationEvent{
		Type:      cachecore.InvalidationTypeTags,
		CacheName: cacheName,
		Tags:      tags,
		Source:    "manual",
		Timestamp: time.Now(),
		Metadata:  make(map[string]interface{}),
	}

	return im.publishEvent(ctx, event)
}

// InvalidateAll invalidates all keys in a cache
func (im *InvalidationManager) InvalidateAll(ctx context.Context, cacheName string) error {
	event := cachecore.InvalidationEvent{
		Type:      cachecore.InvalidationTypeAll,
		CacheName: cacheName,
		Source:    "manual",
		Timestamp: time.Now(),
		Metadata:  make(map[string]interface{}),
	}

	return im.publishEvent(ctx, event)
}

// publishEvent publishes an invalidation event
func (im *InvalidationManager) publishEvent(ctx context.Context, event cachecore.InvalidationEvent) error {
	select {
	case im.buffer <- event:
		if im.metrics != nil {
			im.metrics.Counter("forge.cache.invalidation.events.buffered").Inc()
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
		if im.metrics != nil {
			im.metrics.Counter("forge.cache.invalidation.events.dropped").Inc()
		}
		return fmt.Errorf("invalidation buffer full")
	}
}

// eventProcessor processes invalidation events
func (im *InvalidationManager) eventProcessor() {
	defer im.wg.Done()

	for {
		select {
		case <-im.shutdown:
			return
		case event := <-im.buffer:
			im.processEvent(event)
		}
	}
}

// batchProcessor processes events in batches
func (im *InvalidationManager) batchProcessor() {
	defer im.wg.Done()

	ticker := time.NewTicker(im.flushInterval)
	defer ticker.Stop()

	batch := make([]cachecore.InvalidationEvent, 0, im.batchSize)

	for {
		select {
		case <-im.shutdown:
			// Process remaining events
			if len(batch) > 0 {
				im.publishBatch(context.Background(), batch)
			}
			return
		case <-ticker.C:
			if len(batch) > 0 {
				im.publishBatch(context.Background(), batch)
				batch = batch[:0]
			}
		case event := <-im.buffer:
			batch = append(batch, event)
			if len(batch) >= im.batchSize {
				im.publishBatch(context.Background(), batch)
				batch = batch[:0]
			}
		}
	}
}

// processEvent processes a single invalidation event
func (im *InvalidationManager) processEvent(event cachecore.InvalidationEvent) {
	start := time.Now()

	// Execute callbacks
	for _, callback := range im.callbacks {
		if err := callback(event); err != nil {
			im.logger.Error("invalidation callback failed",
				logger.String("event_type", string(event.Type)),
				logger.String("cache", event.CacheName),
				logger.Error(err),
			)

			if im.metrics != nil {
				im.metrics.Counter("forge.cache.invalidation.callbacks.failed").Inc()
			}
		}
	}

	// Check patterns for cascade invalidation
	im.checkPatterns(event)

	if im.metrics != nil {
		im.metrics.Counter("forge.cache.invalidation.events.processed").Inc()
		im.metrics.Histogram("forge.cache.invalidation.latency").Observe(time.Since(start).Seconds())
	}
}

// publishBatch publishes a batch of events
func (im *InvalidationManager) publishBatch(ctx context.Context, events []cachecore.InvalidationEvent) {
	im.mu.RLock()
	defer im.mu.RUnlock()

	for name, publisher := range im.publishers {
		if err := publisher.PublishBatch(ctx, events); err != nil {
			im.logger.Error("failed to publish invalidation batch",
				logger.String("publisher", name),
				logger.Int("batch_size", len(events)),
				logger.Error(err),
			)

			if im.metrics != nil {
				im.metrics.Counter("forge.cache.invalidation.publish.failed", "publisher", name).Inc()
			}
		} else {
			if im.metrics != nil {
				im.metrics.Counter("forge.cache.invalidation.publish.success", "publisher", name).Inc()
				im.metrics.Counter("forge.cache.invalidation.events.published").Add(float64(len(events)))
			}
		}
	}
}

// checkPatterns checks if event matches any cascade patterns
func (im *InvalidationManager) checkPatterns(event cachecore.InvalidationEvent) {
	for _, pattern := range im.patterns {
		if !pattern.Enabled || !pattern.Cascade {
			continue
		}

		// Check if event matches pattern
		if im.matchesPattern(event, pattern) {
			// Create cascade event
			cascadeEvent := cachecore.InvalidationEvent{
				Type:      cachecore.InvalidationTypePattern,
				CacheName: event.CacheName,
				Pattern:   pattern.Pattern,
				Source:    "cascade",
				Timestamp: time.Now(),
				Metadata: map[string]interface{}{
					"triggered_by": event,
					"pattern":      pattern.Name,
				},
			}

			// Publish cascade event
			if err := im.publishEvent(context.Background(), cascadeEvent); err != nil {
				im.logger.Error("failed to publish cascade event",
					logger.String("pattern", pattern.Name),
					logger.Error(err),
				)
			}
		}
	}
}

// matchesPattern checks if an event matches a pattern
func (im *InvalidationManager) matchesPattern(event cachecore.InvalidationEvent, pattern cachecore.InvalidationPattern) bool {
	// Simple pattern matching - in production, use more sophisticated matching
	switch event.Type {
	case cachecore.InvalidationTypeKey:
		return matchPattern(pattern.Pattern, event.Key)
	case cachecore.InvalidationTypeTag:
		for _, tag := range pattern.Tags {
			if tag == event.Tag {
				return true
			}
		}
	case cachecore.InvalidationTypeTags:
		for _, eventTag := range event.Tags {
			for _, patternTag := range pattern.Tags {
				if eventTag == patternTag {
					return true
				}
			}
		}
	}
	return false
}

// GetStats returns invalidation statistics
func (im *InvalidationManager) GetStats() cachecore.InvalidationStats {
	im.mu.RLock()
	defer im.mu.RUnlock()

	stats := cachecore.InvalidationStats{
		Publishers:     make(map[string]interface{}),
		Subscribers:    make(map[string]interface{}),
		PatternsActive: 0,
	}

	// Count active patterns
	for _, pattern := range im.patterns {
		if pattern.Enabled {
			stats.PatternsActive++
		}
	}

	// Get publisher stats
	for name := range im.publishers {
		stats.Publishers[name] = map[string]interface{}{
			"name": name,
			"type": "publisher",
		}
	}

	// Get subscriber stats
	for name := range im.subscribers {
		stats.Subscribers[name] = map[string]interface{}{
			"name": name,
			"type": "subscriber",
		}
	}

	stats.EventsBuffered = int64(len(im.buffer))

	return stats
}

// matchPattern performs simple pattern matching
func matchPattern(pattern, key string) bool {
	// Simple wildcard matching - in production, use more sophisticated matching
	if pattern == "*" {
		return true
	}

	// Exact match
	if pattern == key {
		return true
	}

	// Prefix match with *
	if len(pattern) > 0 && pattern[len(pattern)-1] == '*' {
		prefix := pattern[:len(pattern)-1]
		return len(key) >= len(prefix) && key[:len(prefix)] == prefix
	}

	return false
}
