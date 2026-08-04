// v2/cmd/forge/plugins/client_diff_test.go
package plugins

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xraph/forge/cli"
)

// These drive the real `forge client diff` command over two real spec files on
// disk, because the exit code is the whole product here: a CI job gates on it,
// and a test that called the classifier directly would never notice the command
// mapping a bucket onto the wrong code.

// writeTwoSpecs writes an old and a new spec into a temp directory and returns
// their paths.
func writeTwoSpecs(t *testing.T, oldDoc, newDoc map[string]any) (oldPath, newPath string) {
	t.Helper()

	dir := t.TempDir()

	oldPath = filepath.Join(dir, "old.json")
	newPath = filepath.Join(dir, "new.json")

	writeSpecFile(t, oldPath, oldDoc)
	writeSpecFile(t, newPath, newDoc)

	return oldPath, newPath
}

// renameOrderEntity rewrites the Order component to PurchaseOrder without
// touching a single path, method, status code or field name. The HTTP contract
// is byte-identical; only the cache partitioning changes.
func renameOrderEntity(doc map[string]any) map[string]any {
	schemas := doc["components"].(map[string]any)["schemas"].(map[string]any)
	schemas["PurchaseOrder"] = schemas["Order"]

	delete(schemas, "Order")

	raw, err := json.Marshal(doc["paths"])
	if err != nil {
		panic(err)
	}

	rewritten := strings.ReplaceAll(string(raw), "#/components/schemas/Order", "#/components/schemas/PurchaseOrder")

	var paths any
	if err := json.Unmarshal([]byte(rewritten), &paths); err != nil {
		panic(err)
	}

	doc["paths"] = paths

	return doc
}

func TestClientDiffIdenticalSpecsExitZero(t *testing.T) {
	oldPath, newPath := writeTwoSpecs(t, ordersSpec(), ordersSpec())

	out, err := runClientCLI(t, "client", "diff", oldPath, newPath)
	if err != nil {
		t.Fatalf("identical specs should exit 0, got %v (exit %d)\n%s", err, cli.GetExitCode(err), out)
	}

	if !strings.Contains(out, "No changes.") {
		t.Fatalf("identical specs should report no changes, printed:\n%s", out)
	}
}

func TestClientDiffCompatibleChangesExitZero(t *testing.T) {
	newDoc := ordersSpec()
	newDoc["paths"].(map[string]any)["/invoices"] = map[string]any{
		"get": map[string]any{
			"operationId": "invoiceList",
			"responses":   map[string]any{"200": map[string]any{"description": "ok"}},
		},
	}

	oldPath, newPath := writeTwoSpecs(t, ordersSpec(), newDoc)

	out, err := runClientCLI(t, "client", "diff", oldPath, newPath)
	if err != nil {
		t.Fatalf("compatible-only changes should exit 0, got %v (exit %d)\n%s", err, cli.GetExitCode(err), out)
	}

	if !strings.Contains(out, "COMPATIBLE") || !strings.Contains(out, "added endpoint") {
		t.Fatalf("expected the added endpoint in the compatible section, printed:\n%s", out)
	}
}

func TestClientDiffBreakingAPIExitsOne(t *testing.T) {
	newDoc := ordersSpec()
	delete(newDoc["paths"].(map[string]any), "/orders")

	oldPath, newPath := writeTwoSpecs(t, ordersSpec(), newDoc)

	out, err := runClientCLI(t, "client", "diff", oldPath, newPath)
	if err == nil {
		t.Fatalf("a removed endpoint should exit non-zero, printed:\n%s", out)
	}

	if code := cli.GetExitCode(err); code != cli.ExitError {
		t.Fatalf("exit code = %d, want %d (breaking)", code, cli.ExitError)
	}

	if !strings.Contains(out, "BREAKING (API)") || !strings.Contains(out, "removed endpoint") {
		t.Fatalf("expected a breaking API section, printed:\n%s", out)
	}
}

// Adding a required query parameter is the most routine breaking change there
// is, and a hard signature break for a generated client. The first cut of the
// differ never looked at parameters at all, so this exact spec pair printed "No
// changes." and exited 0 -- a gate greenlighting a break, which is worse than
// no gate because teams stop looking.
func TestClientDiffAddedRequiredQueryParameterExitsOne(t *testing.T) {
	newDoc := ordersSpec()
	newDoc["paths"].(map[string]any)["/orders"].(map[string]any)["get"].(map[string]any)["parameters"] = []any{
		map[string]any{
			"name":     "tenant",
			"in":       "query",
			"required": true,
			"schema":   map[string]any{"type": "string"},
		},
	}

	oldPath, newPath := writeTwoSpecs(t, ordersSpec(), newDoc)

	out, err := runClientCLI(t, "client", "diff", oldPath, newPath)
	if err == nil {
		t.Fatalf("an added required query parameter must not exit 0, printed:\n%s", out)
	}

	if code := cli.GetExitCode(err); code != cli.ExitError {
		t.Fatalf("exit code = %d, want %d (breaking)", code, cli.ExitError)
	}

	if !strings.Contains(out, `added required query parameter "tenant"`) {
		t.Fatalf("expected the parameter named in the breaking section, printed:\n%s", out)
	}
}

// The case the third column exists for. Nothing about the wire format changes,
// so an API-only differ exits 0 here and the cache defect ships.
func TestClientDiffEntityRenameExitsOneOnCacheBreak(t *testing.T) {
	oldPath, newPath := writeTwoSpecs(t, ordersSpec(), renameOrderEntity(ordersSpec()))

	out, err := runClientCLI(t, "client", "diff", oldPath, newPath)
	if err == nil {
		t.Fatalf("an entity rename should exit non-zero, printed:\n%s", out)
	}

	if code := cli.GetExitCode(err); code != cli.ExitError {
		t.Fatalf("exit code = %d, want %d (breaking)", code, cli.ExitError)
	}

	if !strings.Contains(out, "BREAKING (CACHE)") {
		t.Fatalf("expected a breaking cache section, printed:\n%s", out)
	}

	if !strings.Contains(out, "entity typename changed Order -> PurchaseOrder") {
		t.Fatalf("expected the rename named explicitly, printed:\n%s", out)
	}

	if strings.Contains(out, "BREAKING (API)") {
		t.Fatalf("a rename breaks no HTTP contract; nothing belongs in the API section:\n%s", out)
	}
}

// An unclassifiable change with nothing breaking gets its own exit code: it is
// not a break, but it is not a green light either.
func TestClientDiffUnknownOnlyExitsThree(t *testing.T) {
	newDoc := ordersSpec()
	newDoc["components"].(map[string]any)["schemas"].(map[string]any)["Order"].(map[string]any)["properties"].(map[string]any)["total"] = map[string]any{
		"type":       "object",
		"properties": map[string]any{"amount": map[string]any{"type": "integer"}},
	}

	oldPath, newPath := writeTwoSpecs(t, ordersSpec(), newDoc)

	out, err := runClientCLI(t, "client", "diff", oldPath, newPath)
	if err == nil {
		t.Fatalf("an unclassifiable change should not exit 0, printed:\n%s", out)
	}

	if code := cli.GetExitCode(err); code != cli.ExitInternalError {
		t.Fatalf("exit code = %d, want %d (unclassified)", code, cli.ExitInternalError)
	}

	if !strings.Contains(out, "UNKNOWN") {
		t.Fatalf("expected an UNKNOWN section, printed:\n%s", out)
	}
}

func TestClientDiffJSONFormatIsParseable(t *testing.T) {
	oldPath, newPath := writeTwoSpecs(t, ordersSpec(), renameOrderEntity(ordersSpec()))

	out, _ := runClientCLI(t, "client", "diff", oldPath, newPath, "--format", "json")

	var report struct {
		Changes []struct {
			Kind     string `json:"kind"`
			Category string `json:"category"`
			Subject  string `json:"subject"`
			Detail   string `json:"detail"`
		} `json:"changes"`
		Summary struct {
			Compatible    int `json:"compatible"`
			BreakingAPI   int `json:"breaking_api"`
			BreakingCache int `json:"breaking_cache"`
			Unknown       int `json:"unknown"`
			Total         int `json:"total"`
		} `json:"summary"`
	}

	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &report); err != nil {
		t.Fatalf("--format json must emit parseable JSON on stdout: %v\n%s", err, out)
	}

	if report.Summary.BreakingCache == 0 {
		t.Fatalf("expected cache breaks in the summary: %+v", report.Summary)
	}

	if report.Summary.BreakingAPI != 0 {
		t.Fatalf("a rename breaks no HTTP contract: %+v", report.Summary)
	}

	if report.Summary.Total != len(report.Changes) {
		t.Fatalf("summary total %d does not match %d changes", report.Summary.Total, len(report.Changes))
	}
}

// Two runs over the same pair of specs must produce byte-identical output --
// this gets pasted into pull requests and diffed against the previous run.
func TestClientDiffOutputIsStableAcrossRuns(t *testing.T) {
	oldPath, newPath := writeTwoSpecs(t, ordersSpec(), renameOrderEntity(ordersSpec()))

	first, _ := runClientCLI(t, "client", "diff", oldPath, newPath)

	for i := 0; i < 5; i++ {
		again, _ := runClientCLI(t, "client", "diff", oldPath, newPath)
		if again != first {
			t.Fatalf("run %d differs:\n--- first ---\n%s--- again ---\n%s", i, first, again)
		}
	}
}

func TestClientDiffRejectsWrongArgumentCount(t *testing.T) {
	oldPath, _ := writeTwoSpecs(t, ordersSpec(), ordersSpec())

	_, err := runClientCLI(t, "client", "diff", oldPath)
	if err == nil {
		t.Fatal("diff with one argument should have failed")
	}

	if code := cli.GetExitCode(err); code != cli.ExitUsageError {
		t.Fatalf("exit code = %d, want %d (usage)", code, cli.ExitUsageError)
	}
}

// A spec that cannot be read is a usage error, not a breaking change. Sharing
// exit code 1 with "breaking changes present" would have CI report a typo'd
// path as an API break.
func TestClientDiffUnreadableSpecIsUsageError(t *testing.T) {
	oldPath, _ := writeTwoSpecs(t, ordersSpec(), ordersSpec())

	_, err := runClientCLI(t, "client", "diff", oldPath, filepath.Join(t.TempDir(), "does-not-exist.json"))
	if err == nil {
		t.Fatal("diff against a missing spec should have failed")
	}

	if code := cli.GetExitCode(err); code != cli.ExitUsageError {
		t.Fatalf("exit code = %d, want %d (usage)", code, cli.ExitUsageError)
	}
}

func TestClientDiffRejectsUnknownFormat(t *testing.T) {
	oldPath, newPath := writeTwoSpecs(t, ordersSpec(), ordersSpec())

	_, err := runClientCLI(t, "client", "diff", oldPath, newPath, "--format", "yaml")
	if err == nil {
		t.Fatal("an unknown --format should have failed")
	}

	if code := cli.GetExitCode(err); code != cli.ExitUsageError {
		t.Fatalf("exit code = %d, want %d (usage)", code, cli.ExitUsageError)
	}
}
