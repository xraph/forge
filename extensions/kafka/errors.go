package kafka

import "errors"

var (
	// ErrClientNotInitialized is returned when client is not initialized
	ErrClientNotInitialized = errors.New("kafka: client not initialized")

	// ErrProducerNotEnabled is returned when producer operations are attempted but producer is disabled
	ErrProducerNotEnabled = errors.New("kafka: producer not enabled")

	// ErrConsumerNotEnabled is returned when consumer operations are attempted but consumer is disabled
	ErrConsumerNotEnabled = errors.New("kafka: consumer not enabled")

	// ErrAlreadyConsuming is returned when attempting to start consuming while already consuming
	ErrAlreadyConsuming = errors.New("kafka: already consuming")

	// ErrNotConsuming is returned when attempting to stop consuming while not consuming
	ErrNotConsuming = errors.New("kafka: not consuming")

	// ErrInConsumerGroup is returned when attempting consumer operations while in consumer group
	ErrInConsumerGroup = errors.New("kafka: already in consumer group")

	// ErrNotInConsumerGroup is returned when attempting to leave consumer group while not in one
	ErrNotInConsumerGroup = errors.New("kafka: not in consumer group")

	// ErrSendFailed is returned when message send fails
	ErrSendFailed = errors.New("kafka: send failed")

	// ErrConsumeFailed is returned when message consumption fails
	ErrConsumeFailed = errors.New("kafka: consume failed")

	// ErrTopicNotFound is returned when topic doesn't exist
	ErrTopicNotFound = errors.New("kafka: topic not found")

	// ErrInvalidPartition is returned when partition is invalid
	ErrInvalidPartition = errors.New("kafka: invalid partition")

	// ErrConnectionFailed is returned when connection fails
	ErrConnectionFailed = errors.New("kafka: connection failed")

	// ErrClientClosed is returned when operations are attempted on closed client
	ErrClientClosed = errors.New("kafka: client closed")
)
