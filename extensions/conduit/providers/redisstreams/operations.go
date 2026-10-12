package redisstreams

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/xraph/forge/extensions/conduit/core"
)

// InspectConsumer reports the group's backlog, unacknowledged and redelivered counts.
// The backlog counts every retained entry the group has not read, whatever its type.
func (p *Provider) InspectConsumer(ctx context.Context, binding core.Binding) (core.ConsumerInfo, error) {
	c, err := p.client()
	if err != nil {
		return core.ConsumerInfo{}, err
	}

	key := streamKey(binding.Identity.Namespace, binding.Stream.Name)
	id := binding.ConsumerID()

	groups, err := c.XInfoGroups(ctx, key).Result()
	if err != nil {
		if strings.Contains(err.Error(), "no such key") {
			return core.ConsumerInfo{}, core.ErrNotFound
		}

		return core.ConsumerInfo{}, err
	}

	index := slices.IndexFunc(groups, func(g redis.XInfoGroup) bool { return g.Name == id })
	if index < 0 {
		return core.ConsumerInfo{}, core.ErrNotFound
	}

	group := groups[index]
	info := core.ConsumerInfo{Subscription: binding.Subscription, ConsumerID: id, AckPending: uint64(max(0, group.Pending))}

	if group.Lag >= 0 {
		info.Pending = uint64(group.Lag)
	} else {
		// Redis can't compute lag after a deletion inside the unread range; count it instead.
		entries, err := c.XRangeN(ctx, key, "("+group.LastDeliveredID, "+", 10000).Result()
		if err != nil {
			return core.ConsumerInfo{}, err
		}

		info.Pending = uint64(len(entries))
	}

	// The pending list is bounded by MaxInFlight, so scanning it is cheap.
	if group.Pending > 0 {
		pending, err := c.XPendingExt(ctx, &redis.XPendingExtArgs{Stream: key, Group: id, Start: "-", End: "+", Count: group.Pending}).Result()
		if err != nil {
			return core.ConsumerInfo{}, err
		}

		for _, entry := range pending {
			if entry.RetryCount > 1 {
				info.Redelivered++
			}
		}
	}

	paused, err := c.Exists(ctx, pausedKey(key, id)).Result()
	if err != nil {
		return core.ConsumerInfo{}, err
	}

	info.Paused = paused == 1

	return info, nil
}

// PauseConsumer holds or releases every replica of the logical consumer.
func (p *Provider) PauseConsumer(ctx context.Context, binding core.Binding, paused bool) error {
	c, err := p.client()
	if err != nil {
		return err
	}

	key := streamKey(binding.Identity.Namespace, binding.Stream.Name)
	id := binding.ConsumerID()

	exists, err := c.Exists(ctx, key+":group:"+id).Result()
	if err != nil {
		return err
	}

	if exists == 0 {
		return core.ErrNotFound
	}

	return p.persisted(ctx, binding.Stream.Replicas, func(conn *redis.Conn) error {
		if paused {
			return conn.Set(ctx, pausedKey(key, id), "1", 0).Err()
		}

		return conn.Del(ctx, pausedKey(key, id)).Err()
	})
}

type storedBackfill struct {
	Job     core.Backfill `json:"job"`
	ClaimAt time.Time     `json:"claimAt,omitzero"`
}

func backfillsKey(identity core.Identity) string {
	return "fc:{" + hash(identity.Namespace, identity.ServiceID) + "}:backfills"
}

// swapScript replaces a hash field only when it still holds the value the caller read.
const swapScript = `if redis.call('HGET',KEYS[1],ARGV[1])~=ARGV[2] then return 0 end
redis.call('HSET',KEYS[1],ARGV[1],ARGV[3]); return 1`

func (p *Provider) swap(ctx context.Context, replicas int, key, field, expected, next string) (bool, error) {
	var swapped bool

	err := p.persisted(ctx, replicas, func(conn *redis.Conn) error {
		n, err := conn.Eval(ctx, swapScript, []string{key}, field, expected, next).Int()
		swapped = n == 1

		return err
	})

	return swapped, err
}

// RunBackfill republishes a retained range to one consumer, resumable by operation ID.
func (p *Provider) RunBackfill(ctx context.Context, binding core.Binding, in core.BackfillInput) (core.Backfill, error) {
	if err := in.Validate(); err != nil {
		return core.Backfill{}, err
	}

	c, err := p.client()
	if err != nil {
		return core.Backfill{}, err
	}

	key := backfillsKey(binding.Identity)
	replicas := binding.Stream.Replicas
	initial := storedBackfill{Job: core.Backfill{
		MessageType: binding.Subscription.MessageType, Input: in, ConsumerID: binding.ConsumerID(), Stream: binding.Stream.Name,
		Provider: binding.Stream.Provider, Next: in.Start, State: "running", UpdatedAt: time.Now().UTC(), Persisted: true,
	}}

	data, err := json.Marshal(initial)
	if err != nil {
		return core.Backfill{}, err
	}

	if err := p.persisted(ctx, replicas, func(conn *redis.Conn) error { return conn.HSetNX(ctx, key, in.ID, string(data)).Err() }); err != nil {
		return core.Backfill{}, err
	}

	current, err := c.HGet(ctx, key, in.ID).Result()
	if err != nil {
		return core.Backfill{}, err
	}

	var stored storedBackfill
	if err := json.Unmarshal([]byte(current), &stored); err != nil {
		return core.Backfill{}, err
	}

	if stored.Job.Input != in || stored.Job.ConsumerID != binding.ConsumerID() || stored.Job.Stream != binding.Stream.Name || stored.Job.MessageType != binding.Subscription.MessageType {
		return stored.Job, core.ErrConflict
	}

	if stored.Job.State == "complete" {
		return stored.Job, nil
	}

	if !stored.ClaimAt.IsZero() && time.Since(stored.ClaimAt) < time.Minute {
		return stored.Job, core.ErrConflict
	}

	stored.ClaimAt = time.Now().UTC()

	claimed, err := json.Marshal(stored)
	if err != nil {
		return stored.Job, err
	}

	won, err := p.swap(ctx, replicas, key, in.ID, current, string(claimed))
	if err != nil {
		return stored.Job, err
	}

	if !won {
		return stored.Job, core.ErrConflict
	}

	current = string(claimed)
	events := streamKey(binding.Identity.Namespace, binding.Stream.Name)

	save := func(ctx context.Context, job core.Backfill) error {
		stored.Job = job
		if job.State != "running" {
			stored.ClaimAt = time.Time{}
		}

		next, err := json.Marshal(stored)
		if err != nil {
			return err
		}

		ok, err := p.swap(ctx, replicas, key, in.ID, current, string(next))
		if err != nil {
			return err
		}

		if !ok {
			return core.ErrOutcomeUnknown
		}

		current = string(next)

		return nil
	}

	read := func(ctx context.Context, sequence uint64) (core.Envelope, error) {
		id := strconv.FormatUint(sequence, 10) + "-0"

		entries, err := c.XRangeN(ctx, events, id, id, 1).Result()
		if err != nil {
			return core.Envelope{}, err
		}

		if len(entries) == 0 {
			return core.Envelope{}, core.ErrNotFound
		}

		raw, ok := entries[0].Values["envelope"].(string)
		if !ok {
			return core.Envelope{}, errors.New("conduit/redis: invalid retained envelope")
		}

		var msg core.Envelope
		if err := json.Unmarshal([]byte(raw), &msg); err != nil {
			return core.Envelope{}, errors.New("conduit/redis: invalid retained envelope")
		}

		return msg, nil
	}

	publish := func(ctx context.Context, msg core.Envelope, id string) error {
		_, err := p.publish(ctx, binding.Identity.Namespace, binding.Stream, msg, hash("backfill", id))

		return err
	}

	return core.ExecuteBackfill(ctx, binding, stored.Job, read, publish, save)
}

// ListBackfills returns one service's operations ordered by ID.
func (p *Provider) ListBackfills(ctx context.Context, identity core.Identity, cursor string, limit int) ([]core.Backfill, string, error) {
	if limit < 1 || limit > 100 {
		return nil, "", core.ErrConflict
	}

	c, err := p.client()
	if err != nil {
		return nil, "", err
	}

	values, err := c.HGetAll(ctx, backfillsKey(identity)).Result()
	if err != nil {
		return nil, "", err
	}

	rows := []core.Backfill{}

	for _, raw := range values {
		var stored storedBackfill
		if err := json.Unmarshal([]byte(raw), &stored); err != nil {
			return nil, "", err
		}

		if stored.Job.Input.ID > cursor {
			rows = append(rows, stored.Job)
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

var _ core.Operations = (*Provider)(nil)
