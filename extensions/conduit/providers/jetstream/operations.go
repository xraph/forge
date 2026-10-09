package jetstream

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	js "github.com/nats-io/nats.go/jetstream"
	"github.com/xraph/forge/extensions/conduit/core"
)

func (p *Provider) InspectConsumer(ctx context.Context, binding core.Binding) (core.ConsumerInfo, error) {
	client, err := p.client()
	if err != nil {
		return core.ConsumerInfo{}, err
	}

	consumer, err := client.Consumer(ctx, streamName(binding.Identity.Namespace, binding.Stream.Name), binding.ConsumerID())
	if err != nil {
		return core.ConsumerInfo{}, err
	}

	info, err := consumer.Info(ctx)
	if err != nil {
		return core.ConsumerInfo{}, err
	}

	return core.ConsumerInfo{Subscription: binding.Subscription, ConsumerID: binding.ConsumerID(), Pending: info.NumPending, AckPending: uint64(info.NumAckPending), Redelivered: uint64(info.NumRedelivered), Paused: info.Paused}, nil //nolint:gosec // JetStream reports nonnegative outstanding and redelivery counts.
}
func (p *Provider) PauseConsumer(ctx context.Context, binding core.Binding, paused bool) error {
	client, err := p.client()
	if err != nil {
		return err
	}

	name := streamName(binding.Identity.Namespace, binding.Stream.Name)
	if paused {
		_, err = client.PauseConsumer(ctx, name, binding.ConsumerID(), time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC))
	} else {
		_, err = client.ResumeConsumer(ctx, name, binding.ConsumerID())
	}

	return err
}

type storedBackfill struct {
	Job     core.Backfill `json:"job"`
	ClaimAt time.Time     `json:"claimAt,omitzero"`
}

func backfillKey(service, id string) string { return "b." + hash(service) + "." + id }
func (p *Provider) RunBackfill(ctx context.Context, binding core.Binding, in core.BackfillInput) (core.Backfill, error) {
	if err := in.Validate(); err != nil {
		return core.Backfill{}, err
	}

	bucket, err := p.bucket(ctx, binding.Identity.Namespace)
	if err != nil {
		return core.Backfill{}, err
	}

	key := backfillKey(binding.Identity.ServiceID, in.ID)
	stored := storedBackfill{Job: core.Backfill{Input: in, ConsumerID: binding.ConsumerID(), Stream: binding.Stream.Name, Provider: binding.Stream.Provider, Next: in.Start, State: "running", UpdatedAt: time.Now().UTC(), Persisted: true}}

	data, err := json.Marshal(stored)
	if err != nil {
		return core.Backfill{}, err
	}

	if _, err := bucket.Create(ctx, key, data); err != nil && !errors.Is(err, js.ErrKeyExists) {
		return core.Backfill{}, err
	}

	entry, err := bucket.Get(ctx, key)
	if err != nil {
		return core.Backfill{}, err
	}

	if err := json.Unmarshal(entry.Value(), &stored); err != nil {
		return core.Backfill{}, err
	}

	if stored.Job.Input != in || stored.Job.ConsumerID != binding.ConsumerID() {
		return stored.Job, core.ErrConflict
	}

	if stored.Job.State == "complete" {
		return stored.Job, nil
	}

	if !stored.ClaimAt.IsZero() && time.Since(stored.ClaimAt) < time.Minute {
		return stored.Job, core.ErrConflict
	}

	stored.ClaimAt = time.Now().UTC()

	data, err = json.Marshal(stored)
	if err != nil {
		return stored.Job, err
	}

	revision, err := bucket.Update(ctx, key, data, entry.Revision())
	if err != nil {
		return stored.Job, core.ErrConflict
	}

	client, err := p.client()
	if err != nil {
		return stored.Job, err
	}

	stream, err := client.Stream(ctx, streamName(binding.Identity.Namespace, binding.Stream.Name))
	if err != nil {
		return stored.Job, err
	}

	save := func(ctx context.Context, job core.Backfill) error {
		stored.Job = job
		if job.State != "running" {
			stored.ClaimAt = time.Time{}
		}

		data, err := json.Marshal(stored)
		if err != nil {
			return err
		}

		next, err := bucket.Update(ctx, key, data, revision)
		if err != nil {
			return core.ErrOutcomeUnknown
		}

		revision = next

		return nil
	}

	return core.ExecuteBackfill(ctx, binding, stored.Job, func(ctx context.Context, sequence uint64) (core.Envelope, error) {
		raw, err := stream.GetMsg(ctx, sequence)
		if errors.Is(err, js.ErrMsgNotFound) {
			return core.Envelope{}, core.ErrNotFound
		}

		if err != nil {
			return core.Envelope{}, err
		}

		var msg core.Envelope
		if err := json.Unmarshal(raw.Data, &msg); err != nil {
			return core.Envelope{}, err
		}

		return msg, nil
	}, func(ctx context.Context, msg core.Envelope, id string) error {
		_, err := p.publish(ctx, binding.Identity.Namespace, binding.Stream, msg, id)

		return err
	}, save)
}
func (p *Provider) ListBackfills(ctx context.Context, identity core.Identity, cursor string, limit int) ([]core.Backfill, string, error) {
	if limit < 1 || limit > 100 {
		return nil, "", core.ErrConflict
	}

	bucket, err := p.bucket(ctx, identity.Namespace)
	if err != nil {
		return nil, "", err
	}

	lister, err := bucket.ListKeysFiltered(ctx, "b."+hash(identity.ServiceID)+".*")
	if errors.Is(err, js.ErrNoKeysFound) {
		return []core.Backfill{}, "", nil
	}

	if err != nil {
		return nil, "", err
	}

	defer func() { _ = lister.Stop() }()

	rows := []core.Backfill{}

	for key := range lister.Keys() {
		entry, err := bucket.Get(ctx, key)
		if err != nil {
			return nil, "", err
		}

		var stored storedBackfill
		if err := json.Unmarshal(entry.Value(), &stored); err != nil {
			return nil, "", err
		}

		if stored.Job.Input.ID > cursor {
			rows = append(rows, stored.Job)
			slices.SortFunc(rows, func(a, b core.Backfill) int { return strings.Compare(a.Input.ID, b.Input.ID) })

			if len(rows) > limit+1 {
				rows = rows[:limit+1]
			}
		}
	}

	if err := ctx.Err(); err != nil {
		return nil, "", err
	}

	next := ""

	if len(rows) > limit {
		rows = rows[:limit]
		next = rows[len(rows)-1].Input.ID
	}

	return rows, next, nil
}

var _ core.Operations = (*Provider)(nil)
