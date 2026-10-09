package jetstream

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/xraph/forge/extensions/conduit/core"
)

func rpcSubject(namespace, service, method string) string {
	return "fc.rpc." + hash(namespace) + "." + hash(service) + "." + hash(method)
}
func rpcKey(message core.Envelope) string {
	return message.Source.Namespace + "\x00" + message.Source.ServiceID + "\x00" + message.Source.InstanceID + "\x00" + message.ID
}

func (p *Provider) rpcConnection() (*nats.Conn, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.conn == nil {
		return nil, core.ErrNotRunning
	}

	return p.conn, nil
}

// RequestRPC uses live NATS request/reply and sends cancellation on the same method scope.
func (p *Provider) RequestRPC(ctx context.Context, identity core.Identity, request core.RPCRequest) (core.RPCResponse, error) {
	conn, err := p.rpcConnection()
	if err != nil {
		return core.RPCResponse{}, err
	}

	data, err := json.Marshal(request)
	if err != nil {
		return core.RPCResponse{}, err
	}

	subject := rpcSubject(identity.Namespace, request.Service, request.Method)

	message, err := conn.RequestMsgWithContext(ctx, &nats.Msg{Subject: subject + ".request", Data: data})
	if err != nil {
		if ctx.Err() != nil {
			cancelData, encodeErr := json.Marshal(core.Envelope{ID: request.Envelope.ID, Source: identity})
			if encodeErr == nil {
				_ = conn.Publish(subject+".cancel", cancelData)
			}

			return core.RPCResponse{}, ctx.Err()
		}

		if errors.Is(err, nats.ErrNoResponders) {
			return core.RPCResponse{}, &core.RPCError{Code: core.RPCNotFound, Message: "No service instance handles this procedure"}
		}

		if errors.Is(err, nats.ErrMaxPayload) {
			return core.RPCResponse{}, &core.RPCError{Code: core.RPCBadRequest, Message: "Request exceeded broker limits"}
		}

		return core.RPCResponse{}, core.ErrOutcomeUnknown
	}

	var response core.RPCResponse
	if err := json.Unmarshal(message.Data, &response); err != nil {
		return core.RPCResponse{}, &core.RPCError{Code: core.RPCInternal, Message: "Invalid broker reply"}
	}

	return response, nil
}

type rpcWork struct {
	request core.RPCRequest
	reply   string
	ctx     context.Context //nolint:containedctx // Queued work retains its own deadline and explicit cancel function.
	cancel  context.CancelFunc
}
type rpcServer struct {
	conn     *nats.Conn
	binding  core.RPCBinding
	handler  func(context.Context, core.RPCRequest) core.RPCResponse
	mu       sync.Mutex
	active   map[string]*rpcWork
	closing  bool
	jobs     chan *rpcWork
	requests *nats.Subscription
	cancels  *nats.Subscription
	workers  sync.WaitGroup
	once     sync.Once
	done     chan struct{}
	base     context.Context //nolint:containedctx // Server lifetime is canceled by the owning runtime after draining.
}

// ServeRPC shares one service queue group while bounding each instance's accepted work.
func (p *Provider) ServeRPC(ctx context.Context, binding core.RPCBinding, handler func(context.Context, core.RPCRequest) core.RPCResponse) (core.RPCServer, error) {
	conn, err := p.rpcConnection()
	if err != nil {
		return nil, err
	}

	s := &rpcServer{conn: conn, binding: binding, handler: handler, base: ctx, active: map[string]*rpcWork{}, jobs: make(chan *rpcWork, binding.Config.MaxInFlight), done: make(chan struct{})}
	subject := rpcSubject(binding.Identity.Namespace, binding.Identity.ServiceID, binding.Method)

	s.cancels, err = conn.Subscribe(subject+".cancel", func(message *nats.Msg) {
		var envelope core.Envelope
		if json.Unmarshal(message.Data, &envelope) != nil || envelope.Source.Namespace != binding.Identity.Namespace {
			return
		}

		s.mu.Lock()
		if work := s.active[rpcKey(envelope)]; work != nil {
			work.cancel()
		}
		s.mu.Unlock()
	})
	if err != nil {
		return nil, err
	}

	s.requests, err = conn.QueueSubscribe(subject+".request", "conduit-"+hash(binding.Identity.Namespace+"\x00"+binding.Identity.ServiceID+"\x00"+binding.Method), s.accept)
	if err != nil {
		_ = s.cancels.Unsubscribe()

		return nil, err
	}

	if err := s.requests.SetPendingLimits(binding.Config.MaxInFlight, min(64*1024*1024, max(binding.MaxPayloadBytes, 1)*binding.Config.MaxInFlight)); err != nil {
		_ = s.requests.Unsubscribe()
		_ = s.cancels.Unsubscribe()

		return nil, err
	}

	ready, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := conn.FlushWithContext(ready); err != nil {
		_ = s.requests.Unsubscribe()
		_ = s.cancels.Unsubscribe()

		return nil, err
	}

	for range binding.Config.Concurrency {
		s.workers.Add(1)
		go s.run()
	}

	go func() { s.workers.Wait(); close(s.done) }()

	return s, nil
}

func (s *rpcServer) response(ctx context.Context, reply string, response core.RPCResponse) {
	data, err := json.Marshal(response)
	if err != nil {
		return
	}

	if int64(len(data)) > s.conn.MaxPayload() {
		response.Data = nil
		response.Error = &core.RPCError{Code: core.RPCInternal, Message: "Response exceeded broker limits"}

		data, err = json.Marshal(response)
		if err != nil {
			return
		}
	}

	if err := s.conn.Publish(reply, data); err != nil {
		return
	}

	flush, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()

	_ = s.conn.FlushWithContext(flush)
}
func (s *rpcServer) refuse(reply, id string, code core.RPCCode, message string) {
	s.response(s.base, reply, core.RPCResponse{ID: id, Source: s.binding.Identity, Error: &core.RPCError{Code: code, Message: message}})
}
func (s *rpcServer) accept(message *nats.Msg) {
	if !strings.HasPrefix(message.Reply, "_INBOX.") && (s.conn.Opts.InboxPrefix == "" || !strings.HasPrefix(message.Reply, s.conn.Opts.InboxPrefix+".")) {
		return
	}

	var request core.RPCRequest
	if len(message.Data) > s.binding.MaxPayloadBytes+64*1024 || json.Unmarshal(message.Data, &request) != nil || request.Deadline.IsZero() || request.Service != s.binding.Identity.ServiceID || request.Method != s.binding.Method || request.Envelope.Source.Namespace != s.binding.Identity.Namespace || request.Envelope.Source.Validate() != nil || core.ValidateMessageID(request.Envelope.ID) != nil {
		s.refuse(message.Reply, request.Envelope.ID, core.RPCBadRequest, "Invalid RPC request")

		return
	}

	deadline := minTime(request.Deadline, time.Now().Add(s.binding.Config.Timeout))
	ctx, cancel := context.WithDeadline(s.base, deadline)
	work := &rpcWork{request: request, reply: message.Reply, ctx: ctx, cancel: cancel}
	key := rpcKey(request.Envelope)

	s.mu.Lock()
	switch {
	case s.closing:
		s.mu.Unlock()
		cancel()
		s.refuse(message.Reply, request.Envelope.ID, core.RPCUnavailable, "Service is draining")

		return
	case s.active[key] != nil:
		s.mu.Unlock()
		cancel()
		s.refuse(message.Reply, request.Envelope.ID, core.RPCConflict, "Request is already in progress")

		return
	case len(s.active) >= s.binding.Config.MaxInFlight:
		s.mu.Unlock()
		cancel()
		s.refuse(message.Reply, request.Envelope.ID, core.RPCUnavailable, "Service concurrency limit reached")

		return
	}

	s.active[key] = work
	s.jobs <- work
	s.mu.Unlock()
}
func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}

	return b
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

		s.response(work.ctx, work.reply, response)
		work.cancel()
		s.mu.Lock()
		delete(s.active, rpcKey(work.request.Envelope))
		s.mu.Unlock()
	}
}
func (s *rpcServer) Close(ctx context.Context) error {
	s.once.Do(func() { s.mu.Lock(); s.closing = true; close(s.jobs); s.mu.Unlock(); _ = s.requests.Unsubscribe() })

	select {
	case <-s.done:
		_ = s.cancels.Unsubscribe()

		return nil
	case <-ctx.Done():
		s.mu.Lock()
		for _, work := range s.active {
			work.cancel()
		}
		s.mu.Unlock()
		_ = s.cancels.Unsubscribe()

		return ctx.Err()
	}
}

var _ core.RPCProvider = (*Provider)(nil)
