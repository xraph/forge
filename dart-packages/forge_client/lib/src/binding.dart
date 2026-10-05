import 'cache.dart';
import 'codec.dart';
import 'invalidate.dart';
import 'operation.dart';
import 'overlay.dart';
import 'state.dart';
import 'tags.dart';

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
  Stream<QueryState<T>> watch(
    QueryCache client, {
    bool live = false,
    Duration? staleTime,
  }) {
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

  T _model(Object? value) => binding.fromClient(value);

  QueryState<T> _typed(QueryState<Object?> raw) {
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
