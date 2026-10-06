import 'package:forge_client/forge_client.dart';
import 'package:meta/meta.dart';

/// The request header every outbox write carries.
const String idempotencyKeyHeader = 'Idempotency-Key';

/// Marks a mutation the outbox re-issued through the cache; never sent.
const String outboxReplayHeader = 'x-forge-outbox-replay';

/// What [OutboxTransport] hands each request to once a client is attached.
typedef OutboxHandler = Future<Object?> Function(TransportRequest request);

/// The transport a `QueryCache` must be built with for an `OfflineClient`
/// to queue its writes. Until a client attaches, it forwards everything to
/// [inner].
///
/// Retries are split by owner. [inner] (normally a `RestTransport`) keeps its
/// method-based policy: it retries GET, HEAD, PUT and DELETE only. Every
/// other write is retried by the `OfflineClient`, which resends it with the
/// same `Idempotency-Key` on every attempt so the server's idempotency
/// middleware can answer a repeat. A generated `RestClient` keeps its default
/// of one attempt for those writes.
final class OutboxTransport implements Transport {
  /// Wraps [inner], the transport that reaches the network.
  OutboxTransport(this.inner);

  /// The transport that reaches the network.
  final Transport inner;

  OutboxHandler? _handler;

  /// Attaches the OfflineClient's handler, or detaches it with null.
  @internal
  void attach(OutboxHandler? handler) {
    if (handler != null && _handler != null) {
      throw StateError(
        'an OfflineClient is already attached to this OutboxTransport',
      );
    }
    _handler = handler;
  }

  @override
  Future<Object?> execute(TransportRequest request) {
    final handler = _handler;
    if (handler != null) return handler(request);
    return inner.execute(withoutReplayMarker(request));
  }
}

/// [request] without the outbox's internal marker header.
TransportRequest withoutReplayMarker(TransportRequest request) {
  if (!request.headers.containsKey(outboxReplayHeader)) return request;
  return TransportRequest(
    meta: request.meta,
    args: request.args,
    headers: {...request.headers}..remove(outboxReplayHeader),
    cancel: request.cancel,
  );
}
