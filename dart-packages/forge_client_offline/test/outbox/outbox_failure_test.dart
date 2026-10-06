import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:forge_client/forge_client.dart';
import 'package:forge_client_offline/forge_client_offline.dart';

final class RecordingControl implements OutboxControl {
  final List<String> calls = [];

  @override
  Future<void> retry(String mutationId) async => calls.add('retry $mutationId');

  @override
  Future<void> discard(String mutationId) async =>
      calls.add('discard $mutationId');

  @override
  Future<void> edit(String mutationId, TagContext args) async =>
      calls.add('edit $mutationId ${args.body}');
}

void main() {
  final control = RecordingControl();

  OutboxFailure roundTrip(OutboxFailure failure) => OutboxFailure.fromJson(
    failure.toJson(),
    mutationId: 'm1',
    operationId: 'op',
    control: control,
  );

  test('every failure survives toJson and fromJson', () {
    final conflict = roundTrip(
      OutboxConflict(
        mutationId: 'm1',
        operationId: 'op',
        control: control,
        status: 409,
        body: {'error': 'stale'},
      ),
    );
    final validation = roundTrip(
      OutboxValidation(
        mutationId: 'm1',
        operationId: 'op',
        control: control,
        status: 422,
        body: 'bad',
      ),
    );
    final unauthorized = roundTrip(
      OutboxUnauthorized(
        mutationId: 'm1',
        operationId: 'op',
        control: control,
        status: 401,
      ),
    );
    final gone = roundTrip(
      OutboxGone(
        mutationId: 'm1',
        operationId: 'op',
        control: control,
        status: 410,
        body: null,
      ),
    );
    final uncertain = roundTrip(
      OutboxUncertain(
        mutationId: 'm1',
        operationId: 'op',
        control: control,
        reason: 'timed out',
      ),
    );

    expect(
      conflict,
      isA<OutboxConflict>().having((f) => f.status, 'status', 409).having(
        (f) => f.body,
        'body',
        {'error': 'stale'},
      ),
    );
    expect(
      validation,
      isA<OutboxValidation>()
          .having((f) => f.status, 'status', 422)
          .having((f) => f.body, 'body', 'bad'),
    );
    expect(
      unauthorized,
      isA<OutboxUnauthorized>().having((f) => f.status, 'status', 401),
    );
    expect(gone, isA<OutboxGone>().having((f) => f.status, 'status', 410));
    expect(
      uncertain,
      isA<OutboxUncertain>().having((f) => f.reason, 'reason', 'timed out'),
    );
    expect(uncertain.mutationId, 'm1');
    expect(uncertain.operationId, 'op');
  });

  group('OutboxOffline', () {
    const offline = OutboxOffline('m1');
    const status = OutboxOffline(
      'm2',
      cause: OutboxOfflineCause.retryableStatus,
      status: 503,
    );
    const held = OutboxOffline('m3', cause: OutboxOfflineCause.credentialsHeld);

    test('says why the write could not go', () {
      expect(offline.cause, OutboxOfflineCause.offline);
      expect(offline.toString(), contains('could not reach the server'));
      expect(
        status.toString(),
        allOf(contains('503'), isNot(contains('could not reach'))),
      );
      expect(
        held.toString(),
        allOf(contains('credentials'), isNot(contains('could not reach'))),
      );
      for (final e in [offline, status, held]) {
        expect(e.toString(), contains('still queued'));
      }
    });

    test('survives toJson and fromJson', () {
      for (final e in [offline, status, held]) {
        final back = OutboxOffline.fromJson(
          jsonDecode(jsonEncode(e.toJson())) as Map<String, Object?>,
        );
        expect(back.mutationId, e.mutationId);
        expect(back.cause, e.cause);
        expect(back.status, e.status);
        expect(back.toString(), e.toString());
      }
    });

    test('a malformed record is a FormatException', () {
      for (final json in <Map<String, Object?>>[
        {'kind': 'offline', 'cause': 'offline'},
        {'kind': 'offline', 'mutationId': 'm', 'cause': 'mystery'},
        {
          'kind': 'offline',
          'mutationId': 'm',
          'cause': 'retryableStatus',
          'status': 'x',
        },
        {'kind': 'gone', 'mutationId': 'm', 'cause': 'offline'},
      ]) {
        expect(() => OutboxOffline.fromJson(json), throwsFormatException);
      }
    });
  });

  test('an unknown kind is a FormatException', () {
    expect(
      () => OutboxFailure.fromJson(
        {'kind': 'mystery'},
        mutationId: 'm',
        operationId: 'op',
        control: control,
      ),
      throwsFormatException,
    );
  });

  test('the actions go to the control with the mutation id', () async {
    final failure = OutboxConflict(
      mutationId: 'm9',
      operationId: 'op',
      control: control,
      status: 409,
    );

    await failure.retry();
    await failure.discard();
    await failure.edit(const TagContext(body: {'note': 'x'}));

    expect(control.calls, ['retry m9', 'discard m9', 'edit m9 {note: x}']);
  });

  test('a switch over a failure is exhaustive', () {
    String describe(OutboxFailure f) => switch (f) {
      OutboxConflict() => 'conflict',
      OutboxValidation() => 'validation',
      OutboxUnauthorized() => 'unauthorized',
      OutboxGone() => 'gone',
      OutboxUncertain() => 'uncertain',
    };

    expect(
      describe(
        OutboxGone(
          mutationId: 'm',
          operationId: 'op',
          control: control,
          status: 404,
        ),
      ),
      'gone',
    );
  });
}
