package forge

import (
	"bufio"
	"context"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// An open event stream used to hold a graceful shutdown for its whole timeout,
// because http.Server.Shutdown waits for every active request and a stream
// never returns on its own.
func TestGracefulShutdown_endsOpenStreamsInsteadOfWaitingOutTheTimeout(t *testing.T) {
	a := newApp(AppConfig{
		Name:            "shutdown-test",
		Version:         "1.0.0",
		Environment:     "test",
		ShutdownTimeout: 10 * time.Second,
	})

	require.NoError(t, a.Start(context.Background()))

	ended := make(chan struct{})

	require.NoError(t, a.Router().EventStream("/events", func(ctx Context, stream Stream) error {
		defer close(ended)

		_ = stream.SendComment("ready")

		<-ctx.Context().Done()

		return nil
	}))

	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)

	a.httpServer = &http.Server{Handler: a.router, ReadHeaderTimeout: time.Second}

	go func() { _ = a.httpServer.Serve(ln) }()

	resp, err := http.Get("http://" + ln.Addr().String() + "/events") //nolint:noctx // the shutdown ends this stream
	require.NoError(t, err)

	defer resp.Body.Close()

	reader := bufio.NewReader(resp.Body)

	for {
		line, err := reader.ReadString('\n')
		require.NoError(t, err)

		if line == ": ready\n" {
			break
		}
	}

	start := time.Now()

	require.NoError(t, a.gracefulShutdown())
	assert.Less(t, time.Since(start), 2*time.Second, "shutdown waited on the open stream")

	select {
	case <-ended:
	default:
		t.Fatal("the stream's handler was still running after shutdown")
	}
}
