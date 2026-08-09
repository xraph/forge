package streaming

import (
	"encoding/json"
	"testing"
)

// fixedCodec is a codec whose Encode result is a fixed payload, so a test can
// tell which codec the registry actually dispatched to.
type fixedCodec struct {
	ct      string
	encoded []byte
}

func (c *fixedCodec) ContentType() string { return c.ct }

func (c *fixedCodec) Encode(msg *Message) ([]byte, error) { return c.encoded, nil }

func (c *fixedCodec) Decode(data []byte, msg *Message) error {
	msg.ContentType = c.ct

	return nil
}

// TestCodecRegistry_EncodeWithType pins C3. The delivery path resolves a content
// type from the message and then falls back to the connection's preference, so
// encoding has to accept that resolved type. Encode(msg) re-reads
// msg.ContentType, finds it empty, and silently drops back to JSON — which is
// how a connection's SetContentType preference never reached a codec.
func TestCodecRegistry_EncodeWithType(t *testing.T) {
	jsonMsg := &Message{ID: "1", Type: MessageTypeMessage, Data: "hi"}

	wantJSON, err := json.Marshal(jsonMsg)
	if err != nil {
		t.Fatalf("marshalling fixture: %v", err)
	}

	tests := []struct {
		name        string
		contentType string
		msg         *Message
		want        string
		wantErr     bool
	}{
		{
			name:        "empty content type falls back to the default codec",
			contentType: "",
			msg:         jsonMsg,
			want:        string(wantJSON),
		},
		{
			name:        "explicit json content type uses the json codec",
			contentType: ContentTypeJSON,
			msg:         jsonMsg,
			want:        string(wantJSON),
		},
		{
			name:        "connection preference reaches the text codec when msg.ContentType is empty",
			contentType: ContentTypeText,
			msg:         &Message{ID: "2", Data: "hello"},
			want:        "hello",
		},
		{
			name:        "connection preference reaches the binary codec when msg.ContentType is empty",
			contentType: ContentTypeBinary,
			msg:         &Message{ID: "3", RawData: []byte{0x01, 0x02, 0x03}},
			want:        "\x01\x02\x03",
		},
		{
			name:        "explicit content type overrides a conflicting msg.ContentType",
			contentType: ContentTypeText,
			msg:         &Message{ID: "4", ContentType: ContentTypeJSON, Data: "hello"},
			want:        "hello",
		},
		{
			name:        "unregistered content type is an error",
			contentType: ContentTypeProtobuf,
			msg:         &Message{ID: "5"},
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewCodecRegistry()

			got, err := r.EncodeWithType(tt.contentType, tt.msg)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("EncodeWithType(%q, ...) = %q, want error", tt.contentType, got)
				}

				return
			}

			if err != nil {
				t.Fatalf("EncodeWithType(%q, ...) = %v, want nil", tt.contentType, err)
			}

			if string(got) != tt.want {
				t.Errorf("EncodeWithType(%q, ...) = %q, want %q", tt.contentType, got, tt.want)
			}
		})
	}
}

func TestCodecRegistry_EncodeWithTypeUsesRegisteredCustomCodecs(t *testing.T) {
	r := NewCodecRegistry()
	r.Register(&fixedCodec{ct: ContentTypeMsgPack, encoded: []byte("stubbed")})

	got, err := r.EncodeWithType(ContentTypeMsgPack, &Message{ID: "1"})
	if err != nil {
		t.Fatalf("EncodeWithType() = %v, want nil", err)
	}

	if string(got) != "stubbed" {
		t.Errorf("EncodeWithType() = %q, want %q", got, "stubbed")
	}
}

func TestCodecRegistry_EncodeWithTypeFollowsSetDefault(t *testing.T) {
	r := NewCodecRegistry()

	if err := r.SetDefault(ContentTypeText); err != nil {
		t.Fatalf("SetDefault() = %v, want nil", err)
	}

	got, err := r.EncodeWithType("", &Message{ID: "1", Data: "plain"})
	if err != nil {
		t.Fatalf("EncodeWithType() = %v, want nil", err)
	}

	if string(got) != "plain" {
		t.Errorf("EncodeWithType(%q, ...) = %q, want %q (empty type must follow the new default)", "", got, "plain")
	}
}

// TestCodecRegistry_EncodeWithTypeMatchesEncode checks the two entry points stay
// consistent when the resolved type is the message's own content type.
func TestCodecRegistry_EncodeWithTypeMatchesEncode(t *testing.T) {
	tests := []struct {
		name string
		msg  *Message
	}{
		{name: "json", msg: &Message{ID: "1", ContentType: ContentTypeJSON, Data: "x"}},
		{name: "text", msg: &Message{ID: "2", ContentType: ContentTypeText, Data: "x"}},
		{name: "binary", msg: &Message{ID: "3", ContentType: ContentTypeBinary, RawData: []byte("x")}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewCodecRegistry()

			viaEncode, err := r.Encode(tt.msg)
			if err != nil {
				t.Fatalf("Encode() = %v, want nil", err)
			}

			viaType, err := r.EncodeWithType(tt.msg.ContentType, tt.msg)
			if err != nil {
				t.Fatalf("EncodeWithType() = %v, want nil", err)
			}

			if string(viaEncode) != string(viaType) {
				t.Errorf("Encode() = %q but EncodeWithType(%q) = %q", viaEncode, tt.msg.ContentType, viaType)
			}
		})
	}
}
