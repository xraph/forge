package mqtt

import (
	"context"
	"testing"
	"time"

	"github.com/xraph/forge/internal/logger"
	"github.com/xraph/forge/internal/metrics"
)

func TestNewMQTTClient(t *testing.T) {
	config := DefaultConfig()
	log := logger.NewLogger(logger.Config{Level: "info"})
	met := metrics.NewMetrics(metrics.Config{})

	client := NewMQTTClient(config, log, met)
	if client == nil {
		t.Fatal("expected client to be created")
	}

	mqttClient, ok := client.(*mqttClient)
	if !ok {
		t.Fatal("expected client to be *mqttClient")
	}

	if mqttClient.config.Broker != config.Broker {
		t.Errorf("expected broker %s, got %s", config.Broker, mqttClient.config.Broker)
	}

	if mqttClient.subscriptions == nil {
		t.Error("expected subscriptions map to be initialized")
	}
}

func TestMQTTClientGetStats(t *testing.T) {
	config := DefaultConfig()
	log := logger.NewLogger(logger.Config{Level: "info"})
	met := metrics.NewMetrics(metrics.Config{})

	client := NewMQTTClient(config, log, met).(*mqttClient)

	// Set some stats
	client.mu.Lock()
	client.stats.Connected = true
	client.stats.MessagesSent = 10
	client.stats.MessagesReceived = 20
	client.stats.Subscriptions = 5
	client.stats.Reconnects = 2
	client.mu.Unlock()

	stats := client.GetStats()

	if !stats.Connected {
		t.Error("expected connected to be true")
	}

	if stats.MessagesSent != 10 {
		t.Errorf("expected 10 messages sent, got %d", stats.MessagesSent)
	}

	if stats.MessagesReceived != 20 {
		t.Errorf("expected 20 messages received, got %d", stats.MessagesReceived)
	}

	if stats.Subscriptions != 5 {
		t.Errorf("expected 5 subscriptions, got %d", stats.Subscriptions)
	}

	if stats.Reconnects != 2 {
		t.Errorf("expected 2 reconnects, got %d", stats.Reconnects)
	}
}

func TestMQTTClientGetSubscriptions(t *testing.T) {
	config := DefaultConfig()
	log := logger.NewLogger(logger.Config{Level: "info"})
	met := metrics.NewMetrics(metrics.Config{})

	client := NewMQTTClient(config, log, met).(*mqttClient)

	// Add some subscriptions
	client.mu.Lock()
	client.subscriptions["topic1"] = &subscription{
		Topic: "topic1",
		QoS:   1,
	}
	client.subscriptions["topic2"] = &subscription{
		Topic: "topic2",
		QoS:   2,
	}
	client.mu.Unlock()

	subs := client.GetSubscriptions()

	if len(subs) != 2 {
		t.Errorf("expected 2 subscriptions, got %d", len(subs))
	}

	// Check if both topics are present
	foundTopic1 := false
	foundTopic2 := false
	for _, sub := range subs {
		if sub.Topic == "topic1" && sub.QoS == 1 {
			foundTopic1 = true
		}
		if sub.Topic == "topic2" && sub.QoS == 2 {
			foundTopic2 = true
		}
	}

	if !foundTopic1 {
		t.Error("expected to find topic1")
	}

	if !foundTopic2 {
		t.Error("expected to find topic2")
	}
}

func TestMQTTClientIsConnected(t *testing.T) {
	config := DefaultConfig()
	log := logger.NewLogger(logger.Config{Level: "info"})
	met := metrics.NewMetrics(metrics.Config{})

	client := NewMQTTClient(config, log, met).(*mqttClient)

	// Initially not connected (no underlying client)
	if client.IsConnected() {
		t.Error("expected client to not be connected")
	}
}

func TestMQTTClientGetClient(t *testing.T) {
	config := DefaultConfig()
	log := logger.NewLogger(logger.Config{Level: "info"})
	met := metrics.NewMetrics(metrics.Config{})

	client := NewMQTTClient(config, log, met).(*mqttClient)

	underlyingClient := client.GetClient()
	if underlyingClient != nil {
		t.Error("expected underlying client to be nil")
	}
}

func TestMQTTClientPing(t *testing.T) {
	config := DefaultConfig()
	log := logger.NewLogger(logger.Config{Level: "info"})
	met := metrics.NewMetrics(metrics.Config{})

	client := NewMQTTClient(config, log, met)

	ctx := context.Background()

	// Should fail when not connected
	err := client.Ping(ctx)
	if err == nil {
		t.Error("expected ping to fail when not connected")
	}
}

func TestMQTTClientAddRoute(t *testing.T) {
	config := DefaultConfig()
	log := logger.NewLogger(logger.Config{Level: "info"})
	met := metrics.NewMetrics(metrics.Config{})

	client := NewMQTTClient(config, log, met)

	// Should not panic when client is nil
	client.AddRoute("test/topic", func(c mqttclient.Client, m mqttclient.Message) {})
}

func TestMQTTClientSetDefaultHandler(t *testing.T) {
	config := DefaultConfig()
	log := logger.NewLogger(logger.Config{Level: "info"})
	met := metrics.NewMetrics(metrics.Config{})

	client := NewMQTTClient(config, log, met)

	// Should not panic when client is nil
	client.SetDefaultHandler(func(c mqttclient.Client, m mqttclient.Message) {})
}

func TestMQTTClientMetricRecording(t *testing.T) {
	config := DefaultConfig()
	config.EnableMetrics = true
	log := logger.NewLogger(logger.Config{Level: "info"})
	met := metrics.NewMetrics(metrics.Config{})

	client := NewMQTTClient(config, log, met).(*mqttClient)

	// Test metric recording methods (should not panic)
	client.recordConnectionMetric(true)
	client.recordConnectionMetric(false)
	client.recordPublishMetric("test/topic")
	client.recordReceiveMetric("test/topic")
	client.recordReconnectMetric()
}

func TestMQTTClientMetricRecordingDisabled(t *testing.T) {
	config := DefaultConfig()
	config.EnableMetrics = false
	log := logger.NewLogger(logger.Config{Level: "info"})

	client := NewMQTTClient(config, log, nil).(*mqttClient)

	// Test metric recording with nil metrics (should not panic)
	client.recordConnectionMetric(true)
	client.recordPublishMetric("test/topic")
	client.recordReceiveMetric("test/topic")
	client.recordReconnectMetric()
}

func TestSubscriptionStruct(t *testing.T) {
	sub := &subscription{
		Topic:   "test/topic",
		QoS:     1,
		Handler: func(c mqttclient.Client, m mqttclient.Message) {},
	}

	if sub.Topic != "test/topic" {
		t.Errorf("expected topic 'test/topic', got %s", sub.Topic)
	}

	if sub.QoS != 1 {
		t.Errorf("expected QoS 1, got %d", sub.QoS)
	}

	if sub.Handler == nil {
		t.Error("expected handler to not be nil")
	}
}

func TestMQTTClientSetHandlers(t *testing.T) {
	config := DefaultConfig()
	log := logger.NewLogger(logger.Config{Level: "info"})
	met := metrics.NewMetrics(metrics.Config{})

	client := NewMQTTClient(config, log, met)

	// These should log warnings but not panic
	client.SetOnConnectHandler(func(c mqttclient.Client) {})
	client.SetConnectionLostHandler(func(c mqttclient.Client, err error) {})
	client.SetReconnectingHandler(func(c mqttclient.Client, opts *mqttclient.ClientOptions) {})
}

func TestBuildTLSConfigErrors(t *testing.T) {
	config := DefaultConfig()
	config.EnableTLS = true
	config.TLSCertFile = "/nonexistent/cert.pem"
	config.TLSKeyFile = "/nonexistent/key.pem"
	config.TLSSkipVerify = false
	log := logger.NewLogger(logger.Config{Level: "info"})
	met := metrics.NewMetrics(metrics.Config{})

	client := NewMQTTClient(config, log, met).(*mqttClient)

	_, err := client.buildTLSConfig()
	if err == nil {
		t.Error("expected error when loading nonexistent cert files")
	}
}

func TestBuildTLSConfigWithCA(t *testing.T) {
	config := DefaultConfig()
	config.EnableTLS = true
	config.TLSSkipVerify = true
	config.TLSCAFile = "/nonexistent/ca.pem"
	log := logger.NewLogger(logger.Config{Level: "info"})
	met := metrics.NewMetrics(metrics.Config{})

	client := NewMQTTClient(config, log, met).(*mqttClient)

	_, err := client.buildTLSConfig()
	if err == nil {
		t.Error("expected error when loading nonexistent CA file")
	}
}

func TestBuildTLSConfigSkipVerify(t *testing.T) {
	config := DefaultConfig()
	config.EnableTLS = true
	config.TLSSkipVerify = true
	log := logger.NewLogger(logger.Config{Level: "info"})
	met := metrics.NewMetrics(metrics.Config{})

	client := NewMQTTClient(config, log, met).(*mqttClient)

	tlsConfig, err := client.buildTLSConfig()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if !tlsConfig.InsecureSkipVerify {
		t.Error("expected InsecureSkipVerify to be true")
	}
}
