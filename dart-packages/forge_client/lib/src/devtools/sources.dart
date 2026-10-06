/// Where the Outbox and Sync panels get their rows. New in Dart.
///
/// `forge_client` cannot import the offline or grove packages. The Outbox
/// panel lists queued writes from the cache's `StorageSession` and follows
/// them through the `OutboxEnqueued`, `OutboxReplayed` and `OutboxFailed`
/// events. A stored failure is reduced to its kind and status (or, for an
/// uncertain write, its reason): never a response body, never the arguments,
/// never the idempotency key. Replay and discard go through `OutboxInspector`
/// and sync detail through `DevtoolsInspectable`, both declared in
/// `observe.dart`; this file holds only the mirrors built from events and the
/// reader of a record's state.
///
/// Nothing crosses principals: both mirrors have a [OutboxMirror.clear] and a
/// [SyncMirror.clear], which `Devtools` calls from the same synchronous
/// listener that purges the event log.
library;

import 'dart:convert';

import '../types.dart';
import 'seams.dart';

const _unknown = (state: 'unknown', failure: null, since: null);

/// Reads a `PendingMutationRecord.stateJson` (`{"kind":"queued"}`,
/// `{"kind":"sending","at":ms}` or `{"kind":"failed","failure":{...}}`). A
/// failure is reduced to its kind and status (`conflict 409`), or for an
/// `uncertain` write to its reason; the stored `body` and any raw text are
/// never shown. A state this cannot parse reads as `unknown`, so a panel call
/// never throws over it.
({String state, String? failure, int? since}) outboxStateOf(String stateJson) {
  Object? decoded;
  try {
    decoded = jsonDecode(stateJson);
  } on FormatException {
    return _unknown;
  }
  if (decoded is! Map<String, Object?>) return _unknown;

  final state = switch (decoded['kind']) {
    final String kind => shortMessage(kind),
    _ => 'unknown',
  };
  final since = switch (decoded['at']) {
    final num at => at.toInt(),
    _ => null,
  };
  final failure = switch (decoded['failure']) {
    null => null,
    final Map<String, Object?> stored => _failureOf(stored),
    _ => 'unreadable',
  };

  return (state: state, failure: failure, since: since);
}

String _failureOf(Map<String, Object?> failure) {
  final kind = switch (failure['kind']) {
    final String kind => shortMessage(kind),
    _ => 'failure',
  };

  return switch ((failure['reason'], failure['status'])) {
    (final String reason, _) when kind == 'uncertain' => shortMessage(
      '$kind: $reason',
    ),
    (_, final num status) => '$kind ${status.toInt()}',
    _ => kind,
  };
}

/// One write the outbox mirror has seen.
final class OutboxRow {
  /// Creates a row.
  const OutboxRow({
    required this.id,
    required this.operation,
    required this.createdAt,
    required this.state,
    required this.failure,
    required this.at,
  });

  /// The pending mutation id.
  final String id;

  /// The operation's table key.
  final String? operation;

  /// When it was queued, in epoch milliseconds. Known only from the storage
  /// session: the outbox events carry no timestamp.
  final int? createdAt;

  /// `queued`, `failed` or `replayed` (the session adds `sending`).
  final String state;

  /// The failure message, at most 200 characters.
  final String? failure;

  /// The devtools clock at the last change.
  final int at;

  /// The JSON form. Never the arguments, never the idempotency key.
  Json toJson() => {
    'id': id,
    'operation': operation,
    'createdAt': createdAt,
    'state': state,
    'failure': failure,
    'at': at,
  };
}

/// A bounded table of outbox writes, built from events.
final class OutboxMirror {
  /// Creates a mirror holding at most [capacity] writes.
  OutboxMirror({this.capacity = 200});

  /// Writes kept before the oldest is dropped.
  final int capacity;

  final Map<String, OutboxRow> _rows = {};

  /// Records a queued write.
  void enqueued(String id, String operation, int at) {
    _rows[id] = OutboxRow(
      id: id,
      operation: operation,
      createdAt: null,
      state: 'queued',
      failure: null,
      at: at,
    );
    _trim();
  }

  /// Records a successful replay.
  void replayed(String id, String operation, int at) =>
      _update(id, 'replayed', operation, null, at);

  /// Records a failed replay.
  void failed(String id, String operation, String failure, int at) =>
      _update(id, 'failed', operation, shortMessage(failure), at);

  /// Every write, in the order first seen.
  List<OutboxRow> entries() => [..._rows.values];

  /// Forgets every write.
  void clear() => _rows.clear();

  void _update(
    String id,
    String state,
    String operation,
    String? failure,
    int at,
  ) {
    final known = _rows[id];
    _rows[id] = OutboxRow(
      id: id,
      operation: known?.operation ?? operation,
      createdAt: known?.createdAt,
      state: state,
      failure: failure,
      at: at,
    );
    _trim();
  }

  void _trim() {
    while (_rows.length > capacity) {
      _rows.remove(_rows.keys.first);
    }
  }
}

/// The last status one entity reported.
final class SyncEntry {
  /// Creates an entry.
  const SyncEntry({
    required this.entity,
    required this.status,
    required this.pending,
    required this.error,
    required this.at,
  });

  /// The entity typename.
  final String entity;

  /// `synced`, `pending`, `offline` or `failed`.
  final String status;

  /// Pending change count.
  final int pending;

  /// The failure message, when failed.
  final String? error;

  /// The devtools clock when it arrived.
  final int at;

  /// The JSON form.
  Json toJson() => {
    'entity': entity,
    'status': status,
    'pending': pending,
    'error': error,
    'at': at,
  };
}

/// The latest sync status per entity, built from events.
final class SyncMirror {
  final Map<String, SyncEntry> _byEntity = {};

  /// Records one status event.
  void apply(DevSyncStatus event, int at) =>
      _byEntity[event.entity] = SyncEntry(
        entity: event.entity,
        status: event.status,
        pending: event.pending,
        error: event.error,
        at: at,
      );

  /// Every entity's latest status, sorted by entity.
  List<SyncEntry> entries() =>
      [..._byEntity.values]..sort((a, b) => a.entity.compareTo(b.entity));

  /// Forgets every entity.
  void clear() => _byEntity.clear();
}
