package conformance

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/xraph/forge/extensions/conduit/core"
)

type settings struct {
	leaseTTL time.Duration
}

// runCapabilities runs each group the provider claims. Claiming a capability without
// implementing its interface fails the group instead of skipping it.
func runCapabilities(t *testing.T, factory Factory, cfg settings) {
	t.Helper()

	probe := factory()
	caps := probe.Capabilities()
	_, registry := probe.(core.Registry)

	groups := []struct {
		name    string
		enabled bool
		run     func(*testing.T, Factory, settings)
	}{
		{"operations", caps.ConsumerControls, runOperations},
		{"backfill", caps.Backfill, runBackfill},
		{"rpc", caps.RPC, runRPC},
		{"registry", registry, runRegistry},
	}

	for _, group := range groups {
		if !group.enabled {
			continue
		}

		t.Run(group.name, func(t *testing.T) { group.run(t, factory, cfg) })
	}
}

type harness struct {
	t       *testing.T
	ctx     context.Context //nolint:containedctx // One group owns this deadline for its whole run.
	factory Factory
	ns      string
	stream  core.StreamConfig
}

func newHarness(t *testing.T, factory Factory) *harness {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	t.Cleanup(cancel)

	return &harness{
		t:       t,
		ctx:     ctx,
		factory: factory,
		ns:      "conformance-" + core.NewID(),
		stream:  core.StreamConfig{Name: "orders", Subjects: []string{"orders.>"}, Replicas: 1},
	}
}

func (h *harness) provider() core.Provider {
	h.t.Helper()

	p := h.factory()
	if err := p.Connect(h.ctx); err != nil {
		h.t.Fatal(err)
	}

	h.t.Cleanup(func() {
		if err := p.Close(context.WithoutCancel(h.t.Context())); err != nil {
			h.t.Error(err)
		}
	})

	if err := p.EnsureStream(h.ctx, h.ns, h.stream); err != nil {
		h.t.Fatal(err)
	}

	return p
}

func (h *harness) binding(p core.Provider, instance string, mode core.DeliveryMode, sub string) core.Binding {
	return core.Binding{
		Identity: core.Identity{Namespace: h.ns, ServiceID: "billing", InstanceID: instance},
		Stream:   h.stream,
		Subscription: core.SubscriptionConfig{
			ID: sub, Stream: h.stream.Name, MessageType: "orders.placed.v1", Mode: mode,
			Durable: p.Capabilities().Durable, Concurrency: 1, MaxInFlight: 4, Timeout: time.Second,
			MaxAttempts: 2, RetryDelay: time.Millisecond, StartAt: "all", BroadcastID: instance,
		},
	}
}

func (h *harness) subscribe(p core.Provider, b core.Binding) core.Subscription {
	h.t.Helper()

	s, err := p.Subscribe(h.ctx, b)
	if err != nil {
		h.t.Fatal(err)
	}

	h.t.Cleanup(func() {
		if err := s.Close(context.WithoutCancel(h.t.Context())); err != nil {
			h.t.Error(err)
		}
	})

	return s
}

func (h *harness) publish(p core.Provider, id string) core.Receipt {
	h.t.Helper()

	return h.publishType(p, id, "orders.placed.v1")
}

func (h *harness) publishType(p core.Provider, id, messageType string) core.Receipt {
	h.t.Helper()

	r, err := p.Publish(h.ctx, h.ns, h.stream, core.Envelope{
		ID: id, Type: messageType, ContentType: "application/json", Data: []byte(`{"id":"42"}`), CreatedAt: time.Now(),
		Source: core.Identity{Namespace: h.ns, ServiceID: "orders", InstanceID: "publisher"},
	})
	if err != nil {
		h.t.Fatal(err)
	}

	return r
}

func (h *harness) next(s core.Subscription) core.Delivery {
	h.t.Helper()

	d, err := s.Next(h.ctx)
	if err != nil {
		h.t.Fatal(err)
	}

	return d
}

func (h *harness) ack(d core.Delivery) {
	h.t.Helper()

	if err := d.Ack(h.ctx); err != nil {
		h.t.Fatal(err)
	}
}

// quiet fails if the subscription delivers anything within 400ms.
func (h *harness) quiet(s core.Subscription) {
	h.t.Helper()

	ctx, cancel := context.WithTimeout(h.ctx, 400*time.Millisecond)
	defer cancel()

	if d, err := s.Next(ctx); !errors.Is(err, context.DeadlineExceeded) {
		if err == nil {
			h.t.Fatalf("unexpected delivery %s", d.Message().ID)
		}

		h.t.Fatalf("unexpected error: %v", err)
	}
}

// eventually polls check for up to five seconds. Broker counters are allowed to lag.
func (h *harness) eventually(what string, check func() (bool, error)) {
	h.t.Helper()

	deadline := time.Now().Add(5 * time.Second)

	for {
		ok, err := check()
		if err != nil {
			h.t.Fatal(err)
		}

		if ok {
			return
		}

		if time.Now().After(deadline) {
			h.t.Fatalf("timed out waiting for %s", what)
		}

		time.Sleep(20 * time.Millisecond)
	}
}

func runRegistry(t *testing.T, factory Factory, cfg settings) {
	h := newHarness(t, factory)
	if cfg.leaseTTL <= 0 {
		t.Fatal("registry providers need conformance.WithLeaseTTL")
	}

	p := h.provider()
	registry, _ := p.(core.Registry) // runCapabilities only enables this group for registries

	lister, ok := p.(core.InstanceLister)
	if !ok {
		t.Fatal("a registry must also implement core.InstanceLister")
	}

	instance := func(service, id string, ready bool) core.Instance {
		return core.Instance{
			Identity:  core.Identity{Namespace: h.ns, ServiceID: service, InstanceID: id},
			Version:   "1.0.0",
			Endpoints: []core.Endpoint{{Protocol: "http", URL: "http://10.0.0.1:8080"}},
			Ready:     ready,
		}
	}
	names := func(list []core.Instance) []string {
		out := make([]string, 0, len(list))
		for _, i := range list {
			out = append(out, i.Identity.ServiceID+"/"+i.Identity.InstanceID)
		}

		slices.Sort(out)

		return out
	}
	expect := func(what string, got []core.Instance, err error, want ...string) {
		t.Helper()

		if err != nil {
			t.Fatal(err)
		}

		if !slices.Equal(names(got), want) {
			t.Fatalf("%s: got %v, want %v", what, names(got), want)
		}
	}

	a, b, idle, orders := instance("billing", "a", true), instance("billing", "b", true), instance("billing", "idle", false), instance("orders", "x", true)
	for _, i := range []core.Instance{a, b, idle, orders} {
		if err := registry.Register(h.ctx, i); err != nil {
			t.Fatal(err)
		}
	}

	foreign := instance("billing", "z", true)
	foreign.Identity.Namespace = h.ns + "-other"

	foreignRegistry, _ := h.provider().(core.Registry) // same provider type as p
	if err := foreignRegistry.Register(h.ctx, foreign); err != nil {
		t.Fatal(err)
	}

	resolved, err := registry.Resolve(h.ctx, h.ns, "billing")
	expect("resolve ready billing replicas", resolved, err, "billing/a", "billing/b")

	listed, err := lister.List(h.ctx, h.ns)
	expect("list the namespace", listed, err, "billing/a", "billing/b", "billing/idle", "orders/x")

	if err := registry.Deregister(h.ctx, a.Identity); err != nil {
		t.Fatal(err)
	}

	resolved, err = registry.Resolve(h.ctx, h.ns, "billing")
	expect("deregister removes one instance", resolved, err, "billing/b")

	// Keep b and x renewed the way the runtime does, and let idle's lease run out.
	stop, done := make(chan struct{}), make(chan struct{})

	go func() {
		defer close(done)

		ticker := time.NewTicker(cfg.leaseTTL / 4)
		defer ticker.Stop()

		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				for _, i := range []core.Instance{b, orders} {
					_ = registry.Register(h.ctx, i) // A missed renewal shows up in the assertion below.
				}
			}
		}
	}()

	timer := time.NewTimer(2*cfg.leaseTTL + 500*time.Millisecond)
	select {
	case <-h.ctx.Done():
		t.Fatal(h.ctx.Err())
	case <-timer.C:
	}

	close(stop)
	<-done

	listed, err = lister.List(h.ctx, h.ns)
	expect("expired lease disappears", listed, err, "billing/b", "orders/x")
}

// Stubs for groups that later tasks fill in. Each starts as a failing test so a provider
// claiming the capability can't pass silently while the group is unwritten.
func runOperations(t *testing.T, factory Factory, _ settings) {
	h := newHarness(t, factory)
	p := h.provider()

	ops, ok := p.(core.Operations)
	if !ok {
		t.Fatal("ConsumerControls requires core.Operations")
	}

	b := h.binding(p, "a", core.Competing, "inspect")
	s := h.subscribe(p, b)

	for i := range 3 {
		h.publish(p, fmt.Sprintf("inspect-%d", i))
	}

	expect := func(what string, pending, ackPending, redelivered uint64, paused bool) {
		t.Helper()

		h.eventually(what, func() (bool, error) {
			info, err := ops.InspectConsumer(h.ctx, b)
			if err != nil {
				return false, err
			}

			return info.ConsumerID == b.ConsumerID() && info.Pending == pending && info.AckPending == ackPending && info.Redelivered == redelivered && info.Paused == paused, nil
		})
	}

	expect("three pending", 3, 0, 0, false)

	d := h.next(s)

	expect("one delivered", 2, 1, 0, false)

	if err := d.Retry(h.ctx, time.Millisecond); err != nil {
		t.Fatal(err)
	}

	// Let the retry come due so it is delivered ahead of the messages still pending.
	time.Sleep(100 * time.Millisecond)

	d = h.next(s)
	if d.Info().Attempt < 2 {
		t.Fatal("retry did not redeliver")
	}

	expect("one redelivered", 2, 1, 1, false)
	h.ack(d)
	expect("first settled", 2, 0, 0, false)

	if err := ops.PauseConsumer(h.ctx, b, true); err != nil {
		t.Fatal(err)
	}

	expect("paused", 2, 0, 0, true)

	// A second replica of the same group must be held by the pause too.
	other := b
	other.Identity.InstanceID = "b"
	s2 := h.subscribe(h.provider(), other)

	h.quiet(s)
	h.quiet(s2)

	if err := ops.PauseConsumer(h.ctx, b, false); err != nil {
		t.Fatal(err)
	}

	seen := map[string]bool{}

	for range 2 {
		d := h.next(s)
		seen[d.Message().ID] = true
		h.ack(d)
	}

	if !seen["inspect-1"] || !seen["inspect-2"] {
		t.Fatalf("backlog lost while paused: %v", seen)
	}

	expect("drained", 0, 0, 0, false)
}
func runBackfill(t *testing.T, factory Factory, _ settings) {
	h := newHarness(t, factory)
	p := h.provider()

	ops, ok := p.(core.Operations)
	if !ok {
		t.Fatal("Backfill requires core.Operations")
	}

	b := h.binding(p, "bf", core.Competing, "backfill-target")
	s := h.subscribe(p, b)
	other := h.subscribe(p, h.binding(p, "observer", core.Broadcast, "backfill-other"))

	first := h.publish(p, "backfill-1")
	last := h.publish(p, "backfill-2")

	for _, sub := range []core.Subscription{s, other} {
		for range 2 {
			h.ack(h.next(sub))
		}
	}

	in := core.BackfillInput{ID: "op-" + core.NewID(), Subscription: b.Subscription.ID, Start: first.Sequence, End: last.Sequence}

	job, err := ops.RunBackfill(h.ctx, b, in)
	if err != nil || job.State != "complete" || job.Published != 2 {
		t.Fatalf("backfill did not complete: %+v %v", job, err)
	}

	got := map[string]bool{}

	for range 2 {
		d := h.next(s)
		if d.Message().TargetConsumer != b.ConsumerID() {
			t.Fatal("backfill is not consumer-scoped")
		}

		got[d.Message().ID] = true
		h.ack(d)
	}

	if !got["backfill-1"] || !got["backfill-2"] {
		t.Fatalf("backfill republished the wrong messages: %v", got)
	}

	h.quiet(other)

	again, err := ops.RunBackfill(h.ctx, b, in)
	if err != nil || again.State != "complete" || again.Published != 2 {
		t.Fatalf("rerun did not return the finished job: %+v %v", again, err)
	}

	h.quiet(s)

	changed := in
	changed.End = in.Start

	if _, err := ops.RunBackfill(h.ctx, b, changed); !errors.Is(err, core.ErrConflict) {
		t.Fatalf("reused operation ID accepted a new range: %v", err)
	}

	rows, _, err := ops.ListBackfills(h.ctx, b.Identity, "", 10)
	if err != nil || len(rows) != 1 || rows[0].Input.ID != in.ID {
		t.Fatalf("backfill history missing: %+v %v", rows, err)
	}

	foreign := b.Identity
	foreign.ServiceID = "another"

	rows, _, err = ops.ListBackfills(h.ctx, foreign, "", 10)
	if err != nil || len(rows) != 0 {
		t.Fatalf("backfill history leaked across services: %+v %v", rows, err)
	}
}
func runRPC(t *testing.T, factory Factory, _ settings) {
	h := newHarness(t, factory)
	serverA, serverB, caller := h.provider(), h.provider(), h.provider()

	rpcOf := func(p core.Provider) core.RPCProvider {
		t.Helper()

		r, ok := p.(core.RPCProvider)
		if !ok {
			t.Fatal("RPC requires core.RPCProvider")
		}

		return r
	}
	pricing := func(instance string) core.Identity {
		return core.Identity{Namespace: h.ns, ServiceID: "pricing", InstanceID: instance}
	}
	serve := func(p core.Provider, instance, method string, concurrency, inFlight int, handler func(context.Context, core.RPCRequest) core.RPCResponse) core.RPCServer {
		t.Helper()

		binding := core.RPCBinding{Identity: pricing(instance), Method: method, Config: core.RPCConfig{Timeout: 5 * time.Second, Concurrency: concurrency, MaxInFlight: inFlight}, MaxPayloadBytes: 1 << 20}

		server, err := rpcOf(p).ServeRPC(h.ctx, binding, handler)
		if err != nil {
			t.Fatal(err)
		}

		t.Cleanup(func() { _ = server.Close(context.WithoutCancel(t.Context())) })

		return server
	}
	reply := func(instance string, request core.RPCRequest, data string) core.RPCResponse {
		return core.RPCResponse{ID: request.Envelope.ID, Source: pricing(instance), Data: []byte(data)}
	}
	callerID := core.Identity{Namespace: h.ns, ServiceID: "web", InstanceID: "caller"}
	call := func(identity core.Identity, method string, within time.Duration) (core.RPCResponse, error) {
		ctx, cancel := context.WithTimeout(h.ctx, within)
		defer cancel()

		deadline, _ := ctx.Deadline()
		envelope := core.Envelope{ID: core.NewID(), Type: method, Source: identity, ContentType: "application/json", Data: []byte(`{}`), CreatedAt: time.Now()}

		response, err := rpcOf(caller).RequestRPC(ctx, identity, core.RPCRequest{Service: "pricing", Method: method, Envelope: envelope, Deadline: deadline})
		if err == nil && response.ID != envelope.ID {
			t.Fatalf("response %s answers request %s", response.ID, envelope.ID)
		}

		return response, err
	}
	codeOf := func(response core.RPCResponse, err error) core.RPCCode {
		var public *core.RPCError
		if errors.As(err, &public) {
			return public.Code
		}

		if err != nil {
			return core.PublicRPCError(err).Code
		}

		if response.Error != nil {
			return response.Error.Code
		}

		return ""
	}

	// Two replicas share one procedure, and each request runs once.
	var mu sync.Mutex

	handled := map[string]int{}
	byReplica := map[string]int{}
	echo := func(instance string) func(context.Context, core.RPCRequest) core.RPCResponse {
		return func(_ context.Context, request core.RPCRequest) core.RPCResponse {
			mu.Lock()
			handled[request.Envelope.ID]++
			byReplica[instance]++
			mu.Unlock()

			return reply(instance, request, `{"ok":true}`)
		}
	}

	serve(serverA, "a", "quote", 2, 8, echo("a"))
	serve(serverB, "b", "quote", 2, 8, echo("b"))

	for range 32 {
		response, err := call(callerID, "quote", 5*time.Second)
		if err != nil || response.Error != nil || string(response.Data) != `{"ok":true}` || response.Source.ServiceID != "pricing" {
			t.Fatalf("call failed: %+v %v", response, err)
		}
	}

	mu.Lock()
	for id, count := range handled {
		if count != 1 {
			t.Fatalf("request %s ran %d times", id, count)
		}
	}

	if len(handled) != 32 || byReplica["a"] == 0 || byReplica["b"] == 0 {
		t.Fatalf("replicas did not share requests: %v", byReplica)
	}

	before := len(handled)
	mu.Unlock()

	// The caller's deadline reaches the handler.
	seen := make(chan time.Time, 1)

	serve(serverA, "a", "deadline", 1, 1, func(ctx context.Context, request core.RPCRequest) core.RPCResponse {
		deadline, _ := ctx.Deadline()
		seen <- deadline

		return reply("a", request, `{}`)
	})

	started := time.Now()

	if _, err := call(callerID, "deadline", 2*time.Second); err != nil {
		t.Fatal(err)
	}

	if deadline := <-seen; deadline.IsZero() || deadline.After(started.Add(2*time.Second+250*time.Millisecond)) {
		t.Fatalf("caller deadline did not reach the handler: %v", deadline)
	}

	// A replica at MaxInFlight refuses instead of queueing.
	release, entered := make(chan struct{}), make(chan struct{}, 1)

	serve(serverA, "a", "slow", 1, 1, func(ctx context.Context, request core.RPCRequest) core.RPCResponse {
		entered <- struct{}{}

		select {
		case <-release:
		case <-ctx.Done():
		}

		return reply("a", request, `{}`)
	})

	firstDone := make(chan error, 1)

	go func() {
		_, err := call(callerID, "slow", 5*time.Second)
		firstDone <- err
	}()

	select {
	case <-entered:
	case <-h.ctx.Done():
		t.Fatal(h.ctx.Err())
	}

	if code := codeOf(call(callerID, "slow", 2*time.Second)); code != core.RPCUnavailable {
		t.Fatalf("overloaded replica returned %q", code)
	}

	close(release)

	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}

	// A handler's public error comes back unchanged.
	serve(serverA, "a", "refuse", 1, 1, func(_ context.Context, request core.RPCRequest) core.RPCResponse {
		response := reply("a", request, "")
		response.Data = nil
		response.Error = &core.RPCError{Code: core.RPCConflict, Message: "quote expired"}

		return response
	})

	response, err := call(callerID, "refuse", 2*time.Second)
	if err != nil || response.Error == nil || response.Error.Code != core.RPCConflict || response.Error.Message != "quote expired" {
		t.Fatalf("public error lost: %+v %v", response, err)
	}

	// Close finishes work it already accepted.
	draining := make(chan struct{}, 1)
	drainServer := serve(serverB, "b", "drain", 1, 1, func(_ context.Context, request core.RPCRequest) core.RPCResponse {
		draining <- struct{}{}

		time.Sleep(200 * time.Millisecond)

		return reply("b", request, `{}`)
	})
	result := make(chan error, 1)

	go func() {
		response, err := call(callerID, "drain", 5*time.Second)
		if err == nil && response.Error != nil {
			err = response.Error
		}

		result <- err
	}()

	select {
	case <-draining:
	case <-h.ctx.Done():
		t.Fatal(h.ctx.Err())
	}

	closeCtx, cancel := context.WithTimeout(h.ctx, 5*time.Second)
	defer cancel()

	if err := drainServer.Close(closeCtx); err != nil {
		t.Fatalf("close did not drain: %v", err)
	}

	if err := <-result; err != nil {
		t.Fatalf("accepted call lost while draining: %v", err)
	}

	// Requests never cross namespaces.
	foreign := core.Identity{Namespace: h.ns + "-other", ServiceID: "web", InstanceID: "caller"}
	if code := codeOf(call(foreign, "quote", 400*time.Millisecond)); code == "" {
		t.Fatal("request crossed namespaces")
	}

	mu.Lock()
	defer mu.Unlock()

	if len(handled) != before {
		t.Fatal("a foreign namespace reached a handler")
	}
}
