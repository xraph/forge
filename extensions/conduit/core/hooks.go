package core

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"
)

// Stage names observable outcomes without treating handler success as acknowledgement.
type Stage string

const (
	Starting            Stage = "starting"
	Ready               Stage = "ready"
	Draining            Stage = "draining"
	Stopped             Stage = "stopped"
	ProviderConnected   Stage = "provider.connected"
	SubscriptionStarted Stage = "subscription.started"
	SubscriptionFailed  Stage = "subscription.failed"
	Publishing          Stage = "publishing"
	Published           Stage = "published"
	PublishFailed       Stage = "publish.failed"
	Received            Stage = "received"
	Handling            Stage = "handling"
	Handled             Stage = "handled"
	HandleFailed        Stage = "handle.failed"
	Acknowledged        Stage = "acknowledged"
	SettlementFailed    Stage = "settlement.failed"
	RetryScheduled      Stage = "retry.scheduled"
	RetryExhausted      Stage = "retry.exhausted"
	DeadLettered        Stage = "deadletter.stored"
	Replayed            Stage = "deadletter.replayed"
	Duplicate           Stage = "duplicate"
)

// HookEvent is copied before observers receive it.
type HookEvent struct {
	Stage    Stage         `json:"stage"`
	Identity Identity      `json:"identity"`
	Provider string        `json:"provider,omitempty"`
	Message  *Envelope     `json:"message,omitempty"`
	Delivery *DeliveryInfo `json:"delivery,omitempty"`
	Receipt  *Receipt      `json:"receipt,omitempty"`
	Error    string        `json:"error,omitempty"`
	At       time.Time     `json:"at"`
}

// Hook identifies one optional set of callbacks.
type Hook interface{ Name() string }

// Observer receives outcomes and cannot change delivery guarantees.
type Observer interface {
	Hook
	Observe(ctx context.Context, event HookEvent)
}

// PublishHook can enrich or reject a draft before final validation and serialization.
type PublishHook interface {
	Hook
	BeforePublish(ctx context.Context, message *Envelope) error
}

// HandleHook validates a delivery before the handler executes.
type HandleHook interface {
	Hook
	BeforeHandle(ctx context.Context, message Envelope, info DeliveryInfo) error
}

// HookFuncs adapts callbacks without requiring unrelated lifecycle methods.
type HookFuncs struct {
	HookName  string
	OnEvent   func(context.Context, HookEvent)
	OnPublish func(context.Context, *Envelope) error
	OnHandle  func(context.Context, Envelope, DeliveryInfo) error
}

// Name returns the registration identity.
func (h HookFuncs) Name() string { return h.HookName }

// Observe receives an immutable event.
func (h HookFuncs) Observe(ctx context.Context, event HookEvent) {
	if h.OnEvent != nil {
		h.OnEvent(ctx, event)
	}
}

// BeforePublish runs the configured control callback.
func (h HookFuncs) BeforePublish(ctx context.Context, msg *Envelope) error {
	if h.OnPublish != nil {
		return h.OnPublish(ctx, msg)
	}

	return nil
}

// BeforeHandle runs the configured control callback.
func (h HookFuncs) BeforeHandle(ctx context.Context, msg Envelope, info DeliveryInfo) error {
	if h.OnHandle != nil {
		return h.OnHandle(ctx, msg, info)
	}

	return nil
}

func protect(fn func() error) (err error) {
	defer func() {
		if value := recover(); value != nil {
			err = fmt.Errorf("conduit: callback panic: %v", value)
		}
	}()

	return fn()
}

type observerPool struct {
	queue   chan HookEvent
	dropped atomic.Uint64
}

func (r *Runtime) emit(ctx context.Context, event HookEvent) {
	event.Identity = r.config.Identity

	event.At = time.Now().UTC()
	if event.Message != nil {
		msg := event.Message.Clone()
		event.Message = &msg
	}

	if event.Delivery != nil {
		info := *event.Delivery
		event.Delivery = &info
	}

	if event.Receipt != nil {
		receipt := *event.Receipt
		event.Receipt = &receipt
	}
	// Retain only metadata for diagnostics. Payloads and headers stay in the broker.
	metadata := event
	if event.Message != nil {
		msg := *event.Message
		msg.Data, msg.Headers = nil, nil
		metadata.Message = &msg
	}

	metadata.Error = ""

	r.historyMu.Lock()

	r.history = append(r.history, metadata)
	if len(r.history) > 100 {
		r.history = append([]HookEvent(nil), r.history[len(r.history)-100:]...)
	}
	r.historyMu.Unlock()

	if r.observers == nil {
		return
	}

	select {
	case r.observers.queue <- event:
	default:
		r.observers.dropped.Add(1)
	}
}

func (r *Runtime) observe(ctx context.Context) {
	defer r.observerWorkers.Done()

	for {
		select {
		case event := <-r.observers.queue:
			r.dispatchObservers(ctx, event)
		case <-ctx.Done():
			for {
				select {
				case event := <-r.observers.queue:
					r.dispatchObservers(ctx, event)
				default:
					return
				}
			}
		}
	}
}

func (r *Runtime) dispatchObservers(ctx context.Context, event HookEvent) {
	for _, hook := range r.hooks {
		if observer, ok := hook.(Observer); ok {
			copyEvent := event
			if event.Message != nil {
				msg := event.Message.Clone()
				copyEvent.Message = &msg
			}

			if event.Delivery != nil {
				copyInfo := *event.Delivery
				copyEvent.Delivery = &copyInfo
			}

			if event.Receipt != nil {
				copyReceipt := *event.Receipt
				copyEvent.Receipt = &copyReceipt
			}

			_ = protect(func() error {
				observer.Observe(ctx, copyEvent)

				return nil
			})
		}
	}
}

// RecentEvents returns bounded diagnostics without message data, headers or error text.
func (r *Runtime) RecentEvents() []HookEvent {
	r.historyMu.Lock()
	defer r.historyMu.Unlock()

	result := make([]HookEvent, len(r.history))
	for i, event := range r.history {
		result[i] = event
		if event.Message != nil {
			message := event.Message.Clone()
			result[i].Message = &message
		}

		if event.Delivery != nil {
			info := *event.Delivery
			result[i].Delivery = &info
		}

		if event.Receipt != nil {
			receipt := *event.Receipt
			result[i].Receipt = &receipt
		}
	}

	return result
}
