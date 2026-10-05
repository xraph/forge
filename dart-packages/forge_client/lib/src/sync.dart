/// Where a sync source stands for one entity. Surfaced on every `QueryState`
/// as `syncStatus`, folded across the entities the query touches.
///
/// Plan 01a declares the status family because `QueryState` carries it; plan
/// 01b adds the `SyncSource` seam and the declarations beside it in this file.
sealed class SyncStatus {
  /// Const base constructor.
  const SyncStatus();
}

/// Everything local has reached the server.
final class Synced extends SyncStatus {
  /// Creates the status.
  const Synced();

  @override
  bool operator ==(Object other) => other is Synced;

  @override
  int get hashCode => (Synced).hashCode;
}

/// [count] local changes have not reached the server yet.
final class Pending extends SyncStatus {
  /// Creates the status.
  const Pending(this.count);

  /// How many changes are waiting.
  final int count;

  @override
  bool operator ==(Object other) => other is Pending && other.count == count;

  @override
  int get hashCode => Object.hash(Pending, count);
}

/// The source cannot reach the server.
final class Offline extends SyncStatus {
  /// Creates the status.
  const Offline();

  @override
  bool operator ==(Object other) => other is Offline;

  @override
  int get hashCode => (Offline).hashCode;
}

/// The source failed with [error], for example a server sync hook rejection.
final class SyncFailed extends SyncStatus {
  /// Creates the status.
  const SyncFailed(this.error);

  /// What went wrong.
  final Object error;

  @override
  bool operator ==(Object other) => other is SyncFailed && other.error == error;

  @override
  int get hashCode => Object.hash(SyncFailed, error);
}

/// Folds several statuses into one: [SyncFailed] beats [Offline], which beats
/// [Pending] (counts summed), which beats [Synced]. The first failure wins.
SyncStatus foldSyncStatus(Iterable<SyncStatus> statuses) {
  SyncFailed? failed;
  var offline = false;
  var pending = 0;

  for (final status in statuses) {
    switch (status) {
      case SyncFailed():
        failed ??= status;
      case Offline():
        offline = true;
      case Pending(:final count):
        pending += count;
      case Synced():
        break;
    }
  }

  if (failed != null) return failed;
  if (offline) return const Offline();
  if (pending > 0) return Pending(pending);

  return const Synced();
}
