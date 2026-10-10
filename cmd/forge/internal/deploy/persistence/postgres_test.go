//go:build integration

package persistence

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestPostgresAuthorityContracts(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Fatal("Docker is required for PostgreSQL acceptance")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	name := "forge-deploy-postgres-" + time.Now().UTC().Format("20060102150405.000000000")
	name = strings.ReplaceAll(name, ".", "-")
	command := exec.CommandContext(ctx, "docker", "run", "-d", "--name", name, "-p", "127.0.0.1::5432", "-e", "POSTGRES_PASSWORD=acceptance-fixture", "postgres:16")

	raw, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("owned PostgreSQL fixture: %v %s", err, raw)
	}

	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if raw, err := exec.CommandContext(cleanup, "docker", "rm", "-f", "-v", name).CombinedOutput(); err != nil {
			t.Errorf("fixture cleanup: %v %s", err, raw)
		}
	})

	port, err := exec.CommandContext(ctx, "docker", "inspect", "--format", `{{(index (index .NetworkSettings.Ports "5432/tcp") 0).HostPort}}`, name).Output()
	if err != nil {
		t.Fatal(err)
	}

	dsn := "postgres://postgres:acceptance-fixture@127.0.0.1:" + strings.TrimSpace(string(port)) + "/postgres?sslmode=disable&connect_timeout=2"
	t.Setenv("FORGE_DEPLOY_POSTGRES_IT", dsn)
	root := t.TempDir()

	var ready *DB

	deadline := time.Now().Add(time.Minute)
	for time.Now().Before(deadline) {
		ready, err = Open(ctx, root, Options{Backend: "postgres", Reference: "env:FORGE_DEPLOY_POSTGRES_IT", Project: "qualification"})
		if err == nil {
			break
		}

		timer := time.NewTimer(200 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			t.Fatal(ctx.Err())
		case <-timer.C:
		}
	}

	if err != nil {
		t.Fatal("PostgreSQL fixture did not become ready", err)
	}

	defer ready.Close()

	for _, contract := range []struct {
		name  string
		check func(*testing.T, *DB)
	}{{"revision", settingsContract}, {"fencing", leaseContract}, {"cancellation", cancellationContract}, {"journal", journalContract}, {"retirement", retirementContract}} {
		t.Run(contract.name, func(t *testing.T) {
			db, err := Open(ctx, root, Options{Backend: "postgres", Reference: "env:FORGE_DEPLOY_POSTGRES_IT", Project: contract.name})
			if err != nil {
				t.Fatal(err)
			}

			t.Cleanup(func() { _ = db.Close() })
			contract.check(t, db)
		})
	}

	if _, err := ready.sql.ExecContext(ctx, "CREATE ROLE forge_denied LOGIN PASSWORD 'denied-fixture'"); err != nil {
		t.Fatal(err)
	}

	t.Setenv("FORGE_DEPLOY_POSTGRES_DENIED", strings.Replace(dsn, "postgres:acceptance-fixture@", "forge_denied:denied-fixture@", 1))

	denied, err := Open(ctx, root, Options{Backend: "postgres", Reference: "env:FORGE_DEPLOY_POSTGRES_DENIED", Project: "denied"})
	if err == nil {
		_ = denied.Close()

		t.Fatal("denied schema access accepted")
	}

	if !errors.Is(err, ErrUnavailable) || strings.Contains(err.Error(), "denied-fixture") {
		t.Fatal("denial details leak credentials", err)
	}
}
