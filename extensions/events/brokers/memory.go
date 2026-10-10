package brokers

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/errors"
	"github.com/xraph/forge/extensions/events/core"
)

// MemoryBroker implements MessageBroker interface using in-memory channels.
type MemoryBroker struct {
	name          string
	subscriptions map[string][]core.EventHandler
	topics        map[string]chan core.Event
	logger        forge.Logger
	metrics       forge.Metrics
	connected     bool
	mu            sync.RWMutex
	wg            sync.WaitGroup
	done          <-chan struct{}
	closing       chan struct{}
	cancel        context.CancelFunc
}

// NewMemoryBroker creates a new memory broker.
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

// Connect implements MessageBroker.
func (mb *MemoryBroker) Connect(ctx context.Context, config any) error {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	if mb.connected || mb.closing != nil {
		return errors.New("memory broker already connected")
	}

	workerCtx, cancel := context.WithCancel(ctx)
	mb.done = workerCtx.Done()
	mb.cancel = cancel
	mb.connected = true

	if mb.logger != nil {
		mb.logger.Info("memory broker connected", forge.F("broker", mb.name))
	}

	if mb.metrics != nil {
		mb.metrics.Counter("forge.events.broker.connected", forge.WithLabel("broker", mb.name)).Inc()
	}

	return nil
}

// Publish implements MessageBroker.
func (mb *MemoryBroker) Publish(ctx context.Context, topic string, event core.Event) error {
	mb.mu.Lock()
	if !mb.connected || mb.closing != nil {
		mb.mu.Unlock()

		return errors.New("memory broker not connected")
	}

	topicChan, exists := mb.topics[topic]
	if !exists {
		topicChan = make(chan core.Event, 1000)
		mb.topics[topic] = topicChan

		mb.wg.Add(1)
		//nolint:gosec // Topic workers follow the broker lifetime, not a single publication.
		go mb.processTopicEvents(mb.done, topic, topicChan)
	}

	done := mb.done
	mb.wg.Add(1)

	mb.mu.Unlock()
	defer mb.wg.Done()

	start := time.Now()

	// Publish event to topic
	select {
	case topicChan <- event:
		// Successfully published
		if mb.metrics != nil {
			duration := time.Since(start)

			mb.metrics.Counter("forge.events.broker.published", forge.WithLabel("broker", mb.name), forge.WithLabel("topic", topic)).Inc()
			mb.metrics.Histogram("forge.events.broker.publish_duration", forge.WithLabel("broker", mb.name)).Observe(duration.Seconds())
		}

		if mb.logger != nil {
			mb.logger.Debug("event published to memory broker", forge.F("broker", mb.name), forge.F("topic", topic), forge.F("event_id", event.ID))
		}

		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
		return errors.New("memory broker closed")
	case <-time.After(time.Second * 5):
		return fmt.Errorf("timeout publishing event to topic %s", topic)
	}
}

// Subscribe implements MessageBroker.
func (mb *MemoryBroker) Subscribe(ctx context.Context, topic string, handler core.EventHandler) error {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	if !mb.connected {
		return errors.New("memory broker not connected")
	}

	if mb.subscriptions[topic] == nil {
		mb.subscriptions[topic] = make([]core.EventHandler, 0)
	}

	mb.subscriptions[topic] = append(mb.subscriptions[topic], handler)

	if mb.logger != nil {
		mb.logger.Info("subscribed to topic", forge.F("broker", mb.name), forge.F("topic", topic), forge.F("handler", handler.Name()))
	}

	if mb.metrics != nil {
		mb.metrics.Counter("forge.events.broker.subscriptions", forge.WithLabel("broker", mb.name), forge.WithLabel("topic", topic)).Inc()
	}

	return nil
}

// Unsubscribe implements MessageBroker.
func (mb *MemoryBroker) Unsubscribe(ctx context.Context, topic string, handlerName string) error {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	if !mb.connected {
		return errors.New("memory broker not connected")
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

// Close implements MessageBroker.
func (mb *MemoryBroker) Close(ctx context.Context) error {
	mb.mu.Lock()
	if mb.closing != nil {
		done := mb.closing
		mb.mu.Unlock()

		select {
		case <-done:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	if !mb.connected {
		mb.mu.Unlock()

		return nil
	}

	mb.connected = false
	mb.closing = make(chan struct{})
	done := mb.closing
	mb.cancel()

	mb.mu.Unlock()
	go func() {
		mb.wg.Wait()
		mb.mu.Lock()
		mb.subscriptions = make(map[string][]core.EventHandler)
		mb.topics = make(map[string]chan core.Event)
		mb.cancel = nil
		mb.done = nil
		mb.closing = nil

		close(done)
		mb.mu.Unlock()
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// HealthCheck implements MessageBroker.
func (mb *MemoryBroker) HealthCheck(ctx context.Context) error {
	mb.mu.RLock()
	defer mb.mu.RUnlock()

	if !mb.connected {
		return errors.New("memory broker not connected")
	}

	return nil
}

// GetStats implements MessageBroker.
func (mb *MemoryBroker) GetStats() map[string]any {
	mb.mu.RLock()
	defer mb.mu.RUnlock()

	return map[string]any{
		"name":          mb.name,
		"connected":     mb.connected,
		"topics_count":  len(mb.topics),
		"subscriptions": len(mb.subscriptions),
	}
}

// processTopicEvents processes events for a topic.
func (mb *MemoryBroker) processTopicEvents(done <-chan struct{}, topic string, eventChan <-chan core.Event) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		select {
		case <-done:
			cancel()
		case <-ctx.Done():
		}
	}()

	defer mb.wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-eventChan:
			if !ok {
				// Channel closed
				return
			}

			mb.dispatchToHandlers(ctx, topic, &event)
		}
	}
}

// dispatchToHandlers dispatches an event to all topic handlers.
func (mb *MemoryBroker) dispatchToHandlers(ctx context.Context, topic string, event *core.Event) {
	mb.mu.RLock()
	handlers, exists := mb.subscriptions[topic]
	handlers = append([]core.EventHandler(nil), handlers...)

	mb.mu.RUnlock()

	if !exists || len(handlers) == 0 {
		return
	}

	// Dispatch to all handlers
	for _, handler := range handlers {
		if handler.CanHandle(event) {
			mb.wg.Add(1)
			go func(h core.EventHandler) {
				defer mb.wg.Done()

				ctx, cancel := context.WithTimeout(ctx, time.Second*30)
				defer cancel()

				if err := h.Handle(ctx, event); err != nil {
					if mb.logger != nil {
						mb.logger.Error("handler failed", forge.F("broker", mb.name), forge.F("topic", topic), forge.F("handler", h.Name()), forge.F("error", err))
					}

					if mb.metrics != nil {
						mb.metrics.Counter("forge.events.broker.handler_errors", forge.WithLabel("broker", mb.name), forge.WithLabel("topic", topic)).Inc()
					}
				}
			}(handler)
		}
	}
}
