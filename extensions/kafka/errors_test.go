package kafka

import (
	"errors"
	"testing"
)

func TestErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
		msg  string
	}{
		{"ErrClientNotInitialized", ErrClientNotInitialized, "kafka: client not initialized"},
		{"ErrProducerNotEnabled", ErrProducerNotEnabled, "kafka: producer not enabled"},
		{"ErrConsumerNotEnabled", ErrConsumerNotEnabled, "kafka: consumer not enabled"},
		{"ErrAlreadyConsuming", ErrAlreadyConsuming, "kafka: already consuming"},
		{"ErrNotConsuming", ErrNotConsuming, "kafka: not consuming"},
		{"ErrInConsumerGroup", ErrInConsumerGroup, "kafka: already in consumer group"},
		{"ErrNotInConsumerGroup", ErrNotInConsumerGroup, "kafka: not in consumer group"},
		{"ErrSendFailed", ErrSendFailed, "kafka: send failed"},
		{"ErrConsumeFailed", ErrConsumeFailed, "kafka: consume failed"},
		{"ErrTopicNotFound", ErrTopicNotFound, "kafka: topic not found"},
		{"ErrInvalidPartition", ErrInvalidPartition, "kafka: invalid partition"},
		{"ErrConnectionFailed", ErrConnectionFailed, "kafka: connection failed"},
		{"ErrClientClosed", ErrClientClosed, "kafka: client closed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err.Error() != tt.msg {
				t.Errorf("expected error message '%s', got '%s'", tt.msg, tt.err.Error())
			}

			if !errors.Is(tt.err, tt.err) {
				t.Errorf("errors.Is check failed for %s", tt.name)
			}
		})
	}
}
