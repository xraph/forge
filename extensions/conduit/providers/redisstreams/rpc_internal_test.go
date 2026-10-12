package redisstreams

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/xraph/forge/extensions/conduit/core"
)

// TestNoReplyReaderStartsAfterClose covers RequestRPC racing Close: the call fetched the
// client just before Close stopped the readers and closed it. A reader started on that
// closed client would retry forever, so the call has to give up instead.
func TestNoReplyReaderStartsAfterClose(t *testing.T) {
	url := os.Getenv("CONDUIT_TEST_REDIS")
	if url == "" {
		if os.Getenv("CONDUIT_REQUIRE_PROVIDERS") == "1" {
			t.Fatal("CONDUIT_TEST_REDIS is required")
		}

		t.Skip("set CONDUIT_TEST_REDIS to run Redis provider tests")
	}

	p := New(Options{URL: url})
	if err := p.Connect(t.Context()); err != nil {
		t.Fatal(err)
	}

	c, err := p.client()
	if err != nil {
		t.Fatal(err)
	}

	if err := p.Close(t.Context()); err != nil {
		t.Fatal(err)
	}

	err = p.await(c, core.NewID(), core.NewID(), make(chan core.RPCResponse, 1))

	p.calls.mu.Lock()
	started := p.calls.done != nil
	p.calls.mu.Unlock()

	if started {
		p.stopReplies()
		t.Fatal("a reply reader started on a closed client")
	}

	if !errors.Is(err, core.ErrNotRunning) {
		t.Fatalf("a call after Close got %v, want ErrNotRunning", err)
	}

	// The provider still works once it connects again.
	if err := p.Connect(t.Context()); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = p.Close(context.WithoutCancel(t.Context())) })

	// The closed client stays refused after the reconnect.
	if err := p.await(c, core.NewID(), core.NewID(), make(chan core.RPCResponse, 1)); !errors.Is(err, core.ErrNotRunning) {
		t.Fatalf("a call holding the closed client got %v after reconnecting, want ErrNotRunning", err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()

	request := core.RPCRequest{Service: "pricing", Method: "quote", Envelope: core.Envelope{ID: core.NewID()}}
	if _, err := p.RequestRPC(ctx, core.Identity{Namespace: "rd-" + core.NewID()}, request); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("a call after reconnecting got %v, want it to wait for a reply", err)
	}
}
