package streaming

import (
	"encoding/json"
	"testing"
	"time"

	streaming "github.com/xraph/forge/extensions/streaming/internal"
)

func TestNewCodecRegistry_BuiltIns(t *testing.T) {
	r := NewCodecRegistry()

	tests := []struct {
		name        string
		contentType string
		wantType    Codec
	}{
		{name: "json", contentType: ContentTypeJSON, wantType: &JSONCodec{}},
		{name: "binary", contentType: ContentTypeBinary, wantType: &BinaryCodec{}},
		{name: "text", contentType: ContentTypeText, wantType: &TextCodec{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			codec, ok := r.Get(tt.contentType)
			if !ok {
				t.Fatalf("Get(%q): not registered", tt.contentType)
			}

			if got, want := codec.ContentType(), tt.contentType; got != want {
				t.Errorf("ContentType() = %q, want %q", got, want)
			}

			if got, want := typeName(codec), typeName(tt.wantType); got != want {
				t.Errorf("codec is %s, want %s", got, want)
			}
		})
	}

	if got := r.Default().ContentType(); got != ContentTypeJSON {
		t.Errorf("Default().ContentType() = %q, want %q", got, ContentTypeJSON)
	}
}

func typeName(c Codec) string {
	switch c.(type) {
	case *JSONCodec:
		return "*JSONCodec"
	case *BinaryCodec:
		return "*BinaryCodec"
	case *TextCodec:
		return "*TextCodec"
	default:
		return "unknown"
	}
}

func TestJSONCodec_RoundTrip(t *testing.T) {
	codec := &JSONCodec{}

	ts := time.Date(2026, 3, 7, 12, 0, 0, 0, time.UTC)
	original := &Message{
		ID:          "msg-1",
		Type:        MessageTypeMessage,
		Event:       "chat",
		RoomID:      "room-1",
		UserID:      "user-1",
		Data:        "hello",
		ContentType: ContentTypeJSON,
		Metadata:    map[string]any{"k": "v"},
		Timestamp:   ts,
		ThreadID:    "thread-1",
	}

	data, err := codec.Encode(original)
	assertNoError(t, err)

	var decoded Message
	assertNoError(t, codec.Decode(data, &decoded))

	if decoded.ID != original.ID ||
		decoded.Type != original.Type ||
		decoded.Event != original.Event ||
		decoded.RoomID != original.RoomID ||
		decoded.UserID != original.UserID ||
		decoded.ThreadID != original.ThreadID ||
		decoded.ContentType != original.ContentType {
		t.Errorf("round trip lost scalar fields:\n got %+v\nwant %+v", decoded, original)
	}

	if got, ok := decoded.Data.(string); !ok || got != "hello" {
		t.Errorf("Data = %#v, want %q", decoded.Data, "hello")
	}

	if !decoded.Timestamp.Equal(ts) {
		t.Errorf("Timestamp = %v, want %v", decoded.Timestamp, ts)
	}
}

func TestJSONCodec_RawDataIsNotSerialized(t *testing.T) {
	// Message.RawData carries `json:"-"`, so the JSON codec deliberately drops
	// it. Binary payloads must go through BinaryCodec instead.
	codec := &JSONCodec{}

	data, err := codec.Encode(&Message{ID: "m", RawData: []byte{1, 2, 3}})
	assertNoError(t, err)

	var decoded Message
	assertNoError(t, codec.Decode(data, &decoded))

	if decoded.RawData != nil {
		t.Errorf("RawData = %v, want nil (json:\"-\")", decoded.RawData)
	}
}

func TestBinaryCodec_RoundTrip(t *testing.T) {
	codec := &BinaryCodec{}

	payload := []byte{0x00, 0xff, 0x10, 0x7f}

	encoded, err := codec.Encode(&Message{RawData: payload})
	assertNoError(t, err)

	if string(encoded) != string(payload) {
		t.Errorf("Encode = %v, want %v", encoded, payload)
	}

	var decoded Message
	assertNoError(t, codec.Decode(encoded, &decoded))

	if string(decoded.RawData) != string(payload) {
		t.Errorf("RawData = %v, want %v", decoded.RawData, payload)
	}

	if decoded.ContentType != ContentTypeBinary {
		t.Errorf("ContentType = %q, want %q", decoded.ContentType, ContentTypeBinary)
	}

	if decoded.Type != MessageTypeMessage {
		t.Errorf("Type = %q, want %q (defaulted on decode)", decoded.Type, MessageTypeMessage)
	}
}

func TestBinaryCodec_DecodeCopiesInput(t *testing.T) {
	// Decode must not alias the caller's buffer: the read loop reuses it.
	codec := &BinaryCodec{}

	buf := []byte{1, 2, 3}

	var msg Message
	assertNoError(t, codec.Decode(buf, &msg))

	buf[0] = 99

	if msg.RawData[0] != 1 {
		t.Errorf("RawData aliases the input buffer: got %v", msg.RawData)
	}
}

func TestBinaryCodec_DecodePreservesExistingType(t *testing.T) {
	codec := &BinaryCodec{}

	msg := Message{Type: MessageTypeJoin}
	assertNoError(t, codec.Decode([]byte("x"), &msg))

	if msg.Type != MessageTypeJoin {
		t.Errorf("Type = %q, want %q (non-empty type must be preserved)", msg.Type, MessageTypeJoin)
	}
}

func TestBinaryCodec_EncodeWithoutRawDataFallsBackToJSON(t *testing.T) {
	codec := &BinaryCodec{}

	msg := &Message{ID: "m-1", Data: "hi"}

	encoded, err := codec.Encode(msg)
	assertNoError(t, err)

	var probe map[string]any
	if err := json.Unmarshal(encoded, &probe); err != nil {
		t.Fatalf("fallback encoding is not JSON: %v", err)
	}

	if probe["id"] != "m-1" {
		t.Errorf("fallback JSON id = %v, want m-1", probe["id"])
	}
}

func TestTextCodec_RoundTrip(t *testing.T) {
	codec := &TextCodec{}

	encoded, err := codec.Encode(&Message{Data: "hello world"})
	assertNoError(t, err)

	if string(encoded) != "hello world" {
		t.Errorf("Encode = %q, want %q", encoded, "hello world")
	}

	var decoded Message
	assertNoError(t, codec.Decode(encoded, &decoded))

	if got, ok := decoded.Data.(string); !ok || got != "hello world" {
		t.Errorf("Data = %#v, want %q", decoded.Data, "hello world")
	}

	if decoded.ContentType != ContentTypeText {
		t.Errorf("ContentType = %q, want %q", decoded.ContentType, ContentTypeText)
	}

	if decoded.Type != MessageTypeMessage {
		t.Errorf("Type = %q, want %q (defaulted on decode)", decoded.Type, MessageTypeMessage)
	}
}

func TestTextCodec_EncodePrecedence(t *testing.T) {
	codec := &TextCodec{}

	tests := []struct {
		name string
		msg  *Message
		want string
	}{
		{
			name: "raw data wins over data",
			msg:  &Message{RawData: []byte("raw"), Data: "ignored"},
			want: "raw",
		},
		{
			name: "string data encoded verbatim",
			msg:  &Message{Data: "plain"},
			want: "plain",
		},
		{
			name: "non-string data falls back to JSON of Data only",
			msg:  &Message{Data: map[string]any{"a": float64(1)}},
			want: `{"a":1}`,
		},
		{
			name: "nil data encodes as JSON null",
			msg:  &Message{},
			want: "null",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := codec.Encode(tt.msg)
			assertNoError(t, err)

			if string(got) != tt.want {
				t.Errorf("Encode = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCodecRegistry_EncodeDispatch(t *testing.T) {
	r := NewCodecRegistry()

	tests := []struct {
		name     string
		msg      *Message
		want     string
		wantJSON bool
		wantErr  bool
	}{
		{
			name:     "empty content type uses default (JSON)",
			msg:      &Message{ID: "m", Data: "x"},
			wantJSON: true,
		},
		{
			name: "text content type uses text codec",
			msg:  &Message{ContentType: ContentTypeText, Data: "hi"},
			want: "hi",
		},
		{
			name: "binary content type uses binary codec",
			msg:  &Message{ContentType: ContentTypeBinary, RawData: []byte("bin")},
			want: "bin",
		},
		{
			name:    "unregistered content type is an error",
			msg:     &Message{ContentType: ContentTypeProtobuf},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := r.Encode(tt.msg)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("Encode = %q, want error", got)
				}

				return
			}

			assertNoError(t, err)

			if tt.wantJSON {
				var probe map[string]any
				if err := json.Unmarshal(got, &probe); err != nil {
					t.Fatalf("default codec did not produce JSON: %v (%q)", err, got)
				}

				if probe["id"] != "m" || probe["data"] != "x" {
					t.Errorf("JSON encoding = %q, want id=m data=x", got)
				}

				return
			}

			if string(got) != tt.want {
				t.Errorf("Encode = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCodecRegistry_DecodeUsesDefault(t *testing.T) {
	r := NewCodecRegistry()

	var msg Message
	assertNoError(t, r.Decode([]byte(`{"id":"m-9","type":"message"}`), &msg))

	if msg.ID != "m-9" {
		t.Errorf("ID = %q, want m-9", msg.ID)
	}
}

func TestCodecRegistry_DecodeWithType(t *testing.T) {
	r := NewCodecRegistry()

	tests := []struct {
		name        string
		contentType string
		data        []byte
		wantErr     bool
		check       func(t *testing.T, msg *Message)
	}{
		{
			name:        "empty content type falls back to default",
			contentType: "",
			data:        []byte(`{"id":"a"}`),
			check: func(t *testing.T, msg *Message) {
				if msg.ID != "a" {
					t.Errorf("ID = %q, want a", msg.ID)
				}
			},
		},
		{
			name:        "text",
			contentType: ContentTypeText,
			data:        []byte("plain body"),
			check: func(t *testing.T, msg *Message) {
				if msg.Data != "plain body" {
					t.Errorf("Data = %#v, want %q", msg.Data, "plain body")
				}
			},
		},
		{
			name:        "binary",
			contentType: ContentTypeBinary,
			data:        []byte{7, 8},
			check: func(t *testing.T, msg *Message) {
				if len(msg.RawData) != 2 || msg.RawData[0] != 7 {
					t.Errorf("RawData = %v, want [7 8]", msg.RawData)
				}
			},
		},
		{
			name:        "unregistered content type is an error",
			contentType: ContentTypeMsgPack,
			data:        []byte("x"),
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var msg Message

			err := r.DecodeWithType(tt.contentType, tt.data, &msg)

			if tt.wantErr {
				if err == nil {
					t.Fatal("DecodeWithType: want error, got nil")
				}

				return
			}

			assertNoError(t, err)
			tt.check(t, &msg)
		})
	}
}

func TestCodecRegistry_Register(t *testing.T) {
	r := NewCodecRegistry()

	custom := &stubCodec{contentType: ContentTypeMsgPack, encoded: []byte("stubbed")}
	r.Register(custom)

	got, ok := r.Get(ContentTypeMsgPack)
	if !ok {
		t.Fatal("Get: custom codec not registered")
	}

	if got != Codec(custom) {
		t.Errorf("Get returned %#v, want the registered codec", got)
	}

	encoded, err := r.Encode(&Message{ContentType: ContentTypeMsgPack})
	assertNoError(t, err)

	if string(encoded) != "stubbed" {
		t.Errorf("Encode = %q, want %q", encoded, "stubbed")
	}
}

func TestCodecRegistry_RegisterReplacesSameContentType(t *testing.T) {
	r := NewCodecRegistry()

	replacement := &stubCodec{contentType: ContentTypeJSON, encoded: []byte("replaced")}
	r.Register(replacement)

	encoded, err := r.Encode(&Message{ContentType: ContentTypeJSON})
	assertNoError(t, err)

	if string(encoded) != "replaced" {
		t.Errorf("Encode = %q, want %q — Register must replace an existing content type", encoded, "replaced")
	}
}

func TestCodecRegistry_SetDefault(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		wantErr     bool
		wantDefault string
	}{
		{name: "switch to text", contentType: ContentTypeText, wantDefault: ContentTypeText},
		{name: "switch to binary", contentType: ContentTypeBinary, wantDefault: ContentTypeBinary},
		{name: "unregistered content type", contentType: ContentTypeProtobuf, wantErr: true, wantDefault: ContentTypeJSON},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewCodecRegistry()

			err := r.SetDefault(tt.contentType)

			if tt.wantErr {
				if err == nil {
					t.Fatal("SetDefault: want error, got nil")
				}
			} else {
				assertNoError(t, err)
			}

			if got := r.Default().ContentType(); got != tt.wantDefault {
				t.Errorf("Default().ContentType() = %q, want %q", got, tt.wantDefault)
			}
		})
	}
}

func TestCodecRegistry_SetDefaultChangesEncodeAndDecode(t *testing.T) {
	r := NewCodecRegistry()
	assertNoError(t, r.SetDefault(ContentTypeText))

	// Encode of a message with no explicit content type now goes through text.
	encoded, err := r.Encode(&Message{Data: "body"})
	assertNoError(t, err)

	if string(encoded) != "body" {
		t.Errorf("Encode = %q, want %q after SetDefault(text)", encoded, "body")
	}

	// Decode with no explicit content type now goes through text too.
	var msg Message
	assertNoError(t, r.Decode([]byte(`{"id":"not-json-anymore"}`), &msg))

	if msg.Data != `{"id":"not-json-anymore"}` {
		t.Errorf("Data = %#v, want the raw text body", msg.Data)
	}
}

func TestCodecRegistry_ConcurrentAccess(t *testing.T) {
	r := NewCodecRegistry()

	done := make(chan struct{})

	go func() {
		defer close(done)

		for i := 0; i < 200; i++ {
			r.Register(&stubCodec{contentType: ContentTypeMsgPack, encoded: []byte("x")})
			_ = r.SetDefault(ContentTypeJSON)
		}
	}()

	for i := 0; i < 200; i++ {
		_, _ = r.Get(ContentTypeJSON)
		_ = r.Default()
		_, _ = r.Encode(&Message{Data: "x"})
	}

	<-done
}

// stubCodec is a codec whose Encode result is fixed, for dispatch assertions.
type stubCodec struct {
	contentType string
	encoded     []byte
	decodeErr   error
}

func (c *stubCodec) ContentType() string { return c.contentType }

func (c *stubCodec) Encode(msg *streaming.Message) ([]byte, error) { return c.encoded, nil }

func (c *stubCodec) Decode(data []byte, msg *streaming.Message) error {
	if c.decodeErr != nil {
		return c.decodeErr
	}

	msg.ContentType = c.contentType

	return nil
}
