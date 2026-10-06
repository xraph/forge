import 'package:http/http.dart' as http;
import 'package:web/web.dart' as web;

import 'network_failure.dart';

/// A fetch failure while the browser reports itself offline never left the
/// device; any other is uncertain.
NetworkFailure? classifyPlatformError(Object error) {
  if (error is! http.ClientException) return null;
  return web.window.navigator.onLine
      ? NetworkFailure.uncertain
      : NetworkFailure.notSent;
}
