import 'package:flutter/widgets.dart';
import 'package:forge_client/forge_client.dart';

import 'scope.dart';

/// Marks queries of [binding] stale on [client]: every cached variant when
/// [args] is null, exactly the variant for [args] otherwise.
///
/// A mounted match refetches in the invalidator's next batch. An unmounted
/// match keeps the stale flag and refetches when it is next mounted, so a
/// list on a screen the user left costs nothing until they go back.
///
/// Matching reads the registry, which remembers every query the cache holds:
/// settled, failed, and still on their first fetch. A query that failed is
/// retried the same way a stale one is.
void invalidateBinding<T, A extends OperationArgs>(
  QueryCache client,
  QueryBinding<T, A> binding, [
  A? args,
]) {
  for (final entry in _matching(client, binding, args)) {
    client.invalidateQuery(binding.meta, _argsFor(client, binding, entry));
  }
}

/// Refetches queries of [binding] on [client] and completes when the mounted
/// ones have settled. Throws when a refetch fails, unlike [invalidateBinding].
///
/// Mounted matches start now, directly. They are not marked stale and left to
/// the batch: the batch runs on the cache's scheduler, so awaiting a request
/// here would let it settle first, and the batch would then find nothing in
/// flight and spend a second request on an answer already on screen. Unmounted
/// matches are marked stale as [invalidateBinding] marks them, and are not
/// waited for.
Future<void> refetchBinding<T, A extends OperationArgs>(
  QueryCache client,
  QueryBinding<T, A> binding, [
  A? args,
]) async {
  final running = <Future<Object?>>[];

  for (final entry in _matching(client, binding, args)) {
    final reproduced = _argsFor(client, binding, entry);

    if (entry.mounts == 0) {
      client.invalidateQuery(binding.meta, reproduced);
      continue;
    }

    running.add(client.refetch(binding.meta, reproduced));
  }

  await Future.wait(running);
}

/// The registry entries this call selects, collected before anything is done
/// to them. Refetching opens a record, and opening one can reap, which would
/// otherwise delete from the collection being walked.
///
/// The operation name comes back out of [QueryCache.key] rather than being
/// rebuilt from the method and path, so the key scheme stays the runtime's
/// business.
List<QueryEntry> _matching<T, A extends OperationArgs>(
  QueryCache client,
  QueryBinding<T, A> binding,
  A? args,
) {
  final operation = client.key(binding.meta, TagContext.empty);
  final target = args == null ? null : binding(args).key;

  return [
    for (final entry in client.registry.all())
      if (target == null ? entry.operation == operation : entry.key == target)
        entry,
  ];
}

/// The arguments that reproduce [entry]'s key.
///
/// A query keyed by its operation alone is refetched with
/// [TagContext.empty], never with whatever the entry holds, so the call can
/// only ever reach the record the widget is watching. `queryKey` already keys
/// an empty [TagContext] as the operation alone and the registry stores
/// [TagContext.empty] for a query opened with no arguments, so today the two
/// answers coincide. This keeps the guarantee local instead of depending on
/// that.
TagContext _argsFor<T, A extends OperationArgs>(
  QueryCache client,
  QueryBinding<T, A> binding,
  QueryEntry entry,
) => client.key(binding.meta, TagContext.empty) == entry.key
    ? TagContext.empty
    : entry.args;

/// Invalidation by binding, for widgets that do not hold the query they
/// need to refresh.
extension ForgeInvalidation on BuildContext {
  /// Marks every cached variant of [binding] stale, or only the one for
  /// [args]. Resolves the cache as [ForgeScope.of] does, without subscribing.
  void forgeInvalidate<T, A extends OperationArgs>(
    QueryBinding<T, A> binding, [
    A? args,
  ]) => invalidateBinding(ForgeScope.of(this, listen: false), binding, args);

  /// Refetches every mounted variant of [binding], or only the one for
  /// [args], and completes when they settle. Unmounted variants are marked
  /// stale and refetch when next mounted. Throws when a refetch fails.
  ///
  /// Every failure arrives through the returned future, a missing client
  /// included, so a caller that only awaits it sees them all.
  Future<void> forgeRefetch<T, A extends OperationArgs>(
    QueryBinding<T, A> binding, [
    A? args,
  ]) async => refetchBinding(ForgeScope.of(this, listen: false), binding, args);

  /// Invalidates already-resolved tags, as a settled mutation or a stream
  /// frame would.
  void forgeInvalidateTags(List<String> tags) =>
      ForgeScope.of(this, listen: false).invalidate(tags);
}
