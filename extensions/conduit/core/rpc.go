package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"
)

// RPCConfig bounds each instance's transient request/reply work.
type RPCConfig struct {
	Provider    string        `json:"provider"    yaml:"provider"`
	Timeout     time.Duration `json:"timeout"     yaml:"timeout"`
	Concurrency int           `json:"concurrency" yaml:"concurrency"`
	MaxInFlight int           `json:"maxInFlight" yaml:"max_in_flight"`
}

// RPCCode is a public error category independent of a broker.
type RPCCode string

const (
	RPCBadRequest       RPCCode = "BAD_REQUEST"
	RPCNotFound         RPCCode = "NOT_FOUND"
	RPCPermissionDenied RPCCode = "PERMISSION_DENIED"
	RPCConflict         RPCCode = "CONFLICT"
	RPCUnavailable      RPCCode = "UNAVAILABLE"
	RPCDeadlineExceeded RPCCode = "DEADLINE_EXCEEDED"
	RPCCanceled         RPCCode = "CANCELED"
	RPCInternal         RPCCode = "INTERNAL"
)

// RPCError exposes only an intentional public error message.
type RPCError struct {
	Code    RPCCode `json:"code"`
	Message string  `json:"message"`
}

func (e *RPCError) Error() string { return "conduit RPC " + string(e.Code) + ": " + e.Message }
func (e *RPCError) Is(target error) bool {
	return e.Code == RPCDeadlineExceeded && target == context.DeadlineExceeded || e.Code == RPCCanceled && target == context.Canceled || e.Code == RPCNotFound && target == ErrNotFound || e.Code == RPCConflict && target == ErrConflict
}

// PublicRPCError strips arbitrary handler causes from responses.
func PublicRPCError(err error) *RPCError {
	if err == nil {
		return nil
	}

	var public *RPCError
	if errors.As(err, &public) {
		switch public.Code {
		case RPCBadRequest, RPCNotFound, RPCPermissionDenied, RPCConflict, RPCUnavailable, RPCDeadlineExceeded, RPCCanceled, RPCInternal:
			copyError := *public

			return &copyError
		}
	}

	if errors.Is(err, context.DeadlineExceeded) {
		return &RPCError{Code: RPCDeadlineExceeded, Message: "Request deadline exceeded"}
	}

	if errors.Is(err, context.Canceled) {
		return &RPCError{Code: RPCCanceled, Message: "Request canceled"}
	}

	return &RPCError{Code: RPCInternal, Message: "Request failed"}
}

// RPCRequest preserves caller identity, tracing metadata and the remote deadline.
type RPCRequest struct {
	Service  string    `json:"service"`
	Method   string    `json:"method"`
	Envelope Envelope  `json:"envelope"`
	Deadline time.Time `json:"deadline"`
}

// RPCResponse correlates one reply and identifies the responding replica.
type RPCResponse struct {
	ID     string          `json:"id"`
	Source Identity        `json:"source"`
	Data   json.RawMessage `json:"data,omitempty"`
	Error  *RPCError       `json:"error,omitempty"`
}

type RPCBinding struct {
	Identity        Identity
	Method          string
	Config          RPCConfig
	MaxPayloadBytes int
}

// RPCServer stops intake and drains accepted work until the shutdown deadline.
type RPCServer interface {
	Close(ctx context.Context) error
}

// RPCProvider supplies optional transient RPC without weakening durable event guarantees.
type RPCProvider interface {
	RequestRPC(ctx context.Context, identity Identity, request RPCRequest) (RPCResponse, error)
	ServeRPC(ctx context.Context, binding RPCBinding, handler func(context.Context, RPCRequest) RPCResponse) (RPCServer, error)
}

// RPCHandler returns serialized typed data or an intentional public error.
type RPCHandler func(context.Context, Envelope) (json.RawMessage, error)

func defaultsRPC(cfg RPCConfig) (RPCConfig, error) {
	if cfg.Timeout == 0 {
		cfg.Timeout = 5 * time.Second
	}

	if cfg.Concurrency == 0 {
		cfg.Concurrency = 8
	}

	if cfg.MaxInFlight == 0 {
		cfg.MaxInFlight = max(64, cfg.Concurrency)
	}

	if cfg.Timeout <= 0 || cfg.Timeout > 24*time.Hour || cfg.Concurrency < 1 || cfg.Concurrency > 1024 || cfg.MaxInFlight < cfg.Concurrency || cfg.MaxInFlight > 65536 {
		return cfg, errors.New("conduit: invalid RPC limits")
	}

	return cfg, nil
}

func (r *Runtime) rpcProvider() (string, RPCProvider, error) {
	if name := r.config.RPC.Provider; name != "" {
		p, ok := r.providers[name].(RPCProvider)
		if !ok {
			return name, nil, ErrUnsupported
		}

		return name, p, nil
	}

	var selected RPCProvider

	name := ""

	for key, p := range r.providers {
		if provider, ok := p.(RPCProvider); ok {
			if selected != nil {
				return "", nil, fmt.Errorf("%w: select an RPC provider", ErrConflict)
			}

			name, selected = key, provider
		}
	}

	if selected == nil {
		return "", nil, ErrUnsupported
	}

	return name, selected, nil
}

// BindRPC registers one logical procedure before startup; replicas share its queue group.
func (r *Runtime) BindRPC(method string, handler RPCHandler) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.running || r.closed || r.stopping {
		return ErrConflict
	}

	if handler == nil {
		return errors.New("conduit: RPC handler is required")
	}

	if err := ValidateTopic(method, false); err != nil {
		return err
	}

	if r.rpcHandlers[method] != nil || len(r.rpcHandlers) >= 1024 {
		return ErrConflict
	}

	r.rpcHandlers[method] = handler

	return nil
}

func (r *Runtime) startRPC(ctx context.Context) error {
	if len(r.rpcHandlers) == 0 {
		return nil
	}

	name, provider, err := r.rpcProvider()
	if err != nil {
		return err
	}

	methods := make([]string, 0, len(r.rpcHandlers))
	for method := range r.rpcHandlers {
		methods = append(methods, method)
	}

	slices.Sort(methods)

	for _, method := range methods {
		handler := r.rpcHandlers[method]

		server, err := provider.ServeRPC(ctx, RPCBinding{Identity: r.config.Identity, Method: method, Config: r.config.RPC, MaxPayloadBytes: r.config.MaxPayloadBytes}, func(ctx context.Context, request RPCRequest) RPCResponse {
			return r.handleRPC(ctx, name, method, request, handler)
		})
		if err != nil {
			return err
		}

		r.rpcServers = append(r.rpcServers, server)
	}

	return nil
}

func (r *Runtime) handleRPC(ctx context.Context, provider, method string, request RPCRequest, handler RPCHandler) RPCResponse {
	r.mu.RLock()
	running := r.canSend(ctx)
	identity := r.config.Identity
	hooks := slices.Clone(r.hooks)
	limit := r.config.MaxPayloadBytes
	r.mu.RUnlock()

	response := RPCResponse{ID: request.Envelope.ID, Source: identity}
	if !running {
		response.Error = &RPCError{Code: RPCUnavailable, Message: "Service is draining"}

		return response
	}

	msg := request.Envelope.Clone()
	if request.Service != identity.ServiceID || request.Method != method || msg.Type != method || msg.Source.Namespace != identity.Namespace || msg.Source.Validate() != nil || ValidateMessageID(msg.ID) != nil || len(msg.Data) > limit || msg.ContentType != "application/json" || !json.Valid(msg.Data) {
		response.Error = &RPCError{Code: RPCBadRequest, Message: "Invalid RPC request"}

		return response
	}

	ctx = context.WithValue(ctx, handlerRuntimeKey{}, r)
	r.emit(ctx, HookEvent{Stage: RPCReceived, Provider: provider, Message: &msg})

	started := time.Now()

	err := protect(func() error {
		if err := ctx.Err(); err != nil {
			return err
		}

		info := DeliveryInfo{Destination: identity, SubscriptionID: "rpc:" + method, Mode: Competing, Attempt: 1}

		for _, hook := range hooks {
			if control, ok := hook.(HandleHook); ok {
				if err := control.BeforeHandle(ctx, msg.Clone(), info); err != nil {
					return err
				}
			}
		}

		r.emit(ctx, HookEvent{Stage: RPCHandling, Provider: provider, Message: &msg})

		data, err := handler(ctx, msg.Clone())
		if err != nil {
			return err
		}

		if len(data) > limit || !json.Valid(data) {
			return errors.New("conduit: invalid RPC response payload")
		}

		response.Data = slices.Clone(data)

		return ctx.Err()
	})
	if err != nil {
		response.Data = nil
		response.Error = PublicRPCError(err)

		r.rpcFailed.Add(1)
		r.emit(ctx, HookEvent{Stage: RPCFailed, Provider: provider, Message: &msg, Error: string(response.Error.Code), Duration: time.Since(started)})
	} else {
		r.rpcHandled.Add(1)
		r.emit(ctx, HookEvent{Stage: RPCHandled, Provider: provider, Message: &msg, Duration: time.Since(started)})
	}

	return response
}

// CallRPC sends once by service name. A canceled call may already have caused an effect.
func (r *Runtime) CallRPC(ctx context.Context, service, method string, draft Envelope, validate func(Envelope) error) (RPCResponse, error) {
	r.mu.RLock()

	if !r.canSend(ctx) {
		r.mu.RUnlock()

		return RPCResponse{}, ErrNotRunning
	}

	name, provider, err := r.rpcProvider()
	identity := r.config.Identity
	cfg := r.config.RPC
	limit := r.config.MaxPayloadBytes
	hooks := slices.Clone(r.hooks)
	r.mu.RUnlock()

	if err != nil {
		return RPCResponse{}, err
	}

	if service == "" || len(service) > 128 {
		return RPCResponse{}, &RPCError{Code: RPCBadRequest, Message: "Service name is required"}
	}

	if err := ValidateTopic(method, false); err != nil {
		return RPCResponse{}, err
	}

	ctx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()

	if err := ctx.Err(); err != nil {
		return RPCResponse{}, err
	}

	msg := draft.Clone()
	if msg.ID == "" {
		msg.ID = NewID()
	}

	msg.Source = identity
	msg.Type = method
	msg.ContentType = "application/json"
	msg.CreatedAt = time.Now().UTC()
	original := msg.Clone()

	if err := protect(func() error {
		for _, hook := range hooks {
			if control, ok := hook.(PublishHook); ok {
				if err := control.BeforePublish(ctx, &msg); err != nil {
					return err
				}
			}
		}

		return nil
	}); err != nil {
		return RPCResponse{}, err
	}

	if msg.ID != original.ID || msg.Source != original.Source || msg.Type != original.Type || msg.TargetConsumer != "" || msg.ContentType != "application/json" || ValidateMessageID(msg.ID) != nil || len(msg.Data) > limit || !json.Valid(msg.Data) {
		return RPCResponse{}, &RPCError{Code: RPCBadRequest, Message: "Invalid RPC request"}
	}

	if validate != nil {
		if err := validate(msg); err != nil {
			return RPCResponse{}, err
		}
	}

	deadline, _ := ctx.Deadline()

	r.rpcCalls.Add(1)
	r.emit(ctx, HookEvent{Stage: RPCCalling, Provider: name, Message: &msg})

	response, err := provider.RequestRPC(ctx, identity, RPCRequest{Service: service, Method: method, Envelope: msg, Deadline: deadline})
	if err == nil && (response.ID != msg.ID || response.Source.Namespace != identity.Namespace || response.Source.ServiceID != service || response.Source.Validate() != nil || len(response.Data) > limit) {
		err = &RPCError{Code: RPCInternal, Message: "Invalid RPC response"}
	}

	if err == nil && response.Error != nil {
		err = response.Error
	}

	if err != nil {
		r.rpcFailed.Add(1)

		if errors.Is(err, context.DeadlineExceeded) {
			r.rpcTimedOut.Add(1)
		}

		r.emit(ctx, HookEvent{Stage: RPCFailed, Provider: name, Message: &msg, Error: string(PublicRPCError(err).Code)})

		return RPCResponse{}, err
	}

	r.emit(ctx, HookEvent{Stage: RPCReturned, Provider: name, Message: &msg})

	return response, nil
}

type handlerRuntimeKey struct{}

func (r *Runtime) canSend(ctx context.Context) bool {
	return r.running || r.stopping && ctx.Value(handlerRuntimeKey{}) == r
}
