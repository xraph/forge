package observability

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestParseTarget(t *testing.T) {
	got, err := ParseTarget("api=10.0.0.5:8080")
	if err != nil || got.Name != "api" || got.Address != "10.0.0.5:8080" || got.Rewritten {
		t.Fatalf("got %+v, %v", got, err)
	}

	for _, bad := range []string{"api", "=host:1", "api=host", "api=:8080", "api=host:0", "api=host:99999", "api=host:x"} {
		if _, err := ParseTarget(bad); !errors.Is(err, ErrInvalidTarget) {
			t.Errorf("ParseTarget(%q) = %v", bad, err)
		}
	}
}

func TestParseTargetRewritesLoopback(t *testing.T) {
	for _, in := range []string{"api=localhost:8080", "api=127.0.0.1:8080", "api=[::1]:8080"} {
		got, err := ParseTarget(in)
		if err != nil || got.Address != "host.docker.internal:8080" || !got.Rewritten {
			t.Errorf("%s: got %+v, %v", in, got, err)
		}
	}
}

func TestWriteTargetSanitizesFileNameKeepsJob(t *testing.T) {
	dir := t.TempDir()
	if err := WriteTarget(dir, Target{Name: "api/v2 beta", Address: "host.docker.internal:8080"}); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, "targets", "api-v2-beta.json"))
	if err != nil {
		t.Fatal(err)
	}

	var groups []struct {
		Targets []string          `json:"targets"`
		Labels  map[string]string `json:"labels"`
	}
	if err := json.Unmarshal(raw, &groups); err != nil {
		t.Fatal(err)
	}

	if len(groups) != 1 || groups[0].Targets[0] != "host.docker.internal:8080" || groups[0].Labels["job"] != "api/v2 beta" {
		t.Fatalf("groups = %+v", groups)
	}

	if err := RemoveTarget(dir, "api/v2 beta"); err != nil {
		t.Fatal(err)
	}

	if err := RemoveTarget(dir, "api/v2 beta"); err != nil {
		t.Fatalf("second remove should be a no-op: %v", err)
	}

	if entries, _ := os.ReadDir(filepath.Join(dir, "targets")); len(entries) != 0 {
		t.Fatalf("left behind: %v", entries)
	}
}
