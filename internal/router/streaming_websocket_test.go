package router

import (
	"bytes"
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/gobwas/ws"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// readFrameOpCode parses the opcode off the single frame written to the mock.
func readFrameOpCode(t *testing.T, raw []byte) ws.OpCode {
	t.Helper()

	header, err := ws.ReadHeader(bytes.NewReader(raw))
	require.NoError(t, err)

	return header.OpCode
}

// mockConn implements net.Conn for testing.
type mockConn struct {
	readData   []byte
	writeData  []byte
	readErr    error
	writeErr   error
	closed     bool
	remoteAddr net.Addr
	localAddr  net.Addr

	// writeDeadlines records every SetWriteDeadline call in order, so tests can
	// assert both that a bound was armed before the write and that it was
	// cleared afterwards.
	writeDeadlines []time.Time
}

func (m *mockConn) Read(b []byte) (n int, err error) {
	if m.readErr != nil {
		return 0, m.readErr
	}

	n = copy(b, m.readData)
	m.readData = m.readData[n:]

	return n, nil
}

func (m *mockConn) Write(b []byte) (n int, err error) {
	if m.writeErr != nil {
		return 0, m.writeErr
	}

	m.writeData = append(m.writeData, b...)

	return len(b), nil
}

func (m *mockConn) Close() error {
	m.closed = true

	return nil
}

func (m *mockConn) LocalAddr() net.Addr  { return m.localAddr }
func (m *mockConn) RemoteAddr() net.Addr { return m.remoteAddr }

func (m *mockConn) SetDeadline(t time.Time) error     { return nil }
func (m *mockConn) SetReadDeadline(t time.Time) error { return nil }

func (m *mockConn) SetWriteDeadline(t time.Time) error {
	m.writeDeadlines = append(m.writeDeadlines, t)

	return nil
}

type mockAddr struct {
	addr string
}

func (m *mockAddr) Network() string { return "tcp" }
func (m *mockAddr) String() string  { return m.addr }

func TestNewWSConnection(t *testing.T) {
	conn := &mockConn{
		remoteAddr: &mockAddr{addr: "127.0.0.1:1234"},
		localAddr:  &mockAddr{addr: "127.0.0.1:8080"},
	}

	ctx := context.Background()
	wsConn := newWSConnection("test-id", conn, ctx)

	assert.NotNil(t, wsConn)
	assert.Equal(t, "test-id", wsConn.ID())
	assert.Equal(t, "127.0.0.1:1234", wsConn.RemoteAddr())
	assert.Equal(t, "127.0.0.1:8080", wsConn.LocalAddr())
	assert.NotNil(t, wsConn.Context())
}

func TestWSConnection_ID(t *testing.T) {
	conn := &mockConn{}
	wsConn := newWSConnection("custom-id", conn, context.Background())

	assert.Equal(t, "custom-id", wsConn.ID())
}

func TestWSConnection_Close(t *testing.T) {
	conn := &mockConn{}
	wsConn := newWSConnection("test-id", conn, context.Background())

	err := wsConn.Close()
	assert.NoError(t, err)
	assert.True(t, conn.closed)

	// Double close should not error
	err = wsConn.Close()
	assert.NoError(t, err)
}

func TestWSConnection_Context(t *testing.T) {
	conn := &mockConn{}
	ctx := context.Background()
	wsConn := newWSConnection("test-id", conn, ctx)

	connCtx := wsConn.Context()
	assert.NotNil(t, connCtx)

	// Context should be cancelled after close
	err := wsConn.Close()
	assert.NoError(t, err)

	select {
	case <-connCtx.Done():
		// Context cancelled as expected
	case <-time.After(100 * time.Millisecond):
		t.Fatal("context not cancelled after close")
	}
}

func TestWSConnection_RemoteAddr(t *testing.T) {
	conn := &mockConn{
		remoteAddr: &mockAddr{addr: "192.168.1.100:5678"},
	}
	wsConn := newWSConnection("test-id", conn, context.Background())

	assert.Equal(t, "192.168.1.100:5678", wsConn.RemoteAddr())
}

func TestWSConnection_LocalAddr(t *testing.T) {
	conn := &mockConn{
		localAddr: &mockAddr{addr: "0.0.0.0:8080"},
	}
	wsConn := newWSConnection("test-id", conn, context.Background())

	assert.Equal(t, "0.0.0.0:8080", wsConn.LocalAddr())
}

func TestGenerateConnectionID(t *testing.T) {
	id1 := generateConnectionID()

	// Small sleep to ensure different timestamp
	time.Sleep(time.Microsecond)

	id2 := generateConnectionID()

	assert.NotEmpty(t, id1)
	assert.NotEmpty(t, id2)
	assert.NotEqual(t, id1, id2, "IDs should be unique")
	assert.Contains(t, id1, "ws_")
	assert.Contains(t, id2, "ws_")
}

func TestWSConnection_WriteClosed(t *testing.T) {
	conn := &mockConn{}
	wsConn := newWSConnection("test-id", conn, context.Background())

	// Close connection
	err := wsConn.Close()
	require.NoError(t, err)

	// Write should fail
	err = wsConn.Write([]byte("test"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "connection closed")
}

func TestWSConnection_WriteUsesTextFrame(t *testing.T) {
	conn := &mockConn{}
	wsConn := newWSConnection("test-id", conn, context.Background())

	require.NoError(t, wsConn.Write([]byte("hello")))

	assert.Equal(t, ws.OpText, readFrameOpCode(t, conn.writeData))
}

func TestWSConnection_WriteBinaryUsesBinaryFrame(t *testing.T) {
	conn := &mockConn{}
	wsConn := newWSConnection("test-id", conn, context.Background())

	require.NoError(t, wsConn.WriteBinary([]byte{0x00, 0x01, 0xff}))

	assert.Equal(t, ws.OpBinary, readFrameOpCode(t, conn.writeData))
}

// Invalid UTF-8 in a text frame makes browsers fail the connection with 1007,
// so a binary payload must survive the write byte-for-byte in a binary frame.
func TestWSConnection_WriteBinaryPreservesNonUTF8(t *testing.T) {
	conn := &mockConn{}
	wsConn := newWSConnection("test-id", conn, context.Background())

	payload := []byte{0xff, 0xfe, 0x00, 0x80}
	require.NoError(t, wsConn.WriteBinary(payload))

	reader := bytes.NewReader(conn.writeData)
	header, err := ws.ReadHeader(reader)
	require.NoError(t, err)
	require.Equal(t, ws.OpBinary, header.OpCode)

	got := make([]byte, header.Length)
	_, err = reader.Read(got)
	require.NoError(t, err)
	assert.Equal(t, payload, got)
}

func TestWSConnection_WriteBinaryClosed(t *testing.T) {
	conn := &mockConn{}
	wsConn := newWSConnection("test-id", conn, context.Background())

	require.NoError(t, wsConn.Close())

	err := wsConn.WriteBinary([]byte("test"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "connection closed")
}

func TestWSConnection_WriteSetsAndClearsDeadline(t *testing.T) {
	conn := &mockConn{}
	wsConn := newWSConnection("test-id", conn, context.Background())

	before := time.Now()

	require.NoError(t, wsConn.Write([]byte("hello")))
	require.Len(t, conn.writeDeadlines, 2, "expected a deadline set then cleared")

	// Armed with roughly the default timeout ahead of the write.
	assert.WithinDuration(t,
		before.Add(DefaultWebSocketWriteTimeout),
		conn.writeDeadlines[0],
		time.Second,
	)

	// Cleared afterwards, so the next write is not judged against a stale bound.
	assert.True(t, conn.writeDeadlines[1].IsZero(), "deadline should be cleared after the write")
}

func TestWSConnection_WriteBinarySetsDeadline(t *testing.T) {
	conn := &mockConn{}
	wsConn := newWSConnection("test-id", conn, context.Background())

	require.NoError(t, wsConn.WriteBinary([]byte("hello")))

	require.Len(t, conn.writeDeadlines, 2)
	assert.False(t, conn.writeDeadlines[0].IsZero())
	assert.True(t, conn.writeDeadlines[1].IsZero())
}

func TestWSConnection_SetWriteTimeout(t *testing.T) {
	conn := &mockConn{}
	wsConn := newWSConnection("test-id", conn, context.Background())

	wsConn.SetWriteTimeout(50 * time.Millisecond)

	before := time.Now()

	require.NoError(t, wsConn.Write([]byte("hello")))
	require.Len(t, conn.writeDeadlines, 2)
	assert.WithinDuration(t,
		before.Add(50*time.Millisecond),
		conn.writeDeadlines[0],
		time.Second,
	)
}

func TestWSConnection_SetWriteTimeoutNonPositiveRestoresDefault(t *testing.T) {
	conn := &mockConn{}
	wsConn := newWSConnection("test-id", conn, context.Background())

	wsConn.SetWriteTimeout(50 * time.Millisecond)
	wsConn.SetWriteTimeout(0)

	before := time.Now()

	require.NoError(t, wsConn.Write([]byte("hello")))
	require.Len(t, conn.writeDeadlines, 2)
	assert.WithinDuration(t,
		before.Add(DefaultWebSocketWriteTimeout),
		conn.writeDeadlines[0],
		time.Second,
	)
}

// A write that times out must still clear the deadline, or every later write on
// the connection fails against the expired one.
func TestWSConnection_WriteClearsDeadlineOnError(t *testing.T) {
	conn := &mockConn{writeErr: errors.New("i/o timeout")}
	wsConn := newWSConnection("test-id", conn, context.Background())

	err := wsConn.Write([]byte("hello"))
	require.Error(t, err)

	require.Len(t, conn.writeDeadlines, 2)
	assert.True(t, conn.writeDeadlines[1].IsZero())
}

func TestWSConnection_ReadClosed(t *testing.T) {
	conn := &mockConn{}
	wsConn := newWSConnection("test-id", conn, context.Background())

	// Close connection
	err := wsConn.Close()
	require.NoError(t, err)

	// Read should fail
	_, err = wsConn.Read()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "connection closed")
}
