package config

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge/v0/pkg/common"
	"github.com/xraph/forge/v0/pkg/logger"
)

// Watcher handles watching configuration sources for changes
type Watcher struct {
	interval       time.Duration
	sources        map[string]*WatchContext
	callbacks      map[string][]SourceEventHandler
	changeHandlers map[string]func(string, map[string]interface{})
	logger         common.Logger
	metrics        common.Metrics
	errorHandler   common.ErrorHandler
	mu             sync.RWMutex
	stopChannels   map[string]chan struct{}
	stopped        bool
}

// WatcherConfig contains configuration for the watcher
type WatcherConfig struct {
	Interval       time.Duration
	BufferSize     int
	MaxRetries     int
	RetryInterval  time.Duration
	Logger         common.Logger
	Metrics        common.Metrics
	ErrorHandler   common.ErrorHandler
	EnableDebounce bool
	DebounceTime   time.Duration
}

// WatchEvent represents a configuration change event
type WatchEvent struct {
	SourceName string                 `json:"source_name"`
	EventType  WatchEventType         `json:"event_type"`
	Data       map[string]interface{} `json:"data"`
	Timestamp  time.Time              `json:"timestamp"`
	Checksum   string                 `json:"checksum,omitempty"`
	Error      error                  `json:"error,omitempty"`
}

// WatchEventType represents the type of watch event
type WatchEventType string

const (
	WatchEventTypeChange WatchEventType = "change"
	WatchEventTypeCreate WatchEventType = "create"
	WatchEventTypeDelete WatchEventType = "delete"
	WatchEventTypeError  WatchEventType = "error"
	WatchEventTypeReload WatchEventType = "reload"
)

// WatchCallback represents a callback function for configuration changes
type WatchCallback func(string, map[string]interface{})

// ChangeDetector detects changes in configuration data
type ChangeDetector interface {
	DetectChanges(old, new map[string]interface{}) []ConfigChange
	CalculateChecksum(data map[string]interface{}) string
}

// FileWatcher represents a file system watcher
type FileWatcher interface {
	Watch(path string, callback func(string)) error
	Stop() error
}

// NewWatcher creates a new configuration watcher
func NewWatcher(config WatcherConfig) *Watcher {
	if config.Interval == 0 {
		config.Interval = 30 * time.Second
	}
	if config.BufferSize == 0 {
		config.BufferSize = 100
	}
	if config.MaxRetries == 0 {
		config.MaxRetries = 3
	}
	if config.RetryInterval == 0 {
		config.RetryInterval = 5 * time.Second
	}
	if config.DebounceTime == 0 {
		config.DebounceTime = 1 * time.Second
	}

	return &Watcher{
		interval:       config.Interval,
		sources:        make(map[string]*WatchContext),
		callbacks:      make(map[string][]SourceEventHandler),
		changeHandlers: make(map[string]func(string, map[string]interface{})),
		logger:         config.Logger,
		metrics:        config.Metrics,
		errorHandler:   config.ErrorHandler,
		stopChannels:   make(map[string]chan struct{}),
		stopped:        false,
	}
}

// WatchSource starts watching a configuration source
func (w *Watcher) WatchSource(ctx context.Context, source ConfigSource, callback WatchCallback) error {
	if !source.IsWatchable() {
		return common.ErrConfigError(fmt.Sprintf("source %s is not watchable", source.Name()), nil)
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	if w.stopped {
		return common.ErrConfigError("watcher is stopped", nil)
	}

	sourceName := source.Name()

	// Check if already watching
	if _, exists := w.sources[sourceName]; exists {
		return common.ErrConfigError(fmt.Sprintf("already watching source %s", sourceName), nil)
	}

	// Create watch context
	watchCtx := &WatchContext{
		Source:      source,
		Interval:    w.interval,
		LastCheck:   time.Now(),
		ChangeCount: 0,
		ErrorCount:  0,
		Active:      true,
	}

	w.sources[sourceName] = watchCtx
	w.changeHandlers[sourceName] = callback

	// Create stop channel
	stopChan := make(chan struct{})
	w.stopChannels[sourceName] = stopChan

	// Start watching in a goroutine
	go w.watchSourceLoop(ctx, source, callback, stopChan)

	if w.logger != nil {
		w.logger.Info("started watching configuration source",
			logger.String("source", sourceName),
			logger.Duration("interval", w.interval),
		)
	}

	if w.metrics != nil {
		w.metrics.Counter("forge.config.watcher.sources_started").Inc()
		w.metrics.Gauge("forge.config.watcher.active_sources").Set(float64(len(w.sources)))
	}

	return nil
}

// StopWatching stops watching a specific source
func (w *Watcher) StopWatching(sourceName string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	watchCtx, exists := w.sources[sourceName]
	if !exists {
		return common.ErrConfigError(fmt.Sprintf("not watching source %s", sourceName), nil)
	}

	// Signal stop
	if stopChan, exists := w.stopChannels[sourceName]; exists {
		close(stopChan)
		delete(w.stopChannels, sourceName)
	}

	// Update context
	watchCtx.Active = false

	// Clean up
	delete(w.sources, sourceName)
	delete(w.changeHandlers, sourceName)

	if w.logger != nil {
		w.logger.Info("stopped watching configuration source",
			logger.String("source", sourceName),
		)
	}

	if w.metrics != nil {
		w.metrics.Counter("forge.config.watcher.sources_stopped").Inc()
		w.metrics.Gauge("forge.config.watcher.active_sources").Set(float64(len(w.sources)))
	}

	return nil
}

// StopAll stops watching all sources
func (w *Watcher) StopAll() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.stopped = true

	// Stop all sources
	for sourceName := range w.sources {
		if stopChan, exists := w.stopChannels[sourceName]; exists {
			close(stopChan)
		}
	}

	// Clear all data
	w.sources = make(map[string]*WatchContext)
	w.changeHandlers = make(map[string]func(string, map[string]interface{}))
	w.stopChannels = make(map[string]chan struct{})

	if w.logger != nil {
		w.logger.Info("stopped watching all configuration sources")
	}

	if w.metrics != nil {
		w.metrics.Counter("forge.config.watcher.stopped_all").Inc()
		w.metrics.Gauge("forge.config.watcher.active_sources").Set(0)
	}

	return nil
}

// GetWatchedSources returns the list of currently watched sources
func (w *Watcher) GetWatchedSources() []string {
	w.mu.RLock()
	defer w.mu.RUnlock()

	sources := make([]string, 0, len(w.sources))
	for sourceName := range w.sources {
		sources = append(sources, sourceName)
	}

	return sources
}

// GetSourceStats returns statistics for a watched source
func (w *Watcher) GetSourceStats(sourceName string) (*WatchStats, error) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	watchCtx, exists := w.sources[sourceName]
	if !exists {
		return nil, common.ErrConfigError(fmt.Sprintf("source %s is not being watched", sourceName), nil)
	}

	return &WatchStats{
		SourceName:  sourceName,
		Active:      watchCtx.Active,
		LastCheck:   watchCtx.LastCheck,
		ChangeCount: watchCtx.ChangeCount,
		ErrorCount:  watchCtx.ErrorCount,
		Interval:    watchCtx.Interval,
		Uptime:      time.Since(watchCtx.LastCheck),
	}, nil
}

// GetAllStats returns statistics for all watched sources
func (w *Watcher) GetAllStats() map[string]*WatchStats {
	w.mu.RLock()
	defer w.mu.RUnlock()

	stats := make(map[string]*WatchStats)
	for sourceName, watchCtx := range w.sources {
		stats[sourceName] = &WatchStats{
			SourceName:  sourceName,
			Active:      watchCtx.Active,
			LastCheck:   watchCtx.LastCheck,
			ChangeCount: watchCtx.ChangeCount,
			ErrorCount:  watchCtx.ErrorCount,
			Interval:    watchCtx.Interval,
			Uptime:      time.Since(watchCtx.LastCheck),
		}
	}

	return stats
}

// watchSourceLoop is the main watching loop for a source
func (w *Watcher) watchSourceLoop(ctx context.Context, source ConfigSource, callback WatchCallback, stopChan chan struct{}) {
	sourceName := source.Name()
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	var lastData map[string]interface{}
	var lastChecksum string

	// Initial load
	if data, err := source.Load(ctx); err == nil {
		lastData = data
		lastChecksum = w.calculateChecksum(data)
	} else {
		w.handleWatchError(sourceName, err)
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-stopChan:
			return
		case <-ticker.C:
			w.checkForChanges(ctx, source, &lastData, &lastChecksum, callback)
		}
	}
}

// checkForChanges checks for changes in a configuration source
func (w *Watcher) checkForChanges(ctx context.Context, source ConfigSource, lastData *map[string]interface{}, lastChecksum *string, callback WatchCallback) {
	sourceName := source.Name()

	// Update last check time
	w.mu.Lock()
	if watchCtx, exists := w.sources[sourceName]; exists {
		watchCtx.LastCheck = time.Now()
	}
	w.mu.Unlock()

	// Load current data
	currentData, err := source.Load(ctx)
	if err != nil {
		w.handleWatchError(sourceName, err)
		return
	}

	// Calculate checksum
	currentChecksum := w.calculateChecksum(currentData)

	// Check if data has changed
	if *lastChecksum != "" && currentChecksum != *lastChecksum {
		// Data has changed
		w.handleChange(sourceName, *lastData, currentData, callback)

		// Update change count
		w.mu.Lock()
		if watchCtx, exists := w.sources[sourceName]; exists {
			watchCtx.ChangeCount++
		}
		w.mu.Unlock()

		if w.metrics != nil {
			w.metrics.Counter("forge.config.watcher.changes_detected", "source", sourceName).Inc()
		}
	}

	// Update last data and checksum
	*lastData = currentData
	*lastChecksum = currentChecksum
}

// handleChange handles a configuration change
func (w *Watcher) handleChange(sourceName string, oldData, newData map[string]interface{}, callback WatchCallback) {
	if w.logger != nil {
		w.logger.Info("configuration change detected",
			logger.String("source", sourceName),
			logger.Int("old_keys", len(oldData)),
			logger.Int("new_keys", len(newData)),
		)
	}

	// Create change event
	event := WatchEvent{
		SourceName: sourceName,
		EventType:  WatchEventTypeChange,
		Data:       newData,
		Timestamp:  time.Now(),
		Checksum:   w.calculateChecksum(newData),
	}

	// Notify callback
	if callback != nil {
		go func() {
			defer func() {
				if r := recover(); r != nil {
					if w.logger != nil {
						w.logger.Error("panic in watch callback",
							logger.String("source", sourceName),
							logger.Any("panic", r),
						)
					}
				}
			}()

			callback(sourceName, newData)
		}()
	}

	// Notify event handlers
	w.notifyEventHandlers(sourceName, event)
}

// handleWatchError handles errors during watching
func (w *Watcher) handleWatchError(sourceName string, err error) {
	// Update error count
	w.mu.Lock()
	if watchCtx, exists := w.sources[sourceName]; exists {
		watchCtx.ErrorCount++
	}
	w.mu.Unlock()

	if w.logger != nil {
		w.logger.Error("error watching configuration source",
			logger.String("source", sourceName),
			logger.Error(err),
		)
	}

	if w.errorHandler != nil {
		w.errorHandler.HandleError(nil, common.ErrConfigError(fmt.Sprintf("watch error for source %s", sourceName), err))
	}

	if w.metrics != nil {
		w.metrics.Counter("forge.config.watcher.errors", "source", sourceName).Inc()
	}

	// Create error event
	event := WatchEvent{
		SourceName: sourceName,
		EventType:  WatchEventTypeError,
		Timestamp:  time.Now(),
		Error:      err,
	}

	// Notify event handlers
	w.notifyEventHandlers(sourceName, event)
}

// RegisterEventHandler registers an event handler for a source
func (w *Watcher) RegisterEventHandler(sourceName string, handler SourceEventHandler) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.callbacks[sourceName] == nil {
		w.callbacks[sourceName] = make([]SourceEventHandler, 0)
	}
	w.callbacks[sourceName] = append(w.callbacks[sourceName], handler)

	if w.logger != nil {
		w.logger.Debug("event handler registered",
			logger.String("source", sourceName),
		)
	}
}

// UnregisterEventHandler unregisters an event handler for a source
func (w *Watcher) UnregisterEventHandler(sourceName string, handler SourceEventHandler) {
	w.mu.Lock()
	defer w.mu.Unlock()

	handlers := w.callbacks[sourceName]
	for i, h := range handlers {
		if h == handler {
			w.callbacks[sourceName] = append(handlers[:i], handlers[i+1:]...)
			break
		}
	}

	if w.logger != nil {
		w.logger.Debug("event handler unregistered",
			logger.String("source", sourceName),
		)
	}
}

// notifyEventHandlers notifies all registered event handlers
func (w *Watcher) notifyEventHandlers(sourceName string, event WatchEvent) {
	w.mu.RLock()
	handlers := w.callbacks[sourceName]
	w.mu.RUnlock()

	for _, handler := range handlers {
		go func(h SourceEventHandler) {
			defer func() {
				if r := recover(); r != nil {
					if w.logger != nil {
						w.logger.Error("panic in event handler",
							logger.String("source", sourceName),
							logger.Any("panic", r),
						)
					}
				}
			}()

			sourceEvent := SourceEvent{
				SourceName: event.SourceName,
				EventType:  string(event.EventType),
				Data:       event.Data,
				Timestamp:  event.Timestamp,
				Error:      event.Error,
			}

			if err := h.HandleEvent(sourceEvent); err != nil {
				if w.logger != nil {
					w.logger.Error("error in event handler",
						logger.String("source", sourceName),
						logger.Error(err),
					)
				}
			}
		}(handler)
	}
}

// calculateChecksum calculates a simple checksum for configuration data
func (w *Watcher) calculateChecksum(data map[string]interface{}) string {
	// Simple checksum based on string representation
	// In production, you might want to use a proper hash function
	return fmt.Sprintf("%x", fmt.Sprintf("%v", data))
}

// detectChanges detects specific changes between old and new configuration
func (w *Watcher) detectChanges(oldData, newData map[string]interface{}) []ConfigChange {
	var changes []ConfigChange
	timestamp := time.Now()

	// Check for added/modified keys
	for key, newValue := range newData {
		if oldValue, exists := oldData[key]; exists {
			// Key exists - check if value changed
			if !w.deepEqual(oldValue, newValue) {
				changes = append(changes, ConfigChange{
					Type:      ChangeTypeUpdate,
					Key:       key,
					OldValue:  oldValue,
					NewValue:  newValue,
					Timestamp: timestamp,
				})
			}
		} else {
			// New key
			changes = append(changes, ConfigChange{
				Type:      ChangeTypeSet,
				Key:       key,
				NewValue:  newValue,
				Timestamp: timestamp,
			})
		}
	}

	// Check for deleted keys
	for key, oldValue := range oldData {
		if _, exists := newData[key]; !exists {
			changes = append(changes, ConfigChange{
				Type:      ChangeTypeDelete,
				Key:       key,
				OldValue:  oldValue,
				Timestamp: timestamp,
			})
		}
	}

	return changes
}

// deepEqual compares two values for deep equality
func (w *Watcher) deepEqual(a, b interface{}) bool {
	// Simple deep equality check
	// In production, you might want a more sophisticated comparison
	return fmt.Sprintf("%v", a) == fmt.Sprintf("%v", b)
}

// WatchStats contains statistics about a watched source
type WatchStats struct {
	SourceName  string        `json:"source_name"`
	Active      bool          `json:"active"`
	LastCheck   time.Time     `json:"last_check"`
	ChangeCount int64         `json:"change_count"`
	ErrorCount  int64         `json:"error_count"`
	Interval    time.Duration `json:"interval"`
	Uptime      time.Duration `json:"uptime"`
}

// DefaultChangeDetector is a simple implementation of ChangeDetector
type DefaultChangeDetector struct{}

func (d *DefaultChangeDetector) DetectChanges(old, new map[string]interface{}) []ConfigChange {
	var changes []ConfigChange
	timestamp := time.Now()

	// Check for added/modified keys
	for key, newValue := range new {
		if oldValue, exists := old[key]; exists {
			if fmt.Sprintf("%v", oldValue) != fmt.Sprintf("%v", newValue) {
				changes = append(changes, ConfigChange{
					Type:      ChangeTypeUpdate,
					Key:       key,
					OldValue:  oldValue,
					NewValue:  newValue,
					Timestamp: timestamp,
				})
			}
		} else {
			changes = append(changes, ConfigChange{
				Type:      ChangeTypeSet,
				Key:       key,
				NewValue:  newValue,
				Timestamp: timestamp,
			})
		}
	}

	// Check for deleted keys
	for key, oldValue := range old {
		if _, exists := new[key]; !exists {
			changes = append(changes, ConfigChange{
				Type:      ChangeTypeDelete,
				Key:       key,
				OldValue:  oldValue,
				Timestamp: timestamp,
			})
		}
	}

	return changes
}

func (d *DefaultChangeDetector) CalculateChecksum(data map[string]interface{}) string {
	return fmt.Sprintf("%x", fmt.Sprintf("%v", data))
}

// DebounceWatcher wraps another watcher with debouncing functionality
type DebounceWatcher struct {
	*Watcher
	debounceTime time.Duration
	timers       map[string]*time.Timer
	mu           sync.Mutex
}

// NewDebounceWatcher creates a watcher with debouncing
func NewDebounceWatcher(config WatcherConfig, debounceTime time.Duration) *DebounceWatcher {
	return &DebounceWatcher{
		Watcher:      NewWatcher(config),
		debounceTime: debounceTime,
		timers:       make(map[string]*time.Timer),
	}
}

// WatchSource starts watching with debouncing
func (dw *DebounceWatcher) WatchSource(ctx context.Context, source ConfigSource, callback WatchCallback) error {
	// Wrap the callback with debouncing
	debouncedCallback := func(sourceName string, data map[string]interface{}) {
		dw.debounceCallback(sourceName, data, callback)
	}

	return dw.Watcher.WatchSource(ctx, source, debouncedCallback)
}

// debounceCallback implements debouncing for configuration changes
func (dw *DebounceWatcher) debounceCallback(sourceName string, data map[string]interface{}, callback WatchCallback) {
	dw.mu.Lock()
	defer dw.mu.Unlock()

	// Cancel existing timer
	if timer, exists := dw.timers[sourceName]; exists {
		timer.Stop()
	}

	// Create new timer
	dw.timers[sourceName] = time.AfterFunc(dw.debounceTime, func() {
		callback(sourceName, data)

		// Clean up timer
		dw.mu.Lock()
		delete(dw.timers, sourceName)
		dw.mu.Unlock()
	})
}
