package kafka

import (
	"fmt"
	"testing"

	broker "github.com/segmentio/kafka-go"
)

func TestRetryOnlyBrokerRejectionsBeforeAppend(t *testing.T) {
	t.Parallel()

	for _, code := range []broker.Error{broker.UnknownTopicOrPartition, broker.LeaderNotAvailable, broker.NotLeaderForPartition, broker.NotEnoughReplicas} {
		t.Run(code.Title(), func(t *testing.T) {
			t.Parallel()

			if !rejectedBeforeAppend(fmt.Errorf("broker response: %w", code)) {
				t.Fatal("a confirmed rejection before append should permit a retry")
			}
		})
	}

	for _, code := range []broker.Error{broker.NotEnoughReplicasAfterAppend, broker.RequestTimedOut, broker.NetworkException, broker.KafkaStorageError, broker.Unknown, broker.TopicAuthorizationFailed} {
		t.Run(code.Title(), func(t *testing.T) {
			t.Parallel()

			if rejectedBeforeAppend(code) {
				t.Fatal("an ambiguous append or authorization failure must not retry")
			}
		})
	}
}
