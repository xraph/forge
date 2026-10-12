package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/xraph/forge/extensions/conduit/core"
)

// rpcChannel and replyChannel stay under the 63-byte identifier limit. A request's NOTIFY
// carries no payload and a reply's carries only the request ID, so payloads never meet the
// 8KB cap, and each reply wakes only the call it answers.
func rpcChannel(namespace, service, method string) string {
	return "fcr_" + hash(namespace, service, method)
}

func (p *Provider) replyChannel() string { return "fcp_" + hash(p.owner) }

func validReplyChannel(channel string) bool {
	return len(channel) == 36 && strings.HasPrefix(channel, "fcp_")
}

// RequestRPC stores the request, wakes the method's servers and waits for the reply. A
// caller that gives up withdraws a request nobody has claimed yet.
func (p *Provider) RequestRPC(ctx context.Context, identity core.Identity, request core.RPCRequest) (core.RPCResponse, error) {
	pool, err := p.db()
	if err != nil {
		return core.RPCResponse{}, err
	}

	data, err := json.Marshal(request)
	if err != nil {
		return core.RPCResponse{}, err
	}

	id := hash(identity.Namespace, identity.ServiceID, identity.InstanceID, request.Envelope.ID)
	channel := rpcChannel(identity.Namespace, request.Service, request.Method)

	signal, unwatch := p.watchID(p.replyChannel(), id)
	defer unwatch()

	tx, err := pool.Begin(ctx)
	if err != nil {
		return core.RPCResponse{}, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	tag, err := tx.Exec(ctx, fmt.Sprintf(`INSERT INTO %s (id, channel, request, reply_channel, deadline) VALUES ($1, $2, $3, $4, $5) ON CONFLICT (id) DO NOTHING`, p.t.rpcRequests),
		id, channel, string(data), p.replyChannel(), request.Deadline)
	if err != nil {
		return core.RPCResponse{}, err
	}

	if tag.RowsAffected() == 0 {
		return core.RPCResponse{}, &core.RPCError{Code: core.RPCConflict, Message: "Request is already in progress"}
	}

	// No request with this ID is in flight, and a server commits its reply in the same
	// transaction that removes the request. Any reply still stored answers an earlier
	// attempt that reused the ID, so this call must not take it.
	if _, err := tx.Exec(ctx, fmt.Sprintf(`DELETE FROM %s WHERE id = $1`, p.t.rpcResponses), id); err != nil {
		return core.RPCResponse{}, err
	}

	if _, err := tx.Exec(ctx, `SELECT pg_notify($1, '')`, channel); err != nil {
		return core.RPCResponse{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			return core.RPCResponse{}, err
		}

		return core.RPCResponse{}, core.ErrOutcomeUnknown
	}

	answered := false

	defer func() {
		if answered {
			return
		}

		withdraw, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer cancel()

		_, _ = pool.Exec(withdraw, fmt.Sprintf(`DELETE FROM %s WHERE id = $1 AND claimed_by IS NULL`, p.t.rpcRequests), id)
	}()

	for {
		var raw []byte

		err := pool.QueryRow(ctx, fmt.Sprintf(`DELETE FROM %s WHERE id = $1 RETURNING response::text`, p.t.rpcResponses), id).Scan(&raw)
		if err == nil {
			answered = true

			var response core.RPCResponse
			if err := json.Unmarshal(raw, &response); err != nil {
				return core.RPCResponse{}, &core.RPCError{Code: core.RPCInternal, Message: "Invalid broker reply"}
			}

			return response, nil
		}

		if !errors.Is(err, pgx.ErrNoRows) {
			if ctx.Err() != nil {
				return core.RPCResponse{}, ctx.Err()
			}

			return core.RPCResponse{}, err
		}

		timer := time.NewTimer(p.options.PollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()

			return core.RPCResponse{}, ctx.Err()
		case <-signal:
			timer.Stop()
		case <-timer.C:
		}
	}
}

type rpcWork struct {
	id      string
	request core.RPCRequest
	reply   string
	ctx     context.Context //nolint:containedctx // Accepted work keeps its own deadline and cancel function.
	cancel  context.CancelFunc
}

type rpcServer struct {
	provider *Provider
	binding  core.RPCBinding
	handler  func(context.Context, core.RPCRequest) core.RPCResponse
	channel  string
	claimant string
	base     context.Context //nolint:containedctx // The owning runtime cancels the server's lifetime after draining.
	mu       sync.Mutex
	active   map[string]*rpcWork
	closing  bool
	jobs     chan *rpcWork
	stop     context.CancelFunc
	reading  chan struct{}
	workers  sync.WaitGroup
	once     sync.Once
	done     chan struct{}
}

// ServeRPC claims the method's requests with SKIP LOCKED, so each request runs on one
// replica. A replica at MaxInFlight claims and refuses, the way a NATS queue member does.
func (p *Provider) ServeRPC(ctx context.Context, binding core.RPCBinding, handler func(context.Context, core.RPCRequest) core.RPCResponse) (core.RPCServer, error) {
	if _, err := p.db(); err != nil {
		return nil, err
	}

	readCtx, stop := context.WithCancel(ctx) //nolint:gosec // The server stores stop and calls it from Close.
	s := &rpcServer{
		provider: p, binding: binding, handler: handler, base: ctx,
		channel:  rpcChannel(binding.Identity.Namespace, binding.Identity.ServiceID, binding.Method),
		claimant: hash(p.owner, binding.Identity.InstanceID, core.NewID()),
		active:   map[string]*rpcWork{}, jobs: make(chan *rpcWork, binding.Config.MaxInFlight), stop: stop,
		reading: make(chan struct{}), done: make(chan struct{}),
	}

	signal, unwatch := p.watch(s.channel)

	for range binding.Config.Concurrency {
		s.workers.Add(1)

		go s.run()
	}

	go func() {
		defer unwatch()

		s.read(readCtx, signal)
	}()

	go func() { <-s.reading; s.workers.Wait(); close(s.done) }()

	return s, nil
}

func (s *rpcServer) read(ctx context.Context, signal <-chan struct{}) {
	defer close(s.reading)
	defer func() { s.mu.Lock(); s.closing = true; close(s.jobs); s.mu.Unlock() }()

	for ctx.Err() == nil {
		for s.claim(ctx) {
		}

		timer := time.NewTimer(s.provider.options.PollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()

			return
		case <-signal:
			timer.Stop()
		case <-timer.C:
		}
	}
}

// claim takes one pending request and reports whether there was one.
func (s *rpcServer) claim(ctx context.Context) bool {
	pool, err := s.provider.db()
	if err != nil {
		return false
	}

	var (
		id, reply string
		raw       []byte
		deadline  time.Time
	)

	err = pool.QueryRow(ctx, fmt.Sprintf(`UPDATE %[1]s SET claimed_by = $2
WHERE id = (SELECT id FROM %[1]s WHERE channel = $1 AND claimed_by IS NULL ORDER BY created_at FOR UPDATE SKIP LOCKED LIMIT 1)
RETURNING id, request::text, reply_channel, deadline`, s.provider.t.rpcRequests), s.channel, s.claimant).Scan(&id, &raw, &reply, &deadline)
	if err != nil {
		return false
	}

	s.accept(id, raw, reply, deadline)

	return true
}

func (s *rpcServer) accept(id string, raw []byte, reply string, deadline time.Time) {
	var request core.RPCRequest
	if len(raw) > s.binding.MaxPayloadBytes+64*1024 || json.Unmarshal(raw, &request) != nil || !validReplyChannel(reply) {
		s.drop(id)

		return
	}

	if request.Service != s.binding.Identity.ServiceID || request.Method != s.binding.Method || request.Envelope.Source.Namespace != s.binding.Identity.Namespace || request.Envelope.Source.Validate() != nil || core.ValidateMessageID(request.Envelope.ID) != nil {
		s.refuse(id, reply, deadline, request, core.RPCBadRequest, "Invalid RPC request")

		return
	}

	if !time.Now().Before(deadline) {
		s.drop(id) // the caller has already given up

		return
	}

	limit := deadline
	if bound := time.Now().Add(s.binding.Config.Timeout); bound.Before(limit) {
		limit = bound
	}

	workCtx, cancel := context.WithDeadline(s.base, limit)
	work := &rpcWork{id: id, request: request, reply: reply, ctx: workCtx, cancel: cancel}

	s.mu.Lock()
	switch {
	case s.closing:
		s.mu.Unlock()
		cancel()
		s.refuse(id, reply, deadline, request, core.RPCUnavailable, "Service is draining")

		return
	case len(s.active) >= s.binding.Config.MaxInFlight:
		s.mu.Unlock()
		cancel()
		s.refuse(id, reply, deadline, request, core.RPCUnavailable, "Service concurrency limit reached")

		return
	}

	s.active[id] = work
	s.jobs <- work
	s.mu.Unlock()
}

func (s *rpcServer) drop(id string) {
	pool, err := s.provider.db()
	if err != nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.WithoutCancel(s.base), 5*time.Second)
	defer cancel()

	_, _ = pool.Exec(ctx, fmt.Sprintf(`DELETE FROM %s WHERE id = $1`, s.provider.t.rpcRequests), id)
}

func (s *rpcServer) refuse(id, reply string, deadline time.Time, request core.RPCRequest, code core.RPCCode, message string) {
	s.respond(id, reply, deadline, core.RPCResponse{ID: request.Envelope.ID, Source: s.binding.Identity, Error: &core.RPCError{Code: code, Message: message}})
}

// respond removes the request, stores the reply and wakes the caller in one transaction.
// It answers only a request this server still holds: one swept after its deadline and
// stored again by a retry belongs to whoever claims the new copy.
func (s *rpcServer) respond(id, reply string, deadline time.Time, response core.RPCResponse) {
	data, err := json.Marshal(response)
	if err != nil {
		return
	}

	pool, err := s.provider.db()
	if err != nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.WithoutCancel(s.base), 5*time.Second)
	defer cancel()

	tx, err := pool.Begin(ctx)
	if err != nil {
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(ctx, fmt.Sprintf(`DELETE FROM %s WHERE id = $1 AND claimed_by = $2`, s.provider.t.rpcRequests), id, s.claimant)
	if err != nil || tag.RowsAffected() == 0 {
		return
	}

	if _, err := tx.Exec(ctx, fmt.Sprintf(`INSERT INTO %s (id, response, expires_at) VALUES ($1, $2, $3)
ON CONFLICT (id) DO UPDATE SET response = EXCLUDED.response, expires_at = EXCLUDED.expires_at`, s.provider.t.rpcResponses), id, string(data), deadline.Add(time.Minute)); err != nil {
		return
	}

	if _, err := tx.Exec(ctx, `SELECT pg_notify($1, $2)`, reply, id); err != nil {
		return
	}

	_ = tx.Commit(ctx)
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

		s.respond(work.id, work.reply, work.request.Deadline, response)
		work.cancel()

		s.mu.Lock()
		delete(s.active, work.id)
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
