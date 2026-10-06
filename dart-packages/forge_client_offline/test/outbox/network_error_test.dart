@TestOn('vm')
library;

import 'dart:async';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:forge_client/forge_client.dart';
import 'package:forge_client_offline/forge_client_offline.dart';
import 'package:http/http.dart' as http;
import 'package:http/io_client.dart';
import 'package:http/testing.dart';

const _write = OperationMeta(id: 'op_create', method: 'POST', path: '/orders');

/// A client that never answers, so only a timeout or a cancel ends a request.
MockClient silentClient() => MockClient.streaming(
  (request, body) => Completer<http.StreamedResponse>().future,
);

void main() {
  test('a refused connection or failed lookup never left the device', () {
    expect(
      classifyNetworkError(const SocketException('Connection refused')),
      NetworkFailure.notSent,
    );
    expect(
      classifyNetworkError(
        const SocketException('Failed host lookup: api.example.com'),
      ),
      NetworkFailure.notSent,
    );
    expect(
      classifyNetworkError(const SocketException('Network is unreachable')),
      NetworkFailure.notSent,
    );
  });

  test(
    'a reset, a timeout or a broken response may have reached the server',
    () {
      expect(
        classifyNetworkError(const SocketException('Connection reset by peer')),
        NetworkFailure.uncertain,
      );
      expect(
        classifyNetworkError(TimeoutException('no response')),
        NetworkFailure.uncertain,
      );
      expect(
        classifyNetworkError(
          const HttpException(
            'Connection closed before full header was received',
          ),
        ),
        NetworkFailure.uncertain,
      );
      expect(
        classifyNetworkError(
          http.ClientException('Connection closed while receiving data'),
        ),
        NetworkFailure.uncertain,
      );
    },
  );

  test('an HTTP status or a programming error is not a network failure', () {
    expect(classifyNetworkError(const HttpStatusError(503, null)), isNull);
    expect(classifyNetworkError(StateError('bug')), isNull);
  });

  test('a real refused connection through package:http is notSent', () async {
    final server = await ServerSocket.bind(InternetAddress.loopbackIPv4, 0);
    final port = server.port;
    await server.close();

    final client = IOClient();
    addTearDown(client.close);

    Object? error;
    try {
      await client.get(Uri.parse('http://127.0.0.1:$port/'));
    } on Object catch (e) {
      error = e;
    }

    expect(error, isNotNull);
    expect(classifyNetworkError(error!), NetworkFailure.notSent);
  });

  group('a request the caller cancelled', () {
    test('is cancelled, never a network failure of either kind', () {
      expect(
        classifyNetworkError(http.RequestAbortedException()),
        NetworkFailure.cancelled,
      );
      expect(
        classifyNetworkError(
          http.RequestAbortedException(Uri.parse('https://api.example.com/')),
        ),
        NetworkFailure.cancelled,
      );
    });

    test(
      'is cancelled when the transport throws it for a cancel future',
      () async {
        final cancel = Completer<void>();
        final transport = RestTransport(
          baseUrl: Uri.parse('https://api.example.com'),
          client: silentClient(),
        );

        final sending = transport.execute(
          TransportRequest(
            meta: _write,
            args: TagContext.empty,
            cancel: cancel.future,
          ),
        );
        final outcome = expectLater(
          sending,
          throwsA(
            isA<http.RequestAbortedException>().having(
              classifyNetworkError,
              'classification',
              NetworkFailure.cancelled,
            ),
          ),
        );
        cancel.complete();

        await outcome;
      },
    );

    test('is cancelled when the cancel future had already fired', () async {
      final transport = RestTransport(
        baseUrl: Uri.parse('https://api.example.com'),
        client: silentClient(),
      );

      await expectLater(
        transport.execute(
          TransportRequest(
            meta: _write,
            args: TagContext.empty,
            cancel: Future<void>.value(),
          ),
        ),
        throwsA(
          isA<Object>().having(
            classifyNetworkError,
            'classification',
            NetworkFailure.cancelled,
          ),
        ),
      );
    });

    test('is told apart from the transport\'s own timeout', () async {
      final transport = RestTransport(
        baseUrl: Uri.parse('https://api.example.com'),
        client: silentClient(),
        timeout: const Duration(milliseconds: 20),
      );

      await expectLater(
        transport.execute(
          const TransportRequest(meta: _write, args: TagContext.empty),
        ),
        throwsA(
          isA<TimeoutException>().having(
            classifyNetworkError,
            'classification',
            NetworkFailure.uncertain,
          ),
        ),
      );
    });
  });
}
