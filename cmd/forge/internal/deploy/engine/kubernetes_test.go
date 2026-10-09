package engine

import (
	"context"
	"github.com/xraph/forge/cmd/forge/internal/deploy/state"
	"github.com/xraph/forge/cmd/forge/internal/deploy/testdata"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestKubernetesPlanUsesRegisteredCapabilitiesAndApprovedRevision(t *testing.T) {
	root := testdata.Copy(t, "atlas-v2")
	path := filepath.Join(root, ".forge.yml")

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	raw = []byte(strings.Replace(string(raw), "{ provider: compose }", "{ provider: kubernetes, context: test, namespace: atlas-dev }", 1))
	if err := os.WriteFile(path, raw, 0644); err != nil {
		t.Fatal(err)
	}

	e := newEngine(t, root)

	st, err := state.Open(root, "local", "dev")
	if err != nil {
		t.Fatal(err)
	}

	if err := st.SaveSnapshot(state.Snapshot{Revision: 7}); err != nil {
		t.Fatal(err)
	}

	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	p, b, err := e.Plan(context.Background(), "local", "dev")
	if err != nil {
		t.Fatal(err)
	}

	if p.Deployment.Revision != 8 {
		t.Fatal("plan lacks next fenced revision", p.Deployment.Revision)
	}

	if !strings.Contains(string(b.Files["migrations/api.yaml"].Content), "api-migrate-r8") {
		t.Fatal("migration uses an old Job")
	}
}
