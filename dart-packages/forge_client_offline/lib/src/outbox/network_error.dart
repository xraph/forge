import 'dart:async';

import 'package:forge_client/forge_client.dart';
import 'package:http/http.dart' as http;

import 'network_error_stub.dart'
    if (dart.library.io) 'network_error_io.dart'
    if (dart.library.js_interop) 'network_error_web.dart'
    as platform;
import 'network_failure.dart';

/// Classifies [error] from a transport. Null means it is not a network
/// failure: an HTTP status (the server answered) or a programming error.
///
/// A request the caller cancelled is [NetworkFailure.cancelled]. `RestTransport`
/// reports that as an [http.RequestAbortedException], which is a
/// [http.ClientException] and would otherwise read as an uncertain outcome,
/// so it is checked before any platform rule. The transport's own timeout is
/// a [TimeoutException], not an abort, and stays uncertain.
NetworkFailure? classifyNetworkError(Object error) {
  if (statusOf(error) != null) return null;
  if (error is http.RequestAbortedException) return NetworkFailure.cancelled;
  if (error is TimeoutException) return NetworkFailure.uncertain;
  return platform.classifyPlatformError(error);
}
