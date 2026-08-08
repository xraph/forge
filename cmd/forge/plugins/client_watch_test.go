// v2/cmd/forge/plugins/client_watch_test.go
package plugins

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/xraph/forge/cli"
)

// Watch tests are timing-sensitive by nature, so the decomposable parts --
// change detection, path filtering, source resolution, configuration parity --
// are tested directly and deterministically. Only the two behaviours that
// cannot be observed any other way (a rename-replace save reaching the watcher,
// and a broken spec not stopping it) go through a real fsnotify watcher, with
// generous deadlines and a context the test cancels.

// ---------------------------------------------------------------------------
// Change detection
// ---------------------------------------------------------------------------

// The tracker is what stops a poll from regenerating every tick and a
// no-op save from rewriting the whole output tree.
func TestSpecTrackerReportsOnlyRealChanges(t *testing.T) {
	var tracker specTracker

	if !tracker.changed([]byte("openapi: 3.0.0")) {
		t.Fatal("the first content ever seen must count as a change")
	}

	if tracker.changed([]byte("openapi: 3.0.0")) {
		t.Fatal("identical content must not count as a change")
	}

	if !tracker.changed([]byte("openapi: 3.1.0")) {
		t.Fatal("different content must count as a change")
	}

	if tracker.changed([]byte("openapi: 3.1.0")) {
		t.Fatal("identical content must not count as a change after an update")
	}

	// A revert to content seen before is still a change relative to what
	// produced the current output -- this is the "I broke it, undo, fix it
	// properly" path, and it must regenerate.
	if !tracker.changed([]byte("openapi: 3.0.0")) {
		t.Fatal("reverting to earlier content must count as a change")
	}
}

// An empty spec is content like any other: a server that starts returning an
// empty body must not be silently treated as "nothing changed".
func TestSpecTrackerTreatsEmptyContentAsContent(t *testing.T) {
	var tracker specTracker

	if !tracker.changed(nil) {
		t.Fatal("the first content, even empty, must count as a change")
	}

	if tracker.changed([]byte{}) {
		t.Fatal("empty content twice is not a change")
	}

	if !tracker.changed([]byte("openapi: 3.0.0")) {
		t.Fatal("content arriving after empty content is a change")
	}
}

// ---------------------------------------------------------------------------
// Path filtering
// ---------------------------------------------------------------------------

// The watch is registered on the spec's DIRECTORY, so it hears about every
// sibling in it. Everything but the spec itself has to be dropped, or an
// unrelated save next door regenerates the client.
func TestWatchSourceFiltersDirectoryEventsToTheSpec(t *testing.T) {
	dir := "/project/api"
	source := watchSource{dir: dir, file: filepath.Join(dir, "openapi.json")}

	cases := []struct {
		name string
		path string
		op   fsnotify.Op
		want bool
	}{
		{"write to the spec", "openapi.json", fsnotify.Write, true},
		{"spec created by a rename-replace save", "openapi.json", fsnotify.Create, true},
		{"spec renamed away", "openapi.json", fsnotify.Rename, true},
		{"spec removed", "openapi.json", fsnotify.Remove, true},

		// The editor's own scratch files land in the same directory.
		{"editor swap file", ".openapi.json.swp", fsnotify.Write, false},
		{"editor temp file", "openapi.json.tmp", fsnotify.Create, false},
		{"backup file", "openapi.json~", fsnotify.Write, false},

		// Unrelated siblings, including a second spec.
		{"sibling document", "README.md", fsnotify.Write, false},
		{"a different spec", "asyncapi.json", fsnotify.Write, false},

		// A prefix match would wrongly accept this one.
		{"name extending the spec's", "openapi.json.bak", fsnotify.Write, false},

		// Chmod fires on files nobody edited (`touch`, some editors on save).
		{"chmod on the spec", "openapi.json", fsnotify.Chmod, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			event := fsnotify.Event{Name: filepath.Join(dir, tc.path), Op: tc.op}

			if got := source.matches(event); got != tc.want {
				t.Fatalf("matches(%s %s) = %v, want %v", tc.op, event.Name, got, tc.want)
			}
		})
	}
}

// An unnormalised path (the shape fsnotify emits on some platforms) still has
// to match the target.
func TestWatchSourceMatchesUncleanEventPaths(t *testing.T) {
	dir := "/project/api"
	source := watchSource{dir: dir, file: filepath.Join(dir, "openapi.json")}

	event := fsnotify.Event{Name: "/project/api/./openapi.json", Op: fsnotify.Write}
	if !source.matches(event) {
		t.Fatalf("an unclean path for the spec must still match")
	}
}

// ---------------------------------------------------------------------------
// Source resolution
// ---------------------------------------------------------------------------

// The single most important property of the whole command: the registered watch
// is on the parent DIRECTORY. A watch on the file itself goes deaf the first
// time an editor saves by writing a temp file and renaming it over the
// original, because the inode it holds is no longer the one at that path.
func TestResolveWatchSourceWatchesTheParentDirectory(t *testing.T) {
	dir := t.TempDir()
	spec := filepath.Join(dir, "openapi.json")

	if err := os.WriteFile(spec, []byte("{}"), 0o600); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	source, err := resolveWatchSource(&generationPlan{specPaths: []string{spec}})
	if err != nil {
		t.Fatalf("resolve watch source: %v", err)
	}

	if source.url != "" {
		t.Fatalf("a file source must not be polled, got url %q", source.url)
	}

	// Symlink-resolved, because fsnotify reports events under the name the
	// watch was registered with -- on macOS every temp path arrives as
	// /private/....
	wantDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("eval symlinks: %v", err)
	}

	if source.dir != wantDir {
		t.Fatalf("watched directory = %s, want %s (the spec's parent)", source.dir, wantDir)
	}

	if source.file != filepath.Join(wantDir, "openapi.json") {
		t.Fatalf("target file = %s, want %s", source.file, filepath.Join(wantDir, "openapi.json"))
	}
}

// A relative --from-spec, the way it is actually typed, resolves against the
// working directory.
func TestResolveWatchSourceAcceptsRelativeSpecPaths(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	if err := os.WriteFile(filepath.Join(dir, "openapi.json"), []byte("{}"), 0o600); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	source, err := resolveWatchSource(&generationPlan{specPaths: []string{"openapi.json"}})
	if err != nil {
		t.Fatalf("resolve watch source: %v", err)
	}

	if filepath.Base(source.file) != "openapi.json" || source.dir == "" {
		t.Fatalf("relative spec resolved to dir=%q file=%q", source.dir, source.file)
	}
}

// A spec that does not exist yet is watchable: its directory does, and the file
// arriving is exactly the event worth hearing about. The initial generation
// fails and says so, which is the honest report.
func TestResolveWatchSourceAcceptsAnAbsentSpecFile(t *testing.T) {
	dir := t.TempDir()

	if _, err := resolveWatchSource(&generationPlan{specPaths: []string{filepath.Join(dir, "not-written-yet.json")}}); err != nil {
		t.Fatalf("a spec whose directory exists must be watchable, got %v", err)
	}
}

// Anything unwatchable fails at startup with the reason. A watcher that
// silently observes nothing looks exactly like a broken generator.
func TestResolveWatchSourceRejectsUnwatchableSources(t *testing.T) {
	dir := t.TempDir()

	cases := []struct {
		name string
		plan *generationPlan
	}{
		{"no spec source at all", &generationPlan{}},
		{"spec path is a directory", &generationPlan{specPaths: []string{dir}}},
		{"parent directory does not exist", &generationPlan{specPaths: []string{filepath.Join(dir, "nope", "openapi.json")}}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := resolveWatchSource(tc.plan)
			if err == nil {
				t.Fatal("expected an error rather than a watch on nothing")
			}

			if code := cli.GetExitCode(err); code != cli.ExitUsageError {
				t.Fatalf("exit code = %d, want %d (usage/configuration)", code, cli.ExitUsageError)
			}
		})
	}
}

// A spec fetched over HTTP has to be polled: the plan's specPath is a temp file
// nothing will ever write to again, so watching it would be a watch that can
// never fire.
func TestResolveWatchSourcePollsURLSpecs(t *testing.T) {
	source, err := resolveWatchSource(&generationPlan{
		specPaths: []string{filepath.Join(t.TempDir(), "forge-client-spec-123.json")},
		specURLs:  []string{"http://localhost:8080/openapi.json"},
	})
	if err != nil {
		t.Fatalf("resolve watch source: %v", err)
	}

	if source.url != "http://localhost:8080/openapi.json" {
		t.Fatalf("url = %q, want the plan's spec URL", source.url)
	}

	if source.dir != "" || source.file != "" {
		t.Fatalf("a URL source must not register a filesystem watch, got dir=%q file=%q", source.dir, source.file)
	}
}

// ---------------------------------------------------------------------------
// Configuration parity with generate
// ---------------------------------------------------------------------------

// watch runs generate's resolution -- the same resolveGenerationPlan call -- so
// the only way the two can drift is through their flag sets. This is the
// counterpart to TestClientCheckResolvesConfigurationLikeGenerate.
func TestClientWatchSharesGenerateFlagSet(t *testing.T) {
	generateFlags := subcommandFlagNames(t, "generate")
	watchFlags := subcommandFlagNames(t, "watch")

	for name := range generateFlags {
		if !watchFlags[name] {
			t.Errorf("`client watch` is missing generate's --%s: it would resolve configuration differently", name)
		}
	}

	for name := range watchFlags {
		if generateFlags[name] || name == "poll-interval" {
			continue
		}

		t.Errorf("`client watch` has --%s, which generate does not: watch must not invent configuration", name)
	}
}

// Beyond the flag set, watch must land the same bytes in the same directory as
// generate. This runs the watcher's own generation path against output produced
// by the real `client generate` command and compares the trees.
func TestClientWatchGeneratesWhatGenerateGenerates(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	writeSpecFile(t, filepath.Join(dir, "openapi.json"), ordersSpec())

	if out, err := runClientCLI(t, "client", "generate"); err != nil {
		t.Fatalf("generate with defaults: %v\n%s", err, out)
	}

	plan := resolvePlanForTest(t)

	expected, err := readGeneratedTree(plan.outputDir)
	if err != nil {
		t.Fatalf("read generate's output: %v", err)
	}

	if len(expected) == 0 {
		t.Fatal("generate produced no files")
	}

	if err := os.RemoveAll(plan.outputDir); err != nil {
		t.Fatalf("remove generate's output: %v", err)
	}

	runWatchSteps(t, plan, watchStep{
		name:  "the initial generation",
		until: func(text string) bool { return strings.Contains(text, "initial") },
	})

	actual, err := readGeneratedTree(plan.outputDir)
	if err != nil {
		t.Fatalf("read watch's output: %v", err)
	}

	if len(actual) != len(expected) {
		t.Fatalf("watch wrote %d files, generate wrote %d", len(actual), len(expected))
	}

	for path, want := range expected {
		got, ok := actual[path]
		if !ok {
			t.Fatalf("watch did not write %s, which generate writes", path)
		}

		if !bytes.Equal(got, want) {
			t.Fatalf("watch wrote different bytes than generate for %s", path)
		}
	}
}

// ---------------------------------------------------------------------------
// The live watcher
// ---------------------------------------------------------------------------

// The case the whole directory-watch design exists for: editors save by writing
// a temporary file and renaming it over the original, which replaces the inode.
// A watch registered on the file itself hears the first save and then nothing,
// forever -- and looks like it works, because the first save is the one you
// test by hand.
func TestClientWatchRegeneratesAfterARenameReplaceSave(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	specPath := filepath.Join(dir, "openapi.json")
	writeSpecFile(t, specPath, ordersSpec())

	plan := resolvePlanForTest(t)

	// Two rename-replace saves through one watcher, not one. A single save
	// proves nothing: a watch on the file itself passes that and then fails
	// every save after it -- which is exactly why the bug survives manual
	// testing.
	steps := []watchStep{{
		name:  "the initial generation",
		until: func(text string) bool { return strings.Contains(text, "initial") },
	}}

	for round := 1; round <= 2; round++ {
		spec := ordersSpec()
		paths, _ := spec["paths"].(map[string]any)
		paths[fmt.Sprintf("/invoices%d", round)] = map[string]any{
			"get": map[string]any{
				"operationId": fmt.Sprintf("invoiceList%d", round),
				"responses":   map[string]any{"200": map[string]any{"description": "ok"}},
			},
		}

		operation := fmt.Sprintf("invoiceList%d", round)

		steps = append(steps, watchStep{
			name: fmt.Sprintf("regeneration after rename-replace save %d", round),
			do:   func() { renameReplaceSpec(t, dir, specPath, spec) },
			until: func(text string) bool {
				return generatedTreeMentions(t, plan.outputDir, operation)
			},
		})
	}

	runWatchSteps(t, plan, steps...)
}

// A spec is invalid halfway through being edited more often than not. Exiting
// on the first parse error would make the command useless; it has to report and
// recover.
func TestClientWatchSurvivesAnInvalidSpecAndRecovers(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	specPath := filepath.Join(dir, "openapi.json")
	writeSpecFile(t, specPath, ordersSpec())

	plan := resolvePlanForTest(t)

	fixed := ordersSpec()
	paths, _ := fixed["paths"].(map[string]any)
	paths["/recovered"] = map[string]any{
		"get": map[string]any{
			"operationId": "recoveredList",
			"responses":   map[string]any{"200": map[string]any{"description": "ok"}},
		},
	}

	runWatchSteps(t, plan,
		watchStep{
			name:  "the initial generation",
			until: func(text string) bool { return strings.Contains(text, "initial") },
		},
		watchStep{
			name:  "the failure to be reported rather than fatal",
			do:    func() { renameReplaceBytes(t, dir, specPath, []byte("{not json")) },
			until: func(text string) bool { return strings.Contains(text, "generation failed") },
		},
		watchStep{
			// A watcher that died on the parse error never gets here.
			name:  "recovery on the next good save",
			do:    func() { renameReplaceSpec(t, dir, specPath, fixed) },
			until: func(string) bool { return generatedTreeMentions(t, plan.outputDir, "recoveredList") },
		},
	)
}

// A URL spec has no filesystem to watch, so it is polled -- and a poll that
// regenerated on every tick would rewrite the whole output tree every few
// seconds forever, churning every downstream file watcher for nothing. This
// asserts both halves: identical bytes over several ticks produce no
// regeneration at all, and changed bytes produce exactly one.
func TestClientWatchPollsAURLSpecAndRegeneratesOnlyOnRealChanges(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	writeSpecFile(t, filepath.Join(dir, "openapi.json"), ordersSpec())

	var (
		mu    sync.Mutex
		serve = marshalSpec(t, ordersSpec())
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(serve)
	}))

	defer server.Close()

	// The shape resolution produces for a URL source: a temp file already
	// holding the first fetch, plus the URL it came from.
	plan := resolvePlanForTest(t)
	plan.specURLs = []string{server.URL}
	plan.specPaths = []string{filepath.Join(dir, "fetched-spec.json")}

	if err := os.WriteFile(plan.specPaths[0], serve, 0o600); err != nil {
		t.Fatalf("stage fetched spec: %v", err)
	}

	const pollInterval = 50 * time.Millisecond

	changed := ordersSpec()
	paths, _ := changed["paths"].(map[string]any)
	paths["/audits"] = map[string]any{
		"get": map[string]any{
			"operationId": "auditList",
			"responses":   map[string]any{"200": map[string]any{"description": "ok"}},
		},
	}

	runWatchStepsAtInterval(t, plan, pollInterval,
		watchStep{
			name:  "the initial generation",
			until: func(text string) bool { return strings.Contains(text, "initial") },
		},
		watchStep{
			name: "several polls of unchanged bytes to produce nothing",
			// Bounded, and only ever able to fail in the direction that
			// matters: if the poll regenerated per tick there would be half a
			// dozen lines here instead of the one from startup.
			do: func() { time.Sleep(6 * pollInterval) },
			until: func(text string) bool {
				return countGenerations(text) == 1
			},
		},
		watchStep{
			name: "one regeneration once the served bytes change",
			do: func() {
				mu.Lock()
				defer mu.Unlock()

				serve = marshalSpec(t, changed)
			},
			until: func(text string) bool {
				return countGenerations(text) == 2 && generatedTreeMentions(t, plan.outputDir, "auditList")
			},
		},
	)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// countGenerations counts the successful regenerations in what a watcher
// printed.
func countGenerations(printed string) int {
	return strings.Count(printed, "files -> ")
}

func marshalSpec(t *testing.T, doc map[string]any) []byte {
	t.Helper()

	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal spec: %v", err)
	}

	return data
}

// recordingReporter collects what the watch loop printed.
type recordingReporter struct {
	mu    sync.Mutex
	lines []string
}

func (r *recordingReporter) Println(a ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.lines = append(r.lines, fmt.Sprint(a...))
}

func (r *recordingReporter) text() string {
	r.mu.Lock()
	defer r.mu.Unlock()

	return strings.Join(r.lines, "\n")
}

// watchStep is one act of a watcher test: an edit to make (once), and the
// observable condition that edit must produce.
type watchStep struct {
	do    func()
	until func(printed string) bool
	name  string
}

// runWatchSteps starts a real watcher over plan and drives it through steps.
//
// Each step's edit happens EXACTLY once and its condition is then polled. That
// distinction is the whole reliability story: an edit repeated on every poll
// resets the debouncer every time and the watcher, correctly, never fires --
// which looks precisely like a broken watcher. No step asserts on elapsed time;
// the deadline exists only to fail a hung test rather than hang the suite.
func runWatchSteps(t *testing.T, plan *generationPlan, steps ...watchStep) {
	t.Helper()

	runWatchStepsAtInterval(t, plan, defaultWatchPollInterval, steps...)
}

func runWatchStepsAtInterval(
	t *testing.T,
	plan *generationPlan,
	pollInterval time.Duration,
	steps ...watchStep,
) {
	t.Helper()

	source, err := resolveWatchSource(plan)
	if err != nil {
		t.Fatalf("resolve watch source: %v", err)
	}

	gen, err := newClientGenerator()
	if err != nil {
		t.Fatalf("build generator: %v", err)
	}

	watcher, err := newSpecWatcher(plan, gen, source, pollInterval)
	if err != nil {
		t.Fatalf("start watcher: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	reporter := &recordingReporter{}

	var wg sync.WaitGroup

	wg.Go(func() {
		watcher.Run(ctx, reporter)
	})

	var failure string

	for _, step := range steps {
		if step.do != nil {
			step.do()
		}

		deadline := time.Now().Add(30 * time.Second)
		satisfied := false

		for time.Now().Before(deadline) {
			if step.until(reporter.text()) {
				satisfied = true

				break
			}

			time.Sleep(20 * time.Millisecond)
		}

		if !satisfied {
			failure = step.name

			break
		}
	}

	cancel()
	wg.Wait()

	if err := watcher.Close(); err != nil {
		t.Fatalf("close watcher: %v", err)
	}

	if failure != "" {
		t.Fatalf("timed out waiting for %s; the watcher printed:\n%s", failure, reporter.text())
	}
}

// renameReplaceSpec saves a spec the way an editor does: write a new file
// alongside, then rename it over the target. The original inode is gone
// afterwards.
func renameReplaceSpec(t *testing.T, dir, target string, doc map[string]any) {
	t.Helper()

	staging := filepath.Join(t.TempDir(), "staged.json")
	writeSpecFile(t, staging, doc)

	data, err := os.ReadFile(staging)
	if err != nil {
		t.Fatalf("read staged spec: %v", err)
	}

	renameReplaceBytes(t, dir, target, data)
}

func renameReplaceBytes(t *testing.T, dir, target string, data []byte) {
	t.Helper()

	// The temp file must live in the same directory for the rename to be a
	// same-filesystem replace -- which is what makes it atomic, and what makes
	// it swap the inode.
	tmp, err := os.CreateTemp(dir, ".openapi-*.json.tmp")
	if err != nil {
		t.Fatalf("create staging file: %v", err)
	}

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		t.Fatalf("write staging file: %v", err)
	}

	if err := tmp.Close(); err != nil {
		t.Fatalf("close staging file: %v", err)
	}

	if err := os.Rename(tmp.Name(), target); err != nil {
		t.Fatalf("rename over %s: %v", target, err)
	}
}

// generatedTreeMentions reports whether any generated file mentions needle.
//
// Case-insensitive on purpose: what a test cares about is that the new
// operation reached the output, not whether this generator spells an
// operationId `invoiceList1`, `InvoiceList1` or `INVOICE_LIST_1` -- pinning the
// casing here would make an unrelated naming change fail a watcher test.
func generatedTreeMentions(t *testing.T, outputDir, needle string) bool {
	t.Helper()

	tree, err := readGeneratedTree(outputDir)
	if err != nil {
		return false
	}

	lowered := bytes.ToLower([]byte(needle))

	for _, data := range tree {
		if bytes.Contains(bytes.ToLower(data), lowered) {
			return true
		}
	}

	return false
}

// resolvePlanForTest resolves a generation plan through the real CLI flag
// machinery, so a test drives exactly the plan `forge client generate` would
// have built in the current working directory.
func resolvePlanForTest(t *testing.T, args ...string) *generationPlan {
	t.Helper()

	var (
		out     bytes.Buffer
		plugin  = &ClientPlugin{}
		plan    *generationPlan
		planErr error
	)

	app := cli.New(cli.Config{Name: "forge", Version: "test", Output: &out})

	cmd := cli.NewCommand("resolve", "resolve a generation plan", func(ctx cli.CommandContext) error {
		plan, planErr = plugin.resolveGenerationPlan(ctx)

		return planErr
	}, clientGenerationFlags()...)

	if err := app.AddCommand(cmd); err != nil {
		t.Fatalf("register resolve command: %v", err)
	}

	if err := app.Run(append([]string{"forge", "resolve"}, args...)); err != nil {
		t.Fatalf("resolve generation plan: %v\n%s", err, out.String())
	}

	if plan == nil {
		t.Fatalf("no plan resolved\n%s", out.String())
	}

	t.Cleanup(plan.cleanup)

	return plan
}

// subcommandFlagNames returns the flag names registered on a `client`
// subcommand, read from the real command tree.
func subcommandFlagNames(t *testing.T, name string) map[string]bool {
	t.Helper()

	plugin, ok := NewClientPlugin(nil).(*ClientPlugin)
	if !ok {
		t.Fatal("NewClientPlugin did not return a *ClientPlugin")
	}

	commands := plugin.Commands()
	if len(commands) != 1 {
		t.Fatalf("client plugin registered %d root commands, want 1", len(commands))
	}

	sub, found := commands[0].FindSubcommand(name)
	if !found {
		t.Fatalf("`client %s` is not registered", name)
	}

	names := make(map[string]bool)
	for _, flag := range sub.Flags() {
		names[flag.Name()] = true
	}

	return names
}
