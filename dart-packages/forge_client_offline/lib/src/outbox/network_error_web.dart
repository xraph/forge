import 'package:http/http.dart' as http;

import 'network_failure.dart';

/// Every fetch failure is uncertain, including one while the browser reports
/// itself offline: `navigator.onLine` says what the browser knew when the
/// error surfaced, not whether the connection dropped after the request had
/// left, and a browser cannot tell a refused connection from a lost response.
/// Replay sends the same Idempotency-Key, so treating it as uncertain is safe.
NetworkFailure? classifyPlatformError(Object error) =>
    error is http.ClientException ? NetworkFailure.uncertain : null;
