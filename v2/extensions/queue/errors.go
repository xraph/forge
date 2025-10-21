package queue

import "errors"

// Common queue errors
var (
	ErrNotConnected       = errors.New("queue: not connected")
	ErrAlreadyConnected   = errors.New("queue: already connected")
	ErrConnectionFailed   = errors.New("queue: connection failed")
	ErrQueueNotFound      = errors.New("queue: queue not found")
	ErrQueueAlreadyExists = errors.New("queue: queue already exists")
	ErrMessageNotFound    = errors.New("queue: message not found")
	ErrInvalidMessage     = errors.New("queue: invalid message")
	ErrConsumerNotFound   = errors.New("queue: consumer not found")
	ErrPublishFailed      = errors.New("queue: publish failed")
	ErrConsumeFailed      = errors.New("queue: consume failed")
	ErrAckFailed          = errors.New("queue: acknowledgment failed")
	ErrNackFailed         = errors.New("queue: negative acknowledgment failed")
	ErrTimeout            = errors.New("queue: operation timeout")
	ErrInvalidConfig      = errors.New("queue: invalid configuration")
	ErrUnsupportedDriver  = errors.New("queue: unsupported driver")
	ErrMessageTooLarge    = errors.New("queue: message too large")
	ErrQueueFull          = errors.New("queue: queue is full")
)
