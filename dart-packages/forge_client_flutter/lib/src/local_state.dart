import 'package:flutter/foundation.dart';
import 'package:forge_client/forge_client.dart';

import 'state_equality.dart';
import 'subscription.dart';

/// Names a piece of local state owned by a `ForgeScope`.
///
/// Declare keys once at the top level, like generated bindings. Each scope
/// that reads a key gets its own [ForgeState], created on first read from
/// [initial] and disposed with the scope. Keys compare by identity.
final class ForgeStateKey<T> {
  /// Creates a key whose state starts at `initial()`.
  const ForgeStateKey(this.initial, {this.debugLabel});

  /// Produces the starting value the first time a scope reads this key.
  final T Function() initial;

  /// A name for error messages.
  final String? debugLabel;

  @override
  String toString() => 'ForgeStateKey<$T>(${debugLabel ?? 'unnamed'})';
}

/// Local UI state owned by a scope: a [ValueNotifier] with [update].
///
/// Like any [ValueNotifier], set it from an event handler or a callback,
/// never from a `build` method: its listeners rebuild widgets.
final class ForgeState<T> extends ValueNotifier<T> {
  /// Creates state holding [value].
  ForgeState(super.value);

  /// Replaces the value with `change(value)`. Listeners hear about it only
  /// when the new value is not `==` to the old one.
  void update(T Function(T current) change) => value = change(value);
}

/// Names a value derived from states, other computed values, any
/// [ValueListenable] and queries, owned by a `ForgeScope`.
final class ForgeComputedKey<T> {
  /// Creates a key whose value is `compute(read)`.
  const ForgeComputedKey(this.compute, {this.debugLabel});

  /// Computes the value. Everything read through the [ForgeReader] is a
  /// dependency.
  final T Function(ForgeReader read) compute;

  /// A name for error messages.
  final String? debugLabel;

  @override
  String toString() => 'ForgeComputedKey<$T>(${debugLabel ?? 'unnamed'})';
}

/// What a compute function reads through. Every read is a dependency: when
/// it changes, the value is recomputed. Reads not repeated on the next
/// computation are dropped, and a query no longer read is released.
abstract interface class ForgeReader {
  /// The current value of the scope's state for [key].
  S state<S>(ForgeStateKey<S> key);

  /// The current value of the scope's computed value for [key].
  S computed<S>(ForgeComputedKey<S> key);

  /// The current value of any [ValueListenable], such as a
  /// `TextEditingController`.
  S listen<S>(ValueListenable<S> listenable);

  /// The current state of [query] on the scope's client. Reading it mounts
  /// the query, so it fetches like a builder would.
  QueryState<Q> query<Q>(
    QueryRef<Q, OperationArgs> query, {
    bool live = false,
    Duration? staleTime,
    bool enabled = true,
  });
}

/// A derived value owned by a scope. Listen to it with
/// `ValueListenableBuilder`.
///
/// It notifies only when its value changes by identity or deep equality, so
/// a compute function that builds a new list each time does not wake its
/// listeners while the list's contents are unchanged.
///
/// Its queries are read through the same subscription the builders use, so
/// a query update that arrives during a build, layout or paint is applied at
/// the end of that frame rather than in the middle of it. First reading a
/// computed value inside a `build` method is safe even when that read starts
/// a fetch other widgets are watching.
///
/// Recomputing is batched per scope: a change that arrives while any of the
/// scope's computed values is computing only marks the affected values
/// dirty, and they recompute once the outermost computation returns. A
/// value that is read while it is dirty, by a computation or by a listener
/// in the same batch, recomputes on the spot before the read returns, so
/// nothing in a batch reads a value that is waiting to be recomputed, even
/// one it never read before. This holds only within one batch: when a
/// single change made outside any computation notifies two values directly
/// and one of them reads the other, the reader can recompute once against
/// the other's previous value before the other's notification recomputes it
/// again.
///
/// Errors: the first read rethrows what the compute function throws, so a
/// widget reading it shows an error widget. A later recomputation that
/// throws keeps the previous value and reports the error through
/// [FlutterError.reportError]; the value keeps every dependency it had, so it
/// recovers as soon as one of them changes and the compute function stops
/// throwing.
///
/// A recomputation that throws while the scope's client is being swapped is
/// different, because the previous value came from the old client: on an
/// account switch it was derived from the previous user's data. The error is
/// reported, listeners are notified, and from then on reading [value]
/// rethrows that error, exactly like a failing first read, until a
/// recomputation succeeds. Values that read a failed one fail with it.
/// Read [value] in the builder of a `ListenableBuilder` when this matters: a
/// `ValueListenableBuilder` keeps the last value it read, so it would go on
/// showing the old client's value.
final class ForgeComputed<T> extends ChangeNotifier
    implements ValueListenable<T> {
  ForgeComputed._(this._key, this._owner);

  final ForgeComputedKey<T> _key;
  final ForgeScopeOwner _owner;
  final Set<Listenable> _listened = {};
  final Map<String, _QueryRead> _queries = {};
  late T _value;
  bool _computing = false;
  bool _disposed = false;

  /// The error a recomputation threw during a client swap, while no value
  /// from the new client has replaced the old one. Null when healthy.
  Object? _failure;
  StackTrace? _failureStack;

  @override
  T get value {
    if (_computing) {
      throw StateError(
        '$_key read itself while computing. A computed value cannot depend '
        'on itself, directly or through another computed value.',
      );
    }
    // Waiting to be recomputed in this batch: recompute now, so the reader
    // never sees the value from before the change, such as the old client's.
    if (_owner._dirty.remove(this)) _refresh();
    final failure = _failure;
    if (failure != null) Error.throwWithStackTrace(failure, _failureStack!);
    return _value;
  }

  void _start() => _value = _run();

  T _run() {
    final reader = _Reader(this);
    _computing = true;
    final T result;
    try {
      result = _key.compute(reader);
    } catch (_) {
      _computing = false;
      _reconcile(reader, threw: true);
      rethrow;
    }
    _computing = false;
    _reconcile(reader, threw: false);
    return result;
  }

  /// Drops what the pass did not read. A pass that threw drops nothing,
  /// since it may have thrown before reaching its usual reads: the value
  /// keeps listening to all of them, so it can recover. Queries it kept
  /// still follow the scope's client.
  void _reconcile(_Reader reader, {required bool threw}) {
    if (threw) {
      for (final MapEntry(:key, :value) in _queries.entries) {
        if (!reader.queries.contains(key)) value.bindTo(_owner.client);
      }
      return;
    }

    for (final listenable in _listened.difference(reader.listenables)) {
      listenable.removeListener(_changed);
    }
    _listened.retainAll(reader.listenables);

    final unused = [
      for (final id in _queries.keys)
        if (!reader.queries.contains(id)) id,
    ];
    for (final id in unused) {
      _queries.remove(id)!.subscription.dispose();
    }
  }

  /// Listens to [listenable] from the moment it is read, so a change later
  /// in the same computation is not missed.
  void _depend(Listenable listenable) {
    if (_listened.add(listenable)) listenable.addListener(_changed);
  }

  QueryState<Q> _query<Q>(
    String id,
    QueryRef<Q, OperationArgs> query, {
    required bool live,
    Duration? staleTime,
    required bool enabled,
  }) {
    var read = _queries[id];
    if (read == null || read.subscription is! QuerySubscription<Q>) {
      // A different model type under the same key is a different read.
      read?.subscription.dispose();
      read = _QueryRead(QuerySubscription<Q>((_, _) => _changed()));
      _queries[id] = read;
    }
    final subscription = read.subscription as QuerySubscription<Q>;
    read.bindTo = (client) => subscription.bind(
      client,
      query,
      live: live,
      staleTime: staleTime,
      enabled: enabled,
    );
    // A no-op unless this read is new or the scope's client was swapped, in
    // which case it listens on the new client before releasing the old one.
    read.bindTo(_owner.client);
    return subscription.state;
  }

  void _changed() {
    if (_disposed) return;
    _owner._invalidate(this);
  }

  /// Recomputes and notifies when the value changed. An error is reported,
  /// never thrown, so one failing value cannot stop the others in its batch.
  /// It keeps the previous value, unless the scope is moving to a new client
  /// or the value has already failed: then the value fails, so nothing from
  /// the old client stays readable.
  void _refresh() {
    if (_disposed) return;
    final moving = _owner._movingClient;
    final T next;
    try {
      next = _run();
    } catch (error, stack) {
      FlutterError.reportError(
        FlutterErrorDetails(
          exception: error,
          stack: stack,
          library: 'forge_client_flutter',
          context: ErrorDescription(
            moving
                ? 'while moving $_key to a new client'
                : 'while recomputing $_key',
          ),
        ),
      );
      if (moving || _failure != null) {
        final entering = _failure == null;
        _failure = error;
        _failureStack = stack;
        if (entering) notifyListeners();
      }
      return;
    }
    if (_failure != null) {
      // Leaving the failed state is a change whatever the new value is.
      _failure = null;
      _failureStack = null;
      _value = next;
      notifyListeners();
      return;
    }
    if (sameSelection(next, _value)) return;
    _value = next;
    notifyListeners();
  }

  /// The computed values this one read last time, for ordering a batch.
  Iterable<ForgeComputed<Object?>> get _dependencies =>
      _listened.whereType<ForgeComputed<Object?>>();

  @override
  void dispose() {
    _disposed = true;
    for (final listenable in _listened) {
      listenable.removeListener(_changed);
    }
    _listened.clear();
    for (final read in _queries.values) {
      read.subscription.dispose();
    }
    _queries.clear();
    super.dispose();
  }
}

/// One query a computed value reads, and how to bind it to a client again.
final class _QueryRead {
  _QueryRead(this.subscription);

  final QuerySubscription<Object?> subscription;
  late void Function(QueryCache client) bindTo;
}

final class _Reader implements ForgeReader {
  _Reader(this._computed);

  final ForgeComputed<Object?> _computed;
  final Set<Listenable> listenables = {};
  final Set<String> queries = {};

  @override
  S state<S>(ForgeStateKey<S> key) => listen(_computed._owner.state(key));

  @override
  S computed<S>(ForgeComputedKey<S> key) =>
      listen(_computed._owner.computed(key));

  @override
  S listen<S>(ValueListenable<S> listenable) {
    listenables.add(listenable);
    _computed._depend(listenable);
    return listenable.value;
  }

  @override
  QueryState<Q> query<Q>(
    QueryRef<Q, OperationArgs> query, {
    bool live = false,
    Duration? staleTime,
    bool enabled = true,
  }) {
    final id = watchSignature(
      query,
      live: live,
      staleTime: staleTime,
      enabled: enabled,
    );
    queries.add(id);
    return _computed._query(
      id,
      query,
      live: live,
      staleTime: staleTime,
      enabled: enabled,
    );
  }
}

/// Owns one scope's states and computed values. Internal: widgets reach it
/// through `context.forgeState` and `context.forgeComputed`.
final class ForgeScopeOwner {
  /// Creates an owner whose computed values read queries from [client].
  ForgeScopeOwner(this._client);

  /// How many times one batch may recompute the same value before it is
  /// reported as a loop: a compute function that writes what it reads.
  static const int _maxRefreshes = 100;

  QueryCache _client;
  final Map<ForgeStateKey<Object?>, ForgeState<Object?>> _states = {};
  final Map<ForgeComputedKey<Object?>, ForgeComputed<Object?>> _computeds = {};

  /// Values to recompute once the current batch's computations return.
  final Set<ForgeComputed<Object?>> _dirty = {};

  /// Whether a computation or a flush is running in this scope.
  bool _busy = false;

  /// Whether the current batch is moving every value to a new client.
  bool _movingClient = false;
  bool _disposed = false;

  /// The client computed values read queries from.
  QueryCache get client => _client;

  /// Moves every computed value's queries to [next]. Never throws: a value
  /// whose compute function throws is reported, fails rather than keeping
  /// its old-client value, and is still moved; the rest move too, so no query
  /// is left mounted on the old client.
  ///
  /// Every value is marked dirty and the batch recomputes them in dependency
  /// order, so a value never recomputes against an input that still holds
  /// the old client's value.
  set client(QueryCache next) {
    if (identical(next, _client)) return;
    _client = next;
    // Called from didUpdateWidget, never from inside a computation, so this
    // batch is the outermost one and flushes before the flag drops.
    assert(!_busy, 'The client changed during a computation.');
    _movingClient = true;
    try {
      _batch(() => _dirty.addAll(_computeds.values));
    } finally {
      _movingClient = false;
    }
  }

  /// This scope's state for [key], created on first read.
  ForgeState<T> state<T>(ForgeStateKey<T> key) {
    _checkAlive();
    final existing = _states[key];
    if (existing != null) return existing as ForgeState<T>;
    final created = ForgeState<T>(key.initial());
    _states[key] = created;
    return created;
  }

  /// This scope's computed value for [key], created and computed on first
  /// read. Throws a `StateError` when computing it would read itself, and
  /// rethrows what its compute function throws.
  ForgeComputed<T> computed<T>(ForgeComputedKey<T> key) {
    _checkAlive();
    final existing = _computeds[key];
    if (existing != null) return existing as ForgeComputed<T>;
    final created = ForgeComputed<T>._(key, this);
    // In the map before computing, so a cycle finds it and reports itself
    // instead of recursing.
    _computeds[key] = created;
    _batch(() {
      try {
        created._start();
      } catch (_) {
        // Gone before the batch flushes, so nothing recomputes a value that
        // never had one.
        _computeds.remove(key);
        created.dispose();
        rethrow;
      }
    });
    return created;
  }

  void _invalidate(ForgeComputed<Object?> computed) {
    _dirty.add(computed);
    if (!_busy) _batch(() {});
  }

  /// Runs [body] as the outermost computation, then recomputes everything
  /// it left dirty. Nested calls run [body] inside the current batch.
  void _batch(void Function() body) {
    if (_busy) return body();
    _busy = true;
    try {
      body();
    } finally {
      try {
        _flush();
      } finally {
        _busy = false;
      }
    }
  }

  void _flush() {
    final refreshes = <ForgeComputed<Object?>, int>{};
    while (_dirty.isNotEmpty) {
      final next = _nextDirty();
      _dirty.remove(next);
      if (next._disposed) continue;
      final count = refreshes[next] = (refreshes[next] ?? 0) + 1;
      if (count > _maxRefreshes) {
        if (count == _maxRefreshes + 1) {
          FlutterError.reportError(
            FlutterErrorDetails(
              exception: StateError(
                '${next._key} kept changing its own dependencies while '
                'computing. A compute function must only read, never write.',
              ),
              library: 'forge_client_flutter',
              context: ErrorDescription('while recomputing ${next._key}'),
            ),
          );
        }
        continue;
      }
      next._refresh();
    }
  }

  /// A dirty value that read no other dirty value last time, directly or
  /// through clean ones. Only saves work: a dirty value that a computation
  /// reads, even for the first time, recomputes on read anyway.
  ForgeComputed<Object?> _nextDirty() {
    for (final candidate in _dirty) {
      if (!_readsDirty(candidate, {})) return candidate;
    }
    return _dirty.first;
  }

  bool _readsDirty(
    ForgeComputed<Object?> computed,
    Set<ForgeComputed<Object?>> seen,
  ) {
    for (final dependency in computed._dependencies) {
      if (!seen.add(dependency)) continue;
      if (_dirty.contains(dependency) || _readsDirty(dependency, seen)) {
        return true;
      }
    }
    return false;
  }

  /// Disposes every computed value, then every state.
  void dispose() {
    _disposed = true;
    for (final computed in _computeds.values) {
      computed.dispose();
    }
    for (final state in _states.values) {
      state.dispose();
    }
    _computeds.clear();
    _states.clear();
    _dirty.clear();
  }

  void _checkAlive() {
    if (_disposed) throw StateError('This ForgeScope has been disposed.');
  }
}
