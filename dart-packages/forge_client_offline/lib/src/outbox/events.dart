import 'package:forge_client/forge_client.dart';

import 'outbox_entry.dart';
import 'outbox_failure.dart';

/// The observer event for a write entering the outbox.
CacheEvent outboxEnqueued(OutboxEntry entry) =>
    OutboxEnqueued(mutationId: entry.id, operationId: entry.operationId);

/// The observer event for a write the server accepted on replay.
CacheEvent outboxReplayed(OutboxEntry entry) =>
    OutboxReplayed(mutationId: entry.id, operationId: entry.operationId);

/// The observer event for a write that failed on replay.
CacheEvent outboxFailed(OutboxEntry entry, OutboxFailure failure) =>
    OutboxFailed(
      mutationId: entry.id,
      operationId: entry.operationId,
      failure: failure,
    );
