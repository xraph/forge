# forge_client_devtools

The forge tab for Flutter DevTools. It shows you why a query did or didn't
refetch, what the cache holds, the log of what caused what, captured stream
frames, the request log, the outbox and sync state, and it lets you switch the
app's network to offline or slow.

## Install

Add it as a dev dependency of your app. Nothing from it ships in the app:
DevTools loads the extension from the package, and `forge_client` registers the
`ext.forge.*` service extensions it talks to in debug and profile builds.

```yaml
dev_dependencies:
  forge_client_devtools:
    path: ../forge/dart-packages/forge_client_devtools
```

This package is read straight from its directory, and the built extension is
not committed, so build it once after you check out the repo:

```sh
cd forge/dart-packages/forge_client_devtools
./tool/build_extension.sh
```

Then run your app in debug or profile mode and open DevTools. The first time,
DevTools asks whether to enable the `forge_client_devtools` extension. Say yes
and the forge tab appears. DevTools remembers the answer in
`devtools_options.yaml` at your project root. You need Flutter 3.47 or newer.

## What you get

- Queries: every query the cache remembers, with status, mounts, tag and
  dependency counts. Open one to see what it provides, its tags, its
  dependencies and its last error, and to refetch, invalidate, mark stale or
  drop it. Filter by key.
- Entities: the store, a hundred rows at a time, filtered by type or by a
  piece of the key. Open one for its fields as the store holds them, the same
  record with pending optimistic writes folded in, what it references and which
  queries reached it. You can evict it.
- Tags: the tag graph, with the queries that carry each tag and the ones
  that are mounted. You can invalidate a tag from here. Below it are two
  questions you can ask about a query: why did it refetch, and why didn't it.
  The second lists the nearest misses (an invalidation of `Order:9` against a
  query that carries `Order[]`) and what to change. The same tab previews what an
  operation would invalidate without running it, if you gave the app's
  operations table to `registerForgeServiceExtensions`.
- Events: the event log, live, filtered by kind. Entries that were caused by
  another entry say which.
- Frames: captured stream frames with their payloads. Capture is off until
  you switch it on, and the ring has a fixed capacity.
- Requests: one line per request: the operation, attempts against the limit,
  duration, each retry with its status and delay, and time spent waiting on an
  auth refresh.
- Outbox: queued and failed writes, with replay and discard when the app
  passed an `OutboxInspector`.
- Sync: each sync source and what it says about itself, plus the status of
  each entity.

Across the top sit the cache counts (success, error, fetching, stale,
unmounted, records) and, when the app wired a simulator, the network rail.

### The network rail

The rail switches the app's network between Online, Slow and Offline, adds
extra latency (250 ms, 1 s or 3 s), and has a Fail next button that makes the
next request fail once with a 500. Disarm it if you change your mind. The
revalidation toggles the app registered show up here too.

The rail only has network controls if `configureClient` wrapped a
`RestTransport` for you, or you passed `controls:` to
`registerForgeServiceExtensions`. Offline apps need the simulator beneath the
outbox; the `forge_client` README shows the wiring.

## Privacy

Nothing crosses from one account to the next.

When the app changes account, `forge_client` drops its event log, frame ring,
request log, and the outbox and sync mirrors, and cancels any armed "fail next".
It keeps one marker entry that says the identity changed. The marker carries no
ids, no payloads and no account value. The extension clears its own event list
and every panel's cache when the marker arrives, so what you see afterwards is
the new account only.

During the switch itself, the app answers with empty lists on purpose. The
Queries, Entities, Outbox and Sync panels and the status bar show "switching
account" for that window instead of a cache that looks empty.

Nothing is kept across isolates either. After a hot restart or a new isolate,
the extension forgets everything it held, reads again from the app that is
there now, and reconnects by itself.

The request log keeps no headers and no request or response bodies. It does
show the operation, the path and the query values (each cut short).
The outbox shows ids, operation names, states and a failure kind such as
`conflict 409`, never a write's arguments. The Frames panel is the one place
that shows payloads, and only while you have capture switched on.

### Actions belong to a session

Every action the extension sends (refetch, invalidate, mark stale, drop,
evict, replay, discard, capture, and every change on the rail) carries the
session of the cache as the extension last saw it. If the app has changed
account since, it refuses the action and the panel shows the refusal, so a
click made against the previous account never lands on the new one. Evicting an
entity that sync owns is refused as well.

## When the tab says forge_client is not running

The app might be a release build, where the devtools are compiled out. It might
have been built with `--dart-define=forge.devtools=false`. It might not use
`forge_client` at all. Or it built its cache with the `QueryCache` constructor
and never called `registerForgeServiceExtensions(cache)`. The tab connects by
itself once the extensions appear.

## Working on the extension

```sh
fvm flutter test
./tool/build_extension.sh
fvm flutter run -d chrome --dart-define=use_simulated_environment=true
```

The build script runs `fvm flutter pub get`, builds the extension into
`extension/devtools/build/`, and then runs the validator from
`devtools_extensions` against the result. `flutter pub get` fetches packages the
first time, if your pub cache doesn't have them. `dart pub publish` includes the
build because `.pubignore` leaves it out of the ignore list, while git ignores
it.

The last command runs the extension in a browser inside a simulated DevTools
frame. Paste the VM service URI of a running debug app into its connection bar.
