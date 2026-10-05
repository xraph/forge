import 'dart:async';

import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:forge_client/forge_client.dart';
import 'package:forge_client_flutter/forge_client_flutter.dart' show firstState, sameQueryState;

import 'client_provider.dart';
import 'internal.dart';

/// Turns a query binding into a Riverpod family. Declare it once, at the top
/// level:
///
/// ```dart
/// final getOrderProvider = queryProvider(getOrder);
///
/// // An AsyncValue<Order>.
/// final order = ref.watch(getOrderProvider(GetOrderArgs(id: '7')));
///
/// // The full QueryState<Order>, isFetching and syncStatus included.
/// final label = switch (ref.watch(getOrderProvider.state(GetOrderArgs(id: '7')))) {
///   QueryIdle() => 'Not requested',
///   QueryLoading() => 'Loading',
///   QuerySuccess(:final data) => 'Order ${data.id}',
///   QueryFailure(:final error) => 'Failed: $error',
/// };
/// ```
///
/// The value provider's data always belongs to the current client and
/// principal. A `setPrincipal` or a new `forgeClientProvider` starts it over
/// from loading, without the previous value, so `.value` never shows
/// another user's data.
///
/// The cache notifies synchronously, so a provider's `build` must not start
/// cache work: no `mutate`, `refetch`, `invalidate` or `setPrincipal`, and no
/// listening to a raw `QueryRef.watch` or `cache.watch` stream (a
/// `StreamProvider` over one included). Watch a query through
/// `queryProvider` instead; its providers hold back updates that arrive in
/// the middle of a build, and those calls would not.
ForgeQueryFamily<T, A> queryProvider<T, A extends OperationArgs>(
  QueryBinding<T, A> binding, {
  String? name,
}) =>
    ForgeQueryFamily<T, A>._(binding, name);

/// The family argument: the query plus its watch options.
///
/// Equality is over [QueryRef.key], the key the cache uses, so a new args
/// object with the same key is the same provider. Generated args classes
/// have value equality as well, so this agrees with equality by args; it is
/// also correct for a hand-written args class without `==`.
final class ForgeQueryParams<T, A extends OperationArgs> {
  /// Creates the parameters for [query].
  const ForgeQueryParams(
    this.query, {
    this.enabled = true,
    this.live = false,
    this.staleTime,
  });

  /// The query to watch.
  final QueryRef<T, A> query;

  /// While false the query is idle and nothing is fetched.
  final bool enabled;

  /// Also apply server frames.
  final bool live;

  /// How long this call site considers the result fresh.
  final Duration? staleTime;

  @override
  bool operator ==(Object other) =>
      other is ForgeQueryParams<T, A> &&
      other.query.key == query.key &&
      other.enabled == enabled &&
      other.live == live &&
      other.staleTime == staleTime;

  @override
  int get hashCode => Object.hash(query.key, enabled, live, staleTime);
}

/// The key of one value notifier: the query and its options, and whose data
/// it holds. A new client or principal is a new key, so a new notifier with
/// no previous value.
final class _ValueKey<T, A extends OperationArgs> {
  const _ValueKey(this.params, this.client, this.principal);

  final ForgeQueryParams<T, A> params;
  final QueryCache client;
  final String? principal;

  @override
  bool operator ==(Object other) =>
      other is _ValueKey<T, A> &&
      other.params == params &&
      identical(other.client, client) &&
      other.principal == principal;

  @override
  int get hashCode => Object.hash(params, identityHashCode(client), principal);
}

/// What [queryProvider] returns: call it for the `AsyncValue`, or use
/// [state] for the full [QueryState].
final class ForgeQueryFamily<T, A extends OperationArgs> {
  ForgeQueryFamily._(this.binding, this.name);

  /// The binding this family watches.
  final QueryBinding<T, A> binding;

  /// The name given to the providers, for Riverpod's devtools and errors.
  final String? name;

  // What `call` returns: a derived provider that exposes the value notifier
  // for the current client and principal. Riverpod carries an AsyncNotifier's
  // previous value into every later state, an error included, so the data of
  // the previous principal or client cannot be cleared by a write. A new key
  // gets a fresh notifier, and the old one is disposed, which releases its
  // mount.
  late final _values = Provider.autoDispose.family<AsyncValue<T>, ForgeQueryParams<T, A>>(
    (ref, params) {
      final client = ref.watch(forgeInstalledClientProvider);
      final principal = ref.watch(principalProvider(client));
      return ref.watch(_notifiers(_ValueKey<T, A>(params, client, principal)));
    },
    name: name,
    retry: noRetry,
  );

  late final _notifiers = AsyncNotifierProvider.autoDispose
      .family<ForgeQueryValueNotifier<T, A>, T, _ValueKey<T, A>>(
    (key) => ForgeQueryValueNotifier<T, A>(key.params, key.client),
    name: name == null ? null : '$name.value',
    retry: noRetry,
  );

  late final _states = NotifierProvider.autoDispose
      .family<ForgeQueryStateNotifier<T, A>, QueryState<T>, ForgeQueryParams<T, A>>(
    ForgeQueryStateNotifier<T, A>.new,
    name: name == null ? null : '$name.state',
    retry: noRetry,
  );

  /// The query for [args], as an `AsyncValue<T>`. It holds the query's mount
  /// while watched and releases it when disposed. Its value only ever holds
  /// the current client's and principal's data.
  Provider<AsyncValue<T>> call(
    A args, {
    bool enabled = true,
    bool live = false,
    Duration? staleTime,
  }) =>
      _values(_params(args, enabled, live, staleTime));

  /// The query for [args] as its full [QueryState], including `isFetching`,
  /// `isOptimistic`, `syncStatus` and the idle state of a disabled query.
  NotifierProvider<ForgeQueryStateNotifier<T, A>, QueryState<T>> state(
    A args, {
    bool enabled = true,
    bool live = false,
    Duration? staleTime,
  }) =>
      _states(_params(args, enabled, live, staleTime));

  ForgeQueryParams<T, A> _params(A args, bool enabled, bool live, Duration? staleTime) =>
      ForgeQueryParams<T, A>(binding(args), enabled: enabled, live: live, staleTime: staleTime);
}

/// Holds one query on one client, for one principal, as an `AsyncValue<T>`
/// behind `ForgeQueryFamily.call`.
final class ForgeQueryValueNotifier<T, A extends OperationArgs> extends AsyncNotifier<T> {
  /// Creates the notifier for [params] on [client].
  ForgeQueryValueNotifier(this.params, this.client);

  /// The query and options this notifier watches.
  final ForgeQueryParams<T, A> params;

  /// The cache it watches. A new client gets a new notifier, never this one.
  final QueryCache client;

  // Completed by the first data or error when the seed had none yet.
  Completer<T>? _first;

  // The success or failure last exposed. A failed query re-emits while it
  // refetches and a successful one on every isFetching flip; a repeat of the
  // same data or the same error writes nothing.
  QueryState<T>? _shown;

  @override
  FutureOr<T> build() => building(() {
        _first = null;
        _shown = null;
        final inbox = StateInbox<QueryState<T>>(ref, _apply);
        final subscription = params.query
            .watch(client, live: params.live, staleTime: params.staleTime, enabled: params.enabled)
            .listen(inbox.receive);
        ref.onDispose(() {
          inbox.close();
          unawaited(subscription.cancel());
        });

        // `watch` delivers its first event on a microtask, so the first value
        // comes from the seed, read after listening.
        final seed = firstState(client, params.query, enabled: params.enabled);
        switch (seed) {
          case QuerySuccess(:final data):
            _shown = seed;
            return data;
          case QueryFailure(:final error):
            _shown = seed;
            throw error;
          case QueryIdle() || QueryLoading():
            return (_first = Completer<T>()).future;
        }
      });

  void _apply(QueryState<T> next) {
    switch (next) {
      case QuerySuccess(:final data):
        if (_shown case QuerySuccess(data: final shown) when identical(shown, data)) return;
        _shown = next;
        final first = _first;
        _first = null;
        if (first != null) {
          first.complete(data);
        } else {
          state = AsyncData<T>(data);
        }
      case QueryFailure(:final error):
        if (_shown case QueryFailure(error: final shown) when identical(shown, error)) return;
        _shown = next;
        final first = _first;
        _first = null;
        if (first != null) {
          first.completeError(error, StackTrace.current);
        } else {
          // Riverpod keeps the previous value on an error set through `state`.
          state = AsyncError<T>(error, StackTrace.current);
        }
      case QueryIdle() || QueryLoading():
        return;
    }
  }
}

/// Holds one query's [QueryState] for `ForgeQueryFamily.state`.
final class ForgeQueryStateNotifier<T, A extends OperationArgs> extends Notifier<QueryState<T>> {
  /// Creates the notifier for [params].
  ForgeQueryStateNotifier(this.params);

  /// The query and options this notifier watches.
  final ForgeQueryParams<T, A> params;

  // The state last exposed, so a state that renders the same writes nothing.
  late QueryState<T> _shown;

  @override
  QueryState<T> build() => building(() {
        final client = ref.watch(forgeInstalledClientProvider);
        final inbox = StateInbox<QueryState<T>>(ref, _apply);
        final subscription = params.query
            .watch(client, live: params.live, staleTime: params.staleTime, enabled: params.enabled)
            .listen(inbox.receive);
        ref.onDispose(() {
          inbox.close();
          unawaited(subscription.cancel());
        });
        // `watch` delivers its first event on a microtask, so the starting
        // state comes from the seed, read after listening. The first event
        // repeats it and writes nothing.
        return _shown = firstState(client, params.query, enabled: params.enabled);
      });

  void _apply(QueryState<T> next) {
    if (sameQueryState(_shown, next)) return;
    _shown = next;
    state = next;
  }

  @override
  bool updateShouldNotify(QueryState<T> previous, QueryState<T> next) =>
      !sameQueryState(previous, next);
}
