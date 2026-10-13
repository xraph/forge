package mqtt

import (
	"testing"

	mqttclient "github.com/eclipse/paho.mqtt.golang"
)

func TestMQTTInterface(t *testing.T) {
	// Test that our interface matches expectations
	var _ MQTT = (*mqttClient)(nil)
}

func TestMessageHandlerType(t *testing.T) {
	called := false

	var handler MessageHandler = func(client mqttclient.Client, msg mqttclient.Message) { called = true }

	handler(nil, nil)

	if !called {
		t.Error("handler was not invoked")
	}
}

func TestConnectHandlerType(t *testing.T) {
	called := false

	var handler ConnectHandler = func(client mqttclient.Client) { called = true }

	handler(nil)

	if !called {
		t.Error("handler was not invoked")
	}
}

func TestConnectionLostHandlerType(t *testing.T) {
	called := false

	var handler ConnectionLostHandler = func(client mqttclient.Client, err error) { called = true }

	handler(nil, nil)

	if !called {
		t.Error("handler was not invoked")
	}
}

func TestReconnectingHandlerType(t *testing.T) {
	called := false

	var handler ReconnectingHandler = func(client mqttclient.Client, options *mqttclient.ClientOptions) { called = true }

	handler(nil, nil)

	if !called {
		t.Error("handler was not invoked")
	}
}

func TestSubscriptionInfo(t *testing.T) {
	sub := SubscriptionInfo{
		Topic:   "test/topic",
		QoS:     1,
		Handler: "TestHandler",
	}

	if sub.Topic != "test/topic" {
		t.Errorf("expected topic 'test/topic', got %s", sub.Topic)
	}

	if sub.QoS != 1 {
		t.Errorf("expected QoS 1, got %d", sub.QoS)
	}

	if sub.Handler != "TestHandler" {
		t.Errorf("expected handler 'TestHandler', got %s", sub.Handler)
	}
}

func TestClientStats(t *testing.T) {
	stats := ClientStats{
		Connected:        true,
		MessagesReceived: 100,
		MessagesSent:     50,
		Subscriptions:    5,
		Reconnects:       2,
	}

	if !stats.Connected {
		t.Error("expected connected to be true")
	}

	if stats.MessagesReceived != 100 {
		t.Errorf("expected 100 messages received, got %d", stats.MessagesReceived)
	}

	if stats.MessagesSent != 50 {
		t.Errorf("expected 50 messages sent, got %d", stats.MessagesSent)
	}

	if stats.Subscriptions != 5 {
		t.Errorf("expected 5 subscriptions, got %d", stats.Subscriptions)
	}

	if stats.Reconnects != 2 {
		t.Errorf("expected 2 reconnects, got %d", stats.Reconnects)
	}
}
