package brokers

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge/v2"
	"github.com/xraph/forge/v2/extensions/events/core"
)

// MemoryBroker implements MessageBroker interface using in-memory channels
type MemoryBroker struct {
	name          string
	subscriptions map[string][]core.EventHandler
	topics        map[string]chan core.Event
	logger        forge.Logger
	metrics       forge.Metrics
	connected     bool
	mu            sync.RWMutex
	wg            sync.WaitGroup
	ctx           context.Context
	cancel        context.CancelFunc
}

// NewMemoryBroker creates a new memory broker
func NewMemoryBroker(logger forge.Logger, metrics forge.Metrics) core.MessageBroker {
	return &MemoryBroker{
		name:          "memory",
		subscriptions: make(map[string][]core.EventHandler),
		topics:        make(map[string]chan core.Event),
		logger:        logger,
		metrics:       metrics,
		connected:     false,
	}
}

// Connect implements MessageBroker
func (mb *MemoryBroker) Connect(ctx context.Context, config interface{}) error {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	if mb.connected {
		return fmt.Errorf("memory broker already connected")
	}

	mb.ctx, mb.cancel = context.WithCancel(ctx)
	mb.connected = true

	if mb.logger != nil {
		mb.logger.Info("memory broker connected", forge.F("broker", mb.name))
	}

	if mb.metrics != nil {
		mb.metrics.Counter("forge.events.broker.connected", "broker", mb.name).Inc()
	}

	return nil
}

// Publish implements MessageBroker
func (mb *MemoryBroker) Publish(ctx context.Context, topic string, event core.Event) error {
	mb.mu.RLock()
	if !mb.connected {
		mb.mu.RUnlock()
		return fmt.Errorf("memory broker not connected")
	}

	// Get or create topic channel
	topicChan, exists := mb.topics[topic]
	if !exists {
		mb.mu.RUnlock()
		mb.mu.Lock()

		// Double-check after acquiring write lock
		if topicChan, exists = mb.topics[topic]; !exists {
			topicChan = make(chan core.Event, 1000)
			mb.topics[topic] = topicChan

			// Start topic processor
			mb.wg.Add(1)
			go mb.processTopicEvents(topic, topicChan)
		}
		mb.mu.Unlock()
		mb.mu.RLock()
	}
	mb.mu.RUnlock()

	start := time.Now()

	// Publish event to topic
	select {
	case topicChan <- event:
		// Successfully published
		if mb.metrics != nil {
			duration := time.Since(start)
			mb.metrics.Counter("forge.events.broker.published", "broker", mb.name, "topic", topic).Inc()
			mb.metrics.Histogram("forge.events.broker.publish_duration", "broker", mb.name).Observe(duration.Seconds())
		}

		if mb.logger != nil {
			mb.logger.Debug("event published to memory broker", forge.F("broker", mb.name), forge.F("topic", topic), forge.F("event_id", event.ID))
		}

		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(time.Second * 5):
		return fmt.Errorf("timeout publishing event to topic %s", topic)
	}
}

// Subscribe implements MessageBroker
func (mb *MemoryBroker) Subscribe(ctx context.Context, topic string, handler core.EventHandler) error {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	if !mb.connected {
		return fmt.Errorf("memory broker not connected")
	}

	if mb.subscriptions[topic] == nil {
		mb.subscriptions[topic] = make([]core.EventHandler, 0)
	}

	mb.subscriptions[topic] = append(mb.subscriptions[topic], handler)

	if mb.logger != nil {
		mb.logger.Info("subscribed to topic", forge.F("broker", mb.name), forge.F("topic", topic), forge.F("handler", handler.Name()))
	}

	if mb.metrics != nil {
		mb.metrics.Counter("forge.events.broker.subscriptions", "broker", mb.name, "topic", topic).Inc()
	}

	return nil
}

// Unsubscribe implements MessageBroker
func (mb *MemoryBroker) Unsubscribe(ctx context.Context, topic string, handlerName string) error {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	if !mb.connected {
		return fmt.Errorf("memory broker not connected")
	}

	handlers, exists := mb.subscriptions[topic]
	if !exists {
		return fmt.Errorf("no subscriptions for topic %s", topic)
	}

	for i, handler := range handlers {
		if handler.Name() == handlerName {
			mb.subscriptions[topic] = append(handlers[:i], handlers[i+1:]...)

			if mb.logger != nil {
				mb.logger.Info("unsubscribed from topic", forge.F("broker", mb.name), forge.F("topic", topic), forge.F("handler", handlerName))
			}

			return nil
		}
	}

	return fmt.Errorf("handler %s not found for topic %s", handlerName, topic)
}

// Close implements MessageBroker
func (mb *MemoryBroker) Close(ctx context.Context) error {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	if !mb.connected {
		return nil
	}

	if mb.logger != nil {
		mb.logger.Info("closing memory broker", forge.F("broker", mb.name))
	}

	// Cancel context to stop workers
	if mb.cancel != nil {
		mb.cancel()
	}

	// Close all topic channels
	for _, topicChan := range mb.topics {
		close(topicChan)
	}

	// Wait for workers to finish
	mb.wg.Wait()

	mb.connected = false

	if mb.logger != nil {
		mb.logger.Info("memory broker closed", forge.F("broker", mb.name))
	}

	if mb.metrics != nil {
		mb.metrics.Counter("forge.events.broker.disconnected", "broker", mb.name).Inc()
	}

	return nil
}

// HealthCheck implements MessageBroker
func (mb *MemoryBroker) HealthCheck(ctx context.Context) error {
	mb.mu.RLock()
	defer mb.mu.RUnlock()

	if !mb.connected {
		return fmt.Errorf("memory broker not connected")
	}

	return nil
}

// GetStats implements MessageBroker
func (mb *MemoryBroker) GetStats() map[string]interface{} {
	mb.mu.RLock()
	defer mb.mu.RUnlock()

	return map[string]interface{}{
		"name":          mb.name,
		"connected":     mb.connected,
		"topics_count":  len(mb.topics),
		"subscriptions": len(mb.subscriptions),
	}
}

// processTopicEvents processes events for a topic
func (mb *MemoryBroker) processTopicEvents(topic string, eventChan <-chan core.Event) {
	defer mb.wg.Done()

	for {
		select {
		case <-mb.ctx.Done():
			return
		case event, ok := <-eventChan:
			if !ok {
				// Channel closed
				return
			}

			mb.dispatchToHandlers(topic, &event)
		}
	}
}

// dispatchToHandlers dispatches an event to all topic handlers
func (mb *MemoryBroker) dispatchToHandlers(topic string, event *core.Event) {
	mb.mu.RLock()
	handlers, exists := mb.subscriptions[topic]
	mb.mu.RUnlock()

	if !exists || len(handlers) == 0 {
		return
	}

	// Dispatch to all handlers
	for _, handler := range handlers {
		if handler.CanHandle(event) {
			go func(h core.EventHandler) {
				ctx, cancel := context.WithTimeout(context.Background(), time.Second*30)
				defer cancel()

				if err := h.Handle(ctx, event); err != nil {
					if mb.logger != nil {
						mb.logger.Error("handler failed", forge.F("broker", mb.name), forge.F("topic", topic), forge.F("handler", h.Name()), forge.F("error", err))
					}

					if mb.metrics != nil {
						mb.metrics.Counter("forge.events.broker.handler_errors", "broker", mb.name, "topic", topic).Inc()
					}
				}
			}(handler)
		}
	}
}
