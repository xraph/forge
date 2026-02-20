package contributor

import (
	"testing"
)

func TestSSRContributor_Manifest(t *testing.T) {
	m := &Manifest{
		Name:        "test-ssr",
		DisplayName: "Test SSR",
		Version:     "1.0.0",
	}

	sc := NewSSRContributor(m, "dist/server/entry.mjs")
	if sc.Manifest() != m {
		t.Error("Manifest() should return the same manifest")
	}
}

func TestSSRContributor_NotStarted(t *testing.T) {
	m := &Manifest{Name: "test-ssr"}
	sc := NewSSRContributor(m, "entry.mjs")

	if sc.Started() {
		t.Error("should not be started initially")
	}

	if sc.Port() != 0 {
		t.Errorf("port should be 0 before start, got %d", sc.Port())
	}
}

func TestSSRContributor_StopBeforeStart(t *testing.T) {
	m := &Manifest{Name: "test-ssr"}
	sc := NewSSRContributor(m, "entry.mjs")

	// Stopping before start should be a no-op
	if err := sc.Stop(); err != nil {
		t.Errorf("Stop() before Start() should not error: %v", err)
	}
}

func TestSSRContributor_Options(t *testing.T) {
	m := &Manifest{Name: "test-ssr"}
	sc := NewSSRContributor(m, "entry.mjs",
		WithSSRWorkDir("/tmp/test"),
		WithSSREnv(map[string]string{"NODE_ENV": "production"}),
	)

	if sc.workDir != "/tmp/test" {
		t.Errorf("workDir = %q, want %q", sc.workDir, "/tmp/test")
	}

	if sc.env["NODE_ENV"] != "production" {
		t.Errorf("env[NODE_ENV] = %q, want %q", sc.env["NODE_ENV"], "production")
	}
}

func TestFreePort(t *testing.T) {
	port, err := freePort()
	if err != nil {
		t.Fatalf("freePort() error: %v", err)
	}

	if port <= 0 {
		t.Errorf("freePort() = %d, want positive port", port)
	}
}
