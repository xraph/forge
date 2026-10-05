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
/// dirty, and they recompute once the outermost computation returns, each
/// after the dirty values it reads.
///
/// Errors: the first read rethrows what the compute function throws, so a
/// widget reading it shows an error widget. A later recomputation that
/// throws keeps the previous value and reports the error through
/// [FlutterError.reportError]; the value keeps every dependency it had, so it
/// recovers as soon as one of them changes and the compute function stops
/// throwing.
final class ForgeComputed<T> extends ChangeNotifier implements ValueListenable<T> {
  ForgeComputed._(this._key, this._owner);

  final ForgeComputedKey<T> _key;
  final ForgeScopeOwner _owner;
  final Set<Listenable> _listened = {};
  final Map<String, _QueryRead> _queries = {};
  late T _value;
  bool _computing = false;
  bool _disposed = false;

  @override
  T get value {
    if (_computing) {
      throw StateError(
        '$_key read itself while computing. A computed value cannot depend '
        'on itself, directly or through another computed value.',
      );
    }
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

  /// Recomputes and notifies when the value changed. An error keeps the
  /// previous value and is reported, never thrown, so one failing value
  /// cannot stop the others in its batch.
  void _refresh(String doing) {
    if (_disposed) return;
    final T next;
    try {
      next = _run();
    } catch (error, stack) {
      FlutterError.reportError(FlutterErrorDetails(
        exception: error,
        stack: stack,
        library: 'forge_client_flutter',
        context: ErrorDescription('while $doing $_key'),
      ));
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
  S computed<S>(ForgeComputedKey<S> key) => listen(_computed._owner.computed(key));

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
    final id = watchSignature(query, live: live, staleTime: staleTime, enabled: enabled);
    queries.add(id);
    return _computed._query(id, query, live: live, staleTime: staleTime, enabled: enabled);
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
  bool _disposed = false;

  /// The client computed values read queries from.
  QueryCache get client => _client;

  /// Moves every computed value's queries to [next]. Never throws: a value
  /// whose compute function throws is reported and still moved, and the
  /// rest move too, so no query is left mounted on the old client.
  set client(QueryCache next) {
    if (identical(next, _client)) return;
    _client = next;
    _batch(() {
      for (final computed in _computeds.values.toList()) {
        computed._refresh('moving to a new client');
      }
    });
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
          FlutterError.reportError(FlutterErrorDetails(
            exception: StateError(
              '${next._key} kept changing its own dependencies while '
              'computing. A compute function must only read, never write.',
            ),
            library: 'forge_client_flutter',
            context: ErrorDescription('while recomputing ${next._key}'),
          ));
        }
        continue;
      }
      next._refresh('recomputing');
    }
  }

  /// A dirty value that reads no other dirty value, directly or through
  /// clean ones, so it recomputes against fresh inputs.
  ForgeComputed<Object?> _nextDirty() {
    for (final candidate in _dirty) {
      if (!_readsDirty(candidate, {})) return candidate;
    }
    return _dirty.first;
  }

  bool _readsDirty(ForgeComputed<Object?> computed, Set<ForgeComputed<Object?>> seen) {
    for (final dependency in computed._dependencies) {
      if (!seen.add(dependency)) continue;
      if (_dirty.contains(dependency) || _readsDirty(dependency, seen)) return true;
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
