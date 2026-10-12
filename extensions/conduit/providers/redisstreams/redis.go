// Package redisstreams implements retained events with Redis Streams consumer groups.
package redisstreams

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/xraph/forge/extensions/conduit/core"
)

// Options accepts native Redis TLS and authentication configuration.
// Redis 7.2 or later with AOF enabled is required. Deduplication lasts 24 hours by default.
type Options struct {
	URL                 string
	Redis               *redis.Options
	DeduplicationWindow time.Duration
	PersistenceTimeout  time.Duration
	// LeaseTTL is how long a registered instance stays listed without renewal. Default 45s.
	LeaseTTL time.Duration
}

// Provider owns a dedicated Redis connection pool.
type Provider struct {
	mu      sync.RWMutex
	options Options
	conn    *redis.Client
	// owner and generation identify this process. A newer process wins a registration
	// that an older one still renews, and only the owner can deregister it.
	owner      string
	generation int64
	calls      rpcCalls
}

// New prepares a provider whose connection lifecycle belongs to Conduit.
func New(options Options) *Provider {
	if options.PersistenceTimeout == 0 {
		options.PersistenceTimeout = 5 * time.Second
	}

	if options.DeduplicationWindow == 0 {
		options.DeduplicationWindow = 24 * time.Hour
	}

	if options.LeaseTTL == 0 {
		options.LeaseTTL = 45 * time.Second
	}

	return &Provider{options: options, owner: core.NewID(), generation: time.Now().UnixMilli()}
}

// Name returns the broker type.
func (p *Provider) Name() string { return "redis-streams" }

// Capabilities reports the same guarantees as JetStream: persisted events, recovery,
// consumer controls, backfill and request/reply.
func (p *Provider) Capabilities() core.Capabilities {
	return core.Capabilities{Durable: true, Replay: true, DeadLetters: true, RPC: true, ConsumerControls: true, Backfill: true}
}

// Connect verifies persistence before accepting event traffic.
func (p *Provider) Connect(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.conn != nil {
		return core.ErrConflict
	}

	if p.options.DeduplicationWindow < time.Millisecond || p.options.PersistenceTimeout < time.Millisecond || p.options.PersistenceTimeout > time.Minute {
		return core.ErrConflict
	}

	options := p.options.Redis
	if options == nil {
		var err error

		options, err = redis.ParseURL(p.options.URL)
		if err != nil {
			return errors.New("conduit/redis: invalid connection configuration")
		}
	}

	copyOptions := *options
	copyOptions.MaxRetries = -1 // A failed write must not be silently retried with a different outcome.
	copyOptions.ContextTimeoutEnabled = true
	c := redis.NewClient(&copyOptions)
	fail := func() error {
		_ = c.Close()

		return errors.New("conduit/redis: Redis 7.2+ with AOF persistence is required")
	}

	config, err := c.ConfigGet(ctx, "appendonly").Result()
	if err != nil || config["appendonly"] != "yes" {
		return fail()
	}

	probe := redis.NewSliceCmd(ctx, "WAITAOF", 1, 0, p.options.PersistenceTimeout.Milliseconds())
	if err := c.Process(ctx, probe); err != nil {
		return fail()
	}

	p.conn = c

	p.calls.mu.Lock()
	p.calls.client = c
	p.calls.mu.Unlock()

	return nil
}

// Close releases the pool without deleting durable group progress.
func (p *Provider) Close(context.Context) error {
	p.stopReplies()

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.conn == nil {
		return nil
	}

	err := p.conn.Close()
	p.conn = nil

	return err
}
func (p *Provider) client() (*redis.Client, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.conn == nil {
		return nil, core.ErrNotRunning
	}

	return p.conn, nil
}

// Health checks connectivity without exposing connection configuration.
func (p *Provider) Health(ctx context.Context) error {
	c, err := p.client()
	if err != nil {
		return err
	}

	return c.Ping(ctx).Err()
}
func hash(parts ...string) string {
	data, err := json.Marshal(parts)
	if err != nil {
		panic(err)
	}

	sum := sha256.Sum256(data)

	return hex.EncodeToString(sum[:16])
}
func streamKey(ns, name string) string { return "fc:{" + hash(ns, name) + "}:events" }
func configKey(ns, name string) string { return streamKey(ns, name) + ":config" }

func (p *Provider) persisted(ctx context.Context, replicas int, write func(*redis.Conn) error) error {
	c, err := p.client()
	if err != nil {
		return err
	}

	conn := c.Conn()
	defer func() { _ = conn.Close() }()

	if err := write(conn); err != nil {
		return err
	}

	command := redis.NewSliceCmd(ctx, "WAITAOF", 1, max(0, replicas-1), p.options.PersistenceTimeout.Milliseconds())
	if err := conn.Process(ctx, command); err != nil {
		return core.ErrOutcomeUnknown
	}

	counts, err := command.Result()
	if err != nil || len(counts) != 2 || counts[0] != int64(1) {
		return core.ErrOutcomeUnknown
	}

	count, ok := counts[1].(int64)
	if !ok || count < int64(replicas-1) {
		return core.ErrOutcomeUnknown
	}

	return nil
}

// EnsureStream verifies identical topology across service instances.
func (p *Provider) EnsureStream(ctx context.Context, ns string, cfg core.StreamConfig) error {
	cfg.Subjects = slices.Clone(cfg.Subjects)
	slices.Sort(cfg.Subjects)

	data, err := json.Marshal(cfg)
	if err != nil {
		return err
	}

	return p.persisted(ctx, cfg.Replicas, func(c *redis.Conn) error {
		result, err := c.Eval(ctx, `local old=redis.call('GET',KEYS[1]); if old and old~=ARGV[1] then return 0 end; redis.call('SET',KEYS[1],ARGV[1]); return 1`, []string{configKey(ns, cfg.Name)}, string(data)).Int()
		if err != nil {
			return err
		}

		if result != 1 {
			return core.ErrConflict
		}

		return nil
	})
}

//nolint:dupword // Lua nested blocks require consecutive end keywords.
const pruneScript = `
local age=tonumber(ARGV[1]); local count=tonumber(ARGV[2]); local now=redis.call('TIME'); local ms=now[1]*1000+math.floor(now[2]/1000)
if age>0 then
 local stale=redis.call('ZRANGEBYSCORE',KEYS[2],'-inf',ms-age,'LIMIT',0,1000)
 for _,id in ipairs(stale) do redis.call('XDEL',KEYS[1],id); redis.call('ZREM',KEYS[2],id) end
end
if count>0 then
 redis.call('XTRIM',KEYS[1],'MAXLEN',count)
 local excess=redis.call('ZCARD',KEYS[2])-count
 if excess>0 then redis.call('ZREMRANGEBYRANK',KEYS[2],0,excess-1) end
end
`
const publishScript = `
local existing=redis.call('GET',KEYS[4]); if existing then return {existing,1} end
local seq=redis.call('INCR',KEYS[3]); local id=tostring(seq)..'-0'
redis.call('XADD',KEYS[1],id,'envelope',ARGV[3]); redis.call('ZADD',KEYS[2],ms,id)
redis.call('SET',KEYS[4],tostring(seq),'PX',ARGV[4]); return {tostring(seq),0}
`

// claimScript and readScript check the pause flag inside Redis, so a pause lands between
// reads on every replica with no window where a replica has checked but not yet read.
const claimScript = `if redis.call('EXISTS',KEYS[2])==1 then return {ARGV[4],{}} end
return redis.call('XAUTOCLAIM',KEYS[1],ARGV[1],ARGV[2],ARGV[3],ARGV[4],'COUNT',1)`

const readScript = `if redis.call('EXISTS',KEYS[2])==1 then return {} end
local pending=redis.call('XPENDING',KEYS[1],ARGV[1]); if pending[1]>=tonumber(ARGV[3]) then return {} end
return redis.call('XREADGROUP','GROUP',ARGV[1],ARGV[2],'COUNT',1,'STREAMS',KEYS[1],'>') or {}`

func pausedKey(key, consumerID string) string { return key + ":group:" + consumerID + ":paused" }

// streamEntries reads the entry list of an XAUTOCLAIM or XREADGROUP reply.
func streamEntries(raw any) []redis.XMessage {
	entries, ok := raw.([]any)
	if !ok {
		return nil
	}

	msgs := make([]redis.XMessage, 0, len(entries))

	for _, item := range entries {
		entry, ok := item.([]any)
		if !ok || len(entry) != 2 {
			continue
		}

		id, idOK := entry[0].(string)

		fields, fieldsOK := entry[1].([]any)
		if idOK && fieldsOK && len(fields) == 2 {
			msgs = append(msgs, redis.XMessage{ID: id, Values: map[string]any{"envelope": fields[1]}})
		}
	}

	return msgs
}

// Publish confirms local and configured replica AOF persistence, with bounded ID deduplication.
func (p *Provider) Publish(ctx context.Context, ns string, cfg core.StreamConfig, msg core.Envelope) (core.Receipt, error) {
	return p.publish(ctx, ns, cfg, msg, hash(msg.ID, msg.TargetConsumer))
}

// publish deduplicates on an explicit key. Backfill passes its operation-scoped ID so a new
// operation can republish a message an earlier one already sent.
func (p *Provider) publish(ctx context.Context, ns string, cfg core.StreamConfig, msg core.Envelope, dedup string) (core.Receipt, error) {
	data, err := json.Marshal(msg)
	if err != nil {
		return core.Receipt{}, err
	}

	key := streamKey(ns, cfg.Name)
	receipt := core.Receipt{MessageID: msg.ID, Persisted: true}

	err = p.persisted(ctx, cfg.Replicas, func(c *redis.Conn) error {
		values, err := c.Eval(ctx, pruneScript+publishScript, []string{key, key + ":age", key + ":seq", key + ":dedup:" + dedup}, cfg.MaxAge.Milliseconds(), cfg.MaxMessages, string(data), p.options.DeduplicationWindow.Milliseconds()).Slice()
		if err != nil {
			return core.ErrOutcomeUnknown
		}

		if len(values) != 2 {
			return core.ErrOutcomeUnknown
		}

		seq, ok := values[0].(string)
		if !ok {
			return core.ErrOutcomeUnknown
		}

		receipt.Sequence, err = strconv.ParseUint(seq, 10, 64)
		receipt.Duplicate = values[1] == int64(1)

		return err
	})
	if err != nil {
		return core.Receipt{}, err
	}

	return receipt, nil
}

type subscription struct {
	provider    *Provider
	binding     core.Binding
	key         string
	member      string
	closed      chan struct{}
	once        sync.Once
	mu          sync.Mutex
	claimCursor string
}

// Subscribe creates a shared competing group or an independent broadcast group.
func (p *Provider) Subscribe(ctx context.Context, b core.Binding) (core.Subscription, error) {
	if _, err := p.client(); err != nil {
		return nil, err
	}

	key := streamKey(b.Identity.Namespace, b.Stream.Name)

	start := "0-0"
	if b.Subscription.StartAt == "new" {
		start = "$"
	}

	if b.Subscription.StartAt == "sequence" {
		start = strconv.FormatUint(b.Subscription.StartSequence-1, 10) + "-0"
	}

	policy := b.Subscription

	policy.Concurrency = 0
	if policy.Mode == core.Competing {
		policy.BroadcastID = ""
	}

	data, err := json.Marshal(policy)
	if err != nil {
		return nil, err
	}

	err = p.persisted(ctx, b.Stream.Replicas, func(conn *redis.Conn) error {
		return conn.Eval(ctx, `local old=redis.call('GET',KEYS[2]); if old and old~=ARGV[1] then return redis.error_reply('CONDUIT_CONFLICT') end; redis.call('SET',KEYS[2],ARGV[1]); local r=redis.pcall('XGROUP','CREATE',KEYS[1],ARGV[2],ARGV[3],'MKSTREAM'); if type(r)=='table' and r.err and not string.find(r.err,'BUSYGROUP') then return redis.error_reply(r.err) end; return 1`, []string{key, key + ":group:" + b.ConsumerID()}, string(data), b.ConsumerID(), start).Err()
	})
	if err != nil {
		if errors.Is(err, core.ErrOutcomeUnknown) {
			return nil, err
		}

		if strings.Contains(err.Error(), "CONDUIT_CONFLICT") {
			return nil, core.ErrConflict
		}

		return nil, errors.New("conduit/redis: consumer creation failed")
	}

	return &subscription{provider: p, binding: b, key: key, member: hash(b.Identity.InstanceID, core.NewID()), closed: make(chan struct{}), claimCursor: "0-0"}, nil
}

func (s *subscription) Next(ctx context.Context) (core.Delivery, error) {
	c, err := s.provider.client()
	if err != nil {
		return nil, err
	}

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-s.closed:
			return nil, context.Canceled
		default:
		}

		s.mu.Lock()
		keys := []string{s.key, pausedKey(s.key, s.binding.ConsumerID())}

		var msgs []redis.XMessage

		claimed, readErr := c.Eval(ctx, claimScript, keys, s.binding.ConsumerID(), s.member, (s.binding.Subscription.Timeout + 5*time.Second).Milliseconds(), s.claimCursor).Slice()
		if readErr == nil && len(claimed) >= 2 {
			if next, ok := claimed[0].(string); ok {
				s.claimCursor = next
			}

			msgs = streamEntries(claimed[1])
		}

		if readErr == nil && len(msgs) == 0 {
			result, e := c.Eval(ctx, readScript, keys, s.binding.ConsumerID(), s.member, s.binding.Subscription.MaxInFlight).Slice()

			readErr = e
			if e == nil && len(result) > 0 {
				if stream, ok := result[0].([]any); ok && len(stream) == 2 {
					msgs = streamEntries(stream[1])
				}
			}
		}
		s.mu.Unlock()

		if readErr != nil && !errors.Is(readErr, redis.Nil) {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}

			// The socket read can time out a moment before ctx reports Done. That is the
			// caller's deadline, not a broker failure.
			if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < 50*time.Millisecond {
				<-ctx.Done()

				return nil, ctx.Err()
			}

			return nil, errors.New("conduit/redis: consumer read failed")
		}

		if len(msgs) > 0 {
			msg := msgs[0]

			raw, ok := msg.Values["envelope"].(string)
			if !ok {
				return nil, errors.New("conduit/redis: invalid retained envelope")
			}

			var envelope core.Envelope
			if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
				return nil, errors.New("conduit/redis: invalid retained envelope")
			}
			// Retained records can be read by every group, but recovery stays consumer-scoped.
			expired := false

			if s.binding.Stream.MaxAge > 0 {
				score, e := c.ZScore(ctx, s.key+":age", msg.ID).Result()
				expired = errors.Is(e, redis.Nil) || e == nil && time.Now().UnixMilli()-int64(score) >= s.binding.Stream.MaxAge.Milliseconds()
			}

			ready, gateErr := c.HGet(ctx, s.key+":retry", msg.ID+":"+s.binding.ConsumerID()).Int64()
			if gateErr != nil && !errors.Is(gateErr, redis.Nil) {
				return nil, gateErr
			}

			if ready > time.Now().UnixMilli() {
				if err := c.Eval(ctx, `return redis.call("XCLAIM",KEYS[1],ARGV[1],ARGV[2],0,ARGV[3],"IDLE",ARGV[4],"JUSTID")`, []string{s.key}, s.binding.ConsumerID(), s.member, msg.ID, max(0, (s.binding.Subscription.Timeout+5*time.Second).Milliseconds()-(ready-time.Now().UnixMilli()))).Err(); err != nil {
					return nil, err
				}

				continue
			}

			if expired || envelope.Type != s.binding.Subscription.MessageType || envelope.TargetConsumer != "" && envelope.TargetConsumer != s.binding.ConsumerID() {
				if err := c.XAck(ctx, s.key, s.binding.ConsumerID(), msg.ID).Err(); err != nil {
					return nil, err
				}

				continue
			}

			pending, e := c.XPendingExt(ctx, &redis.XPendingExtArgs{Stream: s.key, Group: s.binding.ConsumerID(), Start: msg.ID, End: msg.ID, Count: 1}).Result()
			if e != nil {
				return nil, e
			}

			attempt := uint64(1)
			if len(pending) > 0 {
				attempt = uint64(max(1, pending[0].RetryCount))
			}

			seq, e := strconv.ParseUint(strings.TrimSuffix(msg.ID, "-0"), 10, 64)
			if e != nil {
				return nil, e
			}

			return &delivery{sub: s, id: msg.ID, envelope: envelope, attempt: attempt, sequence: seq}, nil
		}

		timer := time.NewTimer(20 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()

			return nil, ctx.Err()
		case <-s.closed:
			timer.Stop()

			return nil, context.Canceled
		case <-timer.C:
		}
	}
}
func (s *subscription) Close(ctx context.Context) error {
	s.once.Do(func() { close(s.closed) })

	if s.binding.Subscription.Mode == core.Broadcast && !s.binding.Subscription.Durable {
		c, err := s.provider.client()
		if err != nil {
			return err
		}

		if err := c.XGroupDestroy(ctx, s.key, s.binding.ConsumerID()).Err(); err != nil {
			return err
		}

		return c.Del(ctx, s.key+":group:"+s.binding.ConsumerID(), pausedKey(s.key, s.binding.ConsumerID())).Err()
	}

	return nil
}

type delivery struct {
	sub      *subscription
	id       string
	envelope core.Envelope
	attempt  uint64
	sequence uint64
}

func (d *delivery) Message() core.Envelope { return d.envelope.Clone() }
func (d *delivery) Info() core.DeliveryInfo {
	b := d.sub.binding

	return core.DeliveryInfo{Stream: b.Stream.Name, ConsumerID: b.ConsumerID(), SubscriptionID: b.Subscription.ID, Destination: b.Identity, Mode: b.Subscription.Mode, Attempt: d.attempt, Sequence: d.sequence}
}
func (d *delivery) Ack(ctx context.Context) error {
	return d.sub.provider.persisted(ctx, d.sub.binding.Stream.Replicas, func(c *redis.Conn) error {
		_, err := c.Eval(ctx, `redis.call('HDEL',KEYS[2],ARGV[2]..':'..ARGV[1]); return redis.call('XACK',KEYS[1],ARGV[1],ARGV[2])`, []string{d.sub.key, d.sub.key + ":retry"}, d.sub.binding.ConsumerID(), d.id).Result()

		return err
	})
}
func (d *delivery) Reject(ctx context.Context) error { return d.Ack(ctx) }
func (d *delivery) Retry(ctx context.Context, delay time.Duration) error {
	c, err := d.sub.provider.client()
	if err != nil {
		return err
	}

	_, err = c.Eval(ctx, `redis.call('HSET',KEYS[2],ARGV[2]..':'..ARGV[1],ARGV[4]); return redis.call('XCLAIM',KEYS[1],ARGV[1],ARGV[3],0,ARGV[2],'IDLE',ARGV[5],'JUSTID')`, []string{d.sub.key, d.sub.key + ":retry"}, d.sub.binding.ConsumerID(), d.id, d.sub.member, time.Now().Add(delay).UnixMilli(), max(0, (d.sub.binding.Subscription.Timeout+5*time.Second-delay).Milliseconds())).Result()

	return err
}

// Inspect returns retained message and group counts.
func (p *Provider) Inspect(ctx context.Context, ns string, cfg core.StreamConfig) (core.StreamInfo, error) {
	c, err := p.client()
	if err != nil {
		return core.StreamInfo{}, err
	}

	key := streamKey(ns, cfg.Name)
	if err := c.Eval(ctx, pruneScript+"return 1", []string{key, key + ":age"}, cfg.MaxAge.Milliseconds(), cfg.MaxMessages).Err(); err != nil {
		return core.StreamInfo{}, err
	}

	n, err := c.XLen(ctx, key).Result()
	if err != nil {
		return core.StreamInfo{}, err
	}

	groups, err := c.XInfoGroups(ctx, key).Result()
	if err != nil && !strings.Contains(err.Error(), "no such key") {
		return core.StreamInfo{}, err
	}

	return core.StreamInfo{Config: cfg, Messages: uint64(max(0, n)), Consumers: len(groups)}, nil
}

// StoreDeadLetter stores a stable scoped failure before its original delivery is settled.
func (p *Provider) StoreDeadLetter(ctx context.Context, ns string, l core.DeadLetter) error {
	data, err := json.Marshal(l)
	if err != nil {
		return err
	}

	client, err := p.client()
	if err != nil {
		return err
	}

	raw, err := client.Get(ctx, configKey(ns, l.Delivery.Stream)).Result()
	if err != nil {
		return err
	}

	var cfg core.StreamConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return err
	}

	return p.persisted(ctx, cfg.Replicas, func(c *redis.Conn) error {
		return c.HSetNX(ctx, "fc:{"+hash(ns, l.Delivery.Destination.ServiceID)+"}:letters", l.Delivery.SubscriptionID+":"+l.ID, string(data)).Err()
	})
}

// ListDeadLetters returns a service-scoped ID cursor without discarding retained failures.
func (p *Provider) ListDeadLetters(ctx context.Context, id core.Identity, sub, cursor string, limit int) ([]core.DeadLetter, string, error) {
	c, err := p.client()
	if err != nil {
		return nil, "", err
	}

	if limit < 1 || limit > 100 {
		return nil, "", core.ErrConflict
	}

	values, err := c.HGetAll(ctx, "fc:{"+hash(id.Namespace, id.ServiceID)+"}:letters").Result()
	if err != nil {
		return nil, "", err
	}

	result := make([]core.DeadLetter, 0, limit+1)

	for _, value := range values {
		var l core.DeadLetter
		if err := json.Unmarshal([]byte(value), &l); err != nil {
			return nil, "", err
		}

		if l.ID > cursor && (sub == "" || l.Delivery.SubscriptionID == sub) {
			result = append(result, l)
			slices.SortFunc(result, func(a, b core.DeadLetter) int { return strings.Compare(a.ID, b.ID) })

			if len(result) > limit+1 {
				result = result[:limit+1]
			}
		}
	}

	next := ""

	if len(result) > limit {
		result = result[:limit]
		next = result[len(result)-1].ID
	}

	return result, next, nil
}

// ReplayDeadLetter publishes only to the original consumer with a stable recovery ID.
func (p *Provider) ReplayDeadLetter(ctx context.Context, id core.Identity, sub, messageID string) (core.Receipt, error) {
	c, err := p.client()
	if err != nil {
		return core.Receipt{}, err
	}

	key := "fc:{" + hash(id.Namespace, id.ServiceID) + "}:letters"

	raw, err := c.HGet(ctx, key, sub+":"+messageID).Result()
	if errors.Is(err, redis.Nil) {
		return core.Receipt{}, core.ErrNotFound
	}

	if err != nil {
		return core.Receipt{}, err
	}

	var letter core.DeadLetter
	if err := json.Unmarshal([]byte(raw), &letter); err != nil {
		return core.Receipt{}, err
	}

	if letter.Replayed {
		return core.Receipt{}, core.ErrConflict
	}

	raw, err = c.Get(ctx, configKey(id.Namespace, letter.Delivery.Stream)).Result()
	if err != nil {
		return core.Receipt{}, err
	}

	var cfg core.StreamConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return core.Receipt{}, err
	}

	letter.Message.TargetConsumer = letter.Delivery.ConsumerID

	receipt, err := p.Publish(ctx, id.Namespace, cfg, letter.Message)
	if err != nil {
		return core.Receipt{}, err
	}

	letter.Replayed = true

	data, err := json.Marshal(letter)
	if err != nil {
		return core.Receipt{}, err
	}

	err = p.persisted(ctx, cfg.Replicas, func(c *redis.Conn) error { return c.HSet(ctx, key, sub+":"+messageID, string(data)).Err() })
	if err != nil {
		return core.Receipt{}, err
	}

	return receipt, nil
}

var _ core.Provider = (*Provider)(nil)
var _ core.Management = (*Provider)(nil)
