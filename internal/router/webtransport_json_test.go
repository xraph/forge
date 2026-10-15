package router

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/stretchr/testify/require"
)

type jsonMsg struct {
	N int `json:"n"`
}

func TestStreamReaderReadJSONKeepsReadAhead(t *testing.T) {
	// Both values arrive in one read, which is what a QUIC stream does with
	// small messages. The decoder reads both into its buffer on the first call.
	sr := &streamReader{r: strings.NewReader(`{"n":1}{"n":2}` + "\n" + `{"n":3}`)}

	for want := 1; want <= 3; want++ {
		var got jsonMsg

		require.NoError(t, sr.ReadJSON(&got))
		require.Equal(t, want, got.N)
	}

	var extra jsonMsg

	require.ErrorIs(t, sr.ReadJSON(&extra), io.EOF)
}

func TestStreamReaderRawReadAfterReadJSON(t *testing.T) {
	sr := &streamReader{r: strings.NewReader(`{"n":1}raw bytes`)}

	var got jsonMsg

	require.NoError(t, sr.ReadJSON(&got))
	require.Equal(t, 1, got.N)

	rest, err := io.ReadAll(sr)
	require.NoError(t, err)
	require.Equal(t, "raw bytes", string(rest))
}

func TestStreamReaderAlternatesRawAndJSON(t *testing.T) {
	// The tail is longer than a decoder's first read, so when the second
	// decoder is dropped some of the handed-back bytes are still unread.
	tail := strings.Repeat("x", 2048)
	src := `{"n":1}AB{"n":2}` + tail

	for name, r := range map[string]io.Reader{
		"whole":    strings.NewReader(src),
		"one byte": iotest.OneByteReader(strings.NewReader(src)),
	} {
		t.Run(name, func(t *testing.T) {
			sr := &streamReader{r: r}

			var got jsonMsg

			require.NoError(t, sr.ReadJSON(&got))
			require.Equal(t, 1, got.N)

			ab := make([]byte, 2)
			_, err := io.ReadFull(sr, ab)
			require.NoError(t, err)
			require.Equal(t, "AB", string(ab))

			require.NoError(t, sr.ReadJSON(&got))
			require.Equal(t, 2, got.N)

			rest, err := io.ReadAll(sr)
			require.NoError(t, err)
			require.Equal(t, tail, string(rest))
		})
	}
}

func TestStreamReaderKeepsPendingOrder(t *testing.T) {
	// pending is longer than a decoder's first read, so the decoder stops
	// with part of it unread. Those bytes must come back before the stream's.
	tail := strings.Repeat("x", 2048)
	sr := &streamReader{
		r:       strings.NewReader("TAIL"),
		pending: bytes.NewReader([]byte(`{"n":2}` + tail)),
	}

	var got jsonMsg

	require.NoError(t, sr.ReadJSON(&got))
	require.Equal(t, 2, got.N)

	rest, err := io.ReadAll(sr)
	require.NoError(t, err)
	require.Equal(t, tail+"TAIL", string(rest))
}

func TestWriteJSONLineSeparatesValues(t *testing.T) {
	var buf bytes.Buffer

	require.NoError(t, writeJSONLine(&buf, 1))
	require.NoError(t, writeJSONLine(&buf, 2))
	require.NoError(t, writeJSONLine(&buf, jsonMsg{N: 3}))

	// Two bare numbers back to back would read as 12.
	require.Equal(t, "1\n2\n{\"n\":3}\n", buf.String())

	dec := json.NewDecoder(&buf)

	for _, want := range []any{float64(1), float64(2), map[string]any{"n": float64(3)}} {
		var got any

		require.NoError(t, dec.Decode(&got))
		require.Equal(t, want, got)
	}
}

func TestWriteJSONLineIsOneWrite(t *testing.T) {
	// A value and its newline go out in one Write, so two writers can't
	// interleave between them.
	w := &countingWriter{}

	require.NoError(t, writeJSONLine(w, jsonMsg{N: 1}))
	require.Equal(t, 1, w.writes)
}

type countingWriter struct{ writes int }

func (w *countingWriter) Write(p []byte) (int, error) {
	w.writes++

	return len(p), nil
}
