package persistence

import (
	"context"
	"errors"
	"testing"
)

func sqliteFixture(t *testing.T) *DB {
	t.Helper()

	db, err := Open(context.Background(), t.TempDir(), Options{Backend: "sqlite", Reference: ".forge/deploy.db", Project: "atlas"})
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = db.Close() })

	return db
}
func acquire(t *testing.T, db *DB, scope string) *Lease {
	t.Helper()

	lease, err := db.Acquire(context.Background(), scope)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = lease.Release() })

	return lease
}
func TestSettingsRevisionCAS(t *testing.T) { settingsContract(t, sqliteFixture(t)) }
func settingsContract(t *testing.T, db *DB) {
	if err := db.SaveSettings(context.Background(), 0, []byte("deploy: {value: first}\n")); err != nil {
		t.Fatal(err)
	}

	if err := db.SaveSettings(context.Background(), 0, []byte("deploy: {value: stale}\n")); !errors.Is(err, ErrConflict) {
		t.Fatal("stale editor overwrote settings", err)
	}

	revision, raw, err := db.Settings(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if revision != 1 || string(raw) != "deploy: {value: first}\n" {
		t.Fatal(revision, string(raw))
	}

	if err := db.SaveSettings(context.Background(), revision, []byte("deploy: {value: second}\n")); err != nil {
		t.Fatal(err)
	}
}
func TestConcurrentLeaseAndExpiredOwnerFence(t *testing.T) { leaseContract(t, sqliteFixture(t)) }
func leaseContract(t *testing.T, db *DB) {
	old := acquire(t, db, "local/dev")
	if _, err := db.Acquire(context.Background(), "local/dev"); !errors.Is(err, ErrLocked) {
		t.Fatal("duplicate lease acquired", err)
	}

	if err := old.Write(context.Background(), "snapshot.json", []byte("first")); err != nil {
		t.Fatal(err)
	}

	if _, err := db.sql.ExecContext(context.Background(), "UPDATE forge_deploy_leases SET expires=0 WHERE scope=$1", "local/dev"); err != nil {
		t.Fatal(err)
	}

	next := acquire(t, db, "local/dev")
	if next.Fence <= old.Fence {
		t.Fatal("fencing token reused")
	}

	if err := old.Write(context.Background(), "snapshot.json", []byte("stale")); !errors.Is(err, ErrLeaseLost) {
		t.Fatal("expired owner wrote", err)
	}

	if err := old.Renew(context.Background()); !errors.Is(err, ErrLeaseLost) {
		t.Fatal("expired owner renewed", err)
	}

	if err := next.Write(context.Background(), "snapshot.json", []byte("next")); err != nil {
		t.Fatal(err)
	}

	raw, err := db.Read(context.Background(), "local/dev", "snapshot.json")
	if err != nil || string(raw) != "next" {
		t.Fatal("state lost", err, string(raw))
	}

	if err := old.Release(); err != nil {
		t.Fatal(err)
	}

	if _, err := db.Acquire(context.Background(), "local/dev"); !errors.Is(err, ErrLocked) {
		t.Fatal("stale release unlocked next owner", err)
	}
}
func TestLeaseLossCancelsActiveContext(t *testing.T) { cancellationContract(t, sqliteFixture(t)) }
func cancellationContract(t *testing.T, db *DB) {
	lease := acquire(t, db, "local/dev")
	ctx := lease.Context(context.Background())

	if _, err := db.sql.ExecContext(context.Background(), "UPDATE forge_deploy_leases SET expires=0 WHERE scope=$1", "local/dev"); err != nil {
		t.Fatal(err)
	}

	if err := lease.Renew(context.Background()); !errors.Is(err, ErrLeaseLost) {
		t.Fatal(err)
	}

	select {
	case <-ctx.Done():
	default:
		t.Fatal("active command context not cancelled")
	}

	if !errors.Is(context.Cause(ctx), ErrLeaseLost) {
		t.Fatal("lease loss was reported as caller cancellation", context.Cause(ctx))
	}
}
func TestJournalAppendIsFencedAndDurable(t *testing.T) { journalContract(t, sqliteFixture(t)) }
func journalContract(t *testing.T, db *DB) {
	lease := acquire(t, db, "local/dev")
	if err := lease.Append(context.Background(), "journal.jsonl", []byte("first\n")); err != nil {
		t.Fatal(err)
	}

	if err := lease.Append(context.Background(), "journal.jsonl", []byte("second\n")); err != nil {
		t.Fatal(err)
	}

	raw, err := db.Read(context.Background(), "local/dev", "journal.jsonl")
	if err != nil || string(raw) != "first\nsecond\n" {
		t.Fatal(string(raw), err)
	}
}

func TestSecondaryLeaseCannotWriteAfterAuthorityFenceLoss(t *testing.T) {
	db := sqliteFixture(t)
	authority := acquire(t, db, "authority")

	lease := acquire(t, db, "local/dev")
	if err := lease.GuardedBy(authority); err != nil {
		t.Fatal(err)
	}

	if _, err := db.sql.ExecContext(context.Background(), "UPDATE forge_deploy_leases SET expires=0 WHERE scope=$1", "authority"); err != nil {
		t.Fatal(err)
	}

	next := acquire(t, db, "authority")
	if next.Fence <= authority.Fence {
		t.Fatal("authority fence reused")
	}

	if err := lease.Write(context.Background(), "snapshot.json", []byte("late")); !errors.Is(err, ErrLeaseLost) {
		t.Fatal("environment owner bypassed changed authority", err)
	}
}

func TestRetiredAuthorityRejectsMutations(t *testing.T) { retirementContract(t, sqliteFixture(t)) }
func retirementContract(t *testing.T, db *DB) {
	gate := acquire(t, db, "authority")

	lease := acquire(t, db, "local/dev")
	if err := lease.GuardedBy(gate); err != nil {
		t.Fatal(err)
	}

	if err := gate.retire(context.Background()); err != nil {
		t.Fatal(err)
	}

	if _, err := db.Acquire(context.Background(), "other/dev"); !errors.Is(err, ErrRetired) {
		t.Fatal("retired authority acquired", err)
	}

	if err := lease.Write(context.Background(), "snapshot.json", []byte("stale")); !errors.Is(err, ErrLeaseLost) {
		t.Fatal("retired authority wrote", err)
	}

	if err := gate.Renew(context.Background()); !errors.Is(err, ErrLeaseLost) {
		t.Fatal("retired authority renewed", err)
	}
}
