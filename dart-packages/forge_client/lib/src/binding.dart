import 'cache.dart';
import 'codec.dart';
import 'invalidate.dart';
import 'operation.dart';
import 'overlay.dart';
import 'registry.dart';
import 'state.dart';
import 'tags.dart';

/// Typed models decoded from client-shaped values, per value and per
/// `fromClient`. The store hands back an identical value when nothing changed,
/// so a memo keyed on that value hands back an identical model.
final Expando<Map<Function, Object?>> _models = Expando<Map<Function, Object?>>(
  'forge.models',
);

/// Typed states built from the cache's states, per state object and per
/// `fromClient`. The cache reuses a state object while nothing in it moved.
final Expando<Map<Function, QueryState<Object?>>> _states =
    Expando<Map<Function, QueryState<Object?>>>('forge.states');

/// The last typed value each query decoded, per registry entry and per
/// `fromClient`. Keyed on the entry so it goes when the cache forgets the
/// query or is cleared, and never crosses to another principal.
final Expando<Map<Function, Object?>> _lastGood =
    Expando<Map<Function, Object?>>('forge.lastGood');

/// The idle state a disabled query reports, per binding, so a disabled
/// query reads as one identical state however often it is asked.
final Expando<QueryState<Object?>> _idles = Expando<QueryState<Object?>>(
  'forge.idles',
);

/// Whether [value] can key an [Expando]: strings, numbers, booleans, records
/// and null cannot.
bool _expandable(Object? value) => value is Map || value is List;

/// Decodes [value] with [fromClient], memoized on the value.
///
/// The same memo the identity rule uses. A generated decoder for a list of
/// entities calls this per element, so a row the store kept decodes to an
/// identical model even when the list around it changed.
T decodeCached<T>(FromClient<T> fromClient, Object? value) {
  if (!_expandable(value)) return fromClient(value);

  final memo = _models[value!] ??= <Function, Object?>{};

  if (memo.containsKey(fromClient)) return memo[fromClient] as T;

  final model = fromClient(value);
  memo[fromClient] = model;

  return model;
}

/// One read operation, bound to its model codec. What a generated
/// `bindings/*.dart` file declares: `final getOrder = query<Order,
/// GetOrderArgs>(opGetOrder, Order.fromClient);`.
final class QueryBinding<T, A extends OperationArgs> {
  /// Creates the binding.
  const QueryBinding(this.meta, this.fromClient);

  /// The operation.
  final OperationMeta meta;

  /// Builds the typed model from the client-shaped value.
  final FromClient<T> fromClient;

  /// This query called with [args].
  QueryRef<T, A> call(A args) => QueryRef<T, A>._(this, args);
}

/// One query: a binding plus its arguments. Runtime-agnostic: the Flutter
/// adapter, the Riverpod adapter and plain Dart all consume the same object.
///
/// **Identity rule.** [watch] and [getState] decode the cache's value with
/// `fromClient` and memoize the model on that value, so an unchanged read
/// yields an `identical` model and an unchanged state yields an `identical`
/// typed state. Every adapter relies on this to skip rebuilds.
final class QueryRef<T, A extends OperationArgs> {
  QueryRef._(this.binding, this.args) : context = args.toTagContext();

  /// The binding this query was made from.
  final QueryBinding<T, A> binding;

  /// The typed arguments.
  final A args;

  /// The arguments as the cache sees them.
  final TagContext context;

  /// This query's cache key. Two refs sharing it are the same query.
  late final String key = queryKey(binding.meta, context);

  /// Watches the query as typed states.
  ///
  /// [enabled] false yields a single [QueryIdle] and neither fetches nor
  /// ref-counts the query: the gate for dependent queries.
  Stream<QueryState<T>> watch(
    QueryCache client, {
    bool live = false,
    Duration? staleTime,
    bool enabled = true,
  }) {
    if (!enabled) {
      return Stream<QueryState<T>>.multi((controller) => controller.add(_idle));
    }

    return client
        .watch(binding.meta, context, live: live, staleTime: staleTime)
        .map((raw) => _typed(client, raw));
  }

  /// The current typed state, opening the query's record if it is new.
  ///
  /// [enabled] false returns a [QueryIdle] without touching [client]: no
  /// record is opened, as [watch] opens none.
  QueryState<T> getState(QueryCache client, {bool enabled = true}) {
    if (!enabled) return _idle;

    return _typed(client, client.getState(binding.meta, context));
  }

  QueryState<T> get _idle =>
      (_idles[binding] ??= QueryIdle<T>()) as QueryState<T>;

  /// Resolves with the value, fetching only when the cache holds nothing
  /// fresh.
  Future<T> fetch(QueryCache client) async =>
      _settled(client, await client.fetch(binding.meta, context));

  /// Fetches regardless of what the cache holds.
  Future<T> refetch(QueryCache client) async =>
      _settled(client, await client.refetch(binding.meta, context));

  /// Decodes a fetched value and remembers it. The entry is read after the
  /// fetch, which is what opened it.
  T _settled(QueryCache client, Object? value) =>
      _remember(client.registry.get(key), _model(value));

  T _model(Object? value) => decodeCached(binding.fromClient, value);

  QueryState<T> _typed(QueryCache client, QueryState<Object?> raw) {
    final memo = _states[raw] ??= <Function, QueryState<Object?>>{};
    final cached = memo[binding.fromClient];

    if (cached != null) return cached as QueryState<T>;

    final entry = client.registry.get(key);
    QueryState<T> typed;

    try {
      typed = switch (raw) {
        QueryIdle() => QueryIdle<T>(
          isFetching: raw.isFetching,
          isOptimistic: raw.isOptimistic,
          syncStatus: raw.syncStatus,
        ),
        QueryLoading() => QueryLoading<T>(
          isFetching: raw.isFetching,
          isOptimistic: raw.isOptimistic,
          syncStatus: raw.syncStatus,
        ),
        QuerySuccess(:final data) => QuerySuccess<T>(
          _remember(entry, _model(data)),
          isFetching: raw.isFetching,
          isOptimistic: raw.isOptimistic,
          syncStatus: raw.syncStatus,
        ),
        QueryFailure(:final error, :final previous) => QueryFailure<T>(
          error,
          previous: previous == null
              ? null
              : _remember(entry, _model(previous)),
          isFetching: raw.isFetching,
          isOptimistic: raw.isOptimistic,
          syncStatus: raw.syncStatus,
        ),
      };
    } on Object catch (error) {
      typed = raw.isOptimistic
          ? _optimisticFallback(entry, raw)
          // A value the model codec cannot read is a failure of this query,
          // not a crash in whoever is rendering it.
          : QueryFailure<T>(
              error,
              isFetching: raw.isFetching,
              isOptimistic: raw.isOptimistic,
              syncStatus: raw.syncStatus,
            );
    }

    memo[binding.fromClient] = typed;

    return typed;
  }

  T _remember(QueryEntry? entry, T model) {
    if (entry != null) {
      (_lastGood[entry] ??= <Function, Object?>{})[binding.fromClient] = model;
    }

    return model;
  }

  /// What to show while a pending optimistic change makes the value
  /// undecodable, typically a minted `~opt` id in a list of int ids: the last
  /// value this query decoded, or loading when it never decoded one. A raw
  /// failure stays a failure, carrying that value as its previous.
  QueryState<T> _optimisticFallback(
    QueryEntry? entry,
    QueryState<Object?> raw,
  ) {
    final memo = entry == null ? null : _lastGood[entry];

    if (memo == null || !memo.containsKey(binding.fromClient)) {
      return QueryLoading<T>(
        isFetching: raw.isFetching,
        isOptimistic: raw.isOptimistic,
        syncStatus: raw.syncStatus,
      );
    }

    final last = memo[binding.fromClient] as T;

    return switch (raw) {
      QueryFailure(:final error) => QueryFailure<T>(
        error,
        previous: last,
        isFetching: raw.isFetching,
        isOptimistic: raw.isOptimistic,
        syncStatus: raw.syncStatus,
      ),
      _ => QuerySuccess<T>(
        last,
        isFetching: raw.isFetching,
        isOptimistic: raw.isOptimistic,
        syncStatus: raw.syncStatus,
      ),
    };
  }
}

/// One write operation, bound to its codecs. [E] is the entity an optimistic
/// spec is written against; it differs from [R] for an enveloped response.
final class MutationBinding<R, A extends OperationArgs, E> {
  /// Creates the binding.
  const MutationBinding(
    this.meta,
    this.fromClient, {
    this.entityFromClient,
    this.entityToClient,
  });

  /// The operation.
  final OperationMeta meta;

  /// Builds the typed response from the client-shaped one.
  final FromClient<R> fromClient;

  /// Builds an entity model, for optimistic specs.
  final FromClient<E>? entityFromClient;

  /// Turns an entity model back into client shape, for optimistic specs.
  final ToClient<E>? entityToClient;

  /// Runs the mutation on [client]. A typed [optimistic] spec is converted to
  /// client shape with the entity codecs; without them the value is taken to
  /// be client-shaped already. An [OptimisticMany] is already client-shaped,
  /// so it is accepted only by a binding whose [E] is `Object?`.
  ///
  /// A conversion that throws is reported through the cache's `onError` with
  /// the context `optimistic`, and the write is sent without optimism: not
  /// being optimistic is a far smaller failure than not writing.
  Future<R> call(
    QueryCache client,
    A args, {
    Optimistic<E>? optimistic,
    Map<String, Placement> place = const {},
    RequestOptions options = const RequestOptions(),
  }) async {
    final response = await client.mutate(
      meta,
      args.toTagContext(),
      options: MutateOptions(
        headers: options.headers,
        cancel: options.cancel,
        place: place,
        optimistic: optimistic == null
            ? null
            : _clientShapedOrNull(client, optimistic),
      ),
    );

    return fromClient(response);
  }

  Optimistic<Object?>? _clientShapedOrNull(
    QueryCache client,
    Optimistic<E> spec,
  ) {
    try {
      return _clientShaped(spec);
    } on Object catch (error) {
      try {
        client.report(error, 'optimistic');
      } on Object {
        // Nowhere further to send it that would not risk the same failure.
      }

      return null;
    }
  }

  /// Converts [spec] to client shape. The spec's functions are applied inside
  /// the spec classes and never read off them here: [spec] may be a narrower
  /// spec than `Optimistic<E>` (an `OptimisticUpdate<Json>` passed where [E]
  /// is `Object?`), and reading a function-typed field through the wider view
  /// throws.
  Optimistic<Object?> _clientShaped(Optimistic<E> spec) {
    final fromEntity = entityFromClient;
    final toEntity = entityToClient;

    Object? decode(Object? client) =>
        fromEntity == null ? client : fromEntity(client);
    Object? encode(Object? model) =>
        toEntity == null ? model : toEntity(model as E);

    return switch (spec) {
      OptimisticUpdate(:final key) => OptimisticUpdate<Object?>(
        (previous) => spec.applyClient(previous, decode, encode),
        key: key,
      ),
      OptimisticDelete(:final key) => OptimisticDelete<Object?>(key: key),
      OptimisticCreate() => OptimisticCreate<Object?>(
        spec.encodeClient(encode),
      ),
      OptimisticMany() => spec,
    };
  }
}

/// Binds one read operation. The `query` a generated binding calls.
QueryBinding<T, A> query<T, A extends OperationArgs>(
  OperationMeta meta,
  FromClient<T> fromClient,
) => QueryBinding<T, A>(meta, fromClient);

/// Binds one write operation. The `mutation` a generated binding calls.
MutationBinding<R, A, E> mutation<R, A extends OperationArgs, E>(
  OperationMeta meta,
  FromClient<R> fromClient, {
  FromClient<E>? entityFromClient,
  ToClient<E>? entityToClient,
}) => MutationBinding<R, A, E>(
  meta,
  fromClient,
  entityFromClient: entityFromClient,
  entityToClient: entityToClient,
);
