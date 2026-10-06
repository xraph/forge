import 'dart:io';

import 'package:http/http.dart' as http;

import 'network_failure.dart';

/// OS error codes that mean the request never left the device: connection
/// refused, network or host unreachable or down, and a failed name lookup.
/// Codes differ by platform, so the table is chosen by the running one.
final Set<int> _notSentCodes = () {
  if (Platform.isWindows) {
    return const {
      10050, // WSAENETDOWN
      10051, // WSAENETUNREACH
      10061, // WSAECONNREFUSED
      10064, // WSAEHOSTDOWN
      10065, // WSAEHOSTUNREACH
      11001, // WSAHOST_NOT_FOUND
      11002, // WSATRY_AGAIN
      11004, // WSANO_DATA
    };
  }
  if (Platform.isMacOS || Platform.isIOS) {
    return const {
      50, // ENETDOWN
      51, // ENETUNREACH
      61, // ECONNREFUSED
      64, // EHOSTDOWN
      65, // EHOSTUNREACH
    };
  }
  // Linux and Android.
  return const {
    100, // ENETDOWN
    101, // ENETUNREACH
    111, // ECONNREFUSED
    112, // EHOSTDOWN
    113, // EHOSTUNREACH
  };
}();

/// The fallback for an error with no usable code (a name lookup failure
/// carries a resolver code that collides with errno values). It reads the
/// English message, so a localized OS may fall through to `uncertain`, which
/// is the safe side.
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
///
/// A TLS error (a [HandshakeException], a [CertificateException]) happens
/// while the connection is being set up, before the first request byte is
/// written, so the request never reached the server.
NetworkFailure? classifyPlatformError(Object error) {
  if (error is SocketException) {
    final code = error.osError?.errorCode;
    if (code != null && _notSentCodes.contains(code)) {
      return NetworkFailure.notSent;
    }
    final text = '${error.message} ${error.osError?.message ?? ''}'
        .toLowerCase();
    return _notSentMarkers.any(text.contains)
        ? NetworkFailure.notSent
        : NetworkFailure.uncertain;
  }
  if (error is TlsException) return NetworkFailure.notSent;
  if (error is HttpException || error is http.ClientException) {
    return NetworkFailure.uncertain;
  }
  return null;
}
