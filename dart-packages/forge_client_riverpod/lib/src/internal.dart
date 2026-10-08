// Shared by the query and mutation providers. Not exported.
import 'dart:async';

import 'package:flutter/foundation.dart' show BindingBase, kDebugMode;
import 'package:flutter/scheduler.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:forge_client/forge_client.dart';

/// The retry policy of every Forge provider. Riverpod 3 already skips
/// retrying an `Error` (a `StateError` from `getClient`, say). This stops it
/// retrying an `Exception` too, which would resubscribe and refetch behind
/// the cache's back.
Duration? noRetry(int retryCount, Object error) => null;

/// A client's current principal, following `setPrincipal`.
///
/// It follows `watchPrincipalChanging`, which fires before the cache is
/// cleared, rather than `watchPrincipal`, which fires after. The clear
/// notifies watchers, and a read made from one of those notifications (a
/// `.state` listener, say), or from a `watchPrincipal` listener registered
/// earlier, must already find every provider that watches this one dirty, so
/// that it builds afresh instead of returning the previous principal's data.
final class _PrincipalNotifier extends Notifier<String?> {
  _PrincipalNotifier(this.client);

  final QueryCache client;

  @override
  String? build() {
    ref.onDispose(client.watchPrincipalChanging((next) => state = next));
    return client.principal;
  }
}

/// The principal of a client, for providers whose state belongs to one
/// principal: watching it rebuilds them, synchronously marked dirty, on
/// every `setPrincipal`.
final principalProvider = NotifierProvider.autoDispose
    .family<_PrincipalNotifier, String?, QueryCache>(
      _PrincipalNotifier.new,
      name: 'forgePrincipalProvider',
      retry: noRetry,
    );

/// How many Forge notifiers are inside their `build` right now, across every
/// container.
var _builds = 0;

/// Runs a Forge notifier's [build], counted, so a state that reaches any
/// Forge notifier meanwhile is held until the build has returned.
R building<R>(R Function() build) {
  _builds++;
  try {
    return build();
  } finally {
    _builds--;
  }
}

/// Whether a state written now would land in the middle of a build: a Forge
/// notifier's (so in the middle of whatever provider or widget initialized
/// it), or a widget build.
bool _midBuild() => _builds > 0 || _inWidgetBuild();

bool _inWidgetBuild() {
  final scheduler = _schedulerBinding();
  return scheduler != null &&
      scheduler.schedulerPhase == SchedulerPhase.persistentCallbacks;
}

SchedulerBinding? _scheduler;

SchedulerBinding? _schedulerBinding() {
  if (_scheduler case final scheduler?) return scheduler;
  // In debug the binding says whether it exists; the catch below is the
  // release fallback, where it cannot.
  if (kDebugMode && BindingBase.debugBindingType() == null) return null;
  try {
    return _scheduler = SchedulerBinding.instance;
  } on Object {
    // No binding yet, so no widget tree and no widget build: a plain
    // ProviderContainer in a Dart test or an isolate without Flutter. Not
    // remembered, as a binding can still be initialized later.
    return null;
  }
}

/// Hands one build's states to its notifier.
///
/// The cache notifies its listeners synchronously, so a state can arrive in
/// the middle of a build: a provider initialized during another provider's
/// build, or during a widget build, onto a stale query that this notifier
/// already watches starts a fetch, and that fetch's isFetching transition
/// reaches this notifier at once. A mutation called from a widget's build
/// method changes its state in the middle of that build too. Writing `state`
/// there fails Riverpod's assertion "Providers are not allowed to modify
/// other providers during their initialization" (and, under a widget build,
/// flutter_riverpod's "Tried to modify a provider while the widget tree was
/// building"). So a state that arrives mid-build is held and applied from a
/// microtask, which runs once the build has returned, with the latest state
/// winning. Every other state, the normal case, is applied at once.
///
/// After [close], or once the build's [Ref] is unmounted by a rebuild or a
/// dispose, nothing is applied, held or not.
final class StateInbox<S> {
  /// Creates an inbox applying to the notifier whose build owns [_ref].
  StateInbox(this._ref, this._apply);

  final Ref _ref;
  final void Function(S next) _apply;

  /// The latest state held for the microtask, if any. A record, so that a
  /// held null state is still a held state.
  (S,)? _pending;
  bool _scheduled = false;
  bool _closed = false;

  /// Applies [next] now, or from a microtask when it arrived mid-build.
  void receive(S next) {
    if (_closed) return;
    if (!_midBuild()) {
      // A held state is older than this one, so it must not land after it.
      _pending = null;
      _apply(next);
      return;
    }
    _pending = (next,);
    if (_scheduled) return;
    _scheduled = true;
    scheduleMicrotask(_flush);
  }

  void _flush() {
    _scheduled = false;
    final pending = _pending;
    _pending = null;
    if (pending == null || _closed || !_ref.mounted) return;
    _apply(pending.$1);
  }

  /// Stops applying anything, including a state already held.
  void close() {
    _closed = true;
    _pending = null;
  }
}
