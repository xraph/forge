package redisstreams

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/xraph/forge/extensions/conduit/core"
)

const rpcGroup = "servers"

func rpcRequestKey(namespace, service, method string) string {
	return "fc:{" + hash("rpc", namespace, service, method) + "}:requests"
}

func (p *Provider) replyKey() string { return "fc:{" + hash("reply", p.owner) + "}:replies" }

// validReplyKey stops a request from naming an arbitrary key for the server to write to.
func validReplyKey(key string) bool {
	return len(key) == len("fc:{")+32+len("}:replies") && strings.HasPrefix(key, "fc:{") && strings.HasSuffix(key, "}:replies")
}

type rpcMessage struct {
	Request core.RPCRequest `json:"request"`
	Reply   string          `json:"reply"`
	Call    string          `json:"call"`
}

// rpcCalls routes replies from this process's reply stream to waiting callers. Replies are
// matched by a token minted per call, so a late reply to an abandoned attempt can never
// answer a retry that reuses the envelope ID. pending holds the envelope IDs in flight.
type rpcCalls struct {
	mu      sync.Mutex
	waiting map[string]chan core.RPCResponse
	pending map[string]struct{}
	stop    context.CancelFunc
	done    chan struct{}
	// client is the connection a reply reader may use. Connect sets it, and stopReplies
	// clears it before Close closes that connection.
	client *redis.Client
}

// RequestRPC sends once and waits for the reply or the caller's deadline. A server that
// dies holding the request leaves the caller to time out, so a request runs at most once.
func (p *Provider) RequestRPC(ctx context.Context, identity core.Identity, request core.RPCRequest) (core.RPCResponse, error) {
	c, err := p.client()
	if err != nil {
		return core.RPCResponse{}, err
	}

	id, call := request.Envelope.ID, core.NewID()
	reply := make(chan core.RPCResponse, 1)

	if err := p.await(c, id, call, reply); err != nil {
		return core.RPCResponse{}, err
	}

	defer func() {
		p.calls.mu.Lock()
		delete(p.calls.waiting, call)
		delete(p.calls.pending, id)
		p.calls.mu.Unlock()
	}()

	data, err := json.Marshal(rpcMessage{Request: request, Reply: p.replyKey(), Call: call})
	if err != nil {
		return core.RPCResponse{}, err
	}

	key := rpcRequestKey(identity.Namespace, request.Service, request.Method)
	if err := c.XAdd(ctx, &redis.XAddArgs{Stream: key, MaxLen: 10000, Approx: true, Values: map[string]any{"request": string(data)}}).Err(); err != nil {
		if ctx.Err() != nil {
			return core.RPCResponse{}, ctx.Err()
		}

		return core.RPCResponse{}, core.ErrOutcomeUnknown
	}

	select {
	case response := <-reply:
		return response, nil
	case <-ctx.Done():
		return core.RPCResponse{}, ctx.Err()
	}
}

// await registers a call's reply channel, and starts the reply reader on c if none runs.
// A call that fetched c just before Close gets ErrNotRunning: a reader on a closed
// client would retry forever, and would take the place of the next connection's reader.
func (p *Provider) await(c *redis.Client, id, call string, reply chan core.RPCResponse) error {
	p.calls.mu.Lock()
	defer p.calls.mu.Unlock()

	if c == nil || c != p.calls.client {
		return core.ErrNotRunning
	}

	if p.calls.waiting == nil {
		p.calls.waiting, p.calls.pending = map[string]chan core.RPCResponse{}, map[string]struct{}{}
	}

	if _, busy := p.calls.pending[id]; busy {
		return &core.RPCError{Code: core.RPCConflict, Message: "Request is already in progress"}
	}

	p.calls.pending[id], p.calls.waiting[call] = struct{}{}, reply

	if p.calls.done == nil {
		readCtx, stop := context.WithCancel(context.Background())
		p.calls.stop, p.calls.done = stop, make(chan struct{})

		go p.readReplies(readCtx, c, p.calls.done)
	}

	return nil
}

// readReplies reads this process's reply stream from the start. The key is unique to the
// process, so nothing older than the process can be in it.
func (p *Provider) readReplies(ctx context.Context, c *redis.Client, done chan struct{}) {
	defer close(done)

	key, last := p.replyKey(), "0-0"

	for ctx.Err() == nil {
		streams, err := c.XRead(ctx, &redis.XReadArgs{Streams: []string{key, last}, Count: 64, Block: 250 * time.Millisecond}).Result()
		if errors.Is(err, redis.Nil) {
			continue
		}

		if err != nil {
			if ctx.Err() != nil {
				return
			}

			time.Sleep(50 * time.Millisecond)

			continue
		}

		for _, stream := range streams {
			for _, msg := range stream.Messages {
				last = msg.ID

				raw, ok := msg.Values["response"].(string)
				call, _ := msg.Values["call"].(string)

				if !ok || call == "" {
					continue
				}

				var response core.RPCResponse
				if json.Unmarshal([]byte(raw), &response) != nil {
					continue
				}

				p.calls.mu.Lock()
				waiter := p.calls.waiting[call]
				p.calls.mu.Unlock()

				if waiter != nil {
					select {
					case waiter <- response:
					default:
					}
				}
			}
		}
	}
}

// stopReplies ends the reply reader before the client closes.
func (p *Provider) stopReplies() {
	p.calls.mu.Lock()
	stop, done := p.calls.stop, p.calls.done
	p.calls.stop, p.calls.done, p.calls.client = nil, nil, nil
	p.calls.mu.Unlock()

	if stop != nil {
		stop()
		<-done
	}
}

type rpcWork struct {
	request core.RPCRequest
	reply   string
	call    string
	ctx     context.Context //nolint:containedctx // Accepted work keeps its own deadline and cancel function.
	cancel  context.CancelFunc
}

type rpcServer struct {
	client  *redis.Client
	binding core.RPCBinding
	handler func(context.Context, core.RPCRequest) core.RPCResponse
	key     string
	base    context.Context //nolint:containedctx // The owning runtime cancels the server's lifetime after draining.
	mu      sync.Mutex
	active  map[string]*rpcWork
	closing bool
	jobs    chan *rpcWork
	// leave stops intake: it unregisters the server from the provider's reader, then
	// closes jobs. Close calls it, and so does the end of ServeRPC's context.
	leave   func()
	unwatch func() bool
	workers sync.WaitGroup
	done    chan struct{}
}

// rpcServing is the provider's one request reader. It reads every served method's stream
// in a single XREADGROUP, so serving any number of procedures holds one blocking
// connection.
type rpcServing struct {
	mu      sync.Mutex
	servers map[string][]*rpcServer
	turn    int
	stop    context.CancelFunc
	done    chan struct{}
	// client is the connection the reader may use, guarded like rpcCalls.client.
	client *redis.Client
}

func workKey(msg core.Envelope) string {
	return msg.Source.ServiceID + "\x00" + msg.Source.InstanceID + "\x00" + msg.ID
}

// ServeRPC joins the method's consumer group. Replicas share requests, and each replica
// bounds its own accepted work.
func (p *Provider) ServeRPC(ctx context.Context, binding core.RPCBinding, handler func(context.Context, core.RPCRequest) core.RPCResponse) (core.RPCServer, error) {
	c, err := p.client()
	if err != nil {
		return nil, err
	}

	key := rpcRequestKey(binding.Identity.Namespace, binding.Identity.ServiceID, binding.Method)
	if err := c.XGroupCreateMkStream(ctx, key, rpcGroup, "$").Err(); err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
		return nil, err
	}

	s := &rpcServer{
		client: c, binding: binding, handler: handler, key: key, base: ctx,
		active: map[string]*rpcWork{}, jobs: make(chan *rpcWork, binding.Config.MaxInFlight), done: make(chan struct{}),
	}

	if err := p.register(c, s); err != nil {
		return nil, err
	}

	left := make(chan struct{})
	s.leave = sync.OnceFunc(func() {
		p.unregister(s)

		s.mu.Lock()
		s.closing = true
		close(s.jobs)
		s.mu.Unlock()
		close(left)
	})
	s.unwatch = context.AfterFunc(ctx, s.leave)

	for range binding.Config.Concurrency {
		s.workers.Add(1)

		go s.run()
	}

	go func() { <-left; s.workers.Wait(); close(s.done) }()

	return s, nil
}

// register adds a server to the provider's reader, starting the reader if none runs. A
// server whose client was closed by Close gets ErrNotRunning, as await does for calls.
func (p *Provider) register(c *redis.Client, s *rpcServer) error {
	p.serving.mu.Lock()
	defer p.serving.mu.Unlock()

	if c == nil || c != p.serving.client {
		return core.ErrNotRunning
	}

	if p.serving.servers == nil {
		p.serving.servers = map[string][]*rpcServer{}
	}

	p.serving.servers[s.key] = append(p.serving.servers[s.key], s)

	if p.serving.done == nil {
		readCtx, stop := context.WithCancel(context.Background())
		p.serving.stop, p.serving.done = stop, make(chan struct{})

		go p.readRequests(readCtx, c, p.serving.done)
	}

	return nil
}

func (p *Provider) unregister(s *rpcServer) {
	p.serving.mu.Lock()
	defer p.serving.mu.Unlock()

	servers := slices.DeleteFunc(p.serving.servers[s.key], func(other *rpcServer) bool { return other == s })
	if len(servers) == 0 {
		delete(p.serving.servers, s.key)
	} else {
		p.serving.servers[s.key] = servers
	}
}

// requestBatch returns the keys to read this turn and the servers registered for them. A
// reader with nothing to serve ends, and the next ServeRPC starts another.
func (p *Provider) requestBatch(done chan struct{}) ([]string, map[string][]*rpcServer, bool) {
	p.serving.mu.Lock()
	defer p.serving.mu.Unlock()

	if len(p.serving.servers) == 0 {
		// stopServing may have taken this reader's place already.
		if p.serving.done == done {
			p.serving.stop()
			p.serving.stop, p.serving.done = nil, nil
		}

		return nil, nil, false
	}

	servers := make(map[string][]*rpcServer, len(p.serving.servers))
	for key, registered := range p.serving.servers {
		servers[key] = slices.Clone(registered)
	}

	keys := slices.Sorted(maps.Keys(servers))

	return keys, servers, true
}

// pick shares a method's requests between the servers one provider runs for it.
func (p *Provider) pick(servers []*rpcServer) *rpcServer {
	p.serving.mu.Lock()
	defer p.serving.mu.Unlock()

	p.serving.turn++

	return servers[p.serving.turn%len(servers)]
}

// readRequests reads at most one request per served method each turn, as each server's
// own reader did, so one replica never takes a burst it has to refuse. Keys registered
// since the last turn join the next one, within one BLOCK interval.
func (p *Provider) readRequests(ctx context.Context, c *redis.Client, done chan struct{}) {
	defer close(done)

	member := hash("rpc", p.owner)

	for ctx.Err() == nil {
		keys, servers, ok := p.requestBatch(done)
		if !ok {
			return
		}

		streams := make([]string, 0, 2*len(keys))
		streams = append(streams, keys...)

		for range keys {
			streams = append(streams, ">")
		}

		read, err := c.XReadGroup(ctx, &redis.XReadGroupArgs{Group: rpcGroup, Consumer: member, Streams: streams, Count: 1, Block: 250 * time.Millisecond}).Result()
		if errors.Is(err, redis.Nil) {
			continue
		}

		if err != nil {
			if ctx.Err() != nil {
				return
			}

			// One method's stream lost its group (a flush or an eviction) and the read
			// fails for every method. Recreating the groups keeps the others serving.
			if strings.Contains(err.Error(), "NOGROUP") {
				for _, key := range keys {
					_ = c.XGroupCreateMkStream(ctx, key, rpcGroup, "$").Err()
				}
			}

			time.Sleep(50 * time.Millisecond)

			continue
		}

		// Every request leaves the group before any work starts, so no replica can
		// claim it again.
		pipe := c.Pipeline()

		for _, stream := range read {
			for _, msg := range stream.Messages {
				pipe.XAck(ctx, stream.Stream, rpcGroup, msg.ID)
				pipe.XDel(ctx, stream.Stream, msg.ID)
			}
		}

		_, _ = pipe.Exec(ctx)

		for _, stream := range read {
			// A server that unregistered during the read is still in this turn's batch.
			registered := servers[stream.Stream]
			if len(registered) == 0 {
				continue
			}

			for _, msg := range stream.Messages {
				p.dispatch(stream.Stream, p.pick(registered), msg)
			}
		}
	}
}

// dispatch hands one request to server. A server that started closing after this turn's
// read began passes it to another live server for the same key, and the caller hears
// UNAVAILABLE only when none is left.
func (p *Provider) dispatch(key string, server *rpcServer, msg redis.XMessage) {
	if server.accept(msg) {
		return
	}

	p.serving.mu.Lock()
	live := slices.Clone(p.serving.servers[key])
	p.serving.mu.Unlock()

	for _, other := range live {
		if other != server && other.accept(msg) {
			return
		}
	}

	server.refuseClosing(msg)
}

// stopServing ends the request reader before the client closes.
func (p *Provider) stopServing() {
	p.serving.mu.Lock()
	stop, done := p.serving.stop, p.serving.done
	// Servers left registered belong to the closed client. A reader started after the
	// next Connect must not serve them.
	p.serving.stop, p.serving.done, p.serving.client, p.serving.servers = nil, nil, nil, nil
	p.serving.mu.Unlock()

	if stop != nil {
		stop()
		<-done
	}
}

// decode reads a request entry. It refuses a malformed request from a valid caller, and
// reports false for anything that needs no further work.
func (s *rpcServer) decode(msg redis.XMessage) (rpcMessage, bool) {
	raw, _ := msg.Values["request"].(string)

	var message rpcMessage
	if len(raw) > s.binding.MaxPayloadBytes+64*1024 || json.Unmarshal([]byte(raw), &message) != nil || !validReplyKey(message.Reply) {
		return message, false
	}

	request := message.Request
	if request.Deadline.IsZero() || request.Service != s.binding.Identity.ServiceID || request.Method != s.binding.Method || request.Envelope.Source.Namespace != s.binding.Identity.Namespace || request.Envelope.Source.Validate() != nil || core.ValidateMessageID(request.Envelope.ID) != nil {
		s.refuse(message.Reply, message.Call, request, core.RPCBadRequest, "Invalid RPC request")

		return message, false
	}

	// A caller that has already given up gets no reply.
	return message, time.Now().Before(request.Deadline)
}

// accept starts the request, refuses it or drops it, and reports true. A closing server
// does none of that and reports false, so the reader can hand the request on.
func (s *rpcServer) accept(msg redis.XMessage) bool {
	message, ok := s.decode(msg)
	if !ok {
		return true
	}

	request := message.Request

	deadline := request.Deadline
	if limit := time.Now().Add(s.binding.Config.Timeout); limit.Before(deadline) {
		deadline = limit
	}

	workCtx, cancel := context.WithDeadline(s.base, deadline)
	work := &rpcWork{request: request, reply: message.Reply, call: message.Call, ctx: workCtx, cancel: cancel}
	key := workKey(request.Envelope)

	s.mu.Lock()
	switch {
	case s.closing:
		s.mu.Unlock()
		cancel()

		return false
	case s.active[key] != nil:
		s.mu.Unlock()
		cancel()
		s.refuse(message.Reply, message.Call, request, core.RPCConflict, "Request is already in progress")

		return true
	case len(s.active) >= s.binding.Config.MaxInFlight:
		s.mu.Unlock()
		cancel()
		s.refuse(message.Reply, message.Call, request, core.RPCUnavailable, "Service concurrency limit reached")

		return true
	}

	s.active[key] = work
	s.jobs <- work
	s.mu.Unlock()

	return true
}

// refuseClosing answers a request no live server could take.
func (s *rpcServer) refuseClosing(msg redis.XMessage) {
	if message, ok := s.decode(msg); ok {
		s.refuse(message.Reply, message.Call, message.Request, core.RPCUnavailable, "Service is draining")
	}
}

func (s *rpcServer) refuse(reply, call string, request core.RPCRequest, code core.RPCCode, message string) {
	s.respond(reply, call, request.Deadline, core.RPCResponse{ID: request.Envelope.ID, Source: s.binding.Identity, Error: &core.RPCError{Code: code, Message: message}})
}

// respond skips WAITAOF: RPC is transient, and waiting on an fsync would only add latency.
func (s *rpcServer) respond(reply, call string, deadline time.Time, response core.RPCResponse) {
	data, err := json.Marshal(response)
	if err != nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.WithoutCancel(s.base), 5*time.Second)
	defer cancel()

	pipe := s.client.TxPipeline()
	pipe.XAdd(ctx, &redis.XAddArgs{Stream: reply, MaxLen: 1000, Approx: true, Values: map[string]any{"response": string(data), "call": call}})
	pipe.PExpire(ctx, reply, max(time.Minute, time.Until(deadline)+time.Minute))
	_, _ = pipe.Exec(ctx)
}

func (s *rpcServer) run() {
	defer s.workers.Done()

	for work := range s.jobs {
		response := core.RPCResponse{ID: work.request.Envelope.ID, Source: s.binding.Identity}
		if err := work.ctx.Err(); err != nil {
			response.Error = core.PublicRPCError(err)
		} else {
			response = s.handler(work.ctx, work.request)
		}

		s.respond(work.reply, work.call, work.request.Deadline, response)
		work.cancel()

		s.mu.Lock()
		delete(s.active, workKey(work.request.Envelope))
		s.mu.Unlock()
	}
}

// Close stops intake and drains accepted work until ctx ends.
func (s *rpcServer) Close(ctx context.Context) error {
	s.unwatch()
	s.leave()

	select {
	case <-s.done:
		return nil
	case <-ctx.Done():
		s.mu.Lock()
		for _, work := range s.active {
			work.cancel()
		}
		s.mu.Unlock()

		return ctx.Err()
	}
}

var _ core.RPCProvider = (*Provider)(nil)
