package redisstreams

import (
	"context"
	"encoding/json"
	"errors"
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

	p.calls.mu.Lock()
	if p.calls.waiting == nil {
		p.calls.waiting, p.calls.pending = map[string]chan core.RPCResponse{}, map[string]struct{}{}
	}

	if _, busy := p.calls.pending[id]; busy {
		p.calls.mu.Unlock()

		return core.RPCResponse{}, &core.RPCError{Code: core.RPCConflict, Message: "Request is already in progress"}
	}

	p.calls.pending[id], p.calls.waiting[call] = struct{}{}, reply

	if p.calls.done == nil {
		readCtx, stop := context.WithCancel(context.Background())
		p.calls.stop, p.calls.done = stop, make(chan struct{})

		go p.readReplies(readCtx, c, p.calls.done)
	}
	p.calls.mu.Unlock()

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
	p.calls.stop, p.calls.done = nil, nil
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
	member  string
	base    context.Context //nolint:containedctx // The owning runtime cancels the server's lifetime after draining.
	mu      sync.Mutex
	active  map[string]*rpcWork
	closing bool
	jobs    chan *rpcWork
	stop    context.CancelFunc
	reading chan struct{}
	workers sync.WaitGroup
	once    sync.Once
	done    chan struct{}
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

	readCtx, stop := context.WithCancel(ctx) //nolint:gosec // The server stores stop and calls it from Close.
	s := &rpcServer{
		client: c, binding: binding, handler: handler, key: key, member: hash(binding.Identity.InstanceID, core.NewID()), base: ctx,
		active: map[string]*rpcWork{}, jobs: make(chan *rpcWork, binding.Config.MaxInFlight), stop: stop,
		reading: make(chan struct{}), done: make(chan struct{}),
	}

	for range binding.Config.Concurrency {
		s.workers.Add(1)

		go s.run()
	}

	go s.read(readCtx)
	go func() { <-s.reading; s.workers.Wait(); close(s.done) }()

	return s, nil
}

func (s *rpcServer) read(ctx context.Context) {
	defer close(s.reading)
	defer func() { s.mu.Lock(); s.closing = true; close(s.jobs); s.mu.Unlock() }()

	for ctx.Err() == nil {
		streams, err := s.client.XReadGroup(ctx, &redis.XReadGroupArgs{Group: rpcGroup, Consumer: s.member, Streams: []string{s.key, ">"}, Count: 1, Block: 250 * time.Millisecond}).Result()
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
				s.accept(ctx, msg)
			}
		}
	}
}

func (s *rpcServer) accept(ctx context.Context, msg redis.XMessage) {
	// The request leaves the group before any work starts, so no replica can claim it again.
	_ = s.client.XAck(ctx, s.key, rpcGroup, msg.ID).Err()
	_ = s.client.XDel(ctx, s.key, msg.ID).Err()

	raw, _ := msg.Values["request"].(string)

	var message rpcMessage
	if len(raw) > s.binding.MaxPayloadBytes+64*1024 || json.Unmarshal([]byte(raw), &message) != nil || !validReplyKey(message.Reply) {
		return
	}

	request := message.Request
	if request.Deadline.IsZero() || request.Service != s.binding.Identity.ServiceID || request.Method != s.binding.Method || request.Envelope.Source.Namespace != s.binding.Identity.Namespace || request.Envelope.Source.Validate() != nil || core.ValidateMessageID(request.Envelope.ID) != nil {
		s.refuse(message.Reply, message.Call, request, core.RPCBadRequest, "Invalid RPC request")

		return
	}

	if !time.Now().Before(request.Deadline) {
		return // the caller has already given up
	}

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
		s.refuse(message.Reply, message.Call, request, core.RPCUnavailable, "Service is draining")

		return
	case s.active[key] != nil:
		s.mu.Unlock()
		cancel()
		s.refuse(message.Reply, message.Call, request, core.RPCConflict, "Request is already in progress")

		return
	case len(s.active) >= s.binding.Config.MaxInFlight:
		s.mu.Unlock()
		cancel()
		s.refuse(message.Reply, message.Call, request, core.RPCUnavailable, "Service concurrency limit reached")

		return
	}

	s.active[key] = work
	s.jobs <- work
	s.mu.Unlock()
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
	s.once.Do(s.stop)

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
