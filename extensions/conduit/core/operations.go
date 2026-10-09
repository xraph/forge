package core

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"time"
)

// Latency summarizes processing attempts on this instance, in nanoseconds.
type Latency struct {
	Count   uint64        `json:"count"`
	Total   time.Duration `json:"total"`
	Max     time.Duration `json:"max"`
	Average time.Duration `json:"average"`
}

// ConsumerInfo separates broker backlog from instance-local processing latency.
type ConsumerInfo struct {
	Subscription SubscriptionConfig `json:"subscription"`
	Provider     string             `json:"provider"`
	ConsumerID   string             `json:"consumerID"`
	Pending      uint64             `json:"pending"`
	AckPending   uint64             `json:"ackPending"`
	Redelivered  uint64             `json:"redelivered"`
	Paused       bool               `json:"paused"`
	Processing   Latency            `json:"processing"`
	Delivery     Latency            `json:"delivery"`
}

// BackfillInput selects at most 100 retained stream sequences and a stable operation ID.
type BackfillInput struct {
	ID           string `json:"id"`
	Subscription string `json:"subscription"`
	Start        uint64 `json:"start"`
	End          uint64 `json:"end"`
}

func (in BackfillInput) Validate() error {
	if ValidateMessageID(in.ID) != nil || in.Subscription == "" || in.Start == 0 || in.End < in.Start || in.End-in.Start >= 100 || in.End == ^uint64(0) {
		return ErrConflict
	}

	return nil
}

// Backfill records resumable progress without exposing event data.
type Backfill struct {
	MessageType string        `json:"messageType"`
	Input       BackfillInput `json:"input"`
	ConsumerID  string        `json:"consumerID"`
	Stream      string        `json:"stream"`
	Provider    string        `json:"provider"`
	State       string        `json:"state"`
	Next        uint64        `json:"next"`
	Published   uint64        `json:"published"`
	Skipped     uint64        `json:"skipped"`
	UpdatedAt   time.Time     `json:"updatedAt"`
	Error       string        `json:"error,omitempty"`
	Persisted   bool          `json:"persisted"`
}

// Operations is optional broker-backed consumer management and controlled recovery.
type Operations interface {
	InspectConsumer(ctx context.Context, binding Binding) (ConsumerInfo, error)
	PauseConsumer(ctx context.Context, binding Binding, paused bool) error
	RunBackfill(ctx context.Context, binding Binding, input BackfillInput) (Backfill, error)
	ListBackfills(ctx context.Context, identity Identity, cursor string, limit int) ([]Backfill, string, error)
}

// ExecuteBackfill processes a claimed range, checkpointing after every confirmed publication.
// The provider must hold an exclusive lease longer than the bounded execution deadline.
func ExecuteBackfill(ctx context.Context, binding Binding, job Backfill, read func(context.Context, uint64) (Envelope, error), publish func(context.Context, Envelope, string) error, save func(context.Context, Backfill) error) (Backfill, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	job.State, job.Error = "running", ""
	for job.Next <= job.Input.End {
		if err := ctx.Err(); err != nil {
			return failBackfill(ctx, job, save, err)
		}

		msg, err := read(ctx, job.Next)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return failBackfill(ctx, job, save, err)
		}

		if errors.Is(err, ErrNotFound) || msg.Type != binding.Subscription.MessageType || msg.TargetConsumer != "" {
			job.Skipped++
		} else {
			msg.TargetConsumer = binding.ConsumerID()
			if err := publish(ctx, msg, "backfill-"+binding.ConsumerID()+"-"+job.Input.ID+"-"+strconv.FormatUint(job.Next, 10)); err != nil {
				return failBackfill(ctx, job, save, err)
			}

			job.Published++
		}

		job.Next++

		job.UpdatedAt = time.Now().UTC()
		if err := save(ctx, job); err != nil {
			return job, ErrOutcomeUnknown
		}
	}

	job.State = "complete"
	job.UpdatedAt = time.Now().UTC()

	return job, save(ctx, job)
}
func failBackfill(ctx context.Context, job Backfill, save func(context.Context, Backfill) error, cause error) (Backfill, error) {
	job.State, job.Error = "failed", "Backfill interrupted; resume with the same operation ID"
	job.UpdatedAt = time.Now().UTC()

	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()

	return job, errors.Join(cause, save(cleanup, job))
}
func (r *Runtime) operationBinding(id string) (Binding, Operations, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if !r.running {
		return Binding{}, nil, ErrNotRunning
	}

	reg, ok := r.registrations[id]
	if !ok {
		return Binding{}, nil, ErrNotFound
	}

	stream := r.config.Streams[reg.config.Stream]

	provider, ok := r.providers[stream.Provider].(Operations)
	if !ok {
		return Binding{}, nil, ErrUnsupported
	}

	return Binding{Identity: r.config.Identity, Subscription: reg.config, Stream: stream}, provider, nil
}

// Consumers returns broker progress for this service's registered subscriptions.
func (r *Runtime) Consumers(ctx context.Context) ([]ConsumerInfo, error) {
	r.mu.RLock()

	ids := make([]string, 0, len(r.registrations))
	for id := range r.registrations {
		ids = append(ids, id)
	}

	r.mu.RUnlock()
	slices.Sort(ids)

	result := make([]ConsumerInfo, 0, len(ids))
	for _, id := range ids {
		binding, p, err := r.operationBinding(id)
		if err != nil {
			return nil, err
		}

		row, err := p.InspectConsumer(ctx, binding)
		if err != nil {
			return nil, err
		}

		row.Provider = binding.Stream.Provider

		r.latencyMu.Lock()
		row.Processing = r.processingLatency[id]
		row.Delivery = r.deliveryLatency[id]
		r.latencyMu.Unlock()

		result = append(result, row)
	}

	return result, nil
}

// PauseSubscription changes the actual logical cursor across its attached replicas.
func (r *Runtime) PauseSubscription(ctx context.Context, id string, paused bool) error {
	binding, p, err := r.operationBinding(id)
	if err != nil {
		return err
	}

	return p.PauseConsumer(ctx, binding, paused)
}

// Backfill republishes only to the selected original logical consumer.
func (r *Runtime) Backfill(ctx context.Context, in BackfillInput) (Backfill, error) {
	if err := in.Validate(); err != nil {
		return Backfill{}, err
	}

	binding, p, err := r.operationBinding(in.Subscription)
	if err != nil {
		return Backfill{}, err
	}

	return p.RunBackfill(ctx, binding, in)
}

// Backfills returns a bounded service-scoped operation history for one provider.
func (r *Runtime) Backfills(ctx context.Context, provider, cursor string, limit int) ([]Backfill, string, error) {
	r.mu.RLock()
	p, ok := r.providers[provider].(Operations)
	identity := r.config.Identity
	r.mu.RUnlock()

	if !ok {
		return nil, "", ErrUnsupported
	}

	if limit < 1 || limit > 100 {
		return nil, "", ErrConflict
	}

	return p.ListBackfills(ctx, identity, cursor, limit)
}
func (r *Runtime) recordLatency(id string, processing, delivery time.Duration) {
	r.latencyMu.Lock()
	defer r.latencyMu.Unlock()

	r.processingLatency[id] = addLatency(r.processingLatency[id], processing)
	if delivery >= 0 {
		r.deliveryLatency[id] = addLatency(r.deliveryLatency[id], delivery)
	}
}
func addLatency(value Latency, duration time.Duration) Latency {
	value.Count++
	value.Total += duration
	value.Max = max(value.Max, duration)
	value.Average = value.Total / time.Duration(min(value.Count, uint64(1<<63-1))) //nolint:gosec // The explicit clamp fits the signed duration divisor.

	return value
}
