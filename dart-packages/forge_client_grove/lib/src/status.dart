import 'package:forge_client/forge_client.dart';
import 'package:grove_crdt/grove_crdt.dart';

import 'config.dart';

/// One dataset's status for one entity.
///
/// A terminal engine state wins (`SyncFailed(GroveDatasetGone)`, then
/// `SyncFailed(GroveUnauthorized)`), then refused changes
/// (`SyncFailed(GroveChangeRejected)`), then [Offline] while [online] is false
/// or the last run failed on a network error, then [Pending] with the
/// pushable count, then [Synced].
SyncStatus datasetStatus({
  required SyncEngineState engine,
  required bool online,
  required int pending,
  required List<GroveRejectedChange> rejected,
  required String datasetId,
  String goneMessage = '',
}) {
  if (engine == SyncEngineState.gone) {
    return SyncFailed(GroveDatasetGone(datasetId, goneMessage));
  }

  if (engine == SyncEngineState.unauthorized) {
    return SyncFailed(GroveUnauthorized(datasetId));
  }

  if (rejected.isNotEmpty) return SyncFailed(GroveChangeRejected(rejected));
  if (!online || engine == SyncEngineState.offline) return const Offline();
  if (pending > 0) return Pending(pending);

  return const Synced();
}

/// The devtools name of a status: `synced`, `pending`, `offline` or `failed`.
String statusName(SyncStatus s) => switch (s) {
  Synced() => 'synced',
  Pending() => 'pending',
  Offline() => 'offline',
  SyncFailed() => 'failed',
};
