/// How a request failed before any HTTP response arrived.
enum NetworkFailure {
  /// The request never left the device (connection refused, no route, DNS
  /// failure). Always safe to send again.
  notSent,

  /// The request may have reached the server (timeout, connection reset,
  /// broken response). Safe to send again only for an operation that
  /// tolerates repeats.
  uncertain,

  /// The caller cancelled the request (`TransportRequest.cancel` fired). The
  /// caller asked for this and has the error: rethrow it to them and store
  /// nothing. It is never an uncertain outcome.
  cancelled,
}
