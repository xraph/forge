package persistence

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"sync"
	"time"
)

const leaseTTL = 60 * time.Second
const renewalInterval = 15 * time.Second

type Lease struct {
	db        *DB
	Scope     string
	Fence     uint64
	owner     string
	done      <-chan struct{}
	authority *Lease
	cancel    context.CancelCauseFunc
	stop      chan struct{}
	once      sync.Once
	mu        sync.Mutex
	children  map[context.Context]context.CancelCauseFunc
}

func (d *DB) Acquire(ctx context.Context, scope string) (*Lease, error) {
	if err := d.Active(ctx); err != nil {
		return nil, err
	}

	token := make([]byte, 16)
	if _, err := rand.Read(token); err != nil {
		return nil, err
	}

	owner := hex.EncodeToString(token)

	if _, err := d.sql.ExecContext(ctx, "INSERT INTO forge_deploy_leases(project,scope,fence,owner,expires) VALUES($1,$2,0,'',0) ON CONFLICT(project,scope) DO NOTHING", d.options.Project, scope); err != nil {
		return nil, ErrUnavailable
	}

	query := d.leaseQuery("acquire")

	var fence uint64

	err := d.sql.QueryRowContext(ctx, query, owner, leaseTTL.Milliseconds(), d.options.Project, scope).Scan(&fence)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrLocked
	}

	if err != nil {
		return nil, ErrUnavailable
	}

	leaseCtx, cancel := context.WithCancelCause(context.Background())

	lease := &Lease{db: d, Scope: scope, Fence: fence, owner: owner, done: leaseCtx.Done(), cancel: cancel, stop: make(chan struct{})}
	go lease.heartbeat()

	return lease, nil
}
func (l *Lease) heartbeat() {
	ticker := time.NewTicker(renewalInterval)
	defer ticker.Stop()

	for {
		select {
		case <-l.stop:
			return
		case <-l.done:
			return
		case <-ticker.C:
			ctx, cancel := bounded()
			err := l.Renew(ctx)

			cancel()

			if err != nil {
				return
			}
		}
	}
}
func (l *Lease) Context(parent context.Context) context.Context {
	ctx, cancel := context.WithCancelCause(parent)

	l.mu.Lock()
	if l.lost() {
		cancel(ErrLeaseLost)
	} else {
		if l.children == nil {
			l.children = map[context.Context]context.CancelCauseFunc{}
		}

		l.children[ctx] = cancel
	}
	l.mu.Unlock()
	context.AfterFunc(ctx, func() { l.mu.Lock(); delete(l.children, ctx); l.mu.Unlock() })

	return ctx
}
func (l *Lease) invalidate() {
	l.cancel(ErrLeaseLost)
	l.mu.Lock()
	for _, cancel := range l.children {
		cancel(ErrLeaseLost)
	}

	clear(l.children)
	l.mu.Unlock()
}

func (l *Lease) Renew(ctx context.Context) error {
	result, err := l.db.sql.ExecContext(ctx, l.db.leaseQuery("renew"), leaseTTL.Milliseconds(), l.db.options.Project, l.Scope, l.owner, l.Fence)

	err = changed(result, err, ErrLeaseLost)
	if err != nil {
		l.invalidate()
	}

	return err
}
func (l *Lease) Release() error {
	var releaseErr error

	l.once.Do(func() {
		close(l.stop)
		l.invalidate()

		ctx, cancel := bounded()
		defer cancel()

		_, err := l.db.sql.ExecContext(ctx, "UPDATE forge_deploy_leases SET owner='',expires=0 WHERE project=$1 AND scope=$2 AND owner=$3 AND fence=$4", l.db.options.Project, l.Scope, l.owner, l.Fence)
		if err != nil {
			releaseErr = ErrUnavailable
		}
	})

	return releaseErr
}
func (l *Lease) guard(ctx context.Context, tx *sql.Tx) error {
	l.mu.Lock()
	gate := l.authority
	l.mu.Unlock()

	if gate != nil {
		if err := gate.guard(ctx, tx); err != nil {
			l.invalidate()

			return err
		}
	}

	if l.lost() {
		return ErrLeaseLost
	}

	result, err := tx.ExecContext(ctx, l.db.leaseQuery("guard"), l.db.options.Project, l.Scope, l.owner, l.Fence)

	failure := changed(result, err, ErrLeaseLost)
	if failure != nil {
		l.invalidate()
	}

	return failure
}
func (l *Lease) Write(ctx context.Context, name string, raw []byte) error {
	tx, err := l.db.sql.BeginTx(ctx, nil)
	if err != nil {
		return ErrUnavailable
	}
	defer func() { _ = tx.Rollback() }()

	if err := l.guard(ctx, tx); err != nil {
		return err
	}

	_, err = tx.ExecContext(ctx, "INSERT INTO forge_deploy_blobs(project,scope,name,content) VALUES($1,$2,$3,$4) ON CONFLICT(project,scope,name) DO UPDATE SET content=excluded.content", l.db.options.Project, l.Scope, name, raw)
	if err != nil {
		return ErrUnavailable
	}

	if err := tx.Commit(); err != nil {
		return ErrUnavailable
	}

	return nil
}
func (l *Lease) Append(ctx context.Context, name string, raw []byte) error {
	tx, err := l.db.sql.BeginTx(ctx, nil)
	if err != nil {
		return ErrUnavailable
	}
	defer func() { _ = tx.Rollback() }()

	if err := l.guard(ctx, tx); err != nil {
		return err
	}

	var prior []byte

	err = tx.QueryRowContext(ctx, "SELECT content FROM forge_deploy_blobs WHERE project=$1 AND scope=$2 AND name=$3", l.db.options.Project, l.Scope, name).Scan(&prior)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return ErrUnavailable
	}

	_, err = tx.ExecContext(ctx, "INSERT INTO forge_deploy_blobs(project,scope,name,content) VALUES($1,$2,$3,$4) ON CONFLICT(project,scope,name) DO UPDATE SET content=excluded.content", l.db.options.Project, l.Scope, name, append(prior, raw...))
	if err != nil {
		return ErrUnavailable
	}

	if err := tx.Commit(); err != nil {
		return ErrUnavailable
	}

	return nil
}

func (l *Lease) GuardedBy(gate *Lease) error {
	if gate == nil || gate == l || gate.db != l.db || gate.Scope != "authority" {
		return errors.New("invalid deployment authority guard")
	}

	l.mu.Lock()
	l.authority = gate
	l.mu.Unlock()

	return nil
}
func (l *Lease) lost() bool {
	select {
	case <-l.done:
		return true
	default:
		return false
	}
}

func (l *Lease) SaveSettings(ctx context.Context, expected uint64, raw []byte) error {
	if l.Scope != "authority" {
		return errors.New("settings require an authority lease")
	}

	tx, err := l.db.sql.BeginTx(ctx, nil)
	if err != nil {
		return ErrUnavailable
	}

	defer func() { _ = tx.Rollback() }()

	if err := l.guard(ctx, tx); err != nil {
		return err
	}

	result, err := tx.ExecContext(ctx, "UPDATE forge_deploy_settings SET revision=revision+1,content=$1 WHERE project=$2 AND revision=$3", raw, l.db.options.Project, expected)
	if err := changed(result, err, ErrConflict); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return ErrUnavailable
	}

	return nil
}
