import 'dart:io';

import 'package:http/http.dart' as http;

import 'network_failure.dart';

const List<String> _notSentMarkers = [
  'connection refused',
  'failed host lookup',
  'network is unreachable',
  'no route to host',
  'host is down',
  'nodename nor servname',
];

/// package:http's IOClient rethrows a SocketException as a ClientException
/// that also implements SocketException, so the socket check sees it.
NetworkFailure? classifyPlatformError(Object error) {
  if (error is SocketException) {
    final text = '${error.message} ${error.osError?.message ?? ''}'
        .toLowerCase();
    return _notSentMarkers.any(text.contains)
        ? NetworkFailure.notSent
        : NetworkFailure.uncertain;
  }
  if (error is HttpException || error is http.ClientException) {
    return NetworkFailure.uncertain;
  }
  return null;
}
