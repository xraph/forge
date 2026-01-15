package mqtt

import (
	"context"
	"fmt"

	"github.com/xraph/forge"
)

// MQTTService wraps an MQTT client and provides lifecycle management.
// It implements vessel's di.Service interface so Vessel can manage its lifecycle.
type MQTTService struct {
	config  Config
	client  MQTT
	logger  forge.Logger
	metrics forge.Metrics
}

// NewMQTTService creates a new MQTT service with the given configuration.
// This is the constructor that will be registered with the DI container.
func NewMQTTService(config Config, logger forge.Logger, metrics forge.Metrics) (*MQTTService, error) {
	// Validate config
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid mqtt config: %w", err)
	}

	// Create MQTT client
	client, err := NewMQTTClient(config, logger, metrics)
	if err != nil {
		return nil, fmt.Errorf("failed to create mqtt client: %w", err)
	}

	return &MQTTService{
		config:  config,
		client:  client,
		logger:  logger,
		metrics: metrics,
	}, nil
}

// Name returns the service name for Vessel's lifecycle management.
func (s *MQTTService) Name() string {
	return "mqtt-service"
}

// Start starts the MQTT service by connecting to broker.
// This is called automatically by Vessel during container.Start().
func (s *MQTTService) Start(ctx context.Context) error {
	s.logger.Info("starting mqtt service",
		forge.F("broker", s.config.Broker),
		forge.F("client_id", s.config.ClientID),
	)

	// Connect to MQTT broker
	if err := s.client.Connect(ctx); err != nil {
		return fmt.Errorf("failed to connect to mqtt broker: %w", err)
	}

	s.logger.Info("mqtt service started")
	return nil
}

// Stop stops the MQTT service by disconnecting from broker.
// This is called automatically by Vessel during container.Stop().
func (s *MQTTService) Stop(ctx context.Context) error {
	s.logger.Info("stopping mqtt service")

	if s.client != nil {
		if err := s.client.Disconnect(ctx); err != nil {
			s.logger.Error("failed to disconnect mqtt client", forge.F("error", err))
			// Don't return error, log and continue
		}
	}

	s.logger.Info("mqtt service stopped")
	return nil
}

// Health checks if the MQTT service is healthy.
func (s *MQTTService) Health(ctx context.Context) error {
	if s.client == nil {
		return fmt.Errorf("mqtt client not initialized")
	}

	if err := s.client.Ping(ctx); err != nil {
		return fmt.Errorf("mqtt health check failed: %w", err)
	}

	return nil
}

// Client returns the underlying MQTT client.
func (s *MQTTService) Client() MQTT {
	return s.client
}

// Delegate MQTT interface methods to client

func (s *MQTTService) Connect(ctx context.Context) error {
	return s.client.Connect(ctx)
}

func (s *MQTTService) Disconnect(ctx context.Context) error {
	return s.client.Disconnect(ctx)
}

func (s *MQTTService) Publish(ctx context.Context, topic string, payload []byte, opts ...PublishOption) error {
	return s.client.Publish(ctx, topic, payload, opts...)
}

func (s *MQTTService) Subscribe(ctx context.Context, topic string, handler MessageHandler, opts ...SubscribeOption) error {
	return s.client.Subscribe(ctx, topic, handler, opts...)
}

func (s *MQTTService) Unsubscribe(ctx context.Context, topic string) error {
	return s.client.Unsubscribe(ctx, topic)
}

func (s *MQTTService) IsConnected() bool {
	return s.client.IsConnected()
}

func (s *MQTTService) Ping(ctx context.Context) error {
	return s.client.Ping(ctx)
}
