package kafka

import (
	"github.com/IBM/sarama"
	"go.opentelemetry.io/otel/propagation"

	"github.com/xraph/forge"
	"github.com/xraph/forge/messaging"
)

func newTelemetry(cfg Config, m forge.Metrics) *messaging.Telemetry {
	opts := []messaging.Option{messaging.WithTracing(cfg.EnableTracing)}
	if cfg.EnableMetrics && m != nil {
		opts = append(opts, messaging.WithMetrics(m))
	}

	return messaging.New("kafka", opts...)
}

// ConsumerHeaderCarrier adapts a consumed record's headers to a
// TextMapCarrier. Use it in consumer-group handlers you write yourself:
//
//	err := mt.Process(sess.Context(), kafka.ConsumerHeaderCarrier(msg), msg.Topic, "process", handle)
func ConsumerHeaderCarrier(msg *sarama.ConsumerMessage) propagation.TextMapCarrier {
	return consumerCarrier{msg: msg}
}

// ProducerHeaderCarrier adapts an outgoing record's headers to a
// TextMapCarrier.
func ProducerHeaderCarrier(msg *sarama.ProducerMessage) propagation.TextMapCarrier {
	return producerCarrier{msg: msg}
}

type consumerCarrier struct{ msg *sarama.ConsumerMessage }

func (c consumerCarrier) Get(key string) string {
	for _, h := range c.msg.Headers {
		if h != nil && string(h.Key) == key {
			return string(h.Value)
		}
	}

	return ""
}

// Set replaces a header with the same key or appends one.
func (c consumerCarrier) Set(key, value string) {
	for _, h := range c.msg.Headers {
		if h != nil && string(h.Key) == key {
			h.Value = []byte(value)

			return
		}
	}

	c.msg.Headers = append(c.msg.Headers, &sarama.RecordHeader{Key: []byte(key), Value: []byte(value)})
}

func (c consumerCarrier) Keys() []string {
	keys := make([]string, 0, len(c.msg.Headers))
	for _, h := range c.msg.Headers {
		if h != nil {
			keys = append(keys, string(h.Key))
		}
	}

	return keys
}

type producerCarrier struct{ msg *sarama.ProducerMessage }

func (c producerCarrier) Get(key string) string {
	for _, h := range c.msg.Headers {
		if string(h.Key) == key {
			return string(h.Value)
		}
	}

	return ""
}

// Set replaces a header with the same key or appends one.
func (c producerCarrier) Set(key, value string) {
	for i := range c.msg.Headers {
		if string(c.msg.Headers[i].Key) == key {
			c.msg.Headers[i].Value = []byte(value)

			return
		}
	}

	c.msg.Headers = append(c.msg.Headers, sarama.RecordHeader{Key: []byte(key), Value: []byte(value)})
}

func (c producerCarrier) Keys() []string {
	keys := make([]string, 0, len(c.msg.Headers))
	for _, h := range c.msg.Headers {
		keys = append(keys, string(h.Key))
	}

	return keys
}

// batchDestination is the topic every message shares, or "" for a mixed batch.
func batchDestination(messages []*ProducerMessage) string {
	if len(messages) == 0 {
		return ""
	}

	topic := messages[0].Topic
	for _, msg := range messages[1:] {
		if msg.Topic != topic {
			return ""
		}
	}

	return topic
}
