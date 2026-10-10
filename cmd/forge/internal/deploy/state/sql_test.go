package state

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/xraph/forge/cmd/forge/internal/deploy/persistence"
)

func sqlStateRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	raw := []byte("project: {name: atlas, module: example.com/atlas}\ndeploy: {version: 2}\n")
	if err := os.WriteFile(filepath.Join(root, ".forge.yml"), raw, 0600); err != nil {
		t.Fatal(err)
	}

	if err := persistence.Configure(context.Background(), root, persistence.Options{Backend: "sqlite", Reference: ".forge/deploy.db"}, persistence.Hash(raw)); err != nil {
		t.Fatal(err)
	}

	return root
}
func TestSQLStoreReloadsSnapshotAndJournalWithoutFileFallback(t *testing.T) {
	root := sqlStateRoot(t)

	st, err := Open(root, "local", "dev")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	if err := st.SaveSnapshot(Snapshot{Revision: 1}); !errors.Is(err, ErrLocked) {
		t.Fatal("SQL write accepted without a lease", err)
	}

	unlock, err := st.Lock(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if err := st.SaveSnapshot(Snapshot{Revision: 7, Status: StatusHealthy}); err != nil {
		t.Fatal(err)
	}

	if err := st.Journal().Record(Event{Op: "rollout:api", Status: StatusAccepted, IdempotencyKey: "approved:api"}); err != nil {
		t.Fatal(err)
	}

	if err := st.WriteFile("generated.env", []byte("TOKEN=private")); err != nil {
		t.Fatal(err)
	}

	unlock()
	// Stale file bytes are deliberately different from the selected authority.
	if err := os.WriteFile(filepath.Join(st.Dir(), "snapshot.json"), []byte(`{"revision":100}`), 0600); err != nil {
		t.Fatal(err)
	}

	next, err := Open(root, "local", "dev")
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()

	snap, err := next.Snapshot()
	if err != nil || snap.Revision != 7 {
		t.Fatal("SQL state did not reload", snap, err)
	}

	if _, completed := next.Journal().Completed("approved:api"); !completed {
		t.Fatal("journal lost")
	}

	db, err := persistence.OpenSelected(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if _, err := db.Read(context.Background(), "local/dev", "generated.env"); !os.IsNotExist(err) {
		t.Fatal("private credential persisted in database", err)
	}
}
func TestAuthoritySwitchRejectsActiveFileAndSQLApply(t *testing.T) {
	for _, backend := range []string{"files", "sqlite"} {
		t.Run(backend, func(t *testing.T) {
			root := t.TempDir()

			raw := []byte("project: {name: atlas}\ndeploy: {version: 2}\n")
			if err := os.WriteFile(filepath.Join(root, ".forge.yml"), raw, 0600); err != nil {
				t.Fatal(err)
			}

			if backend == "sqlite" {
				if err := persistence.Configure(context.Background(), root, persistence.Options{Backend: "sqlite", Reference: ".forge/deploy.db"}, persistence.Hash(raw)); err != nil {
					t.Fatal(err)
				}
			}

			st, err := Open(root, "local", "dev")
			if err != nil {
				t.Fatal(err)
			}
			defer st.Close()

			unlock, err := st.Lock(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			defer unlock()

			err = persistence.Configure(context.Background(), root, persistence.Options{Backend: "sqlite", Reference: ".forge/next.db"}, persistence.Hash(raw))
			if !errors.Is(err, persistence.ErrLocked) {
				t.Fatal("authority changed during apply", err)
			}
		})
	}
}
func TestSQLFenceLossCancelsStoreContextAndRejectsJournal(t *testing.T) {
	root := sqlStateRoot(t)

	st, err := Open(root, "local", "dev")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	unlock, err := st.Lock(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()

	ctx := st.Context(context.Background())

	db, err := sql.Open("sqlite", filepath.Join(root, ".forge/deploy.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if _, err := db.ExecContext(context.Background(), "UPDATE forge_deploy_leases SET expires=0 WHERE scope=$1", "local/dev"); err != nil {
		t.Fatal(err)
	}

	if err := st.Journal().Record(Event{Op: "late"}); !errors.Is(err, persistence.ErrLeaseLost) {
		t.Fatal("stale journal write", err)
	}

	select {
	case <-ctx.Done():
	default:
		t.Fatal("active command not cancelled")
	}
}
