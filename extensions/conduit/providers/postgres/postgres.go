// Package postgres implements Conduit's retained events, recovery, consumer controls,
// backfill, service discovery and request/reply on PostgreSQL 13 or later.
package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/xraph/forge/extensions/conduit/core"
)

// Options configures the provider.
type Options struct {
	// URL opens a pool the provider owns. With Pool set, URL is optional and only used for
	// the LISTEN connection; point it straight at PostgreSQL when the pool goes through PgBouncer.
	URL string
	// Pool shares your application's pool. Closing the provider leaves it open.
	Pool *pgxpool.Pool
	// Schema holds the forge_conduit_ tables. Default "public".
	Schema string
	// PollInterval bounds delivery latency while notifications are unavailable. Default 1s.
	PollInterval time.Duration
	// LeaseTTL is how long a registered instance stays listed without renewal. Default 45s.
	LeaseTTL time.Duration
	// RetentionInterval is how often expired messages, leases and RPC rows are swept. Default 30s.
	RetentionInterval time.Duration
	// AutoMigrate runs Migrate during Connect. Leave it off when a central runner applies Migrations.
	AutoMigrate bool
}

var validSchema = regexp.MustCompile(`^[a-z_][a-z0-9_]{0,62}$`)

type tables struct {
	streams, messages, consumers, claims, deadLetters, backfills, instances, rpcRequests, rpcResponses string
}

func newTables(schema string) tables {
	name := func(table string) string { return pgx.Identifier{schema, "forge_conduit_" + table}.Sanitize() }

	return tables{
		streams: name("streams"), messages: name("messages"), consumers: name("consumers"), claims: name("claims"),
		deadLetters: name("dead_letters"), backfills: name("backfills"), instances: name("instances"),
		rpcRequests: name("rpc_requests"), rpcResponses: name("rpc_responses"),
	}
}

// Provider is safe for concurrent use. The Conduit runtime owns Connect and Close.
type Provider struct {
	mu       sync.RWMutex
	options  Options
	t        tables
	pool     *pgxpool.Pool
	ownsPool bool
	// owner and generation identify this process. A newer process wins a registration
	// that an older one still renews, and only the owner can deregister it.
	owner      string
	generation int64

	listener   *listener
	stop       context.CancelFunc
	background sync.WaitGroup
}

// New prepares a provider. It does no I/O.
func New(options Options) *Provider {
	if options.Schema == "" {
		options.Schema = "public"
	}

	if options.PollInterval == 0 {
		options.PollInterval = time.Second
	}

	if options.LeaseTTL == 0 {
		options.LeaseTTL = 45 * time.Second
	}

	if options.RetentionInterval == 0 {
		options.RetentionInterval = 30 * time.Second
	}

	return &Provider{options: options, t: newTables(options.Schema), owner: core.NewID(), generation: time.Now().UnixNano()}
}

// Name returns the provider type.
func (p *Provider) Name() string { return "postgres" }

// Capabilities reports the same guarantees as JetStream: persisted events, recovery,
// consumer controls, backfill and request/reply.
func (p *Provider) Capabilities() core.Capabilities {
	return core.Capabilities{Durable: true, Replay: true, DeadLetters: true, RPC: true, ConsumerControls: true, Backfill: true}
}

// Connect opens or adopts the pool and checks the server version.
func (p *Provider) Connect(ctx context.Context) error {
	if !validSchema.MatchString(p.options.Schema) {
		return errors.New("conduit/postgres: schema must be a lowercase identifier")
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.pool != nil {
		return core.ErrConflict
	}

	pool, owns, err := p.open(ctx)
	if err != nil {
		return err
	}

	config, err := p.listenerConfig(pool)
	if err != nil {
		if owns {
			pool.Close()
		}

		return err
	}

	if p.options.AutoMigrate {
		if err := migrate(ctx, pool, p.options.Schema); err != nil {
			if owns {
				pool.Close()
			}

			return err
		}
	}

	p.pool, p.ownsPool = pool, owns
	p.start(config)

	return nil
}

// listenerConfig builds the LISTEN connection's settings. Beside a shared pool, URL wins
// when it is set: the pool may go through PgBouncer, which loses LISTEN registrations.
func (p *Provider) listenerConfig(pool *pgxpool.Pool) (*pgx.ConnConfig, error) {
	config := pool.Config().ConnConfig.Copy()

	if p.options.Pool != nil && p.options.URL != "" {
		parsed, err := pgx.ParseConfig(p.options.URL)
		if err != nil {
			return nil, errors.New("conduit/postgres: invalid listener URL")
		}

		config = parsed
	}

	if config.RuntimeParams == nil {
		config.RuntimeParams = map[string]string{}
	}

	config.RuntimeParams["application_name"] = listenerName(p.owner)
	// With a handler set, pgx hands notifications to it and WaitForNotification returns
	// nothing. The listener must read them itself, and the app's handler is not meant for ours.
	config.OnNotification = nil

	return config, nil
}

// start launches the provider's background work. Called with p.mu held.
func (p *Provider) start(config *pgx.ConnConfig) {
	background, stop := context.WithCancel(context.Background())
	p.stop = stop
	p.listener = newListener(config)

	l := p.listener

	p.background.Go(func() { l.run(background) })
	p.background.Go(func() { p.maintain(background) })
}

// watch subscribes to wakeups on channel. After Close it returns a nil channel, and the
// caller's poll timer carries on alone.
func (p *Provider) watch(channel string) (<-chan struct{}, func()) {
	p.mu.RLock()
	l := p.listener
	p.mu.RUnlock()

	if l == nil {
		return nil, func() {}
	}

	return l.watch(channel)
}

// ListenerConnected reports whether wakeups arrive by notification. While it is false,
// delivery continues by polling at Options.PollInterval.
func (p *Provider) ListenerConnected() bool {
	p.mu.RLock()
	l := p.listener
	p.mu.RUnlock()

	return l != nil && l.connected.Load()
}

func (p *Provider) open(ctx context.Context) (*pgxpool.Pool, bool, error) {
	pool, owns := p.options.Pool, false

	if pool == nil {
		if p.options.URL == "" {
			return nil, false, errors.New("conduit/postgres: URL or Pool is required")
		}

		var err error

		pool, err = pgxpool.New(ctx, p.options.URL)
		if err != nil {
			return nil, false, errors.New("conduit/postgres: invalid connection configuration")
		}

		owns = true
	}

	var version int

	err := pool.QueryRow(ctx, `SELECT current_setting('server_version_num')::int`).Scan(&version)
	if err != nil || version < 130000 {
		if owns {
			pool.Close()
		}

		if err != nil {
			return nil, false, errors.New("conduit/postgres: database unavailable")
		}

		return nil, false, errors.New("conduit/postgres: PostgreSQL 13 or later is required")
	}

	return pool, owns, nil
}

// Close stops background work and releases the provider's own connections. A pool you
// passed in stays open.
func (p *Provider) Close(context.Context) error {
	p.mu.Lock()
	pool, owns, stop := p.pool, p.ownsPool, p.stop
	p.pool, p.stop, p.listener = nil, nil, nil
	p.mu.Unlock()

	if pool == nil {
		return nil
	}

	stop()
	p.background.Wait()

	if owns {
		pool.Close()
	}

	return nil
}

func (p *Provider) db() (*pgxpool.Pool, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.pool == nil {
		return nil, core.ErrNotRunning
	}

	return p.pool, nil
}

// Health pings the database without exposing connection settings.
func (p *Provider) Health(ctx context.Context) error {
	pool, err := p.db()
	if err != nil {
		return err
	}

	return pool.Ping(ctx)
}

func hash(parts ...string) string {
	data, err := json.Marshal(parts)
	if err != nil {
		panic(err)
	}

	sum := sha256.Sum256(data)

	return hex.EncodeToString(sum[:16])
}

func streamKey(namespace, name string) string { return hash(namespace, name) }

// streamChannel is the NOTIFY channel for one stream: 35 bytes, under the 63-byte limit.
func streamChannel(key string) string { return "fc_" + key }

// sequence names the stream's own SEQUENCE, so sequence numbers start at 1 per stream.
func (p *Provider) sequence(key string) string {
	return pgx.Identifier{p.options.Schema, "forge_conduit_seq_" + key}.Sanitize()
}

// raced reports a concurrent-DDL or deadlock error that is safe to retry.
func raced(err error) bool {
	var pgErr *pgconn.PgError

	return errors.As(err, &pgErr) && (pgErr.Code == "23505" || pgErr.Code == "42P07" || pgErr.Code == "40P01" || pgErr.Code == "40001")
}

// classify maps a missing stream sequence to ErrNotFound.
func classify(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "42P01" {
		return core.ErrNotFound
	}

	return err
}

// EnsureStream creates the stream's sequence and record, and refuses a different declaration.
// Replicas is recorded and compared; replication itself belongs to your database.
func (p *Provider) EnsureStream(ctx context.Context, namespace string, cfg core.StreamConfig) error {
	pool, err := p.db()
	if err != nil {
		return err
	}

	cfg.Subjects = slices.Clone(cfg.Subjects)
	slices.Sort(cfg.Subjects)

	data, err := json.Marshal(cfg)
	if err != nil {
		return err
	}

	key := streamKey(namespace, cfg.Name)

	for attempt := 0; ; attempt++ {
		err = p.ensureStream(ctx, pool, namespace, key, cfg, data)
		if err != nil && attempt < 3 && raced(err) {
			continue
		}

		return err
	}
}

func (p *Provider) ensureStream(ctx context.Context, pool *pgxpool.Pool, namespace, key string, cfg core.StreamConfig, data []byte) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	// CREATE SEQUENCE IF NOT EXISTS needs CREATE on the schema even when the sequence is
	// there, so a runtime role without it can still declare a stream that already exists.
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, p.sequence(key)).Scan(&exists); err != nil {
		return err
	}

	if !exists {
		if _, err := tx.Exec(ctx, "CREATE SEQUENCE IF NOT EXISTS "+p.sequence(key)); err != nil {
			return err
		}
	}

	_, err = tx.Exec(ctx, fmt.Sprintf(`INSERT INTO %s (stream_key, namespace, name, config, max_age_ms, max_messages)
VALUES ($1, $2, $3, $4, $5, $6) ON CONFLICT (stream_key) DO NOTHING`, p.t.streams),
		key, namespace, cfg.Name, data, cfg.MaxAge.Milliseconds(), cfg.MaxMessages)
	if err != nil {
		return err
	}

	var raw []byte
	if err := tx.QueryRow(ctx, fmt.Sprintf(`SELECT config::text FROM %s WHERE stream_key = $1`, p.t.streams), key).Scan(&raw); err != nil {
		return err
	}

	var stored core.StreamConfig
	if err := json.Unmarshal(raw, &stored); err != nil {
		return err
	}

	if !reflect.DeepEqual(stored, cfg) {
		return fmt.Errorf("%w: stream %s", core.ErrConflict, cfg.Name)
	}

	return tx.Commit(ctx)
}
