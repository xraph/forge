package mqtt

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	mqttclient "github.com/eclipse/paho.mqtt.golang"
	"github.com/xraph/forge"
)

// mqttClient implements MQTT interface
type mqttClient struct {
	config        Config
	logger        forge.Logger
	metrics       forge.Metrics
	client        mqttclient.Client
	subscriptions map[string]*subscription
	stats         ClientStats
	mu            sync.RWMutex
}

type subscription struct {
	Topic   string
	QoS     byte
	Handler MessageHandler
}

// NewMQTTClient creates a new MQTT client
func NewMQTTClient(config Config, logger forge.Logger, metrics forge.Metrics) MQTT {
	return &mqttClient{
		config:        config,
		logger:        logger,
		metrics:       metrics,
		subscriptions: make(map[string]*subscription),
	}
}

func (c *mqttClient) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.client != nil && c.client.IsConnected() {
		return fmt.Errorf("already connected")
	}

	// Create client options
	opts := mqttclient.NewClientOptions()
	opts.AddBroker(c.config.Broker)
	opts.SetClientID(c.config.ClientID)

	if c.config.Username != "" {
		opts.SetUsername(c.config.Username)
	}
	if c.config.Password != "" {
		opts.SetPassword(c.config.Password)
	}

	opts.SetCleanSession(c.config.CleanSession)
	opts.SetConnectTimeout(c.config.ConnectTimeout)
	opts.SetKeepAlive(c.config.KeepAlive)
	opts.SetPingTimeout(c.config.PingTimeout)
	opts.SetMaxReconnectInterval(c.config.MaxReconnectDelay)
	opts.SetAutoReconnect(c.config.AutoReconnect)
	opts.SetResumeSubs(c.config.ResumeSubs)
	opts.SetWriteTimeout(c.config.WriteTimeout)
	opts.SetMessageChannelDepth(c.config.MessageChannelDepth)
	opts.SetOrderMatters(c.config.OrderMatters)

	// Setup TLS if enabled
	if c.config.EnableTLS {
		tlsConfig, err := c.buildTLSConfig()
		if err != nil {
			return fmt.Errorf("failed to build TLS config: %w", err)
		}
		opts.SetTLSConfig(tlsConfig)
	}

	// Setup Last Will and Testament
	if c.config.WillEnabled {
		opts.SetWill(c.config.WillTopic, c.config.WillPayload, c.config.WillQoS, c.config.WillRetained)
	}

	// Setup message store
	if c.config.MessageStore == "file" {
		opts.SetStore(mqttclient.NewFileStore(c.config.StoreDirectory))
	}

	// Setup handlers
	opts.SetOnConnectHandler(func(client mqttclient.Client) {
		c.mu.Lock()
		c.stats.Connected = true
		c.stats.ConnectTime = time.Now()
		c.mu.Unlock()

		if c.config.EnableLogging {
			c.logger.Info("mqtt connected", forge.F("broker", c.config.Broker))
		}

		if c.config.EnableMetrics {
			c.recordConnectionMetric(true)
		}
	})

	opts.SetConnectionLostHandler(func(client mqttclient.Client, err error) {
		c.mu.Lock()
		c.stats.Connected = false
		c.mu.Unlock()

		if c.config.EnableLogging {
			c.logger.Warn("mqtt connection lost", forge.F("error", err.Error()))
		}

		if c.config.EnableMetrics {
			c.recordConnectionMetric(false)
		}
	})

	opts.SetReconnectingHandler(func(client mqttclient.Client, options *mqttclient.ClientOptions) {
		c.mu.Lock()
		c.stats.Reconnects++
		c.mu.Unlock()

		if c.config.EnableLogging {
			c.logger.Info("mqtt reconnecting", forge.F("attempt", c.stats.Reconnects))
		}

		if c.config.EnableMetrics {
			c.recordReconnectMetric()
		}
	})

	// Create and connect
	c.client = mqttclient.NewClient(opts)

	token := c.client.Connect()
	if !token.WaitTimeout(c.config.ConnectTimeout) {
		return fmt.Errorf("connection timeout")
	}

	if err := token.Error(); err != nil {
		return fmt.Errorf("connection failed: %w", err)
	}

	c.stats.Connected = true
	c.stats.ConnectTime = time.Now()

	c.logger.Info("mqtt client connected", forge.F("broker", c.config.Broker))
	return nil
}

func (c *mqttClient) Disconnect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.client == nil || !c.client.IsConnected() {
		return fmt.Errorf("not connected")
	}

	c.client.Disconnect(250) // 250ms quiesce period
	c.stats.Connected = false

	c.logger.Info("mqtt client disconnected")
	return nil
}

func (c *mqttClient) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.client != nil && c.client.IsConnected()
}

func (c *mqttClient) Reconnect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.client == nil {
		return fmt.Errorf("client not initialized")
	}

	token := c.client.Connect()
	if !token.WaitTimeout(c.config.ConnectTimeout) {
		return fmt.Errorf("reconnection timeout")
	}

	if err := token.Error(); err != nil {
		return fmt.Errorf("reconnection failed: %w", err)
	}

	return nil
}

func (c *mqttClient) Publish(topic string, qos byte, retained bool, payload interface{}) error {
	c.mu.RLock()
	client := c.client
	c.mu.RUnlock()

	if client == nil || !client.IsConnected() {
		return fmt.Errorf("not connected")
	}

	// Convert payload to bytes
	var data []byte
	switch v := payload.(type) {
	case []byte:
		data = v
	case string:
		data = []byte(v)
	default:
		var err error
		data, err = json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("failed to marshal payload: %w", err)
		}
	}

	token := client.Publish(topic, qos, retained, data)
	token.Wait()

	if err := token.Error(); err != nil {
		c.logger.Error("mqtt publish failed", forge.F("topic", topic), forge.F("error", err))
		return fmt.Errorf("publish failed: %w", err)
	}

	c.mu.Lock()
	c.stats.MessagesSent++
	c.mu.Unlock()

	if c.config.EnableMetrics {
		c.recordPublishMetric(topic)
	}

	if c.config.EnableLogging {
		c.logger.Debug("mqtt message published", forge.F("topic", topic), forge.F("size", len(data)))
	}

	return nil
}

func (c *mqttClient) PublishAsync(topic string, qos byte, retained bool, payload interface{}) error {
	c.mu.RLock()
	client := c.client
	c.mu.RUnlock()

	if client == nil || !client.IsConnected() {
		return fmt.Errorf("not connected")
	}

	// Convert payload to bytes
	var data []byte
	switch v := payload.(type) {
	case []byte:
		data = v
	case string:
		data = []byte(v)
	default:
		var err error
		data, err = json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("failed to marshal payload: %w", err)
		}
	}

	token := client.Publish(topic, qos, retained, data)

	// Handle result in background
	go func() {
		token.Wait()
		if err := token.Error(); err != nil {
			c.logger.Error("mqtt async publish failed", forge.F("topic", topic), forge.F("error", err))
		} else {
			c.mu.Lock()
			c.stats.MessagesSent++
			c.mu.Unlock()

			if c.config.EnableMetrics {
				c.recordPublishMetric(topic)
			}
		}
	}()

	return nil
}

func (c *mqttClient) Subscribe(topic string, qos byte, handler MessageHandler) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.client == nil || !c.client.IsConnected() {
		return fmt.Errorf("not connected")
	}

	// Wrap handler to track stats
	wrappedHandler := func(client mqttclient.Client, msg mqttclient.Message) {
		c.mu.Lock()
		c.stats.MessagesReceived++
		c.stats.LastMessageTime = time.Now()
		c.mu.Unlock()

		if c.config.EnableMetrics {
			c.recordReceiveMetric(msg.Topic())
		}

		handler(client, msg)
	}

	token := c.client.Subscribe(topic, qos, wrappedHandler)
	token.Wait()

	if err := token.Error(); err != nil {
		return fmt.Errorf("subscribe failed: %w", err)
	}

	c.subscriptions[topic] = &subscription{
		Topic:   topic,
		QoS:     qos,
		Handler: handler,
	}

	c.stats.Subscriptions = len(c.subscriptions)

	c.logger.Info("mqtt subscribed", forge.F("topic", topic), forge.F("qos", qos))
	return nil
}

func (c *mqttClient) SubscribeMultiple(filters map[string]byte, handler MessageHandler) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.client == nil || !c.client.IsConnected() {
		return fmt.Errorf("not connected")
	}

	// Wrap handler to track stats
	wrappedHandler := func(client mqttclient.Client, msg mqttclient.Message) {
		c.mu.Lock()
		c.stats.MessagesReceived++
		c.stats.LastMessageTime = time.Now()
		c.mu.Unlock()

		if c.config.EnableMetrics {
			c.recordReceiveMetric(msg.Topic())
		}

		handler(client, msg)
	}

	token := c.client.SubscribeMultiple(filters, wrappedHandler)
	token.Wait()

	if err := token.Error(); err != nil {
		return fmt.Errorf("subscribe multiple failed: %w", err)
	}

	for topic, qos := range filters {
		c.subscriptions[topic] = &subscription{
			Topic:   topic,
			QoS:     qos,
			Handler: handler,
		}
	}

	c.stats.Subscriptions = len(c.subscriptions)

	c.logger.Info("mqtt subscribed to multiple topics", forge.F("count", len(filters)))
	return nil
}

func (c *mqttClient) Unsubscribe(topics ...string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.client == nil || !c.client.IsConnected() {
		return fmt.Errorf("not connected")
	}

	token := c.client.Unsubscribe(topics...)
	token.Wait()

	if err := token.Error(); err != nil {
		return fmt.Errorf("unsubscribe failed: %w", err)
	}

	for _, topic := range topics {
		delete(c.subscriptions, topic)
	}

	c.stats.Subscriptions = len(c.subscriptions)

	c.logger.Info("mqtt unsubscribed", forge.F("topics", topics))
	return nil
}

func (c *mqttClient) AddRoute(topic string, handler MessageHandler) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.client != nil {
		c.client.AddRoute(topic, handler)
	}
}

func (c *mqttClient) SetDefaultHandler(handler MessageHandler) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.client != nil {
		c.client.AddRoute("#", handler) // Subscribe to all topics
	}
}

func (c *mqttClient) SetOnConnectHandler(handler ConnectHandler) {
	// This needs to be set before connection, so we can't change it dynamically
	c.logger.Warn("SetOnConnectHandler should be called before Connect()")
}

func (c *mqttClient) SetConnectionLostHandler(handler ConnectionLostHandler) {
	c.logger.Warn("SetConnectionLostHandler should be called before Connect()")
}

func (c *mqttClient) SetReconnectingHandler(handler ReconnectingHandler) {
	c.logger.Warn("SetReconnectingHandler should be called before Connect()")
}

func (c *mqttClient) GetClient() mqttclient.Client {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.client
}

func (c *mqttClient) GetStats() ClientStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.stats
}

func (c *mqttClient) GetSubscriptions() []SubscriptionInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()

	subs := make([]SubscriptionInfo, 0, len(c.subscriptions))
	for _, sub := range c.subscriptions {
		subs = append(subs, SubscriptionInfo{
			Topic:   sub.Topic,
			QoS:     sub.QoS,
			Handler: "MessageHandler",
		})
	}

	return subs
}

func (c *mqttClient) Ping(ctx context.Context) error {
	if !c.IsConnected() {
		return fmt.Errorf("not connected")
	}

	return nil
}

// buildTLSConfig creates TLS configuration
func (c *mqttClient) buildTLSConfig() (*tls.Config, error) {
	tlsConfig := &tls.Config{
		InsecureSkipVerify: c.config.TLSSkipVerify,
	}

	if c.config.TLSCertFile != "" && c.config.TLSKeyFile != "" {
		cert, err := tls.LoadX509KeyPair(c.config.TLSCertFile, c.config.TLSKeyFile)
		if err != nil {
			return nil, fmt.Errorf("failed to load client cert: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}

	if c.config.TLSCAFile != "" {
		caCert, err := os.ReadFile(c.config.TLSCAFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read CA cert: %w", err)
		}

		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("failed to append CA cert")
		}

		tlsConfig.RootCAs = caCertPool
	}

	return tlsConfig, nil
}

// Metrics recording methods
func (c *mqttClient) recordConnectionMetric(connected bool) {
	if c.metrics == nil {
		return
	}

	status := "disconnected"
	if connected {
		status = "connected"
	}

	c.metrics.Counter("mqtt.connections", "status", status).Inc()
}

func (c *mqttClient) recordPublishMetric(topic string) {
	if c.metrics == nil {
		return
	}

	c.metrics.Counter("mqtt.messages.published", "topic", topic).Inc()
}

func (c *mqttClient) recordReceiveMetric(topic string) {
	if c.metrics == nil {
		return
	}

	c.metrics.Counter("mqtt.messages.received", "topic", topic).Inc()
}

func (c *mqttClient) recordReconnectMetric() {
	if c.metrics == nil {
		return
	}

	c.metrics.Counter("mqtt.reconnects").Inc()
}
