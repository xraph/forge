package streaming

import (
	"encoding/json"
	"fmt"
	"sync"

	streaming "github.com/xraph/forge/extensions/streaming/internal"
)

// Codec handles encoding and decoding messages for a specific content type.
type Codec interface {
	// ContentType returns the MIME type this codec handles (e.g. "application/json").
	ContentType() string
	// Encode serializes a message to bytes.
	Encode(msg *streaming.Message) ([]byte, error)
	// Decode deserializes bytes into a message.
	Decode(data []byte, msg *streaming.Message) error
}

// CodecRegistry manages codecs by content type and provides
// encode/decode dispatch based on message content type.
type CodecRegistry struct {
	mu           sync.RWMutex
	codecs       map[string]Codec
	defaultCodec Codec
}

// NewCodecRegistry creates a new codec registry pre-loaded with a JSON codec as default.
func NewCodecRegistry() *CodecRegistry {
	jsonCodec := &JSONCodec{}
	r := &CodecRegistry{
		codecs: map[string]Codec{
			jsonCodec.ContentType(): jsonCodec,
		},
		defaultCodec: jsonCodec,
	}

	// Register built-in codecs.
	binaryCodec := &BinaryCodec{}
	r.codecs[binaryCodec.ContentType()] = binaryCodec

	textCodec := &TextCodec{}
	r.codecs[textCodec.ContentType()] = textCodec

	return r
}

// Register adds a codec. If a codec for the same content type already exists, it is replaced.
func (r *CodecRegistry) Register(codec Codec) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.codecs[codec.ContentType()] = codec
}

// Get returns the codec for the given content type.
func (r *CodecRegistry) Get(contentType string) (Codec, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	c, ok := r.codecs[contentType]

	return c, ok
}

// Default returns the default codec (JSON).
func (r *CodecRegistry) Default() Codec {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.defaultCodec
}

// SetDefault changes the default codec to the one registered for the given content type.
func (r *CodecRegistry) SetDefault(contentType string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	c, ok := r.codecs[contentType]
	if !ok {
		return fmt.Errorf("no codec registered for content type %q", contentType)
	}

	r.defaultCodec = c

	return nil
}

// Encode serializes a message using the codec matching msg.ContentType,
// or the default codec if ContentType is empty.
func (r *CodecRegistry) Encode(msg *streaming.Message) ([]byte, error) {
	ct := msg.ContentType
	if ct == "" {
		return r.Default().Encode(msg)
	}

	r.mu.RLock()
	c, ok := r.codecs[ct]
	r.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("no codec registered for content type %q", ct)
	}

	return c.Encode(msg)
}

// Decode deserializes bytes into a message using the default codec.
// If decoding fails with the default codec, it returns the error.
func (r *CodecRegistry) Decode(data []byte, msg *streaming.Message) error {
	return r.Default().Decode(data, msg)
}

// DecodeWithType deserializes bytes into a message using the codec for the given content type.
func (r *CodecRegistry) DecodeWithType(contentType string, data []byte, msg *streaming.Message) error {
	if contentType == "" {
		return r.Default().Decode(data, msg)
	}

	r.mu.RLock()
	c, ok := r.codecs[contentType]
	r.mu.RUnlock()

	if !ok {
		return fmt.Errorf("no codec registered for content type %q", contentType)
	}

	return c.Decode(data, msg)
}

// --- Built-in Codecs ---

// JSONCodec encodes/decodes messages as JSON. This is the default codec.
type JSONCodec struct{}

func (c *JSONCodec) ContentType() string { return streaming.ContentTypeJSON }

func (c *JSONCodec) Encode(msg *streaming.Message) ([]byte, error) {
	return json.Marshal(msg)
}

func (c *JSONCodec) Decode(data []byte, msg *streaming.Message) error {
	return json.Unmarshal(data, msg)
}

// BinaryCodec handles raw binary data. On decode, it stores the raw bytes
// in msg.RawData and sets the content type. On encode, it returns msg.RawData directly.
type BinaryCodec struct{}

func (c *BinaryCodec) ContentType() string { return streaming.ContentTypeBinary }

func (c *BinaryCodec) Encode(msg *streaming.Message) ([]byte, error) {
	if msg.RawData != nil {
		return msg.RawData, nil
	}

	// Fall back to JSON encoding the whole message if no RawData.
	return json.Marshal(msg)
}

func (c *BinaryCodec) Decode(data []byte, msg *streaming.Message) error {
	msg.ContentType = streaming.ContentTypeBinary
	msg.RawData = make([]byte, len(data))
	copy(msg.RawData, data)

	if msg.Type == "" {
		msg.Type = streaming.MessageTypeMessage
	}

	return nil
}

// TextCodec handles plain text data. On decode, it stores the text in msg.Data
// as a string. On encode, it converts msg.Data to a string and returns bytes.
type TextCodec struct{}

func (c *TextCodec) ContentType() string { return streaming.ContentTypeText }

func (c *TextCodec) Encode(msg *streaming.Message) ([]byte, error) {
	if msg.RawData != nil {
		return msg.RawData, nil
	}

	if s, ok := msg.Data.(string); ok {
		return []byte(s), nil
	}

	// Fall back to JSON for non-string data.
	return json.Marshal(msg.Data)
}

func (c *TextCodec) Decode(data []byte, msg *streaming.Message) error {
	msg.ContentType = streaming.ContentTypeText
	msg.Data = string(data)

	if msg.Type == "" {
		msg.Type = streaming.MessageTypeMessage
	}

	return nil
}
