package nats

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/xraph/forge/v0/pkg/common"
	eventscore "github.com/xraph/forge/v0/pkg/events/core"
	"github.com/xraph/forge/v0/pkg/logger"
)

// NATSBroker implements MessageBroker for NATS
type NATSBroker struct {
	conn          *nats.Conn
	config        *NATSConfig
	subscriptions map[string]*nats.Subscription
	handlers      map[string][]eventscore.EventHandler
	logger        common.Logger
	metrics       common.Metrics
	connected     bool
	mu            sync.RWMutex
	stats         *BrokerStats
}

// NewNATSBroker creates a new NATS broker
func NewNATSBroker(config map[string]interface{}, logger common.Logger, metrics common.Metrics) (*NATSBroker, error) {
	natsConfig := DefaultNATSConfig()

	// Parse configuration
	if config != nil {
		if err := parseNATSConfig(config, natsConfig); err != nil {
			return nil, fmt.Errorf("failed to parse NATS config: %w", err)
		}
	}

	return &NATSBroker{
		config:        natsConfig,
		subscriptions: make(map[string]*nats.Subscription),
		handlers:      make(map[string][]eventscore.EventHandler),
		logger:        logger,
		metrics:       metrics,
		stats: &BrokerStats{
			Connected: false,
		},
	}, nil
}

// Connect implements MessageBroker
func (nb *NATSBroker) Connect(ctx context.Context, config interface{}) error {
	nb.mu.Lock()
	defer nb.mu.Unlock()

	if nb.connected {
		return nil
	}

	options := []nats.Option{
		nats.Name(nb.config.Name),
		nats.MaxReconnects(nb.config.MaxReconnects),
		nats.ReconnectWait(nb.config.ReconnectWait),
		nats.Timeout(nb.config.ConnectTimeout),
		nats.PingInterval(nb.config.PingInterval),
		nats.MaxPingsOutstanding(nb.config.MaxPingsOut),
	}

	// Add authentication
	if nb.config.Username != "" && nb.config.Password != "" {
		options = append(options, nats.UserInfo(nb.config.Username, nb.config.Password))
	} else if nb.config.Token != "" {
		options = append(options, nats.Token(nb.config.Token))
	}

	// Add compression
	if nb.config.EnableCompression {
		options = append(options, nats.Compression(true))
	}

	// Add event handlers
	options = append(options,
		nats.ConnectHandler(nb.handleConnect),
		nats.DisconnectErrHandler(nb.handleDisconnect),
		nats.ReconnectHandler(nb.handleReconnect),
		nats.ErrorHandler(nb.handleError),
	)

	// Connect to NATS
	conn, err := nats.Connect(nb.config.URL, options...)
	if err != nil {
		nb.stats.ConnectionErrors++
		return fmt.Errorf("failed to connect to NATS: %w", err)
	}

	nb.conn = conn
	nb.connected = true
	now := time.Now()
	nb.stats.Connected = true
	nb.stats.LastConnected = &now

	if nb.logger != nil {
		nb.logger.Info("connected to NATS",
			logger.String("url", nb.config.URL),
			logger.String("name", nb.config.Name),
		)
	}

	if nb.metrics != nil {
		nb.metrics.Counter("forge.events.nats.connections").Inc()
		nb.metrics.Gauge("forge.events.nats.connected").Set(1)
	}

	return nil
}

// Publish implements MessageBroker
func (nb *NATSBroker) Publish(ctx context.Context, topic string, event eventscore.Event) error {
	nb.mu.RLock()
	defer nb.mu.RUnlock()

	if !nb.connected || nb.conn == nil {
		return fmt.Errorf("not connected to NATS")
	}

	start := time.Now()

	// Serialize event
	data, err := json.Marshal(event)
	if err != nil {
		nb.stats.PublishErrors++
		return fmt.Errorf("failed to serialize event: %w", err)
	}

	// Publish message
	if err := nb.conn.Publish(topic, data); err != nil {
		nb.stats.PublishErrors++
		if nb.metrics != nil {
			nb.metrics.Counter("forge.events.nats.publish_errors", "topic", topic).Inc()
		}
		return fmt.Errorf("failed to publish message: %w", err)
	}

	// Update statistics
	duration := time.Since(start)
	nb.stats.MessagesPublished++
	nb.stats.TotalPublishTime += duration
	nb.stats.AvgPublishTime = nb.stats.TotalPublishTime / time.Duration(nb.stats.MessagesPublished)

	if nb.metrics != nil {
		nb.metrics.Counter("forge.events.nats.messages_published", "topic", topic).Inc()
		nb.metrics.Histogram("forge.events.nats.publish_duration", "topic", topic).Observe(duration.Seconds())
	}

	if nb.logger != nil {
		nb.logger.Debug("published event to NATS",
			logger.String("topic", topic),
			logger.String("event_id", event.ID),
			logger.String("event_type", event.Type),
			logger.Duration("duration", duration),
		)
	}

	return nil
}

// Subscribe implements MessageBroker
func (nb *NATSBroker) Subscribe(ctx context.Context, topic string, handler eventscore.EventHandler) error {
	nb.mu.Lock()
	defer nb.mu.Unlock()

	if !nb.connected || nb.conn == nil {
		return fmt.Errorf("not connected to NATS")
	}

	// Add handler to the list
	if nb.handlers[topic] == nil {
		nb.handlers[topic] = make([]eventscore.EventHandler, 0)
	}
	nb.handlers[topic] = append(nb.handlers[topic], handler)

	// Create subscription if it doesn't exist
	if _, exists := nb.subscriptions[topic]; !exists {
		sub, err := nb.conn.Subscribe(topic, nb.createMessageHandler(topic))
		if err != nil {
			return fmt.Errorf("failed to subscribe to topic %s: %w", topic, err)
		}

		nb.subscriptions[topic] = sub
		nb.stats.Subscriptions++

		if nb.logger != nil {
			nb.logger.Info("subscribed to NATS topic",
				logger.String("topic", topic),
			)
		}

		if nb.metrics != nil {
			nb.metrics.Counter("forge.events.nats.subscriptions", "topic", topic).Inc()
			nb.metrics.Gauge("forge.events.nats.active_subscriptions").Set(float64(nb.stats.Subscriptions))
		}
	}

	if nb.logger != nil {
		nb.logger.Info("handler registered for NATS topic",
			logger.String("topic", topic),
			logger.String("handler", handler.Name()),
		)
	}

	return nil
}

// Unsubscribe implements MessageBroker
func (nb *NATSBroker) Unsubscribe(ctx context.Context, topic string, handlerName string) error {
	nb.mu.Lock()
	defer nb.mu.Unlock()

	// Remove handler from the list
	handlers, exists := nb.handlers[topic]
	if !exists {
		return fmt.Errorf("no handlers for topic %s", topic)
	}

	newHandlers := make([]eventscore.EventHandler, 0)
	removed := false
	for _, h := range handlers {
		if h.Name() != handlerName {
			newHandlers = append(newHandlers, h)
		} else {
			removed = true
		}
	}

	if !removed {
		return fmt.Errorf("handler %s not found for topic %s", handlerName, topic)
	}

	nb.handlers[topic] = newHandlers

	// If no more handlers, unsubscribe from NATS
	if len(newHandlers) == 0 {
		if sub, exists := nb.subscriptions[topic]; exists {
			if err := sub.Unsubscribe(); err != nil {
				return fmt.Errorf("failed to unsubscribe from topic %s: %w", topic, err)
			}

			delete(nb.subscriptions, topic)
			delete(nb.handlers, topic)
			nb.stats.Subscriptions--

			if nb.logger != nil {
				nb.logger.Info("unsubscribed from NATS topic",
					logger.String("topic", topic),
				)
			}

			if nb.metrics != nil {
				nb.metrics.Counter("forge.events.nats.unsubscriptions", "topic", topic).Inc()
				nb.metrics.Gauge("forge.events.nats.active_subscriptions").Set(float64(nb.stats.Subscriptions))
			}
		}
	}

	if nb.logger != nil {
		nb.logger.Info("handler unregistered from NATS topic",
			logger.String("topic", topic),
			logger.String("handler", handlerName),
		)
	}

	return nil
}

// Close implements MessageBroker
func (nb *NATSBroker) Close(ctx context.Context) error {
	nb.mu.Lock()
	defer nb.mu.Unlock()

	if !nb.connected || nb.conn == nil {
		return nil
	}

	// Unsubscribe from all topics
	for topic, sub := range nb.subscriptions {
		if err := sub.Unsubscribe(); err != nil {
			if nb.logger != nil {
				nb.logger.Error("failed to unsubscribe from topic during close",
					logger.String("topic", topic),
					logger.Error(err),
				)
			}
		}
	}

	// Close connection
	nb.conn.Close()
	nb.conn = nil
	nb.connected = false
	nb.stats.Connected = false

	// Clear state
	nb.subscriptions = make(map[string]*nats.Subscription)
	nb.handlers = make(map[string][]eventscore.EventHandler)
	nb.stats.Subscriptions = 0

	if nb.logger != nil {
		nb.logger.Info("NATS connection closed")
	}

	if nb.metrics != nil {
		nb.metrics.Gauge("forge.events.nats.connected").Set(0)
		nb.metrics.Gauge("forge.events.nats.active_subscriptions").Set(0)
	}

	return nil
}

// HealthCheck implements MessageBroker
func (nb *NATSBroker) HealthCheck(ctx context.Context) error {
	nb.mu.RLock()
	defer nb.mu.RUnlock()

	if !nb.connected || nb.conn == nil {
		return fmt.Errorf("not connected to NATS")
	}

	if !nb.conn.IsConnected() {
		return fmt.Errorf("NATS connection is not active")
	}

	return nil
}

// GetStats implements MessageBroker
func (nb *NATSBroker) GetStats() map[string]interface{} {
	nb.mu.RLock()
	defer nb.mu.RUnlock()

	stats := map[string]interface{}{
		"type":               "nats",
		"url":                nb.config.URL,
		"connected":          nb.stats.Connected,
		"subscriptions":      nb.stats.Subscriptions,
		"messages_published": nb.stats.MessagesPublished,
		"messages_received":  nb.stats.MessagesReceived,
		"publish_errors":     nb.stats.PublishErrors,
		"receive_errors":     nb.stats.ReceiveErrors,
		"connection_errors":  nb.stats.ConnectionErrors,
		"last_connected":     nb.stats.LastConnected,
		"last_error":         nb.stats.LastError,
		"avg_publish_time":   nb.stats.AvgPublishTime.String(),
	}

	if nb.conn != nil && nb.connected {
		natsStats := nb.conn.Stats()
		stats["nats_stats"] = map[string]interface{}{
			"in_msgs":    natsStats.InMsgs,
			"out_msgs":   natsStats.OutMsgs,
			"in_bytes":   natsStats.InBytes,
			"out_bytes":  natsStats.OutBytes,
			"reconnects": natsStats.Reconnects,
		}
	}

	return stats
}

// createMessageHandler creates a message handler for NATS subscriptions
func (nb *NATSBroker) createMessageHandler(topic string) nats.MsgHandler {
	return func(msg *nats.Msg) {
		start := time.Now()

		// Deserialize event
		var event eventscore.Event
		if err := json.Unmarshal(msg.Data, &event); err != nil {
			nb.stats.ReceiveErrors++
			if nb.logger != nil {
				nb.logger.Error("failed to deserialize event from NATS",
					logger.String("topic", topic),
					logger.Error(err),
				)
			}
			if nb.metrics != nil {
				nb.metrics.Counter("forge.events.nats.receive_errors", "topic", topic).Inc()
			}
			return
		}

		nb.stats.MessagesReceived++

		// Get handlers for this topic
		nb.mu.RLock()
		handlers := make([]eventscore.EventHandler, len(nb.handlers[topic]))
		copy(handlers, nb.handlers[topic])
		nb.mu.RUnlock()

		// Process with all handlers
		ctx := context.Background()
		for _, handler := range handlers {
			if !handler.CanHandle(&event) {
				continue
			}

			if err := handler.Handle(ctx, &event); err != nil {
				nb.stats.ReceiveErrors++
				if nb.logger != nil {
					nb.logger.Error("handler failed to process NATS event",
						logger.String("topic", topic),
						logger.String("handler", handler.Name()),
						logger.String("event_id", event.ID),
						logger.Error(err),
					)
				}
				if nb.metrics != nil {
					nb.metrics.Counter("forge.events.nats.handler_errors", "topic", topic, "handler", handler.Name()).Inc()
				}
			}
		}

		duration := time.Since(start)
		if nb.metrics != nil {
			nb.metrics.Counter("forge.events.nats.messages_received", "topic", topic).Inc()
			nb.metrics.Histogram("forge.events.nats.receive_duration", "topic", topic).Observe(duration.Seconds())
		}

		if nb.logger != nil {
			nb.logger.Debug("processed NATS message",
				logger.String("topic", topic),
				logger.String("event_id", event.ID),
				logger.String("event_type", event.Type),
				logger.Int("handlers", len(handlers)),
				logger.Duration("duration", duration),
			)
		}
	}
}

// Connection event handlers
func (nb *NATSBroker) handleConnect(conn *nats.Conn) {
	if nb.logger != nil {
		nb.logger.Info("NATS connection established",
			logger.String("url", conn.ConnectedUrl()),
		)
	}
	if nb.metrics != nil {
		nb.metrics.Counter("forge.events.nats.connects").Inc()
	}
}

func (nb *NATSBroker) handleDisconnect(conn *nats.Conn, err error) {
	if nb.logger != nil {
		if err != nil {
			nb.logger.Error("NATS connection lost",
				logger.Error(err),
			)
		} else {
			nb.logger.Info("NATS connection closed")
		}
	}
	if nb.metrics != nil {
		nb.metrics.Counter("forge.events.nats.disconnects").Inc()
	}
}

func (nb *NATSBroker) handleReconnect(conn *nats.Conn) {
	if nb.logger != nil {
		nb.logger.Info("NATS connection restored",
			logger.String("url", conn.ConnectedUrl()),
		)
	}
	if nb.metrics != nil {
		nb.metrics.Counter("forge.events.nats.reconnects").Inc()
	}
}

func (nb *NATSBroker) handleError(conn *nats.Conn, sub *nats.Subscription, err error) {
	nb.stats.ConnectionErrors++
	now := time.Now()
	nb.stats.LastError = &now

	if nb.logger != nil {
		nb.logger.Error("NATS error occurred",
			logger.Error(err),
		)
	}
	if nb.metrics != nil {
		nb.metrics.Counter("forge.events.nats.errors").Inc()
	}
}
