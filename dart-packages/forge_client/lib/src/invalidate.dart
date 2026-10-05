import 'dart:async';
import 'dart:developer' as developer;

import 'operation.dart';
import 'registry.dart';
import 'tags.dart';

/// When an invalidation batch runs.
abstract interface class Scheduler {
  /// Arranges for [flush] to run later.
  void schedule(void Function() flush);
}

final class _MicrotaskScheduler implements Scheduler {
  const _MicrotaskScheduler();

  @override
  void schedule(void Function() flush) => scheduleMicrotask(flush);
}

/// The default: one batch per microtask. Every invalidation raised
/// synchronously in the same turn lands in one batch.
Scheduler microtaskScheduler() => const _MicrotaskScheduler();

/// A scheduler that runs nothing until asked, so a coalescing window is
/// testable without sleeping.
final class ManualScheduler implements Scheduler {
  void Function()? _queued;

  @override
  void schedule(void Function() flush) {
    _queued = flush;
  }

  /// Runs the pending batch, if there is one.
  void flush() {
    final run = _queued;
    _queued = null;
    run?.call();
  }

  /// Whether a batch is waiting.
  bool get pending => _queued != null;
}

/// The placement escape hatch, declared per tag at the mutation site.
///
/// [created] is what the mutation produced, [current] is what the query holds
/// now, [args] are the query's own arguments. Return the new list to place the
/// entity and skip the refetch; return null to fall back to it.
typedef Placement = List<Object?>? Function(
  Object? created,
  Object? current,
  TagContext args,
);

const Object _absent = _Absent();

final class _Absent {
  const _Absent();
}

/// A settled mutation, as the cache reports it to the [Invalidator].
final class MutationSettled {
  /// Creates a settled mutation. When [created] is left out, placement
  /// callbacks are handed [response].
  const MutationSettled({
    this.invalidates = const [],
    this.args = TagContext.empty,
    this.response,
    this._created = _absent,
    this.place,
  });

  /// `invalidates`, still as templates.
  final List<String> invalidates;

  /// The mutation's own arguments.
  final TagContext args;

  /// The mutation's response, for resolving `{res.a.b}`.
  final Object? response;

  final Object? _created;

  /// Per-tag placement callbacks.
  final Map<String, Placement>? place;

  /// What placement callbacks are handed: `created` when given, else the
  /// response.
  Object? get created => identical(_created, _absent) ? response : _created;
}

/// Turns "this mutation invalidates `Order[]`" into "these mounted queries
/// must refetch". Owns the policy; [QueryRegistry] owns the state.
final class Invalidator {
  /// Creates an invalidator over [registry].
  ///
  /// [execute] refetches one batch. [onUnresolved] defaults to a one-time
  /// warning per template. [onInvalidated] is told synchronously, before
  /// placement and before the batch, that a query is behind.
  Invalidator(
    this.registry, {
    required void Function(List<QueryEntry>) execute,
    Scheduler? scheduler,
    void Function(String template, String context)? onUnresolved,
    void Function(Object error, String context)? onError,
    void Function(QueryEntry entry, List<Object?> value)? onPlace,
    void Function(QueryEntry entry, Set<String> matched)? onInvalidated,
    // ignore: prefer_initializing_formals
  }) : _scheduler = scheduler ?? microtaskScheduler(),
       // ignore: prefer_initializing_formals
       _execute = execute,
       _onUnresolved = onUnresolved ?? _warnUnresolved,
       // ignore: prefer_initializing_formals
       _onError = onError,
       // ignore: prefer_initializing_formals
       _onPlace = onPlace,
       // ignore: prefer_initializing_formals
       _onInvalidated = onInvalidated {
    // A query that mounts having been invalidated while unmounted reaches the
    // batch through the same queue, so it refetches once.
    registry.onStale = _enqueue;
    registry.onUnresolved ??= (template, entry) =>
        _onUnresolved(template, '${entry.operation} provides');
  }

  /// The registry this invalidator drives.
  final QueryRegistry registry;

  final Set<QueryEntry> _queue = <QueryEntry>{};
  bool _scheduled = false;
  final Scheduler _scheduler;
  final void Function(List<QueryEntry> batch) _execute;
  final void Function(String template, String context) _onUnresolved;
  final void Function(Object error, String context)? _onError;
  final void Function(QueryEntry entry, List<Object?> value)? _onPlace;
  final void Function(QueryEntry entry, Set<String> matched)? _onInvalidated;

  /// A mutation settled: resolve its tags, then apply them.
  void settled(MutationSettled mutation) {
    final resolved = resolveTags(
      mutation.invalidates,
      mutation.args,
      mutation.response,
    );

    for (final template in resolved.unresolved) {
      _onUnresolved(template, 'invalidates');
    }

    _apply(resolved.tags, mutation);
  }

  /// Invalidates tags that are already resolved.
  void invalidate(Iterable<String> tags) =>
      _apply(tags, const MutationSettled());

  /// Runs the pending batch now, whatever the scheduler had planned.
  void flush() {
    _scheduled = false;

    // Unmounted between the invalidation and this flush: it stays stale and
    // refetches if it mounts again.
    final batch = _queue
        .where((entry) => entry.mounts > 0 && entry.stale)
        .toList();

    _queue.clear();

    if (batch.isEmpty) return;

    try {
      _execute(batch);
    } on Object catch (error) {
      _onError?.call(error, 'execute');
    }
  }

  void _apply(Iterable<String> tags, MutationSettled mutation) {
    for (final MapEntry(key: entry, value: matched)
        in registry.invalidated(tags).entries) {
      _onInvalidated?.call(entry, matched);

      final placed = _place(entry, matched, mutation);

      if (placed != null) {
        registry.place(entry, placed);
        _onPlace?.call(entry, placed);
        continue;
      }

      registry.markStale(entry);
    }
  }

  /// All or nothing per query: a query matched by two tags where only one has
  /// a callback still refetches. Callbacks chain.
  List<Object?>? _place(
    QueryEntry entry,
    Set<String> matched,
    MutationSettled mutation,
  ) {
    final callbacks = mutation.place;

    if (callbacks == null) return null;

    final created = mutation.created;
    Object? current = entry.value;
    List<Object?>? placed;

    for (final tag in matched) {
      final callback = callbacks[tag];

      if (callback == null) return null;

      List<Object?>? next;

      try {
        next = callback(created, current, entry.args);
      } on Object catch (error) {
        _onError?.call(error, 'place $tag');

        return null;
      }

      if (next == null) return null;

      current = next;
      placed = next;
    }

    return placed;
  }

  void _enqueue(QueryEntry entry) {
    _queue.add(entry);

    if (_scheduled) return;

    _scheduled = true;
    _scheduler.schedule(() {
      if (_scheduled) flush();
    });
  }
}

final Set<String> _warned = <String>{};

void _warnUnresolved(String template, String context) {
  if (!_warned.add(template)) return;

  developer.log(
    'tag template $template ($context) resolved to nothing and was skipped',
    name: 'forge',
  );
}
