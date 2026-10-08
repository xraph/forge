import 'dart:io';

import 'package:http/http.dart' as http;
import 'package:meta/meta.dart';

import 'network_failure.dart';

/// Windows. Dart connects through overlapped `ConnectEx`, so a failure that
/// arrives while connecting carries the Win32 code the completion reports
/// (1225, "The remote computer refused the network connection."), not the
/// Winsock one. A failure Winsock reports straight away carries the Winsock
/// code. Both kinds are listed.
const Set<int> _windowsNotSentCodes = {
  1225, // ERROR_CONNECTION_REFUSED
  1231, // ERROR_NETWORK_UNREACHABLE
  1232, // ERROR_HOST_UNREACHABLE
  10050, // WSAENETDOWN
  10051, // WSAENETUNREACH
  10061, // WSAECONNREFUSED
  10064, // WSAEHOSTDOWN
  10065, // WSAEHOSTUNREACH
  11001, // WSAHOST_NOT_FOUND
  11002, // WSATRY_AGAIN
  11004, // WSANO_DATA
};

/// macOS and iOS.
const Set<int> _darwinNotSentCodes = {
  50, // ENETDOWN
  51, // ENETUNREACH
  61, // ECONNREFUSED
  64, // EHOSTDOWN
  65, // EHOSTUNREACH
};

/// Linux and Android.
const Set<int> _linuxNotSentCodes = {
  100, // ENETDOWN
  101, // ENETUNREACH
  111, // ECONNREFUSED
  112, // EHOSTDOWN
  113, // EHOSTUNREACH
};

/// OS error codes that mean the request never left the device on the
/// operating system [operatingSystem] (as `Platform.operatingSystem` names
/// it): connection refused, network or host unreachable or down, and a
/// failed name lookup. A reset, an abort or a timeout is never on the list.
///
/// The same number means different things on different systems (64 is
/// EHOSTDOWN on macOS and ERROR_NETNAME_DELETED, a reset, on Windows), so a
/// code is only read against the table of the system that produced it.
@visibleForTesting
Set<int> notSentCodesFor(String operatingSystem) => switch (operatingSystem) {
  'windows' => _windowsNotSentCodes,
  'macos' || 'ios' => _darwinNotSentCodes,
  _ => _linuxNotSentCodes,
};

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

/// Classifies a [SocketException] raised on [operatingSystem].
@visibleForTesting
NetworkFailure classifySocketException(
  SocketException error, {
  required String operatingSystem,
}) {
  final code = error.osError?.errorCode;
  if (code != null && notSentCodesFor(operatingSystem).contains(code)) {
    return NetworkFailure.notSent;
  }
  final text = '${error.message} ${error.osError?.message ?? ''}'.toLowerCase();
  return _notSentMarkers.any(text.contains)
      ? NetworkFailure.notSent
      : NetworkFailure.uncertain;
}

/// package:http's IOClient rethrows a SocketException as a ClientException
/// that also implements SocketException, so the socket check sees it.
///
/// A TLS error (a [HandshakeException], a [CertificateException]) happens
/// while the connection is being set up, before the first request byte is
/// written, so the request never reached the server.
NetworkFailure? classifyPlatformError(Object error) {
  if (error is SocketException) {
    return classifySocketException(
      error,
      operatingSystem: Platform.operatingSystem,
    );
  }
  if (error is TlsException) return NetworkFailure.notSent;
  if (error is HttpException || error is http.ClientException) {
    return NetworkFailure.uncertain;
  }
  return null;
}
