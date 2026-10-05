// Ported from packages/client-core/__tests__/harness.ts: the fakes every
// suite drives, with no network and no timers.
import 'dart:async';
import 'dart:convert';

import 'package:forge_client/forge_client.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:test/test.dart';

/// Thrown by an [FakeHttp] handler to answer with a non-2xx status. The
/// transport turns it into an [HttpStatusError].
final class HttpFailure implements Exception {
  HttpFailure(this.status);

  final int status;

  @override
  String toString() => 'HTTP $status';
}

/// Matches the [HttpStatusError] the transport throws for [status].
Matcher httpError(int status) =>
    isA<HttpStatusError>().having((error) => error.status, 'status', status);

/// What a handler returns: a JSON value, an [http.Response], or a future of
/// either. Throwing [HttpFailure] answers with that status.
typedef HttpHandler = FutureOr<Object?> Function(
  http.Request request,
  int attempt,
);

/// A stand-in for the network, recording every request it was sent.
final class FakeHttp {
  FakeHttp(this.handler);

  final HttpHandler handler;
  final List<http.Request> calls = [];

  late final MockClient client = MockClient((request) async {
    final attempt = calls.length;
    calls.add(request);

    // One hop, as a real client takes at least one.
    await Future<void>.value();

    try {
      final result = await handler(request, attempt);

      if (result is http.Response) return result;

      return http.Response(
        jsonEncode(result),
        200,
        headers: {'content-type': 'application/json'},
      );
    } on HttpFailure catch (failure) {
      return http.Response(
        jsonEncode({'status': failure.status}),
        failure.status,
        headers: {'content-type': 'application/json'},
      );
    }
  });

  /// The decoded JSON body of call [index].
  Object? bodyOf(int index) => jsonDecode(calls[index].body);
}

/// The base URL every transport test talks to.
final Uri base = Uri.parse('https://api.test');

/// A transport under the test's control, with no HTTP anywhere near it.
final class FakeTransport implements Transport {
  FakeTransport(this.handler);

  final FutureOr<Object?> Function(TransportRequest request, int call) handler;
  final List<TransportRequest> calls = [];

  @override
  Future<Object?> execute(TransportRequest request) {
    final call = calls.length;
    calls.add(request);

    return Future<Object?>.microtask(() => handler(request, call));
  }
}

/// Lets every already-queued microtask and zero-delay timer run.
Future<void> settle() => pumpEventQueue();

/// A fake http.Client for testing timeout and abort behavior.
/// Records requests and when their abort triggers fire.
final class TimeoutTestClient extends http.BaseClient {
  TimeoutTestClient({this.handleRequest});

  /// Called with the request. Return a StreamedResponse or throw.
  /// If response is a StreamedResponse, the test controls when the body stream closes.
  final FutureOr<http.StreamedResponse> Function(http.BaseRequest request)?
  handleRequest;

  final recordedRequests = <http.BaseRequest>[];
  final recordedAborts = <bool>[];

  @override
  Future<http.StreamedResponse> send(http.BaseRequest request) async {
    recordedRequests.add(request);
    recordedAborts.add(false);
    final index = recordedAborts.length - 1;

    if (request is http.Abortable && request.abortTrigger != null) {
      unawaited(
        request.abortTrigger!.then((_) {
          recordedAborts[index] = true;
        }),
      );
    }

    if (handleRequest != null) {
      return await handleRequest!(request);
    }

    return http.StreamedResponse(const Stream.empty(), 200);
  }
}
