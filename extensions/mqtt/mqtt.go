package mqtt

import (
	"context"
	"time"

	mqttclient "github.com/eclipse/paho.mqtt.golang"
)

// MQTT represents a unified MQTT client interface
type MQTT interface {
	// Connection management
	Connect(ctx context.Context) error
	Disconnect(ctx context.Context) error
	IsConnected() bool
	Reconnect() error

	// Publishing
	Publish(topic string, qos byte, retained bool, payload interface{}) error
	PublishAsync(topic string, qos byte, retained bool, payload interface{}) error

	// Subscribing
	Subscribe(topic string, qos byte, handler MessageHandler) error
	SubscribeMultiple(filters map[string]byte, handler MessageHandler) error
	Unsubscribe(topics ...string) error

	// Message handling
	AddRoute(topic string, handler MessageHandler)
	SetDefaultHandler(handler MessageHandler)
	SetOnConnectHandler(handler ConnectHandler)
	SetConnectionLostHandler(handler ConnectionLostHandler)
	SetReconnectingHandler(handler ReconnectingHandler)

	// Client info
	GetClient() mqttclient.Client
	GetStats() ClientStats
	GetSubscriptions() []SubscriptionInfo

	// Health
	Ping(ctx context.Context) error
}

// MessageHandler processes incoming MQTT messages
type MessageHandler func(client mqttclient.Client, msg mqttclient.Message)

// ConnectHandler is called when connection is established
type ConnectHandler func(client mqttclient.Client)

// ConnectionLostHandler is called when connection is lost
type ConnectionLostHandler func(client mqttclient.Client, err error)

// ReconnectingHandler is called when client is reconnecting
type ReconnectingHandler func(client mqttclient.Client, options *mqttclient.ClientOptions)

// SubscriptionInfo contains subscription metadata
type SubscriptionInfo struct {
	Topic   string
	QoS     byte
	Handler string // Handler function name
}

// ClientStats contains client statistics
type ClientStats struct {
	Connected        bool
	ConnectTime      time.Time
	LastMessageTime  time.Time
	MessagesReceived int64
	MessagesSent     int64
	Subscriptions    int
	Reconnects       int64
}
