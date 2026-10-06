import 'dart:convert';
import 'dart:typed_data';

import 'package:forge_client/forge_client.dart';

import 'overlay_intent.dart';

/// Whether [meta] may be resent after the server might already have applied
/// it: PUT and DELETE by HTTP semantics, or any operation whose route uses
/// Forge's idempotency middleware (`idempotent: true`).
bool isSafeToRepeat(OperationMeta meta) =>
    meta.idempotent || _safeByMethod(meta);

/// Whether [meta] is safe to resend only because the server remembers its
/// `Idempotency-Key` (a POST or PATCH on an idempotent route), which it does
/// for a limited time. PUT and DELETE are safe by method and never expire.
bool isSafeOnlyByKey(OperationMeta meta) =>
    meta.idempotent && !_safeByMethod(meta);

bool _safeByMethod(OperationMeta meta) =>
    const {'PUT', 'DELETE'}.contains(meta.method.toUpperCase());

const Set<String> _unpersisted = {
  'authorization',
  'proxy-authorization',
  'cookie',
  'set-cookie',
  'x-api-key',
  'x-csrf-token',
  'x-xsrf-token',
  'idempotency-key',
  'x-forge-outbox-replay',
};

/// Name fragments that mark a header as a credential, matched ignoring case.
/// Deliberately broad: dropping a harmless header such as `X-Author` costs
/// less than writing a secret to disk.
const List<String> _credentialFragments = ['token', 'secret', 'auth'];

bool _persistable(String name) {
  final lower = name.toLowerCase();
  return !_unpersisted.contains(lower) &&
      !_credentialFragments.any(lower.contains);
}

/// The request headers worth keeping with a queued write: everything except
/// credentials, which the transport supplies fresh on replay, and the
/// outbox's own headers. A credential is any of `Authorization`,
/// `Proxy-Authorization`, `Cookie`, `Set-Cookie`, `X-API-Key`,
/// `X-CSRF-Token` and `X-XSRF-Token`, or any header whose name contains
/// `token`, `secret` or `auth`, all ignoring case.
Map<String, String> persistableHeaders(Map<String, String> headers) => {
  for (final MapEntry(:key, :value) in headers.entries)
    if (_persistable(key)) key: value,
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
    this.storedKey,
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

  /// Sent as `Idempotency-Key` on every attempt. A retry after the server
  /// answered with a final status replaces it (see [withRotatedKey]).
  final String idempotencyKey;

  /// The key the record was first stored with, when [idempotencyKey] has
  /// since been rotated; null when the two are the same. The record's key
  /// column cannot be rewritten, so a rotated key lives in [stateJson].
  final String? storedKey;

  /// Wall-clock time the write was made. Informational only: order is [seq].
  final DateTime createdAt;

  /// Monotonic position in this principal's outbox. Replay follows it.
  final int seq;

  /// When the first attempt whose outcome is still unknown was sent, or null
  /// when no attempt is outstanding. A record found with this set after a
  /// restart was in flight when the app stopped. It is not moved by later
  /// attempts, so it dates the earliest moment the server may have stored
  /// this key.
  final DateTime? sentAt;

  /// The stored [OutboxFailure] (its `toJson()`, encoded), or null while
  /// the write is pending.
  final String? failureJson;

  /// The record's state as stored in `PendingMutationRecord.stateJson`:
  /// `failed` with the failure when there is one, else `sending` with
  /// [sentAt], else `queued`. A rotated key rides along as `key`. Rewritten
  /// with `StorageSession.updateState`.
  String get stateJson {
    final stored = storedKey;
    final rotated = stored != null && stored != idempotencyKey
        ? <String, Object?>{'key': idempotencyKey}
        : const <String, Object?>{};
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
        ...rotated,
      });
    }
    final at = sentAt;
    if (at == null) {
      return rotated.isEmpty
          ? _queuedState
          : jsonEncode({'kind': 'queued', ...rotated});
    }
    return jsonEncode({
      'kind': 'sending',
      'at': at.millisecondsSinceEpoch,
      ...rotated,
    });
  }

  /// A copy whose `Idempotency-Key` is [key], for the same record: the
  /// record keeps its original key column and the new key is persisted in
  /// [stateJson]. A retry after a final status (a stored 4xx the server
  /// would replay for the key) sends the write as a new operation this way.
  OutboxEntry withRotatedKey(String key) => OutboxEntry(
    id: id,
    operationId: operationId,
    args: args,
    requestHeaders: requestHeaders,
    intent: intent,
    idempotencyKey: key,
    createdAt: createdAt,
    seq: seq,
    sentAt: sentAt,
    failureJson: failureJson,
    storedKey: storedKey ?? idempotencyKey,
  );

  /// A copy with the given fields replaced. [clearSentAt] and [clearFailure]
  /// set those fields to null. A new [idempotencyKey] makes a new record's
  /// key (the stored and current key agree); [withRotatedKey] changes the key
  /// of this record.
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
    storedKey: idempotencyKey != null ? null : storedKey,
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
    idempotencyKey: storedKey ?? idempotencyKey,
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

    final (sentAt, failureJson, rotatedKey) = _readState(record.stateJson);
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
      idempotencyKey: rotatedKey ?? record.idempotencyKey,
      createdAt: record.createdAt.toUtc(),
      seq: seq,
      sentAt: sentAt,
      failureJson: failureJson,
      storedKey: rotatedKey == null ? null : record.idempotencyKey,
    );
  }
}

const String _queuedState = '{"kind":"queued"}';

/// Splits a stored state into the in-flight time, the failure and a rotated
/// key. A state this version cannot read comes back as a failure (the raw
/// text), so the engine reports it rather than resending a write it does not
/// understand.
(DateTime?, String?, String?) _readState(String? stored) {
  if (stored == null) return (null, null, null);

  final Object? decoded;
  try {
    decoded = jsonDecode(stored);
  } on FormatException {
    return (null, stored, null);
  }

  if (decoded is Map<String, Object?>) {
    final key = decoded['key'];
    final rotated = key is String && key.isNotEmpty ? key : null;
    switch (decoded['kind']) {
      case 'queued':
        return (null, null, rotated);
      case 'sending':
        final at = decoded['at'];
        return (
          at is int
              ? DateTime.fromMillisecondsSinceEpoch(at, isUtc: true)
              : DateTime.utc(1970),
          null,
          rotated,
        );
      case 'failed':
        final failure = decoded['failure'];
        // A failed state with no failure object is unreadable: surface the
        // raw state as the failure rather than a failure of "null".
        return (
          null,
          failure is Map<String, Object?> ? jsonEncode(failure) : stored,
          rotated,
        );
    }
  }
  return (null, stored, null);
}

/// The arguments as stored. A [Uint8List] body cannot go through `jsonEncode`
/// (it would become an array of ints that reads back as a `List`, which the
/// transport refuses for a binary content type), so it is stored as base64
/// under its own key, `bodyBase64`, beside `body`. A JSON body only ever
/// lives under `body`, so no JSON value, however it is shaped, can be read as
/// bytes: the two cannot collide.
///
/// An [Iterable] that is not a [List] (a `Set`, say, as a form field's
/// repeated values) is stored as a list, since JSON has no other collection:
/// the transport sends both the same way.
Map<String, Object?> _argsJson(TagContext args) {
  final body = args.body;

  return {
    'path': _jsonable(args.path),
    'query': _jsonable(args.query),
    'headers': args.headers,
    if (body is Uint8List)
      'bodyBase64': base64Encode(body)
    else
      'body': _jsonable(body),
  };
}

/// [value] with every non-List [Iterable] inside it turned into a [List].
/// Anything else is left for `jsonEncode` to accept or refuse.
Object? _jsonable(Object? value) => switch (value) {
  Map<Object?, Object?>() => {
    for (final MapEntry(:key, value: item) in value.entries)
      key: _jsonable(item),
  },
  List<Object?>() => [for (final item in value) _jsonable(item)],
  Iterable<Object?>() => [for (final item in value) _jsonable(item)],
  _ => value,
};

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
