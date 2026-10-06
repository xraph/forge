import 'cache.dart';
import 'devtools/control.dart';
import 'devtools/release.dart';
import 'devtools/service_extensions.dart';
import 'invalidate.dart';
import 'storage.dart';
import 'sync.dart';
import 'transport.dart';
import 'types.dart';

/// The cache generated bindings use when an adapter is not handed one.
QueryCache? _active;

/// Builds a [QueryCache] and makes it the default. Returns it, for the
/// explicit path. Takes exactly the parameters [QueryCache] does.
///
/// In debug and profile builds it also attaches the devtools, with no code in
/// the app (see `package:forge_client/devtools.dart`). A [RestTransport] is
/// wrapped in the offline and latency simulator, which the cache is built
/// over, and feeds the request log. Any other transport is used as it is and
/// gets no simulator: in particular an `OutboxTransport` is never wrapped,
/// because a simulator outside the outbox fails a write before the outbox can
/// queue it. For an offline setup wire the simulator under the outbox
/// yourself, or let `OfflineClient.open(devtools: true)` do it. Release builds
/// contain none of this.
QueryCache configureClient({
  required Transport transport,
  required EntitySchema entities,
  Scheduler? scheduler,
  CommitScheduler? commitScheduler,
  int limit = 128,
  void Function(Object error, String context)? onError,
  int frameRestarts = 3,
  Clock clock = realClock,
  Duration? staleTime,
  List<SyncSource> syncSources = const [],
  StorageAdapter? storage,
}) {
  // Debug and profile only. `kForgeDevtools` is a constant, so in release this
  // folds to `null` and the simulator class is never compiled in. Only a
  // RestTransport is wrapped: the simulator has to sit directly on the wire,
  // beneath any outbox, so offline writes still queue and drains feel it.
  final controls = kForgeDevtools && transport is RestTransport
      ? ControlledTransport(transport)
      : null;

  final cache = QueryCache(
    transport: controls ?? transport,
    entities: entities,
    scheduler: scheduler,
    commitScheduler: commitScheduler,
    limit: limit,
    onError: onError,
    frameRestarts: frameRestarts,
    clock: clock,
    staleTime: staleTime,
    syncSources: syncSources,
    storage: storage,
  );

  _active = cache;

  if (kForgeDevtools) {
    registerForgeServiceExtensions(
      cache,
      transport: transport is RestTransport ? transport : null,
      controls: controls,
    );
  }

  return cache;
}

/// Installs an already-built cache as the default, or clears it with null.
void setClient(QueryCache? client) => _active = client;

/// The default cache. Throws [StateError] rather than silently caching into a
/// scratch one.
QueryCache getClient() =>
    _active ??
    (throw StateError(
      '[forge] no client configured: call configureClient() before using a binding',
    ));
