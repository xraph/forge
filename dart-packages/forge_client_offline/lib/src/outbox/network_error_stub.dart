import 'package:http/http.dart' as http;

import 'network_failure.dart';

/// Without dart:io or a browser nothing tells a refused connection from a
/// lost response, so every client exception is uncertain.
NetworkFailure? classifyPlatformError(Object error) =>
    error is http.ClientException ? NetworkFailure.uncertain : null;
