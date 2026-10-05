import 'cache.dart';
import 'invalidate.dart';
import 'transport.dart';
import 'types.dart';

/// The cache generated bindings use when an adapter is not handed one.
QueryCache? _active;

/// Builds a [QueryCache] and makes it the default. Returns it, for the
/// explicit path. Takes exactly the parameters [QueryCache] does.
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
}) => _active = QueryCache(
  transport: transport,
  entities: entities,
  scheduler: scheduler,
  commitScheduler: commitScheduler,
  limit: limit,
  onError: onError,
  frameRestarts: frameRestarts,
  clock: clock,
  staleTime: staleTime,
);

/// Installs an already-built cache as the default, or clears it with null.
void setClient(QueryCache? client) => _active = client;

/// The default cache. Throws [StateError] rather than silently caching into a
/// scratch one.
QueryCache getClient() =>
    _active ??
    (throw StateError(
      '[forge] no client configured: call configureClient() before using a binding',
    ));
