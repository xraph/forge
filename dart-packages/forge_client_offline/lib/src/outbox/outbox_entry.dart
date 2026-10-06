import 'dart:convert';
import 'dart:typed_data';

import 'package:forge_client/forge_client.dart';

import 'overlay_intent.dart';

/// Whether [meta] may be resent after the server might already have applied
/// it: PUT and DELETE by HTTP semantics, or any operation whose route uses
/// Forge's idempotency middleware (`idempotent: true`).
bool isSafeToRepeat(OperationMeta meta) =>
    meta.idempotent ||
    const {'PUT', 'DELETE'}.contains(meta.method.toUpperCase());

const Set<String> _unpersisted = {
  'authorization',
  'cookie',
  'idempotency-key',
  'x-forge-outbox-replay',
};

/// The request headers worth keeping with a queued write: everything except
/// credentials, which the transport supplies fresh on replay, and the
/// outbox's own headers.
Map<String, String> persistableHeaders(Map<String, String> headers) => {
  for (final MapEntry(:key, :value) in headers.entries)
    if (!_unpersisted.contains(key.toLowerCase())) key: value,
};

/// The value of header [name] in [headers], ignoring case.
String? headerValue(Map<String, String> headers, String name) {
  final lower = name.toLowerCase();
  for (final MapEntry(:key, :value) in headers.entries) {
    if (key.toLowerCase() == lower) return value;
  }
  return null;
}

/// One queued write.
final class OutboxEntry {
  /// Creates an entry.
  const OutboxEntry({
    required this.id,
    required this.operationId,
    required this.args,
    required this.requestHeaders,
    required this.intent,
    required this.idempotencyKey,
    required this.createdAt,
    required this.seq,
    this.sentAt,
    this.failureJson,
  });

  /// The record id, a uuid v4.
  final String id;

  /// The generated operation id.
  final String operationId;

  /// The operation's arguments, client-shaped. A body that is a [Uint8List]
  /// is stored as base64 and comes back as a [Uint8List].
  final TagContext args;

  /// Extra request headers sent with the write (see [persistableHeaders]).
  final Map<String, String> requestHeaders;

  /// How the write is drawn after a restart.
  final OverlayIntent intent;

  /// Sent as `Idempotency-Key` on every attempt.
  final String idempotencyKey;

  /// Wall-clock time the write was made. Informational only: order is [seq].
  final DateTime createdAt;

  /// Monotonic position in this principal's outbox. Replay follows it.
  final int seq;

  /// When the latest attempt was sent, or null when no attempt is
  /// outstanding. A record found with this set after a restart was in flight
  /// when the app stopped.
  final DateTime? sentAt;

  /// The stored [OutboxFailure] (its `toJson()`, encoded), or null while
  /// the write is pending.
  final String? failureJson;

  /// The record's state as stored in `PendingMutationRecord.stateJson`:
  /// `failed` with the failure when there is one, else `sending` with
  /// [sentAt], else `queued`. Rewritten with `StorageSession.updateState`.
  String get stateJson {
    final failure = failureJson;
    if (failure != null) {
      Object? decoded;
      try {
        decoded = jsonDecode(failure);
      } on FormatException {
        decoded = null;
      }
      // A failed state always carries a failure object. Anything else is
      // kept as raw text, never written as `failure: null`.
      return jsonEncode({
        'kind': 'failed',
        'failure': decoded is Map<String, Object?>
            ? decoded
            : {'kind': 'unreadable', 'raw': failure},
      });
    }
    final at = sentAt;
    return at == null
        ? _queuedState
        : jsonEncode({'kind': 'sending', 'at': at.millisecondsSinceEpoch});
  }

  /// A copy with the given fields replaced. [clearSentAt] and [clearFailure]
  /// set those fields to null.
  OutboxEntry copyWith({
    String? id,
    TagContext? args,
    OverlayIntent? intent,
    String? idempotencyKey,
    DateTime? sentAt,
    bool clearSentAt = false,
    String? failureJson,
    bool clearFailure = false,
  }) => OutboxEntry(
    id: id ?? this.id,
    operationId: operationId,
    args: args ?? this.args,
    requestHeaders: requestHeaders,
    intent: intent ?? this.intent,
    idempotencyKey: idempotencyKey ?? this.idempotencyKey,
    createdAt: createdAt,
    seq: seq,
    sentAt: clearSentAt ? null : (sentAt ?? this.sentAt),
    failureJson: clearFailure ? null : (failureJson ?? this.failureJson),
  );

  /// The arguments as canonical JSON, for spotting a duplicate write.
  String get canonicalArgs => jsonEncode(_argsJson(args));

  /// The storage form, enqueued once with its current [stateJson].
  PendingMutationRecord toRecord() => PendingMutationRecord(
    id: id,
    operationId: operationId,
    argsJson: jsonEncode({
      'v': 1,
      'seq': seq,
      'requestHeaders': requestHeaders,
      'args': _argsJson(args),
    }),
    optimisticJson: intent.encode(),
    idempotencyKey: idempotencyKey,
    createdAt: createdAt,
    stateJson: stateJson,
  );

  /// Rebuilds an entry from storage. Throws [FormatException] for a record
  /// this version cannot read.
  static OutboxEntry fromRecord(PendingMutationRecord record) {
    final decoded = jsonDecode(record.argsJson);
    if (decoded is! Map<String, Object?> || decoded['v'] != 1) {
      throw FormatException(
        'outbox record ${record.id} has an unknown args envelope',
        record.argsJson,
      );
    }

    final args = decoded['args'];
    final seq = decoded['seq'];
    if (args is! Map<String, Object?> || seq is! int) {
      throw FormatException(
        'outbox record ${record.id} is missing args or seq',
        record.argsJson,
      );
    }

    final (sentAt, failureJson) = _readState(record.stateJson);
    return OutboxEntry(
      id: record.id,
      operationId: record.operationId,
      args: TagContext(
        path: _objectMap(args['path']),
        query: _objectMap(args['query']),
        headers: _stringMap(args['headers']),
        body: _readBody(record.id, args),
      ),
      requestHeaders: _stringMap(decoded['requestHeaders']),
      intent: OverlayIntent.decode(record.optimisticJson),
      idempotencyKey: record.idempotencyKey,
      createdAt: record.createdAt.toUtc(),
      seq: seq,
      sentAt: sentAt,
      failureJson: failureJson,
    );
  }
}

const String _queuedState = '{"kind":"queued"}';

/// Splits a stored state into the in-flight time and the failure. A state
/// this version cannot read comes back as a failure (the raw text), so the
/// engine reports it rather than resending a write it does not understand.
(DateTime?, String?) _readState(String? stored) {
  if (stored == null) return (null, null);

  final Object? decoded;
  try {
    decoded = jsonDecode(stored);
  } on FormatException {
    return (null, stored);
  }

  if (decoded is Map<String, Object?>) {
    switch (decoded['kind']) {
      case 'queued':
        return (null, null);
      case 'sending':
        final at = decoded['at'];
        return (
          at is int
              ? DateTime.fromMillisecondsSinceEpoch(at, isUtc: true)
              : DateTime.utc(1970),
          null,
        );
      case 'failed':
        final failure = decoded['failure'];
        // A failed state with no failure object is unreadable: surface the
        // raw state as the failure rather than a failure of "null".
        return (
          null,
          failure is Map<String, Object?> ? jsonEncode(failure) : stored,
        );
    }
  }
  return (null, stored);
}

/// The arguments as stored. A [Uint8List] body cannot go through `jsonEncode`
/// (it would become an array of ints that reads back as a `List`, which the
/// transport refuses for a binary content type), so it is stored as base64
/// under its own key, `bodyBase64`, beside `body`. A JSON body only ever
/// lives under `body`, so no JSON value, however it is shaped, can be read as
/// bytes: the two cannot collide.
Map<String, Object?> _argsJson(TagContext args) {
  final body = args.body;

  return {
    'path': args.path,
    'query': args.query,
    'headers': args.headers,
    if (body is Uint8List) 'bodyBase64': base64Encode(body) else 'body': body,
  };
}

/// The body stored in [args]: the bytes under `bodyBase64`, else `body`.
/// A record that has both, or bytes that are not base64, is unreadable.
Object? _readBody(String id, Map<String, Object?> args) {
  if (!args.containsKey('bodyBase64')) return args['body'];

  final encoded = args['bodyBase64'];
  if (encoded is! String || args['body'] != null) {
    throw FormatException(
      'outbox record $id has an unreadable binary body marker',
    );
  }

  try {
    return base64Decode(encoded);
  } on FormatException {
    // The source is left out: it is the user's data.
    throw FormatException('outbox record $id has a body that is not base64');
  }
}

Map<String, Object?> _objectMap(Object? value) => value is Map<String, Object?>
    ? Map<String, Object?>.of(value)
    : <String, Object?>{};

Map<String, String> _stringMap(Object? value) => value is Map<String, Object?>
    ? {
        for (final MapEntry(:key, value: entry) in value.entries)
          if (entry is String) key: entry,
      }
    : <String, String>{};
