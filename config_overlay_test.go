package forge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTemp(t *testing.T, dir, name, body string) string {
	t.Helper()

	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	return p
}

func TestConfigOverlayFileWinsOverLocal(t *testing.T) {
	dir := t.TempDir()
	writeTemp(t, dir, "config.yaml", "a: 1\nb: base\n")
	writeTemp(t, dir, "config.local.yaml", "b: local\n")
	ov := writeTemp(t, t.TempDir(), "overlay.yaml", "b: overlay\nc: ${OVL_TEST_C}\n")
	t.Setenv("FORGE_CONFIG_OVERLAY", ov)
	t.Setenv("OVL_TEST_C", "expanded")

	app := New(WithAppName("ovlapp"), WithConfigSearchPaths(dir))
	if got := app.Config().Get("b"); got != "overlay" {
		t.Fatalf("b = %v", got)
	}

	if got := app.Config().Get("c"); got != "expanded" {
		t.Fatalf("c = %v", got)
	}

	if got := app.Config().Get("a"); got != 1 {
		t.Fatalf("a = %v (%T)", got, got)
	}
}

func TestConfigOverlayInlineYAML(t *testing.T) {
	dir := t.TempDir()
	writeTemp(t, dir, "config.yaml", "a: 1\n")
	t.Setenv("FORGE_CONFIG_OVERLAY_YAML", "a: 2\n")

	app := New(WithAppName("ovlapp"), WithConfigSearchPaths(dir))
	if got := app.Config().Get("a"); got != 2 {
		t.Fatalf("a = %v", got)
	}
}

func TestConfigOverlayMissingFileFails(t *testing.T) {
	dir := t.TempDir()
	writeTemp(t, dir, "config.yaml", "a: 1\n")
	t.Setenv("FORGE_CONFIG_OVERLAY", filepath.Join(dir, "nope.yaml"))

	defer func() {
		r := recover()

		if r == nil {
			t.Fatal("expected New to panic on a missing overlay")
		}

		if s, ok := r.(string); !ok || !strings.Contains(s, "FORGE_CONFIG_OVERLAY") || !strings.Contains(s, "nope.yaml") {
			t.Fatalf("panic message: %v", r)
		}
	}()

	_ = New(WithAppName("ovlapp"), WithConfigSearchPaths(dir))
}

func TestEnvStillWinsOverOverlay(t *testing.T) {
	dir := t.TempDir()
	writeTemp(t, dir, "config.yaml", "a: 1\n")
	ov := writeTemp(t, t.TempDir(), "overlay.yaml", "a: 2\n")
	t.Setenv("FORGE_CONFIG_OVERLAY", ov)
	t.Setenv("OVLAPP_A", "3")

	app := New(WithAppName("ovlapp"), WithConfigSearchPaths(dir))
	if got := app.Config().Get("a"); got != "3" && got != 3 {
		t.Fatalf("a = %v", got)
	}
}

func TestInlineOverlayExpandsDefaultWithoutEnvironment(t *testing.T) {
	t.Setenv("FORGE_CONFIG_OVERLAY", "")
	t.Setenv("FORGE_CONFIG_OVERLAY_YAML", "store: {endpoint: '${OVL_ENDPOINT:-http://store:9000}'}\n")
	t.Setenv("OVL_ENDPOINT", "")

	app := New(WithAppName("overlay-default"), WithConfigSearchPaths(t.TempDir()))
	if got := app.Config().Get("store.endpoint"); got != "http://store:9000" {
		t.Fatalf("default endpoint = %v", got)
	}
}
