package streaming

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/xraph/forge/extensions/streaming/backends/local"
	"github.com/xraph/forge/extensions/streaming/filters"
	streaming "github.com/xraph/forge/extensions/streaming/internal"
)

// countingCodec counts encodes so a test can see how often a fan-out encoded.
type countingCodec struct {
	encodes atomic.Int64
}

func (c *countingCodec) ContentType() string { return "application/x-count" }

func (c *countingCodec) Encode(msg *Message) ([]byte, error) {
	c.encodes.Add(1)

	return json.Marshal(msg)
}

func (c *countingCodec) Decode(data []byte, msg *Message) error { return json.Unmarshal(data, msg) }

// A broadcast encodes its message once and every recipient gets the same
// bytes, except one whose filter swapped the message out: that recipient gets
// its own message, encoded on its own.
func TestFanOut_EncodesOncePerContentType(t *testing.T) {
	codec := &countingCodec{}
	codecs := NewCodecRegistry()
	codecs.Register(codec)

	chain := filters.NewFilterChain()
	chain.Add(filters.NewSimpleFilter("redact-for-u3", 0,
		func(_ context.Context, msg *streaming.Message, recipient streaming.EnhancedConnection) (*streaming.Message, error) {
			if recipient.GetUserID() != "u3" {
				return msg, nil
			}

			cp := *msg
			cp.Data = "redacted"

			return &cp, nil
		}))

	m, rooms, _ := realManager(t, WithFilterChain(chain), WithCodecRegistry(codecs))
	ctx := context.Background()

	assertNoError(t, rooms.Create(ctx, local.NewRoom(streaming.RoomOptions{ID: "lobby", Name: "lobby", Owner: "o"})))

	const n = 20

	recs := make([]*recordingConn, n)

	for i := range n {
		recs[i] = &recordingConn{id: fmt.Sprintf("c%d", i)}
		conn := NewConnection(recs[i])
		conn.SetUserID(fmt.Sprintf("u%d", i))

		// Half the room prefers the counting codec, so both the JSON path and
		// the codec path are shared.
		if i%2 == 0 {
			conn.SetContentType(codec.ContentType())
		}

		assertNoError(t, m.Register(conn))
		assertNoError(t, m.JoinRoom(ctx, conn.ID(), "lobby"))
		t.Cleanup(func() { _ = conn.Close() })
	}

	assertNoError(t, m.BroadcastToRoom(ctx, "lobby", &Message{ID: "m", Type: MessageTypeMessage, RoomID: "lobby", Data: "secret"}))

	for _, c := range m.GetAllConnections() {
		flushConn(t, c)
	}

	// Ten codec recipients, one encode. u3 prefers JSON, so its filter does
	// not add a codec encode.
	if got := codec.encodes.Load(); got != 1 {
		t.Errorf("codec encoded %d times for one broadcast, want 1", got)
	}

	var shared []byte

	for i, rec := range recs {
		var data []byte

		if i%2 == 0 {
			data = rec.lastRaw(t)
		} else {
			raw, ok := func() (json.RawMessage, bool) {
				rec.mu.Lock()
				defer rec.mu.Unlock()

				r, ok := rec.jsonWrites[len(rec.jsonWrites)-1].(json.RawMessage)

				return r, ok
			}()
			if !ok {
				t.Fatalf("conn %d: JSON frame did not reach the transport as encoded bytes", i)
			}

			data = raw
		}

		var got Message
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("conn %d: %v", i, err)
		}

		want := "secret"
		if i == 3 {
			want = "redacted"
		}

		if got.Data != want {
			t.Errorf("conn %d: data = %v, want %q", i, got.Data, want)
		}

		if i%2 == 1 && i != 3 {
			if shared == nil {
				shared = data
			} else if !bytes.Equal(shared, data) || &shared[0] != &data[0] {
				t.Errorf("conn %d: JSON recipients did not share one encoding", i)
			}
		}
	}
}
