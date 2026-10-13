package redisstreams

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/xraph/forge/extensions/conduit/core"
)

func testURL(t *testing.T) string {
	t.Helper()

	url := os.Getenv("CONDUIT_TEST_REDIS")
	if url == "" {
		if os.Getenv("CONDUIT_REQUIRE_PROVIDERS") == "1" {
			t.Fatal("CONDUIT_TEST_REDIS is required")
		}

		t.Skip("set CONDUIT_TEST_REDIS to run Redis provider tests")
	}

	return url
}

// TestNoReplyReaderStartsAfterClose covers RequestRPC racing Close: the call fetched the
// client just before Close stopped the readers and closed it. A reader started on that
// closed client would retry forever, so the call has to give up instead.
func TestNoReplyReaderStartsAfterClose(t *testing.T) {
	p := New(Options{URL: testURL(t)})
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

// TestNoRequestReaderStartsAfterClose covers ServeRPC racing Close, as the test above does
// for calls.
func TestNoRequestReaderStartsAfterClose(t *testing.T) {
	p := New(Options{URL: testURL(t)})
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

	err = p.register(c, &rpcServer{key: rpcRequestKey("rd-"+core.NewID(), "pricing", "quote")})

	p.serving.mu.Lock()
	started := p.serving.done != nil
	p.serving.mu.Unlock()

	if started {
		p.stopServing()
		t.Fatal("a request reader started on a closed client")
	}

	if !errors.Is(err, core.ErrNotRunning) {
		t.Fatalf("serving after Close got %v, want ErrNotRunning", err)
	}
}

// TestLostGroupDoesNotStopOtherProcedures deletes one served method's stream. The shared
// read then fails for every method until the reader recreates the group.
func TestLostGroupDoesNotStopOtherProcedures(t *testing.T) {
	url := testURL(t)
	server, caller := New(Options{URL: url}), New(Options{URL: url})

	for _, p := range []*Provider{server, caller} {
		if err := p.Connect(t.Context()); err != nil {
			t.Fatal(err)
		}

		t.Cleanup(func() { _ = p.Close(context.WithoutCancel(t.Context())) })
	}

	ns := "rg-" + core.NewID()
	identity := core.Identity{Namespace: ns, ServiceID: "web", InstanceID: "caller"}

	for _, method := range []string{"kept", "lost"} {
		binding := core.RPCBinding{
			Identity: core.Identity{Namespace: ns, ServiceID: "pricing", InstanceID: "a"}, Method: method,
			Config: core.RPCConfig{Timeout: 5 * time.Second, Concurrency: 1, MaxInFlight: 1}, MaxPayloadBytes: 1 << 20,
		}

		served, err := server.ServeRPC(t.Context(), binding, func(_ context.Context, r core.RPCRequest) core.RPCResponse {
			return core.RPCResponse{ID: r.Envelope.ID, Source: binding.Identity, Data: []byte(`{}`)}
		})
		if err != nil {
			t.Fatal(err)
		}

		t.Cleanup(func() { _ = served.Close(context.WithoutCancel(t.Context())) })
	}

	c, err := server.client()
	if err != nil {
		t.Fatal(err)
	}

	if err := c.Del(t.Context(), rpcRequestKey(ns, "pricing", "lost")).Err(); err != nil {
		t.Fatal(err)
	}

	time.Sleep(time.Second) // the reader meets the missing group and repairs it

	for _, method := range []string{"kept", "lost"} {
		ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
		deadline, _ := ctx.Deadline()
		request := core.RPCRequest{
			Service: "pricing", Method: method, Deadline: deadline,
			Envelope: core.Envelope{ID: core.NewID(), Type: method, Source: identity, ContentType: "application/json", Data: []byte(`{}`), CreatedAt: time.Now()},
		}

		response, err := caller.RequestRPC(ctx, identity, request)

		cancel()

		if err != nil || response.Error != nil {
			t.Fatalf("%s after the lost group: %+v %v", method, response, err)
		}
	}
}
