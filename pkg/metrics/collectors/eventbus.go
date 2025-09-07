package collectors

import (
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge/pkg/events"
	"github.com/xraph/forge/pkg/logger"
	metrics "github.com/xraph/forge/pkg/metrics/core"
)

// =============================================================================
// EVENT BUS COLLECTOR
// =============================================================================

// EventBusCollector collects event bus metrics from Phase 2 event system
type EventBusCollector struct {
	name               string
	interval           time.Duration
	eventBus           events.EventBus
	logger             logger.Logger
	metrics            map[string]interface{}
	enabled            bool
	mu                 sync.RWMutex
	lastCollectionTime time.Time
	eventStats         map[string]*EventTypeStats
	brokerStats        map[string]*BrokerStats
	projectorStats     map[string]*ProjectorStats
}

// EventBusCollectorConfig contains configuration for the event bus collector
type EventBusCollectorConfig struct {
	Interval               time.Duration `yaml:"interval" json:"interval"`
	CollectEventStats      bool          `yaml:"collect_event_stats" json:"collect_event_stats"`
	CollectBrokerStats     bool          `yaml:"collect_broker_stats" json:"collect_broker_stats"`
	CollectProjectorStats  bool          `yaml:"collect_projector_stats" json:"collect_projector_stats"`
	CollectSubscriberStats bool          `yaml:"collect_subscriber_stats" json:"collect_subscriber_stats"`
	TrackEventTypes        bool          `yaml:"track_event_types" json:"track_event_types"`
	TrackEventProcessing   bool          `yaml:"track_event_processing" json:"track_event_processing"`
	MaxEventTypeHistory    int           `yaml:"max_event_type_history" json:"max_event_type_history"`
	SlowEventThreshold     time.Duration `yaml:"slow_event_threshold" json:"slow_event_threshold"`
}

// EventTypeStats represents statistics for a specific event type
type EventTypeStats struct {
	EventType          string        `json:"event_type"`
	PublishCount       int64         `json:"publish_count"`
	ProcessCount       int64         `json:"process_count"`
	ErrorCount         int64         `json:"error_count"`
	TotalProcessTime   time.Duration `json:"total_process_time"`
	AverageProcessTime time.Duration `json:"average_process_time"`
	MinProcessTime     time.Duration `json:"min_process_time"`
	MaxProcessTime     time.Duration `json:"max_process_time"`
	LastPublished      time.Time     `json:"last_published"`
	LastProcessed      time.Time     `json:"last_processed"`
	SlowEventCount     int64         `json:"slow_event_count"`
	DeadLetterCount    int64         `json:"dead_letter_count"`
}

// BrokerStats represents statistics for a message broker
type BrokerStats struct {
	BrokerName         string        `json:"broker_name"`
	BrokerType         string        `json:"broker_type"`
	Connected          bool          `json:"connected"`
	MessagesSent       int64         `json:"messages_sent"`
	MessagesReceived   int64         `json:"messages_received"`
	MessagesProcessed  int64         `json:"messages_processed"`
	ConnectionErrors   int64         `json:"connection_errors"`
	PublishErrors      int64         `json:"publish_errors"`
	SubscribeErrors    int64         `json:"subscribe_errors"`
	LastError          error         `json:"last_error,omitempty"`
	LastConnected      time.Time     `json:"last_connected"`
	ConnectionDuration time.Duration `json:"connection_duration"`
	QueueDepth         int64         `json:"queue_depth"`
	ActiveSubscribers  int           `json:"active_subscribers"`
}

// ProjectorStats represents statistics for event projectors
type ProjectorStats struct {
	ProjectorName         string        `json:"projector_name"`
	EventsProcessed       int64         `json:"events_processed"`
	ProjectionErrors      int64         `json:"projection_errors"`
	TotalProcessingTime   time.Duration `json:"total_processing_time"`
	AverageProcessingTime time.Duration `json:"average_processing_time"`
	LastProcessed         time.Time     `json:"last_processed"`
	LastError             error         `json:"last_error,omitempty"`
	CurrentPosition       int64         `json:"current_position"`
	EventsReplayed        int64         `json:"events_replayed"`
	ReplayErrors          int64         `json:"replay_errors"`
	IsRunning             bool          `json:"is_running"`
}

// DefaultEventBusCollectorConfig returns default configuration
func DefaultEventBusCollectorConfig() *EventBusCollectorConfig {
	return &EventBusCollectorConfig{
		Interval:               time.Second * 15,
		CollectEventStats:      true,
		CollectBrokerStats:     true,
		CollectProjectorStats:  true,
		CollectSubscriberStats: true,
		TrackEventTypes:        true,
		TrackEventProcessing:   true,
		MaxEventTypeHistory:    100,
		SlowEventThreshold:     time.Millisecond * 100,
	}
}

// NewEventBusCollector creates a new event bus collector
func NewEventBusCollector(eventBus events.EventBus, logger logger.Logger) metrics.CustomCollector {
	return NewEventBusCollectorWithConfig(eventBus, DefaultEventBusCollectorConfig(), logger)
}

// NewEventBusCollectorWithConfig creates a new event bus collector with configuration
func NewEventBusCollectorWithConfig(eventBus events.EventBus, config *EventBusCollectorConfig, logger logger.Logger) metrics.CustomCollector {
	return &EventBusCollector{
		name:           "eventbus",
		interval:       config.Interval,
		eventBus:       eventBus,
		logger:         logger,
		metrics:        make(map[string]interface{}),
		enabled:        true,
		eventStats:     make(map[string]*EventTypeStats),
		brokerStats:    make(map[string]*BrokerStats),
		projectorStats: make(map[string]*ProjectorStats),
	}
}

// =============================================================================
// CUSTOM COLLECTOR INTERFACE IMPLEMENTATION
// =============================================================================

// Name returns the collector name
func (ebc *EventBusCollector) Name() string {
	return ebc.name
}

// Collect collects event bus metrics
func (ebc *EventBusCollector) Collect() map[string]interface{} {
	ebc.mu.Lock()
	defer ebc.mu.Unlock()

	if !ebc.enabled {
		return ebc.metrics
	}

	now := time.Now()

	// Only collect if enough time has passed
	if !ebc.lastCollectionTime.IsZero() && now.Sub(ebc.lastCollectionTime) < ebc.interval {
		return ebc.metrics
	}

	ebc.lastCollectionTime = now

	// Clear previous metrics
	ebc.metrics = make(map[string]interface{})

	// Collect event statistics
	if err := ebc.collectEventStats(); err != nil && ebc.logger != nil {
		ebc.logger.Error("failed to collect event stats",
			logger.Error(err),
		)
	}

	// Collect broker statistics
	if err := ebc.collectBrokerStats(); err != nil && ebc.logger != nil {
		ebc.logger.Error("failed to collect broker stats",
			logger.Error(err),
		)
	}

	// Collect projector statistics
	if err := ebc.collectProjectorStats(); err != nil && ebc.logger != nil {
		ebc.logger.Error("failed to collect projector stats",
			logger.Error(err),
		)
	}

	// Collect event store statistics
	if err := ebc.collectEventStoreStats(); err != nil && ebc.logger != nil {
		ebc.logger.Error("failed to collect event store stats",
			logger.Error(err),
		)
	}

	return ebc.metrics
}

// Reset resets the collector
func (ebc *EventBusCollector) Reset() {
	ebc.mu.Lock()
	defer ebc.mu.Unlock()

	ebc.metrics = make(map[string]interface{})
	ebc.eventStats = make(map[string]*EventTypeStats)
	ebc.brokerStats = make(map[string]*BrokerStats)
	ebc.projectorStats = make(map[string]*ProjectorStats)
	ebc.lastCollectionTime = time.Time{}
}

// =============================================================================
// EVENT STATISTICS COLLECTION
// =============================================================================

// collectEventStats collects event-related statistics
func (ebc *EventBusCollector) collectEventStats() error {
	if ebc.eventBus == nil {
		return fmt.Errorf("event bus not available")
	}

	// Collect aggregate event statistics
	totalEvents := int64(0)
	totalErrors := int64(0)
	totalProcessingTime := time.Duration(0)
	activeEventTypes := 0

	for eventType, stats := range ebc.eventStats {
		if stats == nil {
			continue
		}

		totalEvents += stats.PublishCount
		totalErrors += stats.ErrorCount
		totalProcessingTime += stats.TotalProcessTime
		activeEventTypes++

		// Add metrics for this event type
		prefix := fmt.Sprintf("eventbus.events.%s", eventType)
		ebc.metrics[prefix+".published"] = stats.PublishCount
		ebc.metrics[prefix+".processed"] = stats.ProcessCount
		ebc.metrics[prefix+".errors"] = stats.ErrorCount
		ebc.metrics[prefix+".avg_processing_time"] = stats.AverageProcessTime.Seconds()
		ebc.metrics[prefix+".min_processing_time"] = stats.MinProcessTime.Seconds()
		ebc.metrics[prefix+".max_processing_time"] = stats.MaxProcessTime.Seconds()
		ebc.metrics[prefix+".slow_events"] = stats.SlowEventCount
		ebc.metrics[prefix+".dead_letter"] = stats.DeadLetterCount

		// Calculate error rate
		if stats.ProcessCount > 0 {
			errorRate := float64(stats.ErrorCount) / float64(stats.ProcessCount) * 100
			ebc.metrics[prefix+".error_rate"] = errorRate
		}

		// Time since last event
		if !stats.LastPublished.IsZero() {
			timeSinceLastEvent := time.Since(stats.LastPublished)
			ebc.metrics[prefix+".time_since_last_event"] = timeSinceLastEvent.Seconds()
		}
	}

	// Aggregate metrics
	ebc.metrics["eventbus.events.total"] = totalEvents
	ebc.metrics["eventbus.events.errors"] = totalErrors
	ebc.metrics["eventbus.events.active_types"] = activeEventTypes

	if totalEvents > 0 {
		ebc.metrics["eventbus.events.avg_processing_time"] = totalProcessingTime.Seconds() / float64(totalEvents)
		ebc.metrics["eventbus.events.error_rate"] = float64(totalErrors) / float64(totalEvents) * 100
	}

	return nil
}

// RecordEvent records an event publication for metrics
func (ebc *EventBusCollector) RecordEvent(event events.Event) {
	ebc.mu.Lock()
	defer ebc.mu.Unlock()

	if !ebc.enabled {
		return
	}

	eventType := event.Type
	stats, exists := ebc.eventStats[eventType]
	if !exists {
		stats = &EventTypeStats{
			EventType:      eventType,
			MinProcessTime: time.Hour, // Initialize with high value
		}
		ebc.eventStats[eventType] = stats
	}

	stats.PublishCount++
	stats.LastPublished = time.Now()
}

// RecordEventProcessing records event processing metrics
func (ebc *EventBusCollector) RecordEventProcessing(eventType string, processingTime time.Duration, err error) {
	ebc.mu.Lock()
	defer ebc.mu.Unlock()

	if !ebc.enabled {
		return
	}

	stats, exists := ebc.eventStats[eventType]
	if !exists {
		stats = &EventTypeStats{
			EventType:      eventType,
			MinProcessTime: time.Hour,
		}
		ebc.eventStats[eventType] = stats
	}

	stats.ProcessCount++
	stats.TotalProcessTime += processingTime
	stats.AverageProcessTime = stats.TotalProcessTime / time.Duration(stats.ProcessCount)
	stats.LastProcessed = time.Now()

	// Update min/max processing time
	if processingTime < stats.MinProcessTime {
		stats.MinProcessTime = processingTime
	}
	if processingTime > stats.MaxProcessTime {
		stats.MaxProcessTime = processingTime
	}

	// Record error if present
	if err != nil {
		stats.ErrorCount++
	}

	// Record slow event
	if processingTime > time.Millisecond*100 {
		stats.SlowEventCount++
	}
}

// =============================================================================
// BROKER STATISTICS COLLECTION
// =============================================================================

// collectBrokerStats collects message broker statistics
func (ebc *EventBusCollector) collectBrokerStats() error {
	if ebc.eventBus == nil {
		return fmt.Errorf("event bus not available")
	}

	// This would typically get broker information from the event bus
	// For now, we'll use placeholder data
	brokers := ebc.getBrokers()

	for brokerName, broker := range brokers {
		if err := ebc.collectBrokerStatsForBroker(brokerName, broker); err != nil {
			if ebc.logger != nil {
				ebc.logger.Error("failed to collect broker stats",
					logger.String("broker", brokerName),
					logger.Error(err),
				)
			}
			continue
		}
	}

	return nil
}

// getBrokers returns available brokers (placeholder implementation)
func (ebc *EventBusCollector) getBrokers() map[string]interface{} {
	// This would be implemented based on the actual events.EventBus interface
	// For now, return placeholder data
	return map[string]interface{}{
		"nats":     nil,
		"kafka":    nil,
		"rabbitmq": nil,
	}
}

// collectBrokerStatsForBroker collects statistics for a specific broker
func (ebc *EventBusCollector) collectBrokerStatsForBroker(brokerName string, broker interface{}) error {
	// Get or create broker stats
	stats, exists := ebc.brokerStats[brokerName]
	if !exists {
		stats = &BrokerStats{
			BrokerName:         brokerName,
			BrokerType:         ebc.getBrokerType(broker),
			Connected:          ebc.isBrokerConnected(broker),
			LastConnected:      time.Now(),
			ConnectionDuration: time.Since(time.Now()),
		}
		ebc.brokerStats[brokerName] = stats
	}

	// Update connection status
	stats.Connected = ebc.isBrokerConnected(broker)
	stats.ActiveSubscribers = ebc.getActiveSubscribers(broker)
	stats.QueueDepth = ebc.getQueueDepth(broker)

	// Add metrics to collection
	prefix := fmt.Sprintf("eventbus.brokers.%s", brokerName)
	ebc.metrics[prefix+".connected"] = stats.Connected
	ebc.metrics[prefix+".messages_sent"] = stats.MessagesSent
	ebc.metrics[prefix+".messages_received"] = stats.MessagesReceived
	ebc.metrics[prefix+".messages_processed"] = stats.MessagesProcessed
	ebc.metrics[prefix+".connection_errors"] = stats.ConnectionErrors
	ebc.metrics[prefix+".publish_errors"] = stats.PublishErrors
	ebc.metrics[prefix+".subscribe_errors"] = stats.SubscribeErrors
	ebc.metrics[prefix+".queue_depth"] = stats.QueueDepth
	ebc.metrics[prefix+".active_subscribers"] = stats.ActiveSubscribers
	ebc.metrics[prefix+".connection_duration"] = stats.ConnectionDuration.Seconds()

	return nil
}

// getBrokerType returns the broker type (placeholder)
func (ebc *EventBusCollector) getBrokerType(broker interface{}) string {
	// This would be implemented based on the actual broker interface
	return "unknown"
}

// isBrokerConnected checks if the broker is connected (placeholder)
func (ebc *EventBusCollector) isBrokerConnected(broker interface{}) bool {
	// This would be implemented based on the actual broker interface
	return true
}

// getActiveSubscribers returns the number of active subscribers (placeholder)
func (ebc *EventBusCollector) getActiveSubscribers(broker interface{}) int {
	// This would be implemented based on the actual broker interface
	return 0
}

// getQueueDepth returns the queue depth (placeholder)
func (ebc *EventBusCollector) getQueueDepth(broker interface{}) int64 {
	// This would be implemented based on the actual broker interface
	return 0
}

// =============================================================================
// PROJECTOR STATISTICS COLLECTION
// =============================================================================

// collectProjectorStats collects event projector statistics
func (ebc *EventBusCollector) collectProjectorStats() error {
	if ebc.eventBus == nil {
		return fmt.Errorf("event bus not available")
	}

	// This would typically get projector information from the event bus
	projectors := ebc.getProjectors()

	for projectorName, projector := range projectors {
		if err := ebc.collectProjectorStatsForProjector(projectorName, projector); err != nil {
			if ebc.logger != nil {
				ebc.logger.Error("failed to collect projector stats",
					logger.String("projector", projectorName),
					logger.Error(err),
				)
			}
			continue
		}
	}

	return nil
}

// getProjectors returns available projectors (placeholder implementation)
func (ebc *EventBusCollector) getProjectors() map[string]interface{} {
	// This would be implemented based on the actual events.EventBus interface
	return map[string]interface{}{
		"user_projection":      nil,
		"order_projection":     nil,
		"analytics_projection": nil,
	}
}

// collectProjectorStatsForProjector collects statistics for a specific projector
func (ebc *EventBusCollector) collectProjectorStatsForProjector(projectorName string, projector interface{}) error {
	// Get or create projector stats
	stats, exists := ebc.projectorStats[projectorName]
	if !exists {
		stats = &ProjectorStats{
			ProjectorName: projectorName,
			IsRunning:     ebc.isProjectorRunning(projector),
		}
		ebc.projectorStats[projectorName] = stats
	}

	// Update projector status
	stats.IsRunning = ebc.isProjectorRunning(projector)
	stats.CurrentPosition = ebc.getProjectorPosition(projector)

	// Add metrics to collection
	prefix := fmt.Sprintf("eventbus.projectors.%s", projectorName)
	ebc.metrics[prefix+".running"] = stats.IsRunning
	ebc.metrics[prefix+".events_processed"] = stats.EventsProcessed
	ebc.metrics[prefix+".projection_errors"] = stats.ProjectionErrors
	ebc.metrics[prefix+".avg_processing_time"] = stats.AverageProcessingTime.Seconds()
	ebc.metrics[prefix+".current_position"] = stats.CurrentPosition
	ebc.metrics[prefix+".events_replayed"] = stats.EventsReplayed
	ebc.metrics[prefix+".replay_errors"] = stats.ReplayErrors

	// Calculate error rate
	if stats.EventsProcessed > 0 {
		errorRate := float64(stats.ProjectionErrors) / float64(stats.EventsProcessed) * 100
		ebc.metrics[prefix+".error_rate"] = errorRate
	}

	return nil
}

// isProjectorRunning checks if the projector is running (placeholder)
func (ebc *EventBusCollector) isProjectorRunning(projector interface{}) bool {
	// This would be implemented based on the actual projector interface
	return true
}

// getProjectorPosition returns the current projector position (placeholder)
func (ebc *EventBusCollector) getProjectorPosition(projector interface{}) int64 {
	// This would be implemented based on the actual projector interface
	return 0
}

// =============================================================================
// EVENT STORE STATISTICS COLLECTION
// =============================================================================

// collectEventStoreStats collects event store statistics
func (ebc *EventBusCollector) collectEventStoreStats() error {
	if ebc.eventBus == nil {
		return fmt.Errorf("event bus not available")
	}

	// Collect event store metrics
	ebc.metrics["eventbus.store.total_events"] = ebc.getEventStoreEventCount()
	ebc.metrics["eventbus.store.total_streams"] = ebc.getEventStoreStreamCount()
	ebc.metrics["eventbus.store.storage_size"] = ebc.getEventStoreSize()
	ebc.metrics["eventbus.store.last_event_time"] = ebc.getLastEventTime()
	ebc.metrics["eventbus.store.snapshots"] = ebc.getSnapshotCount()
	ebc.metrics["eventbus.store.read_operations"] = ebc.getReadOperations()
	ebc.metrics["eventbus.store.write_operations"] = ebc.getWriteOperations()

	return nil
}

// Event store helper methods (placeholder implementations)
func (ebc *EventBusCollector) getEventStoreEventCount() int64 {
	return 0
}

func (ebc *EventBusCollector) getEventStoreStreamCount() int64 {
	return 0
}

func (ebc *EventBusCollector) getEventStoreSize() int64 {
	return 0
}

func (ebc *EventBusCollector) getLastEventTime() time.Time {
	return time.Now()
}

func (ebc *EventBusCollector) getSnapshotCount() int64 {
	return 0
}

func (ebc *EventBusCollector) getReadOperations() int64 {
	return 0
}

func (ebc *EventBusCollector) getWriteOperations() int64 {
	return 0
}

// =============================================================================
// CONFIGURATION METHODS
// =============================================================================

// Enable enables the collector
func (ebc *EventBusCollector) Enable() {
	ebc.mu.Lock()
	defer ebc.mu.Unlock()
	ebc.enabled = true
}

// Disable disables the collector
func (ebc *EventBusCollector) Disable() {
	ebc.mu.Lock()
	defer ebc.mu.Unlock()
	ebc.enabled = false
}

// IsEnabled returns whether the collector is enabled
func (ebc *EventBusCollector) IsEnabled() bool {
	ebc.mu.RLock()
	defer ebc.mu.RUnlock()
	return ebc.enabled
}

// SetInterval sets the collection interval
func (ebc *EventBusCollector) SetInterval(interval time.Duration) {
	ebc.mu.Lock()
	defer ebc.mu.Unlock()
	ebc.interval = interval
}

// GetInterval returns the collection interval
func (ebc *EventBusCollector) GetInterval() time.Duration {
	ebc.mu.RLock()
	defer ebc.mu.RUnlock()
	return ebc.interval
}

// GetStats returns collector statistics
func (ebc *EventBusCollector) GetStats() map[string]interface{} {
	ebc.mu.RLock()
	defer ebc.mu.RUnlock()

	stats := make(map[string]interface{})
	stats["enabled"] = ebc.enabled
	stats["interval"] = ebc.interval
	stats["last_collection"] = ebc.lastCollectionTime
	stats["event_types"] = len(ebc.eventStats)
	stats["brokers"] = len(ebc.brokerStats)
	stats["projectors"] = len(ebc.projectorStats)
	stats["total_events"] = ebc.getTotalEvents()
	stats["total_errors"] = ebc.getTotalEventErrors()

	return stats
}

// getTotalEvents returns the total number of events across all types
func (ebc *EventBusCollector) getTotalEvents() int64 {
	var total int64
	for _, stats := range ebc.eventStats {
		total += stats.PublishCount
	}
	return total
}

// getTotalEventErrors returns the total number of event errors
func (ebc *EventBusCollector) getTotalEventErrors() int64 {
	var total int64
	for _, stats := range ebc.eventStats {
		total += stats.ErrorCount
	}
	return total
}

// =============================================================================
// METRICS INTEGRATION
// =============================================================================

// CreateMetricsWrapper creates a wrapper for metrics integration
func (ebc *EventBusCollector) CreateMetricsWrapper(metricsCollector metrics.MetricsCollector) *EventBusMetricsWrapper {
	return &EventBusMetricsWrapper{
		collector:            ebc,
		metricsCollector:     metricsCollector,
		eventPublishCount:    metricsCollector.Counter("eventbus_events_published_total", "event_type"),
		eventProcessCount:    metricsCollector.Counter("eventbus_events_processed_total", "event_type", "status"),
		eventProcessDuration: metricsCollector.Histogram("eventbus_event_processing_duration_seconds", "event_type"),
		brokerConnected:      metricsCollector.Gauge("eventbus_broker_connected", "broker_name"),
		projectorPosition:    metricsCollector.Gauge("eventbus_projector_position", "projector_name"),
	}
}

// EventBusMetricsWrapper wraps the event bus collector with metrics integration
type EventBusMetricsWrapper struct {
	collector            *EventBusCollector
	metricsCollector     metrics.MetricsCollector
	eventPublishCount    metrics.Counter
	eventProcessCount    metrics.Counter
	eventProcessDuration metrics.Histogram
	brokerConnected      metrics.Gauge
	projectorPosition    metrics.Gauge
}

// RecordEventPublish records event publication metrics
func (ebmw *EventBusMetricsWrapper) RecordEventPublish(event events.Event) {
	ebmw.collector.RecordEvent(event)
	ebmw.eventPublishCount.Inc()
}

// RecordEventProcessing records event processing metrics
func (ebmw *EventBusMetricsWrapper) RecordEventProcessing(eventType string, duration time.Duration, err error) {
	ebmw.collector.RecordEventProcessing(eventType, duration, err)
	ebmw.eventProcessDuration.Observe(duration.Seconds())

	status := "success"
	if err != nil {
		status = "error"
	}
	fmt.Printf("status: %s\n", status)
	ebmw.eventProcessCount.Inc()
}

// UpdateBrokerStatus updates broker connection status
func (ebmw *EventBusMetricsWrapper) UpdateBrokerStatus(brokerName string, connected bool) {
	value := 0.0
	if connected {
		value = 1.0
	}
	ebmw.brokerConnected.Set(value)
}

// UpdateProjectorPosition updates projector position
func (ebmw *EventBusMetricsWrapper) UpdateProjectorPosition(projectorName string, position int64) {
	ebmw.projectorPosition.Set(float64(position))
}
