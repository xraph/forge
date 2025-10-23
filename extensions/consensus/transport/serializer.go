package transport

import (
	"bytes"
	"encoding/gob"
	"encoding/json"
	"fmt"

	"github.com/xraph/forge/extensions/consensus/internal"
	"google.golang.org/protobuf/proto"
)

// SerializerType represents the type of serializer
type SerializerType string

const (
	// SerializerGob uses Go's gob encoding
	SerializerGob SerializerType = "gob"
	// SerializerJSON uses JSON encoding
	SerializerJSON SerializerType = "json"
	// SerializerProtobuf uses Protocol Buffers
	SerializerProtobuf SerializerType = "protobuf"
)

// Serializer defines the interface for message serialization
type Serializer interface {
	Serialize(msg interface{}) ([]byte, error)
	Deserialize(data []byte, msg interface{}) error
	Type() SerializerType
}

// GobSerializer implements gob-based serialization
type GobSerializer struct{}

// NewGobSerializer creates a new gob serializer
func NewGobSerializer() *GobSerializer {
	return &GobSerializer{}
}

// Serialize serializes a message using gob
func (gs *GobSerializer) Serialize(msg interface{}) ([]byte, error) {
	var buf bytes.Buffer
	encoder := gob.NewEncoder(&buf)

	if err := encoder.Encode(msg); err != nil {
		return nil, fmt.Errorf("gob encode failed: %w", err)
	}

	return buf.Bytes(), nil
}

// Deserialize deserializes a message using gob
func (gs *GobSerializer) Deserialize(data []byte, msg interface{}) error {
	buf := bytes.NewReader(data)
	decoder := gob.NewDecoder(buf)

	if err := decoder.Decode(msg); err != nil {
		return fmt.Errorf("gob decode failed: %w", err)
	}

	return nil
}

// Type returns the serializer type
func (gs *GobSerializer) Type() SerializerType {
	return SerializerGob
}

// JSONSerializer implements JSON-based serialization
type JSONSerializer struct{}

// NewJSONSerializer creates a new JSON serializer
func NewJSONSerializer() *JSONSerializer {
	return &JSONSerializer{}
}

// Serialize serializes a message using JSON
func (js *JSONSerializer) Serialize(msg interface{}) ([]byte, error) {
	data, err := json.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("json marshal failed: %w", err)
	}

	return data, nil
}

// Deserialize deserializes a message using JSON
func (js *JSONSerializer) Deserialize(data []byte, msg interface{}) error {
	if err := json.Unmarshal(data, msg); err != nil {
		return fmt.Errorf("json unmarshal failed: %w", err)
	}

	return nil
}

// Type returns the serializer type
func (js *JSONSerializer) Type() SerializerType {
	return SerializerJSON
}

// ProtobufSerializer implements Protocol Buffers serialization
type ProtobufSerializer struct{}

// NewProtobufSerializer creates a new protobuf serializer
func NewProtobufSerializer() *ProtobufSerializer {
	return &ProtobufSerializer{}
}

// Serialize serializes a message using protobuf
func (ps *ProtobufSerializer) Serialize(msg interface{}) ([]byte, error) {
	pbMsg, ok := msg.(proto.Message)
	if !ok {
		return nil, fmt.Errorf("message does not implement proto.Message")
	}

	data, err := proto.Marshal(pbMsg)
	if err != nil {
		return nil, fmt.Errorf("protobuf marshal failed: %w", err)
	}

	return data, nil
}

// Deserialize deserializes a message using protobuf
func (ps *ProtobufSerializer) Deserialize(data []byte, msg interface{}) error {
	pbMsg, ok := msg.(proto.Message)
	if !ok {
		return fmt.Errorf("message does not implement proto.Message")
	}

	if err := proto.Unmarshal(data, pbMsg); err != nil {
		return fmt.Errorf("protobuf unmarshal failed: %w", err)
	}

	return nil
}

// Type returns the serializer type
func (ps *ProtobufSerializer) Type() SerializerType {
	return SerializerProtobuf
}

// MessageEnvelope wraps a message with metadata
type MessageEnvelope struct {
	Type           internal.MessageType
	From           string
	To             string
	Data           []byte
	Timestamp      int64
	SerializerType SerializerType
}

// MessageCodec provides encoding/decoding with envelope
type MessageCodec struct {
	serializer Serializer
}

// NewMessageCodec creates a new message codec
func NewMessageCodec(serializerType SerializerType) *MessageCodec {
	var serializer Serializer

	switch serializerType {
	case SerializerJSON:
		serializer = NewJSONSerializer()
	case SerializerProtobuf:
		serializer = NewProtobufSerializer()
	default:
		serializer = NewGobSerializer()
	}

	return &MessageCodec{
		serializer: serializer,
	}
}

// Encode encodes a message into an envelope
func (mc *MessageCodec) Encode(msg internal.Message) ([]byte, error) {
	// Serialize the message data
	data, err := mc.serializer.Serialize(msg)
	if err != nil {
		return nil, err
	}

	// Create envelope
	envelope := MessageEnvelope{
		Type:           msg.Type,
		From:           msg.From,
		To:             msg.To,
		Data:           data,
		Timestamp:      msg.Timestamp,
		SerializerType: mc.serializer.Type(),
	}

	// Serialize envelope using JSON (for envelope metadata)
	return json.Marshal(envelope)
}

// Decode decodes an envelope into a message
func (mc *MessageCodec) Decode(data []byte) (*internal.Message, error) {
	// Deserialize envelope
	var envelope MessageEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, fmt.Errorf("failed to unmarshal envelope: %w", err)
	}

	// Deserialize message data
	var msg internal.Message
	if err := mc.serializer.Deserialize(envelope.Data, &msg); err != nil {
		return nil, err
	}

	// Populate envelope fields
	msg.Type = envelope.Type
	msg.From = envelope.From
	msg.To = envelope.To
	msg.Timestamp = envelope.Timestamp

	return &msg, nil
}

// GetSerializer returns the underlying serializer
func (mc *MessageCodec) GetSerializer() Serializer {
	return mc.serializer
}

// CompressedSerializer wraps a serializer with compression
type CompressedSerializer struct {
	serializer Serializer
	// TODO: Add compression (gzip, snappy, etc.)
}

// NewCompressedSerializer creates a compressed serializer
func NewCompressedSerializer(serializer Serializer) *CompressedSerializer {
	return &CompressedSerializer{
		serializer: serializer,
	}
}

// Serialize compresses and serializes
func (cs *CompressedSerializer) Serialize(msg interface{}) ([]byte, error) {
	// First serialize
	data, err := cs.serializer.Serialize(msg)
	if err != nil {
		return nil, err
	}

	// TODO: Compress data
	// For now, just return uncompressed
	return data, nil
}

// Deserialize decompresses and deserializes
func (cs *CompressedSerializer) Deserialize(data []byte, msg interface{}) error {
	// TODO: Decompress data
	// For now, data is uncompressed

	// Deserialize
	return cs.serializer.Deserialize(data, msg)
}

// Type returns the underlying serializer type
func (cs *CompressedSerializer) Type() SerializerType {
	return cs.serializer.Type()
}
