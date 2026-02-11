package brokers

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	nats "github.com/nats-io/nats.go"
	"github.com/xraph/forge"
	"github.com/xraph/forge/errors"
	"github.com/xraph/forge/extensions/events/core"
)

// NATSBroker implements MessageBroker for NATS.
type NATSBroker struct {
	conn          *nats.Conn
	config        *NATSConfig
	subscriptions map[string]*nats.Subscription
	handlers      map[string][]core.EventHandler
	logger        forge.Logger
	metrics       forge.Metrics
	connected     bool
	mu            sync.RWMutex
	stats         *NATSBrokerStats
}

// NATSBrokerStats contains NATS broker statistics.
type NATSBrokerStats struct {
	Connected         bool       `json:"connected"`
	Subscriptions     int        `json:"subscriptions"`
	MessagesPublished int64      `json:"messages_published"`
	MessagesReceived  int64      `json:"messages_received"`
	PublishErrors     int64      `json:"publish_errors"`
	ReceiveErrors     int64      `json:"receive_errors"`
	ConnectionErrors  int64      `json:"connection_errors"`
	LastConnected     *time.Time `json:"last_connected"`
	LastError         *time.Time `json:"last_error"`
	TotalPublishTime  time.Duration
	AvgPublishTime    time.Duration
}

// NATSConfig defines configuration for NATS broker.
type NATSConfig struct {
	URL               string        `json:"url"                yaml:"url"`
	Name              string        `json:"name"               yaml:"name"`
	Username          string        `json:"username"           yaml:"username"`
	Password          string        `json:"password"           yaml:"password"`
	Token             string        `json:"token"              yaml:"token"`
	MaxReconnects     int           `json:"max_reconnects"     yaml:"max_reconnects"`
	ReconnectWait     time.Duration `json:"reconnect_wait"     yaml:"reconnect_wait"`
	ConnectTimeout    time.Duration `json:"connect_timeout"    yaml:"connect_timeout"`
	PingInterval      time.Duration `json:"ping_interval"      yaml:"ping_interval"`
	MaxPingsOut       int           `json:"max_pings_out"      yaml:"max_pings_out"`
	EnableCompression bool          `json:"enable_compression" yaml:"enable_compression"`
}

// DefaultNATSConfig returns default NATS configuration.
func DefaultNATSConfig() *NATSConfig {
	return &NATSConfig{
		URL:               "nats://localhost:4222",
		Name:              "forge-events",
		MaxReconnects:     10,
		ReconnectWait:     time.Second * 2,
		ConnectTimeout:    time.Second * 5,
		PingInterval:      time.Second * 20,
		MaxPingsOut:       2,
		EnableCompression: false,
	}
}

// NewNATSBroker creates a new NATS broker.
func NewNATSBroker(config map[string]any, logger forge.Logger, metrics forge.Metrics) (*NATSBroker, error) {
	natsConfig := DefaultNATSConfig()

	// Parse configuration from map
	if config != nil {
		if url, ok := config["url"].(string); ok {
			natsConfig.URL = url
		}

		if name, ok := config["name"].(string); ok {
			natsConfig.Name = name
		}

		if username, ok := config["username"].(string); ok {
			natsConfig.Username = username
		}

		if password, ok := config["password"].(string); ok {
			natsConfig.Password = password
		}

		if token, ok := config["token"].(string); ok {
			natsConfig.Token = token
		}

		if maxReconnects, ok := config["max_reconnects"].(int); ok {
			natsConfig.MaxReconnects = maxReconnects
		}

		if enableCompression, ok := config["enable_compression"].(bool); ok {
			natsConfig.EnableCompression = enableCompression
		}
	}

	return &NATSBroker{
		config:        natsConfig,
		subscriptions: make(map[string]*nats.Subscription),
		handlers:      make(map[string][]core.EventHandler),
		logger:        logger,
		metrics:       metrics,
		stats: &NATSBrokerStats{
			Connected: false,
		},
	}, nil
}

// Connect implements MessageBroker.
func (nb *NATSBroker) Connect(ctx context.Context, config any) error {
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
		nb.logger.Info("connected to NATS", forge.F("url", nb.config.URL), forge.F("name", nb.config.Name))
	}

	if nb.metrics != nil {
		nb.metrics.Counter("forge.events.nats.connections").Inc()
		nb.metrics.Gauge("forge.events.nats.connected").Set(1)
	}

	return nil
}

// Publish implements MessageBroker.
func (nb *NATSBroker) Publish(ctx context.Context, topic string, event core.Event) error {
	nb.mu.RLock()
	defer nb.mu.RUnlock()

	if !nb.connected || nb.conn == nil {
		return errors.New("not connected to NATS")
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
			nb.metrics.Counter("forge.events.nats.publish_errors", forge.WithLabel("topic", topic)).Inc()
		}

		return fmt.Errorf("failed to publish message: %w", err)
	}

	// Update statistics
	duration := time.Since(start)
	nb.stats.MessagesPublished++

	nb.stats.TotalPublishTime += duration
	if nb.stats.MessagesPublished > 0 {
		nb.stats.AvgPublishTime = nb.stats.TotalPublishTime / time.Duration(nb.stats.MessagesPublished)
	}

	if nb.metrics != nil {
		nb.metrics.Counter("forge.events.nats.messages_published", forge.WithLabel("topic", topic)).Inc()
		nb.metrics.Histogram("forge.events.nats.publish_duration", forge.WithLabel("topic", topic)).Observe(duration.Seconds())
	}

	if nb.logger != nil {
		nb.logger.Debug("published event to NATS", forge.F("topic", topic), forge.F("event_id", event.ID), forge.F("event_type", event.Type), forge.F("duration", duration))
	}

	return nil
}

// Subscribe implements MessageBroker.
func (nb *NATSBroker) Subscribe(ctx context.Context, topic string, handler core.EventHandler) error {
	nb.mu.Lock()
	defer nb.mu.Unlock()

	if !nb.connected || nb.conn == nil {
		return errors.New("not connected to NATS")
	}

	// Add handler to the list
	if nb.handlers[topic] == nil {
		nb.handlers[topic] = make([]core.EventHandler, 0)
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
			nb.logger.Info("subscribed to NATS topic", forge.F("topic", topic))
		}

		if nb.metrics != nil {
			nb.metrics.Counter("forge.events.nats.subscriptions", forge.WithLabel("topic", topic)).Inc()
			nb.metrics.Gauge("forge.events.nats.active_subscriptions").Set(float64(nb.stats.Subscriptions))
		}
	}

	if nb.logger != nil {
		nb.logger.Info("handler registered for NATS topic", forge.F("topic", topic), forge.F("handler", handler.Name()))
	}

	return nil
}

// Unsubscribe implements MessageBroker.
func (nb *NATSBroker) Unsubscribe(ctx context.Context, topic string, handlerName string) error {
	nb.mu.Lock()
	defer nb.mu.Unlock()

	// Remove handler from the list
	handlers, exists := nb.handlers[topic]
	if !exists {
		return fmt.Errorf("no handlers for topic %s", topic)
	}

	newHandlers := make([]core.EventHandler, 0)
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
				nb.logger.Info("unsubscribed from NATS topic", forge.F("topic", topic))
			}

			if nb.metrics != nil {
				nb.metrics.Counter("forge.events.nats.unsubscriptions", forge.WithLabel("topic", topic)).Inc()
				nb.metrics.Gauge("forge.events.nats.active_subscriptions").Set(float64(nb.stats.Subscriptions))
			}
		}
	}

	if nb.logger != nil {
		nb.logger.Info("handler unregistered from NATS topic", forge.F("topic", topic), forge.F("handler", handlerName))
	}

	return nil
}

// Close implements MessageBroker.
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
				nb.logger.Error("failed to unsubscribe from topic during close", forge.F("topic", topic), forge.F("error", err))
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
	nb.handlers = make(map[string][]core.EventHandler)
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

// HealthCheck implements MessageBroker.
func (nb *NATSBroker) HealthCheck(ctx context.Context) error {
	nb.mu.RLock()
	defer nb.mu.RUnlock()

	if !nb.connected || nb.conn == nil {
		return errors.New("not connected to NATS")
	}

	if !nb.conn.IsConnected() {
		return errors.New("NATS connection is not active")
	}

	return nil
}

// GetStats implements MessageBroker.
func (nb *NATSBroker) GetStats() map[string]any {
	nb.mu.RLock()
	defer nb.mu.RUnlock()

	stats := map[string]any{
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
		stats["nats_stats"] = map[string]any{
			"in_msgs":    natsStats.InMsgs,
			"out_msgs":   natsStats.OutMsgs,
			"in_bytes":   natsStats.InBytes,
			"out_bytes":  natsStats.OutBytes,
			"reconnects": natsStats.Reconnects,
		}
	}

	return stats
}

// createMessageHandler creates a message handler for NATS subscriptions.
func (nb *NATSBroker) createMessageHandler(topic string) nats.MsgHandler {
	return func(msg *nats.Msg) {
		start := time.Now()

		// Deserialize event
		var event core.Event
		if err := json.Unmarshal(msg.Data, &event); err != nil {
			nb.stats.ReceiveErrors++
			if nb.logger != nil {
				nb.logger.Error("failed to deserialize event from NATS", forge.F("topic", topic), forge.F("error", err))
			}

			if nb.metrics != nil {
				nb.metrics.Counter("forge.events.nats.receive_errors", forge.WithLabel("topic", topic)).Inc()
			}

			return
		}

		nb.stats.MessagesReceived++

		// Get handlers for this topic
		nb.mu.RLock()
		handlers := make([]core.EventHandler, len(nb.handlers[topic]))
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
					nb.logger.Error("handler failed to process NATS event", forge.F("topic", topic), forge.F("handler", handler.Name()), forge.F("event_id", event.ID), forge.F("error", err))
				}

				if nb.metrics != nil {
					nb.metrics.Counter("forge.events.nats.handler_errors", forge.WithLabel("topic", topic), forge.WithLabel("handler", handler.Name())).Inc()
				}
			}
		}

		duration := time.Since(start)

		if nb.metrics != nil {
			nb.metrics.Counter("forge.events.nats.messages_received", forge.WithLabel("topic", topic)).Inc()
			nb.metrics.Histogram("forge.events.nats.receive_duration", forge.WithLabel("topic", topic)).Observe(duration.Seconds())
		}

		if nb.logger != nil {
			nb.logger.Debug("processed NATS message", forge.F("topic", topic), forge.F("event_id", event.ID), forge.F("event_type", event.Type), forge.F("handlers", len(handlers)), forge.F("duration", duration))
		}
	}
}

// Connection event handlers.
func (nb *NATSBroker) handleConnect(conn *nats.Conn) {
	if nb.logger != nil {
		nb.logger.Info("NATS connection established", forge.F("url", conn.ConnectedUrl()))
	}

	if nb.metrics != nil {
		nb.metrics.Counter("forge.events.nats.connects").Inc()
	}
}

func (nb *NATSBroker) handleDisconnect(conn *nats.Conn, err error) {
	if nb.logger != nil {
		if err != nil {
			nb.logger.Error("NATS connection lost", forge.F("error", err))
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
		nb.logger.Info("NATS connection restored", forge.F("url", conn.ConnectedUrl()))
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
		nb.logger.Error("NATS error occurred", forge.F("error", err))
	}

	if nb.metrics != nil {
		nb.metrics.Counter("forge.events.nats.errors").Inc()
	}
}
