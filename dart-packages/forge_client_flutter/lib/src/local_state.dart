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
final class ForgeComputed<T> extends ChangeNotifier implements ValueListenable<T> {
  ForgeComputed._(this._key, this._owner);

  /// How many times one recomputation may start over because a dependency
  /// changed while it ran, before it is reported as a loop.
  static const int _maxRestarts = 100;

  final ForgeComputedKey<T> _key;
  final ForgeScopeOwner _owner;
  final Set<Listenable> _listened = {};
  final Map<String, QuerySubscription<Object?>> _queries = {};
  late T _value;
  bool _computing = false;
  bool _changedWhileComputing = false;
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

  /// Computes the value, and computes it again when something it already
  /// read changed while it ran: a query read later in the same computation
  /// can start a fetch that changes the state of one read earlier, and the
  /// value must not keep the earlier state.
  T _run() {
    for (var restarts = 0;; restarts++) {
      if (restarts > _maxRestarts) {
        throw StateError(
          '$_key kept changing its own dependencies while computing. A '
          'compute function must only read, never write.',
        );
      }
      final reader = _Reader(this);
      _computing = true;
      _changedWhileComputing = false;
      final T result;
      try {
        result = _key.compute(reader);
      } finally {
        _computing = false;
        _reconcile(reader);
      }
      if (!_changedWhileComputing) return result;
    }
  }

  void _reconcile(_Reader reader) {
    for (final listenable in reader.listenables.difference(_listened)) {
      listenable.addListener(_changed);
    }
    for (final listenable in _listened.difference(reader.listenables)) {
      listenable.removeListener(_changed);
    }
    _listened
      ..clear()
      ..addAll(reader.listenables);

    final unused = [
      for (final id in _queries.keys)
        if (!reader.queries.contains(id)) id,
    ];
    for (final id in unused) {
      _queries.remove(id)!.dispose();
    }
  }

  QueryState<Q> _query<Q>(
    String id,
    QueryRef<Q, OperationArgs> query, {
    required bool live,
    Duration? staleTime,
    required bool enabled,
  }) {
    var subscription = _queries[id];
    if (subscription is! QuerySubscription<Q>) {
      // A different model type under the same key is a different read.
      subscription?.dispose();
      subscription = QuerySubscription<Q>((_, _) => _changed());
      _queries[id] = subscription;
    }
    // A no-op unless this read is new or the scope's client was swapped, in
    // which case it listens on the new client before releasing the old one.
    subscription.bind(_owner.client, query, live: live, staleTime: staleTime, enabled: enabled);
    return subscription.state;
  }

  void _changed() {
    if (_disposed) return;
    if (_computing) {
      _changedWhileComputing = true;
      return;
    }
    final next = _run();
    if (sameSelection(next, _value)) return;
    _value = next;
    notifyListeners();
  }

  /// Recomputes against the owner's new client. Every query this value still
  /// reads rebinds to that client as it is read, and the rest are released.
  void _clientChanged() => _changed();

  @override
  void dispose() {
    _disposed = true;
    for (final listenable in _listened) {
      listenable.removeListener(_changed);
    }
    _listened.clear();
    for (final subscription in _queries.values) {
      subscription.dispose();
    }
    _queries.clear();
    super.dispose();
  }
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

  QueryCache _client;
  final Map<ForgeStateKey<Object?>, ForgeState<Object?>> _states = {};
  final Map<ForgeComputedKey<Object?>, ForgeComputed<Object?>> _computeds = {};
  bool _disposed = false;

  /// The client computed values read queries from.
  QueryCache get client => _client;

  /// Moves every computed value's queries to [next].
  set client(QueryCache next) {
    if (identical(next, _client)) return;
    _client = next;
    for (final computed in _computeds.values.toList()) {
      computed._clientChanged();
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
  /// read. Throws a `StateError` when computing it would read itself.
  ForgeComputed<T> computed<T>(ForgeComputedKey<T> key) {
    _checkAlive();
    final existing = _computeds[key];
    if (existing != null) return existing as ForgeComputed<T>;
    final created = ForgeComputed<T>._(key, this);
    // In the map before computing, so a cycle finds it and reports itself
    // instead of recursing.
    _computeds[key] = created;
    try {
      created._start();
    } catch (_) {
      _computeds.remove(key);
      created.dispose();
      rethrow;
    }
    return created;
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
  }

  void _checkAlive() {
    if (_disposed) throw StateError('This ForgeScope has been disposed.');
  }
}
