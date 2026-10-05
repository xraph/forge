import 'cache.dart';
import 'codec.dart';
import 'invalidate.dart';
import 'operation.dart';
import 'overlay.dart';
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
  String get key => queryKey(binding.meta, context);

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
      return Stream<QueryState<T>>.multi(
        (controller) => controller.add(QueryIdle<T>()),
      );
    }

    return client
        .watch(binding.meta, context, live: live, staleTime: staleTime)
        .map(_typed);
  }

  /// The current typed state, opening the query's record if it is new.
  QueryState<T> getState(QueryCache client) =>
      _typed(client.getState(binding.meta, context));

  /// Resolves with the value, fetching only when the cache holds nothing
  /// fresh.
  Future<T> fetch(QueryCache client) async =>
      _model(await client.fetch(binding.meta, context));

  /// Fetches regardless of what the cache holds.
  Future<T> refetch(QueryCache client) async =>
      _model(await client.refetch(binding.meta, context));

  T _model(Object? value) => decodeCached(binding.fromClient, value);

  QueryState<T> _typed(QueryState<Object?> raw) {
    final memo = _states[raw] ??= <Function, QueryState<Object?>>{};
    final cached = memo[binding.fromClient];

    if (cached != null) return cached as QueryState<T>;

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
          _model(data),
          isFetching: raw.isFetching,
          isOptimistic: raw.isOptimistic,
          syncStatus: raw.syncStatus,
        ),
        QueryFailure(:final error, :final previous) => QueryFailure<T>(
          error,
          previous: previous == null ? null : _model(previous),
          isFetching: raw.isFetching,
          isOptimistic: raw.isOptimistic,
          syncStatus: raw.syncStatus,
        ),
      };
    } on Object catch (error) {
      // A value the model codec cannot read is a failure of this query, not a
      // crash in whoever is rendering it.
      typed = QueryFailure<T>(
        error,
        isFetching: raw.isFetching,
        isOptimistic: raw.isOptimistic,
        syncStatus: raw.syncStatus,
      );
    }

    memo[binding.fromClient] = typed;

    return typed;
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
        optimistic: optimistic == null ? null : _clientShaped(optimistic),
      ),
    );

    return fromClient(response);
  }

  Optimistic<Object?> _clientShaped(Optimistic<E> spec) {
    final decode = entityFromClient ?? (Object? value) => value as E;
    final encode = entityToClient ?? (E value) => value;

    return switch (spec) {
      OptimisticUpdate(:final update, :final key) => OptimisticUpdate<Object?>(
        (previous) => encode(update(decode(previous))),
        key: key,
      ),
      OptimisticDelete(:final key) => OptimisticDelete<Object?>(key: key),
      OptimisticCreate(:final value) => OptimisticCreate<Object?>(
        encode(value),
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
