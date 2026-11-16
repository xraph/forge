package router

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
)

// wsPair returns a server-side wsConnection and the raw client socket talking
// to it over loopback TCP.
func wsPair(t *testing.T) (*wsConnection, net.Conn) {
	t.Helper()

	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = ln.Close() })

	ready := make(chan *wsConnection, 1)

	go func() {
		c, err := ln.Accept()
		if err != nil {
			close(ready)

			return
		}

		if _, err := ws.Upgrade(c); err != nil {
			close(ready)

			return
		}

		ready <- newWSConnection("x", c, context.Background())
	}()

	cli, _, _, err := ws.Dial(context.Background(), "ws://"+ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = cli.Close() })

	srv, ok := <-ready
	if !ok {
		t.Fatal("server side of the WebSocket never came up")
	}

	t.Cleanup(func() { _ = srv.Close() })

	return srv, cli
}

// Pong replies used to be written by wsutil.ReadClientData on the reading
// goroutine, straight to the socket and without the write lock, while Write
// put a frame header and its payload on the wire in two separate writes. A
// pong could land between them and the client saw a corrupt stream.
func TestWSConnection_PongDoesNotInterleaveWithDataFrames(t *testing.T) {
	srv, cli := wsPair(t)

	payload := bytes.Repeat([]byte("x"), 32*1024)
	ping := bytes.Repeat([]byte("p"), 100)

	stop := make(chan struct{})

	time.AfterFunc(1500*time.Millisecond, func() { close(stop) })

	// Server reader: answers pings.
	go func() {
		for {
			if _, err := srv.Read(); err != nil {
				return
			}
		}
	}()

	// Server writer: application traffic.
	go func() {
		for {
			select {
			case <-stop:
				return
			default:
			}

			if err := srv.Write(payload); err != nil {
				return
			}
		}
	}()

	// Client pinger.
	go func() {
		for {
			select {
			case <-stop:
				return
			default:
			}

			if err := wsutil.WriteClientMessage(cli, ws.OpPing, ping); err != nil {
				return
			}
		}
	}()

	var dataFrames, pongs atomic.Int64

	_ = cli.SetReadDeadline(time.Now().Add(2500 * time.Millisecond))

	for {
		h, err := ws.ReadHeader(cli)
		if err != nil {
			break
		}

		if h.Length > 1<<20 {
			t.Fatalf("corrupt frame header: length %d", h.Length)
		}

		buf := make([]byte, h.Length)
		if _, err := io.ReadFull(cli, buf); err != nil {
			break
		}

		switch h.OpCode {
		case ws.OpText:
			if !bytes.Equal(buf, payload) {
				t.Fatal("data frame corrupted: a control frame landed inside it")
			}

			dataFrames.Add(1)
		case ws.OpPong:
			if !bytes.Equal(buf, ping) {
				t.Fatal("pong frame corrupted")
			}

			pongs.Add(1)
		default:
			t.Fatalf("unexpected opcode %v: stream is out of frame sync", h.OpCode)
		}
	}

	if dataFrames.Load() == 0 || pongs.Load() == 0 {
		t.Fatalf("test did not exercise the race: data frames=%d pongs=%d", dataFrames.Load(), pongs.Load())
	}
}

// The control frame handling was rewritten to route replies through the write
// lock. A close from the client must still be echoed and end the read loop.
func TestWSConnection_CloseFrameIsEchoed(t *testing.T) {
	srv, cli := wsPair(t)

	body := ws.NewCloseFrameBody(ws.StatusNormalClosure, "bye")
	if err := wsutil.WriteClientMessage(cli, ws.OpClose, body); err != nil {
		t.Fatal(err)
	}

	_, err := srv.Read()

	var closed wsutil.ClosedError
	if !errors.As(err, &closed) || closed.Code != ws.StatusNormalClosure {
		t.Fatalf("Read error = %v, want ClosedError with code 1000", err)
	}

	_ = cli.SetReadDeadline(time.Now().Add(2 * time.Second))

	h, err := ws.ReadHeader(cli)
	if err != nil {
		t.Fatalf("no close reply: %v", err)
	}

	if h.OpCode != ws.OpClose {
		t.Fatalf("reply opcode = %v, want close", h.OpCode)
	}
}

// Pings interleaved with a fragmented message are answered mid-message and do
// not disturb the message itself.
func TestWSConnection_PingBetweenFragmentsIsAnswered(t *testing.T) {
	srv, cli := wsPair(t)

	frames := []ws.Frame{
		ws.MaskFrame(ws.NewFrame(ws.OpText, false, []byte("hel"))),
		ws.MaskFrame(ws.NewPingFrame([]byte("p1"))),
		ws.MaskFrame(ws.NewFrame(ws.OpContinuation, true, []byte("lo"))),
	}

	for _, f := range frames {
		if err := ws.WriteFrame(cli, f); err != nil {
			t.Fatal(err)
		}
	}

	data, err := srv.Read()
	if err != nil {
		t.Fatal(err)
	}

	if string(data) != "hello" {
		t.Fatalf("message = %q, want %q", data, "hello")
	}

	_ = cli.SetReadDeadline(time.Now().Add(2 * time.Second))

	h, err := ws.ReadHeader(cli)
	if err != nil {
		t.Fatalf("no pong: %v", err)
	}

	pong := make([]byte, h.Length)
	if _, err := io.ReadFull(cli, pong); err != nil {
		t.Fatal(err)
	}

	if h.OpCode != ws.OpPong || string(pong) != "p1" {
		t.Fatalf("reply = %v %q, want pong %q", h.OpCode, pong, "p1")
	}
}
