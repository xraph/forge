package mqtt

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
		{"ErrNotConnected", ErrNotConnected, "mqtt: not connected"},
		{"ErrAlreadyConnected", ErrAlreadyConnected, "mqtt: already connected"},
		{"ErrConnectionFailed", ErrConnectionFailed, "mqtt: connection failed"},
		{"ErrPublishFailed", ErrPublishFailed, "mqtt: publish failed"},
		{"ErrSubscribeFailed", ErrSubscribeFailed, "mqtt: subscribe failed"},
		{"ErrUnsubscribeFailed", ErrUnsubscribeFailed, "mqtt: unsubscribe failed"},
		{"ErrInvalidQoS", ErrInvalidQoS, "mqtt: invalid QoS value"},
		{"ErrInvalidTopic", ErrInvalidTopic, "mqtt: invalid topic"},
		{"ErrTimeout", ErrTimeout, "mqtt: operation timeout"},
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
