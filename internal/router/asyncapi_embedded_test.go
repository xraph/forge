package router

import (
	"testing"
)

// AsyncAPI test types.
type EventMetadata struct {
	Timestamp int64  `json:"timestamp"`
	Source    string `json:"source"`
}

type UserEvent struct {
	EventMetadata

	UserID string `json:"user_id"`
	Action string `json:"action"`
}

type NotificationPayload struct {
	*EventMetadata

	Message string `json:"message"`
	Title   string `json:"title"`
}

func TestAsyncAPIGenerator_EmbeddedStructs(t *testing.T) {
	components := make(map[string]*Schema)
	gen := newAsyncAPISchemaGenerator(components, nil)

	// Test message schema with embedded struct
	msg, err := gen.GenerateMessageSchema(UserEvent{}, "application/json")
	if err != nil {
		t.Fatalf("GenerateMessageSchema failed: %v", err)
	}

	if msg.Payload == nil {
		t.Fatal("Expected payload to be generated")
	}

	// Verify embedded fields are flattened
	expectedFields := []string{"timestamp", "source", "user_id", "action"}
	for _, field := range expectedFields {
		if _, exists := msg.Payload.Properties[field]; !exists {
			t.Errorf("Expected field %s to be present in payload", field)
		}
	}

	// Verify field types
	if msg.Payload.Properties["timestamp"].Type != "integer" {
		t.Error("Expected timestamp to be integer")
	}

	if msg.Payload.Properties["source"].Type != "string" {
		t.Error("Expected source to be string")
	}

	if msg.Payload.Properties["user_id"].Type != "string" {
		t.Error("Expected user_id to be string")
	}
}

func TestAsyncAPIGenerator_EmbeddedPointerStruct(t *testing.T) {
	components := make(map[string]*Schema)
	gen := newAsyncAPISchemaGenerator(components, nil)

	// Test message schema with embedded pointer struct
	msg, err := gen.GenerateMessageSchema(NotificationPayload{}, "application/json")
	if err != nil {
		t.Fatalf("GenerateMessageSchema failed: %v", err)
	}

	if msg.Payload == nil {
		t.Fatal("Expected payload to be generated")
	}

	// Verify embedded pointer fields are flattened
	expectedFields := []string{"timestamp", "source", "message", "title"}
	for _, field := range expectedFields {
		if _, exists := msg.Payload.Properties[field]; !exists {
			t.Errorf("Expected field %s to be present in payload", field)
		}
	}
}

func TestAsyncAPIGenerator_SplitMessageComponents_WithEmbedded(t *testing.T) {
	components := make(map[string]*Schema)
	gen := newAsyncAPISchemaGenerator(components, nil)

	// Test splitting message components with embedded struct
	_, payload := gen.SplitMessageComponents(UserEvent{})

	if payload == nil {
		t.Fatal("Expected payload to be generated")
	}

	// Verify embedded fields are flattened in payload
	expectedFields := []string{"timestamp", "source", "user_id", "action"}
	for _, field := range expectedFields {
		if _, exists := payload.Properties[field]; !exists {
			t.Errorf("Expected field %s to be present in payload", field)
		}
	}
}
