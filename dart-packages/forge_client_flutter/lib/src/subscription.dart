import 'dart:async';

import 'package:flutter/scheduler.dart';
import 'package:forge_client/forge_client.dart';

import 'state_equality.dart';

/// One widget's subscription to one query: listen, swap and cancel.
///
/// Its identity is the client, [QueryRef.key] and the watch options, never
/// the args object, so a new args object with the same key keeps the same
/// subscription. A swap listens to the new query before cancelling the old
/// one, so a query that is the same on both sides never drops to zero mounts
/// in between.
///
/// The cache notifies its listeners synchronously, so an update can arrive
/// in the middle of a build: a widget mounting during a build onto a stale
/// query starts a fetch, and that fetch's isFetching transition reaches every
/// other widget watching the query at once. Calling `setState` there throws
/// "setState() or markNeedsBuild() called during build". So an update that
/// arrives during [SchedulerPhase.persistentCallbacks], the phase that runs
/// build, layout, paint and finalizeTree, is held and delivered in a
/// post-frame callback of the same frame, one callback per frame, with the
/// latest state winning. That frame is already running, so the callback is
/// sure to run, and the `setState` it leads to schedules the next frame.
/// Every other update is delivered synchronously.
///
/// The bootstrap build `runApp` runs outside any frame needs no deferral:
/// every subscription alive during it was opened in that same synchronous
/// build, so its first event is still waiting on a microtask, and the cache's
/// synchronous notifications queue behind that event rather than reaching
/// the listener during the build.
///
/// After [dispose], and for the previous query after a [bind] that
/// resubscribed, the change callback is never called again, even for an
/// update held for the end of the frame. Callers rely on that rather than
/// checking `mounted` themselves.
final class QuerySubscription<T> {
  /// Creates a subscription that reports every renderable change to its
  /// callback, with the state before and after it.
  QuerySubscription(this._onChange);

  final void Function(QueryState<T> previous, QueryState<T> next) _onChange;

  QueryCache? _client;
  String? _signature;
  StreamSubscription<QueryState<T>>? _subscription;
  QueryState<T>? _state;

  /// The latest update held for the end of the frame, if any.
  QueryState<T>? _pending;

  /// Bumped by every resubscribe and by [dispose], so a stream listener or a
  /// post-frame callback from an earlier subscription knows it is stale.
  int _generation = 0;

  /// Whether a post-frame callback is scheduled for this generation.
  bool _scheduled = false;

  /// The latest state. Valid as soon as the first [bind] returns, so the
  /// first frame never waits for the stream.
  QueryState<T> get state => _state!;

  /// Subscribes to [query] on [client], or keeps the current subscription
  /// when nothing that identifies it changed. Returns true when it
  /// resubscribed.
  bool bind(
    QueryCache client,
    QueryRef<T, OperationArgs> query, {
    required bool live,
    Duration? staleTime,
    required bool enabled,
  }) {
    final signature =
        '${query.key}\u0000$live\u0000${staleTime?.inMicroseconds}\u0000$enabled';
    if (identical(client, _client) && signature == _signature) return false;

    final previous = _subscription;
    _client = client;
    _signature = signature;
    _forgetPending();

    // The old stream stays live until it is cancelled below, and listening to
    // the new one can notify it synchronously (a same-key resubscribe that
    // starts a fetch). Each listener carries its generation, so an event from
    // a superseded stream reaches neither the callback nor `_pending`.
    final generation = _generation;
    _subscription = query
        .watch(client, live: live, staleTime: staleTime, enabled: enabled)
        .listen((next) {
          if (generation == _generation) _receive(next);
        });
    // `watch` delivers its first event on a microtask (forge_client decision
    // 9), so the starting state comes from getState, read after listening so
    // it already reflects the fetch the listen started.
    _state = firstState(client, query, enabled: enabled);

    if (previous != null) unawaited(previous.cancel());
    return true;
  }

  void _receive(QueryState<T> next) {
    final scheduler = SchedulerBinding.instance;
    if (scheduler.schedulerPhase != SchedulerPhase.persistentCallbacks) {
      // A held update is older than this one, so it must not land after it.
      _pending = null;
      _deliver(next);
      return;
    }

    _pending = next;
    if (_scheduled) return;
    _scheduled = true;
    final generation = _generation;
    scheduler.addPostFrameCallback((_) {
      if (generation != _generation) return;
      _scheduled = false;
      final pending = _pending;
      _pending = null;
      if (pending != null) _deliver(pending);
    }, debugLabel: 'QuerySubscription.deliver');
  }

  void _deliver(QueryState<T> next) {
    final before = _state!;
    // The first event repeats the seed read in bind, and the cache may emit
    // a state that renders the same; neither is a change.
    if (sameQueryState(before, next)) return;
    _state = next;
    _onChange(before, next);
  }

  void _forgetPending() {
    _generation++;
    _scheduled = false;
    _pending = null;
  }

  /// Cancels the subscription, which releases the query's mount, and drops
  /// any update held for the end of the frame.
  void dispose() {
    _forgetPending();
    final subscription = _subscription;
    _subscription = null;
    if (subscription != null) unawaited(subscription.cancel());
  }
}
