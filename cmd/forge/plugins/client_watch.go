// v2/cmd/forge/plugins/client_watch.go
package plugins

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/xraph/forge/cli"
	"github.com/xraph/forge/internal/client"
)

// watchUsage is rendered under USAGE: by `forge client watch --help`.
//
// The exit codes are documented for the same reason `check`'s are: watch is the
// one client command that is expected to run for hours, and "what does it do
// when the spec is broken" is the first thing anyone needs to know before they
// leave it running in a pane.
const watchUsage = `client watch [flags]

Watches the API specification and regenerates the client every time it changes,
using exactly the configuration ` + "`forge client generate`" + ` would have used. Runs
until interrupted.

A spec file is watched through its PARENT DIRECTORY, not through the file
itself: editors save by writing a temporary file and renaming it over the
original, which replaces the inode and makes a watch on the file itself go deaf
after the first save.

A spec behind --from-url is polled (--poll-interval), and only regenerates when
the fetched bytes actually differ from the ones that produced the current
output.

A generation failure NEVER stops the watch. A spec is invalid halfway through
being edited more often than not; the error is printed and the next good save
recovers.

EXIT CODES:
  0  interrupted (SIGINT/SIGTERM) after a clean shutdown
  2  usage or configuration error (no spec found, invalid flag, nothing watchable)
  3  the watcher itself could not be started`

// watchDebounceInterval is how long the watcher waits for the filesystem to go
// quiet before it reads the spec and regenerates.
//
// 300ms, matching the hot-reload default in `forge dev` (dev_docker.go) so the
// two watchers in this CLI feel the same. It is chosen against two facts rather
// than by taste: one editor save produces a burst of several fsnotify events
// (create temp, write, rename, chmod) that arrive within a few milliseconds of
// each other and must collapse into one regeneration; and a spec being rewritten
// in place by another tool -- a server dumping a fresh openapi.json, a formatter
// -- is briefly truncated on disk, so reading it the instant the first event
// lands yields a half file and a spurious parse error. 300ms clears both with
// room to spare and is still under the threshold where a human notices the lag.
const watchDebounceInterval = 300 * time.Millisecond

// defaultWatchPollInterval is how often a --from-url spec is re-fetched.
//
// Polling is the only option for an HTTP source. Five seconds is frequent
// enough that a developer who just restarted their server sees the client
// follow, and infrequent enough that leaving watch running all afternoon is 720
// requests rather than a load test.
const defaultWatchPollInterval = 5 * time.Second

// watchReporter is everything the watch loop needs from its output: one method,
// so the loop can be driven by a test without standing up a full
// cli.CommandContext. cli.CommandContext satisfies it.
type watchReporter interface {
	Println(a ...any)
}

// watchClient regenerates the client whenever the spec changes, until
// interrupted.
func (p *ClientPlugin) watchClient(ctx cli.CommandContext) error {
	// The same resolution generate performs, taken once. Not a reimplementation
	// of it: a watch that resolved configuration differently would regenerate to
	// a different directory, or with different options, than the generate the
	// developer runs by hand -- which is worse than having no watch at all,
	// because the difference only shows up as a confusing diff much later.
	plan, err := p.resolveGenerationPlan(ctx)
	if err != nil {
		return err
	}

	defer plan.cleanup()

	source, err := resolveWatchSource(plan)
	if err != nil {
		return err
	}

	gen, err := newClientGenerator()
	if err != nil {
		return cli.WrapError(err, "watch", cli.ExitInternalError)
	}

	pollInterval := time.Duration(ctx.Duration("poll-interval"))
	if pollInterval <= 0 {
		pollInterval = defaultWatchPollInterval
	}

	watcher, err := newSpecWatcher(plan, gen, source, pollInterval)
	if err != nil {
		return cli.WrapError(err, "start watcher", cli.ExitInternalError)
	}

	defer func() {
		if err := watcher.Close(); err != nil {
			ctx.Warning("closing watcher: " + err.Error())
		}
	}()

	// Signal handling before the first generation: Ctrl+C during a slow initial
	// generation should still shut down through this path rather than killing
	// the process mid-write.
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, terminationSignals...)

	defer signal.Stop(sigChan)

	watchCtx, cancel := context.WithCancel(ctx.Context())
	defer cancel()

	ctx.Println("")
	ctx.Success(source.describe() + " -> " + plan.outputDir)
	ctx.Info("Watching for changes... (Ctrl+C to stop)")
	ctx.Println("")

	var wg sync.WaitGroup

	done := make(chan struct{})

	wg.Go(func() {
		defer close(done)

		watcher.Run(watchCtx, ctx)
	})

	// Three ways out, and all of them exit 0: the user interrupted, the CLI's
	// context was cancelled, or the watcher's event stream ended under it. The
	// last one is the only surprising one and says so -- exiting silently would
	// leave a dead watch sitting in a terminal pane looking alive.
	select {
	case <-sigChan:
	case <-watchCtx.Done():
	case <-done:
		if watchCtx.Err() == nil {
			ctx.Warning("the filesystem watcher stopped on its own; nothing is being watched any more")
		}
	}

	cancel()
	wg.Wait()

	ctx.Println("")
	ctx.Success("Watch stopped")

	return nil
}

// watchSource is what a watch actually observes.
//
// For a file spec that is a DIRECTORY plus a filename to match events against,
// never the file itself -- see watchUsage and resolveWatchSource.
type watchSource struct {
	url  string // non-empty: poll this URL; dir and file are unused
	dir  string // directory handed to fsnotify
	file string // absolute path of the spec file, events are filtered down to it
}

func (s watchSource) describe() string {
	if s.url != "" {
		return "Polling " + s.url
	}

	return "Watching " + s.file
}

// matches reports whether an fsnotify event from the watched directory concerns
// the spec file.
//
// Watching the directory means hearing about every sibling in it -- the
// editor's own swap files, an unrelated README, a second spec. Everything but
// the target path is dropped here.
//
// Chmod is excluded deliberately: `touch` and a handful of editors emit it on
// files they did not change, and a regeneration per touch is exactly the noise
// that gets a watch killed. Remove and Rename are included even though neither
// leaves a readable file behind, because the rename-over-the-original save
// pattern produces them for the target path immediately before the replacement
// appears, and the debounce means the read happens once the dust has settled.
func (s watchSource) matches(event fsnotify.Event) bool {
	if !event.Has(fsnotify.Create) &&
		!event.Has(fsnotify.Write) &&
		!event.Has(fsnotify.Rename) &&
		!event.Has(fsnotify.Remove) {
		return false
	}

	return filepath.Clean(event.Name) == s.file
}

// resolveWatchSource decides what the plan's spec source means for a watcher.
//
// Anything unwatchable fails here, with the reason, rather than starting a
// watcher that silently observes nothing -- a watch that prints "watching" and
// then never fires is indistinguishable from a broken generator, and costs an
// afternoon.
//
// Watching several merged sources at once -- several directories, or a mix of
// files and polled URLs, all feeding one regeneration -- is not implemented:
// a plan with more than one resolved source is rejected outright rather than
// watching only the first of them and silently ignoring the rest.
func resolveWatchSource(plan *generationPlan) (watchSource, error) {
	if len(plan.specPaths) > 1 {
		return watchSource{}, cli.NewError(
			fmt.Sprintf(
				"client watch does not support multiple spec sources yet (%d configured); "+
					"use exactly one --from-spec/--from-url, or a single source.path/source.url",
				len(plan.specPaths),
			),
			cli.ExitUsageError,
		)
	}

	var specURL string
	if len(plan.specURLs) > 0 {
		specURL = plan.specURLs[0]
	}

	if specURL != "" {
		return watchSource{url: specURL}, nil
	}

	if len(plan.specPaths) == 0 || plan.specPaths[0] == "" {
		return watchSource{}, cli.NewError("no spec source to watch", cli.ExitUsageError)
	}

	abs, err := filepath.Abs(plan.specPaths[0])
	if err != nil {
		return watchSource{}, cli.WrapError(err, "resolve spec path", cli.ExitUsageError)
	}

	if info, statErr := os.Stat(abs); statErr == nil && info.IsDir() {
		return watchSource{}, cli.NewError(
			fmt.Sprintf("spec source %s is a directory, not a specification file", abs),
			cli.ExitUsageError,
		)
	}

	dir := filepath.Dir(abs)

	// The directory is resolved through symlinks because fsnotify reports event
	// paths under the name the watch was registered with. On macOS every path
	// under /tmp or /var arrives already resolved to /private/..., so an
	// unresolved dir would make every event fail the path filter and the watch
	// would sit there doing nothing.
	if resolved, resolveErr := filepath.EvalSymlinks(dir); resolveErr == nil {
		dir = resolved
	}

	info, err := os.Stat(dir)
	if err != nil {
		return watchSource{}, cli.NewError(
			fmt.Sprintf("cannot watch %s: its directory %s is not readable: %v", abs, dir, err),
			cli.ExitUsageError,
		)
	}

	if !info.IsDir() {
		return watchSource{}, cli.NewError(
			fmt.Sprintf("cannot watch %s: %s is not a directory", abs, dir),
			cli.ExitUsageError,
		)
	}

	return watchSource{
		dir:  dir,
		file: filepath.Join(dir, filepath.Base(abs)),
	}, nil
}

// specTracker remembers the digest of the spec content that produced the client
// currently on disk.
//
// It is what keeps a poll from regenerating on every tick, and what keeps a
// filesystem event that did not actually change the bytes (a save with no
// edits, a `touch`, an editor rewriting identical content) from rewriting the
// whole output tree and churning every downstream file watcher.
//
// The digest is recorded even when the generation that followed it FAILED. The
// same broken bytes arriving again produce the same error, and reprinting it on
// every save while someone is mid-edit is noise; the next genuinely different
// content -- including a revert to what worked before -- differs from the
// recorded digest and regenerates.
type specTracker struct {
	digest [sha256.Size]byte
	seeded bool
}

// changed reports whether content differs from the last content passed to it,
// recording it either way.
func (t *specTracker) changed(content []byte) bool {
	sum := sha256.Sum256(content)

	if t.seeded && sum == t.digest {
		return false
	}

	t.digest = sum
	t.seeded = true

	return true
}

// specWatcher regenerates the client on every observed change to the spec.
type specWatcher struct {
	plan         *generationPlan
	gen          *client.Generator
	fsw          *fsnotify.Watcher
	debouncer    *debouncer
	source       watchSource
	tracker      specTracker
	lastPollErr  string
	pollInterval time.Duration
	mu           sync.Mutex
	stopped      atomic.Bool
}

func newSpecWatcher(
	plan *generationPlan,
	gen *client.Generator,
	source watchSource,
	pollInterval time.Duration,
) (*specWatcher, error) {
	w := &specWatcher{
		plan:         plan,
		gen:          gen,
		source:       source,
		debouncer:    newDebouncer(watchDebounceInterval),
		pollInterval: pollInterval,
	}

	if source.url != "" {
		return w, nil
	}

	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("create fsnotify watcher: %w", err)
	}

	if err := fsw.Add(source.dir); err != nil {
		fsw.Close()

		return nil, fmt.Errorf("watch %s: %w", source.dir, err)
	}

	w.fsw = fsw

	return w, nil
}

// Close releases the fsnotify handle.
func (w *specWatcher) Close() error {
	if w.fsw == nil {
		return nil
	}

	return w.fsw.Close()
}

// shutdown marks the watcher stopped and blocks until any cycle already in
// flight has returned.
//
// stopped is set first so a debounced callback that has not yet started
// bails out at the top of cycle instead of beginning one. debouncer.Stop
// then guarantees no *new* callback fires. Neither of those touches a
// callback that is already past that check and mid-cycle -- taking and
// releasing mu does, because cycle holds mu for its entire body, including
// the WriteClient call. Without this, Ctrl+C could return while a callback
// is still writing the output tree, and the process would exit out from
// under it.
func (w *specWatcher) shutdown() {
	w.stopped.Store(true)
	w.debouncer.Stop()

	w.mu.Lock()
	w.mu.Unlock()
}

// Run generates once, then keeps generating on every change until ctx is
// cancelled. It never returns an error: a watch that stopped on the first
// unparseable spec would be useless, since a spec is invalid halfway through
// being edited more often than not.
func (w *specWatcher) Run(ctx context.Context, out watchReporter) {
	// Generate once up front. This is both a service (the output matches the
	// spec you started watching, without a separate generate run) and the thing
	// that seeds the digest, so the first real save is compared against what is
	// actually on disk rather than regenerating unconditionally.
	if content, err := os.ReadFile(w.readPath()); err != nil {
		out.Println(watchLine("startup", cli.Red("cannot read spec: "+err.Error())))
	} else {
		w.cycle(ctx, out, "initial", content)
	}

	if w.source.url != "" {
		w.runPoll(ctx, out)

		return
	}

	w.runFS(ctx, out)
}

// readPath is the file the watcher reads spec bytes from. For a URL source that
// is the temp file the plan already fetched into; for a file source it is the
// watched file itself.
func (w *specWatcher) readPath() string {
	if w.source.url != "" {
		return w.plan.specPaths[0]
	}

	return w.source.file
}

func (w *specWatcher) runFS(ctx context.Context, out watchReporter) {
	defer w.shutdown()

	name := filepath.Base(w.source.file)

	for {
		select {
		case <-ctx.Done():
			return

		case event, ok := <-w.fsw.Events:
			if !ok {
				return
			}

			if !w.source.matches(event) {
				continue
			}

			// The read happens inside the debounced callback, not here: by the
			// time it runs the writer has finished, so this reads whole content
			// rather than a mid-write truncation. That protects the digest
			// computed from it, not generation itself: cycle regenerates from
			// plan.specPaths[0], a separate read of what is -- ordinarily -- the
			// same underlying file, and deliberately so. The two are not merged
			// into one read because an existing byte-parity test pins
			// generation to plan.specPaths[0], matching what `forge client
			// generate` would have used.
			w.debouncer.Debounce(func() {
				content, err := os.ReadFile(w.source.file)
				if err != nil {
					out.Println(watchLine(name, cli.Red("cannot read spec: "+err.Error())))

					return
				}

				w.cycle(ctx, out, name, content)
			})

		case err, ok := <-w.fsw.Errors:
			if !ok {
				return
			}

			out.Println(watchLine(name, cli.Red("watcher error: "+err.Error())))
		}
	}
}

func (w *specWatcher) runPoll(ctx context.Context, out watchReporter) {
	defer w.shutdown()

	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return

		case <-ticker.C:
			// A tick and a cancellation can both be ready at once, and at a
			// short poll interval the ticker case wins often enough that
			// relying on the select alone would start another fetch during
			// shutdown instead of returning. Checking here closes that race.
			if ctx.Err() != nil {
				return
			}

			content, err := fetchSpecFromURL(ctx, w.source.url, 0)
			if err != nil {
				// A cancelled context is shutdown, not a poll failure: without
				// this check, Ctrl+C during an in-flight fetch would print a
				// spurious "fetch failed: context canceled" on its way out.
				if ctx.Err() != nil {
					return
				}

				// Only the first of a run of identical failures is printed. A
				// server that is down stays down for minutes, and one line every
				// five seconds saying so buries the line that matters when it
				// comes back.
				if msg := err.Error(); msg != w.lastPollErr {
					w.lastPollErr = msg
					out.Println(watchLine(w.source.url, cli.Red("fetch failed: "+msg)))
				}

				continue
			}

			w.lastPollErr = ""

			w.cycle(ctx, out, w.source.url, content)
		}
	}
}

// cycle regenerates from content, if content is not what the current output was
// generated from. Serialized: a poll tick and a debounced filesystem event must
// never write the output tree at the same time.
func (w *specWatcher) cycle(ctx context.Context, out watchReporter, trigger string, content []byte) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.stopped.Load() {
		return
	}

	if !w.tracker.changed(content) {
		return
	}

	// A URL source generates from the temp file the plan resolved, refreshed
	// with what was just fetched -- so generation reads a file laid out exactly
	// as `forge client generate --from-url` would have left it.
	if w.source.url != "" {
		if err := os.WriteFile(w.plan.specPaths[0], content, 0o600); err != nil {
			out.Println(watchLine(trigger, cli.Red("cannot stage fetched spec: "+err.Error())))

			return
		}
	}

	started := time.Now()

	generated, err := w.gen.GenerateFromFile(ctx, w.plan.specPaths[0], w.plan.config)
	if err != nil {
		// Never fatal. Report and keep watching; the next good save recovers.
		out.Println(watchLine(trigger, cli.Red("generation failed")))
		out.Println("           " + err.Error())

		return
	}

	outputMgr := client.NewOutputManager()
	if err := outputMgr.WriteClient(generated, w.plan.outputDir); err != nil {
		out.Println(watchLine(trigger, cli.Red("write failed")))
		out.Println("           " + err.Error())

		return
	}

	out.Println(watchLine(trigger, cli.Green(fmt.Sprintf(
		"%d files -> %s",
		len(generated.Files),
		w.plan.outputDir,
	))+" "+cli.Gray("("+time.Since(started).Round(time.Millisecond).String()+")")))

	// Warnings are indented under the line they belong to. They are printed on
	// every regeneration rather than once, because the interesting case is the
	// edit that introduced one.
	for _, warning := range generated.Warnings {
		out.Println("           " + cli.Yellow("! "+warning))
	}
}

// watchLine formats one event as one line: when it happened, what triggered it,
// and the outcome. A watch that prints nothing leaves you unsure it is alive;
// one that prints a paragraph per keystroke gets killed.
func watchLine(trigger, outcome string) string {
	return fmt.Sprintf("%s  %s  %s", cli.Gray(time.Now().Format("15:04:05")), cli.Bold(trigger), outcome)
}
