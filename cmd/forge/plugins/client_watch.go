// v2/cmd/forge/plugins/client_watch.go
package plugins

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
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

	sources, err := resolveWatchSources(plan)
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

	watcher, err := newSpecWatcher(plan, gen, sources, pollInterval)
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
	ctx.Success(describeSources(sources) + " -> " + plan.outputDir)
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
// never the file itself -- see watchUsage and resolveWatchSources. A watch
// registers one watchSource per configured spec source, in the same order as
// generationPlan.specPaths, so a source's index here is also its index into
// plan.specPaths and plan.specURLs.
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

// describeSources summarizes every source a watch is observing, in the order
// they were resolved.
func describeSources(sources []watchSource) string {
	descriptions := make([]string, len(sources))
	for i, source := range sources {
		descriptions[i] = source.describe()
	}

	return strings.Join(descriptions, "; ")
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

// resolveWatchSources decides what every one of the plan's spec sources means
// for a watcher, one watchSource per entry in plan.specPaths, in order.
//
// Anything unwatchable fails here, with the reason, rather than starting a
// watcher that silently observes nothing -- a watch that prints "watching" and
// then never fires is indistinguishable from a broken generator, and costs an
// afternoon. Every guard resolveWatchSource (singular) used to apply to the
// lone source is applied here to each one: a client generated from two
// documents is stale when EITHER changes, and watching only the first would
// rebuild on a REST edit and sit still on a stream edit.
func resolveWatchSources(plan *generationPlan) ([]watchSource, error) {
	if len(plan.specPaths) == 0 {
		return nil, cli.NewError("no spec source to watch", cli.ExitUsageError)
	}

	sources := make([]watchSource, 0, len(plan.specPaths))

	for i, path := range plan.specPaths {
		var specURL string
		if i < len(plan.specURLs) {
			specURL = plan.specURLs[i]
		}

		if specURL != "" {
			sources = append(sources, watchSource{url: specURL})

			continue
		}

		if path == "" {
			return nil, cli.NewError("no spec source to watch", cli.ExitUsageError)
		}

		abs, err := filepath.Abs(path)
		if err != nil {
			return nil, cli.WrapError(err, "resolve spec path", cli.ExitUsageError)
		}

		if info, statErr := os.Stat(abs); statErr == nil && info.IsDir() {
			return nil, cli.NewError(
				fmt.Sprintf("spec source %s is a directory, not a specification file", abs),
				cli.ExitUsageError,
			)
		}

		dir := filepath.Dir(abs)

		// The directory is resolved through symlinks because fsnotify reports
		// event paths under the name the watch was registered with. On macOS
		// every path under /tmp or /var arrives already resolved to
		// /private/..., so an unresolved dir would make every event fail the
		// path filter and the watch would sit there doing nothing.
		if resolved, resolveErr := filepath.EvalSymlinks(dir); resolveErr == nil {
			dir = resolved
		}

		info, err := os.Stat(dir)
		if err != nil {
			return nil, cli.NewError(
				fmt.Sprintf("cannot watch %s: its directory %s is not readable: %v", abs, dir, err),
				cli.ExitUsageError,
			)
		}

		if !info.IsDir() {
			return nil, cli.NewError(
				fmt.Sprintf("cannot watch %s: %s is not a directory", abs, dir),
				cli.ExitUsageError,
			)
		}

		sources = append(sources, watchSource{
			dir:  dir,
			file: filepath.Join(dir, filepath.Base(abs)),
		})
	}

	if len(sources) == 0 {
		return nil, cli.NewError("no spec source to watch", cli.ExitUsageError)
	}

	return sources, nil
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

// specWatcher regenerates the client on every observed change to any of its
// sources.
//
// sources, trackers and lastPollErr are parallel to each other and to
// plan.specPaths / plan.specURLs: index i of each describes the same spec
// source.
type specWatcher struct {
	plan         *generationPlan
	gen          *client.Generator
	fsw          *fsnotify.Watcher
	debouncer    *debouncer
	sources      []watchSource
	trackers     []specTracker
	lastPollErr  []string
	pollInterval time.Duration
	mu           sync.Mutex
	stopped      atomic.Bool
}

func newSpecWatcher(
	plan *generationPlan,
	gen *client.Generator,
	sources []watchSource,
	pollInterval time.Duration,
) (*specWatcher, error) {
	w := &specWatcher{
		plan:         plan,
		gen:          gen,
		sources:      sources,
		trackers:     make([]specTracker, len(sources)),
		lastPollErr:  make([]string, len(sources)),
		debouncer:    newDebouncer(watchDebounceInterval),
		pollInterval: pollInterval,
	}

	// Every file source is watched through its own parent directory, but two
	// sources can share one directory (two spec files side by side), so the
	// directories are deduplicated before registering with fsnotify.
	dirs := make(map[string]struct{})

	for _, source := range sources {
		if source.url != "" {
			continue
		}

		dirs[source.dir] = struct{}{}
	}

	if len(dirs) == 0 {
		return w, nil
	}

	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("create fsnotify watcher: %w", err)
	}

	for dir := range dirs {
		if err := fsw.Add(dir); err != nil {
			fsw.Close()

			return nil, fmt.Errorf("watch %s: %w", dir, err)
		}
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

// Run generates once, then keeps generating on every change to any source
// until ctx is cancelled. It never returns an error: a watch that stopped on
// the first unparseable spec would be useless, since a spec is invalid
// halfway through being edited more often than not.
func (w *specWatcher) Run(ctx context.Context, out watchReporter) {
	// Seed every source's digest from what is already on disk before the
	// first generation, so the first real save to any one of them is compared
	// against what actually produced the current output rather than
	// regenerating unconditionally. This is a separate read from the one
	// regenerateAll performs through resolveMergedSpec -- same reasoning as
	// the per-event read below: the digest is protected here, generation reads
	// fresh every time it runs.
	allReadable := true

	for i := range w.sources {
		content, err := os.ReadFile(w.readPath(i))
		if err != nil {
			out.Println(watchLine("startup", cli.Red("cannot read spec: "+err.Error())))

			allReadable = false

			continue
		}

		w.trackers[i].changed(content)
	}

	// This is both a service (the output matches the sources you started
	// watching, without a separate generate run) and what proves the merge
	// path works from a cold start, not only on a later edit.
	if allReadable {
		w.mu.Lock()
		w.regenerateAll(ctx, out, "initial")
		w.mu.Unlock()
	}

	var wg sync.WaitGroup

	// FS and polled sources can coexist in one plan (a local REST document
	// plus a streamed AsyncAPI document fetched over HTTP, for instance), so
	// both loops run concurrently rather than one being chosen over the
	// other.
	if w.fsw != nil {
		wg.Go(func() {
			w.runFS(ctx, out)
		})
	}

	if w.hasPolledSources() {
		wg.Go(func() {
			w.runPoll(ctx, out)
		})
	}

	wg.Wait()
}

// hasPolledSources reports whether any source is a URL, and therefore needs
// runPoll.
func (w *specWatcher) hasPolledSources() bool {
	for _, source := range w.sources {
		if source.url != "" {
			return true
		}
	}

	return false
}

// readPath is the file the watcher reads source i's bytes from. For a URL
// source that is the temp file the plan already fetched into; for a file
// source it is the watched file itself.
func (w *specWatcher) readPath(i int) string {
	if w.sources[i].url != "" {
		return w.plan.specPaths[i]
	}

	return w.sources[i].file
}

func (w *specWatcher) runFS(ctx context.Context, out watchReporter) {
	defer w.shutdown()

	for {
		select {
		case <-ctx.Done():
			return

		case event, ok := <-w.fsw.Events:
			if !ok {
				return
			}

			for i, source := range w.sources {
				if source.url != "" || !source.matches(event) {
					continue
				}

				name := filepath.Base(source.file)

				// The read happens inside the debounced callback, not here: by
				// the time it runs the writer has finished, so this reads whole
				// content rather than a mid-write truncation. That protects the
				// digest computed from it, not generation itself: a change to
				// any one source triggers regenerateAll, which re-reads every
				// source fresh through resolveMergedSpec -- the same merge path
				// generate and check use -- so the regenerated output is never
				// missing a sibling source's latest bytes even if this
				// particular debounced callback is superseded by a later one
				// before it runs.
				w.debouncer.Debounce(func() {
					content, err := os.ReadFile(source.file)
					if err != nil {
						out.Println(watchLine(name, cli.Red("cannot read spec: "+err.Error())))

						return
					}

					w.cycle(ctx, out, i, name, content)
				})
			}

		case err, ok := <-w.fsw.Errors:
			if !ok {
				return
			}

			out.Println(watchLine("watch", cli.Red("watcher error: "+err.Error())))
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

			for i, source := range w.sources {
				if source.url == "" {
					continue
				}

				content, err := fetchSpecFromURL(ctx, source.url, 0)
				if err != nil {
					// A cancelled context is shutdown, not a poll failure:
					// without this check, Ctrl+C during an in-flight fetch
					// would print a spurious "fetch failed: context canceled"
					// on its way out.
					if ctx.Err() != nil {
						return
					}

					// Only the first of a run of identical failures is
					// printed, per source. A server that is down stays down
					// for minutes, and one line every five seconds saying so
					// buries the line that matters when it comes back.
					if msg := err.Error(); msg != w.lastPollErr[i] {
						w.lastPollErr[i] = msg
						out.Println(watchLine(source.url, cli.Red("fetch failed: "+msg)))
					}

					continue
				}

				w.lastPollErr[i] = ""

				w.cycle(ctx, out, i, source.url, content)
			}
		}
	}
}

// cycle regenerates from every source, if source i's fresh content is not
// what the current output was generated from. Serialized: a poll tick and a
// debounced filesystem event, for the same or a different source, must never
// check-and-write at the same time.
func (w *specWatcher) cycle(ctx context.Context, out watchReporter, i int, trigger string, content []byte) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.stopped.Load() {
		return
	}

	if !w.trackers[i].changed(content) {
		return
	}

	// A URL source generates from the temp file the plan resolved, refreshed
	// with what was just fetched -- so generation reads a file laid out
	// exactly as `forge client generate --from-url` would have left it.
	if w.sources[i].url != "" {
		if err := os.WriteFile(w.plan.specPaths[i], content, 0o600); err != nil {
			out.Println(watchLine(trigger, cli.Red("cannot stage fetched spec: "+err.Error())))

			return
		}
	}

	w.regenerateAll(ctx, out, trigger)
}

// regenerateAll parses every one of the plan's spec sources fresh from disk,
// merges them, and writes the client -- through resolveMergedSpec and
// applyPathFilter, the same path generateClient and checkClient take. It is
// called on a change to ANY source, and always reads every source rather than
// only the one that changed: a client generated from two documents is stale
// when either changes, and regenerating from only the source that triggered
// the run would silently drop whatever the other source last contributed if
// this source's file happened to not have moved since start-up.
//
// Callers hold w.mu for the duration; see cycle and Run.
func (w *specWatcher) regenerateAll(ctx context.Context, out watchReporter, trigger string) {
	started := time.Now()

	spec, err := resolveMergedSpec(ctx, w.plan.specPaths)
	if err != nil {
		// Never fatal. Report and keep watching; the next good save recovers.
		out.Println(watchLine(trigger, cli.Red("generation failed")))
		out.Println("           " + err.Error())

		return
	}

	if err := applyPathFilter(spec, w.plan.config.PathFilter); err != nil {
		out.Println(watchLine(trigger, cli.Red("generation failed")))
		out.Println("           " + err.Error())

		return
	}

	generated, err := w.gen.Generate(ctx, spec, w.plan.config)
	if err != nil {
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
