package mqtt

import "errors"

var (
	// ErrNotConnected is returned when operation requires connection
	ErrNotConnected = errors.New("mqtt: not connected")

	// ErrAlreadyConnected is returned when already connected
	ErrAlreadyConnected = errors.New("mqtt: already connected")

	// ErrConnectionFailed is returned when connection fails
	ErrConnectionFailed = errors.New("mqtt: connection failed")

	// ErrPublishFailed is returned when publish fails
	ErrPublishFailed = errors.New("mqtt: publish failed")

	// ErrSubscribeFailed is returned when subscription fails
	ErrSubscribeFailed = errors.New("mqtt: subscribe failed")

	// ErrUnsubscribeFailed is returned when unsubscribe fails
	ErrUnsubscribeFailed = errors.New("mqtt: unsubscribe failed")

	// ErrInvalidQoS is returned when QoS value is invalid
	ErrInvalidQoS = errors.New("mqtt: invalid QoS value")

	// ErrInvalidTopic is returned when topic is invalid
	ErrInvalidTopic = errors.New("mqtt: invalid topic")

	// ErrTimeout is returned when operation times out
	ErrTimeout = errors.New("mqtt: operation timeout")
)
