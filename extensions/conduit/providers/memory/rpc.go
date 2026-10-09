package memory

import (
	"context"
	"github.com/xraph/forge/extensions/conduit/core"
	"sync"
	"time"
)

type rpcServer struct {
	broker   *Broker
	binding  core.RPCBinding
	handler  func(context.Context, core.RPCRequest) core.RPCResponse
	base     context.Context //nolint:containedctx // Server lifetime is canceled by the owning runtime after draining.
	closing  bool
	accepted chan struct{}
	workers  chan struct{}
	active   map[string]context.CancelFunc
	wait     sync.WaitGroup
	once     sync.Once
	done     chan struct{}
}

func rpcKey(namespace, service, method string) string {
	return namespace + "\x00" + service + "\x00" + method
}
func requestKey(request core.RPCRequest) string {
	return request.Envelope.Source.ServiceID + "\x00" + request.Envelope.Source.InstanceID + "\x00" + request.Envelope.ID
}

// ServeRPC registers a bounded process-local replica for development.
func (b *Broker) ServeRPC(ctx context.Context, binding core.RPCBinding, handler func(context.Context, core.RPCRequest) core.RPCResponse) (core.RPCServer, error) {
	s := &rpcServer{broker: b, binding: binding, handler: handler, base: ctx, accepted: make(chan struct{}, binding.Config.MaxInFlight), workers: make(chan struct{}, binding.Config.Concurrency), active: map[string]context.CancelFunc{}, done: make(chan struct{})}
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.rpc == nil {
		b.rpc = map[string][]*rpcServer{}
	}

	key := rpcKey(binding.Identity.Namespace, binding.Identity.ServiceID, binding.Method)
	b.rpc[key] = append(b.rpc[key], s)

	return s, nil
}

// RequestRPC chooses one available logical-service replica without persistent storage.
func (b *Broker) RequestRPC(ctx context.Context, identity core.Identity, request core.RPCRequest) (core.RPCResponse, error) {
	b.mu.Lock()
	key := rpcKey(identity.Namespace, request.Service, request.Method)

	members := b.rpc[key]
	if len(members) == 0 {
		b.mu.Unlock()

		return core.RPCResponse{}, &core.RPCError{Code: core.RPCNotFound, Message: "No service instance handles this procedure"}
	}

	b.rpcCursor = (b.rpcCursor + 1) % len(members)

	var selected *rpcServer

	for i := range members {
		member := members[(b.rpcCursor+i)%len(members)]
		if member.closing {
			continue
		}

		select {
		case member.accepted <- struct{}{}:
			selected = member
		default:
		}

		if selected != nil {
			break
		}
	}

	if selected == nil {
		b.mu.Unlock()

		return core.RPCResponse{}, &core.RPCError{Code: core.RPCUnavailable, Message: "Service concurrency limit reached"}
	}

	id := requestKey(request)
	if selected.active[id] != nil {
		<-selected.accepted
		b.mu.Unlock()

		return core.RPCResponse{}, &core.RPCError{Code: core.RPCConflict, Message: "Request is already in progress"}
	}

	deadline := request.Deadline
	if limit := time.Now().Add(selected.binding.Config.Timeout); limit.Before(deadline) {
		deadline = limit
	}

	work, cancel := context.WithDeadline(selected.base, deadline)
	stopCancellation := context.AfterFunc(ctx, cancel)
	selected.active[id] = cancel
	selected.wait.Add(1)
	b.mu.Unlock()

	response := make(chan core.RPCResponse, 1)

	go func() {
		defer selected.wait.Done()
		defer cancel()
		defer stopCancellation()
		defer func() { b.mu.Lock(); delete(selected.active, id); <-selected.accepted; b.mu.Unlock() }()

		select {
		case selected.workers <- struct{}{}:
			defer func() { <-selected.workers }()
		case <-work.Done():
			response <- core.RPCResponse{ID: request.Envelope.ID, Source: selected.binding.Identity, Error: core.PublicRPCError(work.Err())}

			return
		}

		if err := work.Err(); err != nil {
			response <- core.RPCResponse{ID: request.Envelope.ID, Source: selected.binding.Identity, Error: core.PublicRPCError(err)}

			return
		}

		response <- selected.handler(work, request)
	}()

	select {
	case result := <-response:
		return result, nil
	case <-ctx.Done():
		return core.RPCResponse{}, ctx.Err()
	}
}
func (s *rpcServer) Close(ctx context.Context) error {
	s.once.Do(func() {
		s.broker.mu.Lock()
		s.closing = true
		key := rpcKey(s.binding.Identity.Namespace, s.binding.Identity.ServiceID, s.binding.Method)

		members := s.broker.rpc[key]
		for i, member := range members {
			if member == s {
				members = append(members[:i], members[i+1:]...)

				break
			}
		}

		if len(members) == 0 {
			delete(s.broker.rpc, key)
		} else {
			s.broker.rpc[key] = members
		}

		s.broker.mu.Unlock()
		go func() { s.wait.Wait(); close(s.done) }()
	})

	select {
	case <-s.done:
		return nil
	case <-ctx.Done():
		s.broker.mu.Lock()
		for _, cancel := range s.active {
			cancel()
		}
		s.broker.mu.Unlock()

		return ctx.Err()
	}
}

var _ core.RPCProvider = (*Broker)(nil)
