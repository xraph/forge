import 'package:forge_client/forge_client.dart';

/// What an app can do with a failed outbox write. [OfflineClient] implements
/// it; an [OutboxFailure] forwards its actions here.
abstract interface class OutboxControl {
  /// Sends the write again with the same Idempotency-Key.
  Future<void> retry(String mutationId);

  /// Drops the write and unblocks the writes queued behind it.
  Future<void> discard(String mutationId);

  /// Sends the write again with [args] and a new Idempotency-Key.
  Future<void> edit(String mutationId, TagContext args);
}

/// Why a queued write was not applied. Its optimistic overlay has been rolled
/// back, it stays in the outbox, and writes queued behind it for the same
/// entity wait until the app calls [retry], [edit] or [discard].
///
/// The cases carry an `Outbox` prefix so they never collide with a generated
/// `ApiError` subclass such as `Conflict`.
///
/// A failure carries the server's response body, and an [OutboxFailure] held
/// by a listener belongs to the principal that made the write. Listeners must
/// drop every failure object they hold when the principal changes (sign-out
/// or an account switch): the objects hold the previous principal's server
/// bodies, and acting on one after the switch would reach the wrong outbox.
sealed class OutboxFailure implements Exception {
  /// A failure of mutation [mutationId], an instance of [operationId].
  const OutboxFailure({
    required this.mutationId,
    required this.operationId,
    required this._control,
  });

  /// The outbox record's id.
  final String mutationId;

  /// The generated operation id, a key of the `operations` table.
  final String operationId;

  final OutboxControl _control;

  /// Sends the write again with the same Idempotency-Key.
  Future<void> retry() => _control.retry(mutationId);

  /// Drops the write.
  Future<void> discard() => _control.discard(mutationId);

  /// Sends the write again with new arguments and a new Idempotency-Key.
  Future<void> edit(TagContext args) => _control.edit(mutationId, args);

  /// The persisted form, stored inside the record's `{"kind":"failed"}` state.
  Map<String, Object?> toJson() => switch (this) {
    OutboxConflict(:final status, :final body) => {
      'kind': 'conflict',
      'status': status,
      'body': body,
    },
    OutboxValidation(:final status, :final body) => {
      'kind': 'validation',
      'status': status,
      'body': body,
    },
    OutboxUnauthorized(:final status) => {
      'kind': 'unauthorized',
      'status': status,
    },
    OutboxGone(:final status, :final body) => {
      'kind': 'gone',
      'status': status,
      'body': body,
    },
    OutboxUncertain(:final reason) => {'kind': 'uncertain', 'reason': reason},
  };

  /// Rebuilds a failure stored by [toJson].
  static OutboxFailure fromJson(
    Map<String, Object?> json, {
    required String mutationId,
    required String operationId,
    required OutboxControl control,
  }) {
    final status = (json['status'] as num?)?.toInt() ?? 0;
    final body = json['body'];

    return switch (json['kind']) {
      'conflict' => OutboxConflict(
        mutationId: mutationId,
        operationId: operationId,
        control: control,
        status: status,
        body: body,
      ),
      'validation' => OutboxValidation(
        mutationId: mutationId,
        operationId: operationId,
        control: control,
        status: status,
        body: body,
      ),
      'unauthorized' => OutboxUnauthorized(
        mutationId: mutationId,
        operationId: operationId,
        control: control,
        status: status,
      ),
      'gone' => OutboxGone(
        mutationId: mutationId,
        operationId: operationId,
        control: control,
        status: status,
        body: body,
      ),
      'uncertain' => OutboxUncertain(
        mutationId: mutationId,
        operationId: operationId,
        control: control,
        reason: json['reason'] as String? ?? '',
      ),
      final kind => throw FormatException('unknown outbox failure kind', kind),
    };
  }
}

/// The server's copy changed since the write was made: 409 or 412.
///
/// A 409 that carries `Retry-After` is not this: it is the idempotency
/// middleware's reply that the same key is still in flight, and the outbox
/// retries it.
final class OutboxConflict extends OutboxFailure {
  /// A conflict with [status] and the server's [body].
  const OutboxConflict({
    required super.mutationId,
    required super.operationId,
    required super.control,
    required this.status,
    this.body,
  });

  /// The HTTP status.
  final int status;

  /// The decoded response body.
  final Object? body;

  @override
  String toString() => 'OutboxConflict($status) for $operationId';
}

/// The server refused the write as invalid: 400, 422 or another 4xx not
/// covered elsewhere, or a request that could not be built at all (status 0).
final class OutboxValidation extends OutboxFailure {
  /// A refusal with [status] and the server's [body].
  const OutboxValidation({
    required super.mutationId,
    required super.operationId,
    required super.control,
    required this.status,
    this.body,
  });

  /// The HTTP status, or 0 when the request could not be built.
  final int status;

  /// The decoded response body, or a description.
  final Object? body;

  @override
  String toString() => 'OutboxValidation($status) for $operationId';
}

/// The credentials were refused: 401 or 403. Sign in again, then retry.
final class OutboxUnauthorized extends OutboxFailure {
  /// A refusal with [status].
  const OutboxUnauthorized({
    required super.mutationId,
    required super.operationId,
    required super.control,
    required this.status,
  });

  /// The HTTP status.
  final int status;

  @override
  String toString() => 'OutboxUnauthorized($status) for $operationId';
}

/// The target no longer exists (404 or 410), or the operation is no longer in
/// the generated operations table (status 0).
final class OutboxGone extends OutboxFailure {
  /// A missing target with [status] and the server's [body].
  const OutboxGone({
    required super.mutationId,
    required super.operationId,
    required super.control,
    required this.status,
    this.body,
  });

  /// The HTTP status, or 0 for a withdrawn operation.
  final int status;

  /// The decoded response body, or a description.
  final Object? body;

  @override
  String toString() => 'OutboxGone($status) for $operationId';
}

/// The write was sent and no response arrived, and the operation is not safe
/// to repeat (not PUT, not DELETE, not `idempotent`). It may or may not have
/// been applied. The outbox never resends it on its own.
///
/// Also raised when the outbox gave up retrying a write after repeated
/// retryable failures (408, 429, 5xx, never sent); [reason] says so. Its
/// record and `Idempotency-Key` are kept, so [retry] resends with the same
/// key.
final class OutboxUncertain extends OutboxFailure {
  /// An uncertain outcome, described by [reason].
  const OutboxUncertain({
    required super.mutationId,
    required super.operationId,
    required super.control,
    required this.reason,
  });

  /// What happened, for logs.
  final String reason;

  @override
  String toString() => 'OutboxUncertain for $operationId: $reason';
}

/// A queued write's caller was released because the principal changed. The
/// write stays in that principal's outbox and replays when they sign back in.
final class OutboxSuspended implements Exception {
  /// Creates the error.
  const OutboxSuspended();

  @override
  String toString() =>
      'OutboxSuspended: the principal changed; the write is kept for when '
      'they return';
}

/// A write was refused because it was made for a principal that is no longer
/// signed in: the principal changed while the write waited for that
/// principal's session or outbox to open. Nothing was sent and nothing was
/// stored, so it can never go out under the next principal's credentials.
final class OutboxStale implements Exception {
  /// Creates the error.
  const OutboxStale();

  @override
  String toString() =>
      'OutboxStale: the write was made for a principal that is no longer '
      'signed in; it was not sent or kept';
}

/// A queued write's caller was released because the app discarded the write.
final class OutboxDiscarded implements Exception {
  /// Creates the error.
  const OutboxDiscarded();

  @override
  String toString() => 'OutboxDiscarded: the write was removed from the outbox';
}

/// The write could not be queued because storage failed.
final class OutboxUnavailable implements Exception {
  /// Wraps storage's [cause].
  const OutboxUnavailable(this.cause);

  /// The storage error.
  final Object cause;

  @override
  String toString() =>
      'OutboxUnavailable: the write could not be stored: $cause';
}

/// Thrown by `OfflineClient.replay` when the write could not reach the
/// server (the device is offline, or the outcome was uncertain on an
/// operation that is safe to repeat). The write is still queued.
final class OutboxOffline implements Exception {
  /// Creates the error for [mutationId].
  const OutboxOffline(this.mutationId);

  /// The write that is still queued.
  final String mutationId;

  @override
  String toString() =>
      'OutboxOffline: the write could not reach the server and is still '
      'queued';
}
