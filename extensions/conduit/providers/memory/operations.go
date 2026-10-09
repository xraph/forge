package memory

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/xraph/forge/extensions/conduit/core"
)

func (b *Broker) InspectConsumer(ctx context.Context, binding core.Binding) (core.ConsumerInfo, error) {
	if err := ctx.Err(); err != nil {
		return core.ConsumerInfo{}, err
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	st := b.streams[streamKey(binding.Identity.Namespace, binding.Stream.Name)]
	if st == nil {
		return core.ConsumerInfo{}, core.ErrNotFound
	}

	c := st.consumers[binding.ConsumerID()]
	if c == nil {
		return core.ConsumerInfo{}, core.ErrNotFound
	}

	st.prune(time.Now())

	row := core.ConsumerInfo{Subscription: binding.Subscription, ConsumerID: binding.ConsumerID(), AckPending: uint64(len(c.pending)), Paused: c.paused}
	for _, record := range st.records {
		if record.sequence >= c.cursor && record.message.Type == binding.Subscription.MessageType && (record.message.TargetConsumer == "" || record.message.TargetConsumer == binding.ConsumerID()) {
			row.Pending++
		}
	}

	for _, pending := range c.pending {
		if pending.attempt > 1 {
			row.Redelivered++
		}
	}

	return row, nil
}
func (b *Broker) PauseConsumer(ctx context.Context, binding core.Binding, paused bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	st := b.streams[streamKey(binding.Identity.Namespace, binding.Stream.Name)]
	if st == nil {
		return core.ErrNotFound
	}

	c := st.consumers[binding.ConsumerID()]
	if c == nil {
		return core.ErrNotFound
	}

	c.paused = paused

	return nil
}
func operationKey(identity core.Identity, id string) string {
	return identity.Namespace + "\x00" + identity.ServiceID + "\x00" + id
}
func (b *Broker) RunBackfill(ctx context.Context, binding core.Binding, in core.BackfillInput) (core.Backfill, error) {
	if err := in.Validate(); err != nil {
		return core.Backfill{}, err
	}

	key := operationKey(binding.Identity, in.ID)

	b.mu.Lock()
	if b.backfills == nil {
		b.backfills = map[string]core.Backfill{}
		b.backfillActive = map[string]bool{}
	}

	job, exists := b.backfills[key]
	if exists && (job.Input != in || job.ConsumerID != binding.ConsumerID() || job.Stream != binding.Stream.Name || job.MessageType != binding.Subscription.MessageType) {
		b.mu.Unlock()

		return job, core.ErrConflict
	}

	if job.State == "complete" {
		b.mu.Unlock()

		return job, nil
	}

	if b.backfillActive[key] {
		b.mu.Unlock()

		return job, core.ErrConflict
	}

	if !exists {
		job = core.Backfill{MessageType: binding.Subscription.MessageType, Input: in, ConsumerID: binding.ConsumerID(), Stream: binding.Stream.Name, Provider: binding.Stream.Provider, Next: in.Start, State: "running", UpdatedAt: time.Now().UTC()}
		b.backfills[key] = job
	}

	b.backfillActive[key] = true

	b.mu.Unlock()
	defer func() { b.mu.Lock(); delete(b.backfillActive, key); b.mu.Unlock() }()

	return core.ExecuteBackfill(ctx, binding, job, func(ctx context.Context, sequence uint64) (core.Envelope, error) {
		if err := ctx.Err(); err != nil {
			return core.Envelope{}, err
		}

		b.mu.Lock()
		defer b.mu.Unlock()

		st := b.streams[streamKey(binding.Identity.Namespace, binding.Stream.Name)]
		if st == nil {
			return core.Envelope{}, core.ErrNotFound
		}

		st.prune(time.Now())

		for _, row := range st.records {
			if row.sequence == sequence {
				return row.message.Clone(), nil
			}
		}

		return core.Envelope{}, core.ErrNotFound
	}, func(ctx context.Context, msg core.Envelope, _ string) error {
		_, err := b.Publish(ctx, binding.Identity.Namespace, binding.Stream, msg)

		return err
	}, func(_ context.Context, job core.Backfill) error {
		b.mu.Lock()
		defer b.mu.Unlock()

		b.backfills[key] = job

		return nil
	})
}
func (b *Broker) ListBackfills(ctx context.Context, identity core.Identity, cursor string, limit int) ([]core.Backfill, string, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}

	if limit < 1 || limit > 100 {
		return nil, "", core.ErrConflict
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	rows := []core.Backfill{}

	prefix := identity.Namespace + "\x00" + identity.ServiceID + "\x00"
	for key, job := range b.backfills {
		if strings.HasPrefix(key, prefix) && job.Input.ID > cursor {
			rows = append(rows, job)
		}
	}

	slices.SortFunc(rows, func(a, b core.Backfill) int { return strings.Compare(a.Input.ID, b.Input.ID) })

	next := ""

	if len(rows) > limit {
		rows = rows[:limit]
		next = rows[len(rows)-1].Input.ID
	}

	return rows, next, nil
}

var _ core.Operations = (*Broker)(nil)
