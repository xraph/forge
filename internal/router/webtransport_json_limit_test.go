package router

import (
	"errors"
	"io"
	"runtime"
	"strings"
	"testing"
)

// repeatReader yields n copies of b and then EOF, without holding them all.
type repeatReader struct {
	b    byte
	left int
}

func (r *repeatReader) Read(p []byte) (int, error) {
	if r.left <= 0 {
		return 0, io.EOF
	}

	n := min(len(p), r.left)
	for i := range n {
		p[i] = r.b
	}

	r.left -= n

	return n, nil
}

// jsonString streams one JSON string of n 'a' characters.
func jsonString(n int) io.Reader {
	return io.MultiReader(strings.NewReader(`"`), &repeatReader{b: 'a', left: n}, strings.NewReader(`"`))
}

// ReadJSON had no size cap: the decoder buffered whatever one value the peer
// sent, and a 200 MiB string cost 1.2 GiB of allocations. With the default cap
// it now fails after about 1 MiB.
func TestStreamReader_ReadJSONDefaultCapBoundsMemory(t *testing.T) {
	sr := streamReader{r: jsonString(200 << 20)}

	var before, after runtime.MemStats

	runtime.ReadMemStats(&before)

	var v any

	err := sr.ReadJSON(&v)

	runtime.ReadMemStats(&after)

	if !errors.Is(err, ErrWebTransportMessageTooLarge) {
		t.Fatalf("ReadJSON error = %v, want ErrWebTransportMessageTooLarge", err)
	}

	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 16<<20 {
		t.Fatalf("ReadJSON allocated %d MiB before refusing the value", allocated>>20)
	}
}

func TestStreamReader_ReadJSONCustomCap(t *testing.T) {
	sr := streamReader{r: jsonString(4096), maxJSON: 1024}

	var v string
	if err := sr.ReadJSON(&v); !errors.Is(err, ErrWebTransportMessageTooLarge) {
		t.Fatalf("ReadJSON error = %v, want ErrWebTransportMessageTooLarge", err)
	}
}

// The cap is per value, not per stream: a long run of small values must keep
// decoding long after their total has passed the limit.
func TestStreamReader_ReadJSONCapIsPerValue(t *testing.T) {
	const values = 200

	sr := streamReader{r: strings.NewReader(strings.Repeat(`{"pad":"0123456789012345678901234567890123456789"}`+"\n", values)), maxJSON: 128}

	for i := range values {
		var v struct{ Pad string }
		if err := sr.ReadJSON(&v); err != nil {
			t.Fatalf("value %d: %v", i, err)
		}
	}
}

func TestStreamReader_NegativeCapDisablesLimit(t *testing.T) {
	sr := streamReader{r: jsonString(2 << 20), maxJSON: -1}

	var v string
	if err := sr.ReadJSON(&v); err != nil {
		t.Fatalf("ReadJSON with the cap off: %v", err)
	}

	if len(v) != 2<<20 {
		t.Fatalf("decoded %d bytes, want %d", len(v), 2<<20)
	}
}

func TestWebTransportStream_SetReadLimit(t *testing.T) {
	s := &webTransportStream{reader: streamReader{r: jsonString(4096)}}
	s.SetReadLimit(1024)

	var v string
	if err := s.ReadJSON(&v); !errors.Is(err, ErrWebTransportMessageTooLarge) {
		t.Fatalf("ReadJSON error = %v, want ErrWebTransportMessageTooLarge", err)
	}
}

func TestDefaultWebTransportConfig_CapsJSONValues(t *testing.T) {
	if got := DefaultWebTransportConfig().MaxJSONValueSize; got != DefaultMaxWebSocketMessageSize {
		t.Fatalf("MaxJSONValueSize = %d, want %d", got, DefaultMaxWebSocketMessageSize)
	}
}
