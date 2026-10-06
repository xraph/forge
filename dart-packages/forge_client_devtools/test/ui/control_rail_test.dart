import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:forge_client/devtools_protocol.dart';
import 'package:forge_client_devtools/forge_client_devtools.dart';

import '../support/fake_backend.dart';
import '../support/pump.dart';

Finder get _mode => find.byKey(const ValueKey('control-mode'));

Finder _segment(String label) =>
    find.descendant(of: _mode, matching: find.text(label));

Set<String> _selectedMode(WidgetTester tester) =>
    tester.widget<SegmentedButton<String>>(_mode).selected;

Json _marker(int seq, int session) => {
  'kind': 'principal',
  'seq': seq,
  'at': seq,
  'session': session,
};

void main() {
  testWidgets('switches the app offline and back', (tester) async {
    final fake = await pumpPanel(tester);

    await tester.tap(_segment('Offline'));
    await tester.pumpAndSettle();

    expect(fake.mode, 'offline');
    expect(fake.callsTo(ForgeDevtoolsProtocol.control).last['mode'], 'offline');
    expect(_selectedMode(tester), {'offline'});

    await tester.tap(_segment('Online'));
    await tester.pumpAndSettle();

    expect(fake.mode, 'online');
    expect(_selectedMode(tester), {'online'});
  });

  testWidgets('adds latency and arms one failure, then disarms it', (
    tester,
  ) async {
    final fake = await pumpPanel(tester);

    await tester.tap(find.byKey(const ValueKey('control-latency')));
    await tester.pumpAndSettle();
    await tester.tap(find.text('+1000ms').last);
    await tester.pumpAndSettle();

    expect(fake.latencyMs, 1000);

    await tester.tap(find.byKey(const ValueKey('control-fail-next')));
    await tester.pumpAndSettle();

    expect(fake.armedStatus, 500);
    expect(find.text('armed 500'), findsOneWidget);

    await tester.tap(find.byKey(const ValueKey('control-disarm')));
    await tester.pumpAndSettle();

    expect(fake.armedStatus, isNull);
    expect(find.text('armed 500'), findsNothing);
  });

  // Final fix P3: fail next arms the status picked beside it.
  testWidgets(
    'arms fail next with the status picked: 408, 429, 500, 503 or 409',
    (tester) async {
      final fake = await pumpPanel(tester);

      await tester.tap(find.byKey(const ValueKey('control-fail-status')));
      await tester.pumpAndSettle();
      for (final status in ['408', '429', '500', '503', '409']) {
        expect(find.text(status), findsWidgets, reason: status);
      }
      await tester.tap(find.text('429').last);
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const ValueKey('control-fail-next')));
      await tester.pumpAndSettle();

      final sent = fake.callsTo(ForgeDevtoolsProtocol.control).last;
      expect(sent['failNext'], '429');
      expect(sent['session'], '0');
      expect(fake.armedStatus, 429);
      expect(find.text('armed 429'), findsOneWidget);
    },
  );

  testWidgets('is absent when the app wired no simulator', (tester) async {
    await pumpPanel(tester, FakeForgeBackend()..controlsWired = false);

    expect(find.byKey(const ValueKey('control-mode')), findsNothing);
    expect(find.byKey(const ValueKey('control-rail')), findsNothing);
    expect(find.byKey(const ValueKey('control-latency')), findsNothing);
    expect(find.byKey(const ValueKey('control-fail-next')), findsNothing);
  });

  testWidgets('shows the mode and the latency the app is on, even one that is '
      'not a choice', (tester) async {
    final fake = FakeForgeBackend()
      ..mode = 'slow'
      ..latencyMs = 5000;
    await pumpPanel(tester, fake);

    expect(_selectedMode(tester), {'slow'});
    expect(find.text('+5000ms'), findsOneWidget);
  });

  testWidgets('reads without a session and writes with one', (tester) async {
    final fake = await pumpPanel(tester);

    expect(
      fake.callsTo(ForgeDevtoolsProtocol.control).first,
      isNot(contains('session')),
    );

    await tester.tap(_segment('Slow'));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const ValueKey('control-fail-next')));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const ValueKey('control-disarm')));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const ValueKey('control-latency')));
    await tester.pumpAndSettle();
    await tester.tap(find.text('+250ms').last);
    await tester.pumpAndSettle();

    final writes = [
      for (final call in fake.callsTo(ForgeDevtoolsProtocol.control))
        if (call.keys.any(
          const {'mode', 'latencyMs', 'failNext', 'disarm'}.contains,
        ))
          call,
    ];
    expect(writes, hasLength(4));
    for (final write in writes) {
      expect(write['session'], '0');
    }
  });

  testWidgets('a change aimed at a session the app left is refused, says so, '
      'and the rail reads the app again', (tester) async {
    final fake = await pumpPanel(tester);

    fake.session = 1;
    await tester.tap(_segment('Offline'));
    await tester.pumpAndSettle();

    expect(fake.mode, 'online');
    expect(find.textContaining('aimed at session 0'), findsOneWidget);
    expect(_selectedMode(tester), {'online'});

    // Aimed at session 1 from here.
    await tester.tap(_segment('Offline'));
    await tester.pumpAndSettle();

    expect(fake.mode, 'offline');
    expect(fake.callsTo(ForgeDevtoolsProtocol.control).last['session'], '1');
  });

  testWidgets('an app that refuses says why', (tester) async {
    final fake = FakeForgeBackend();
    await pumpPanel(tester, fake);
    fake.overrides[ForgeDevtoolsProtocol.control] = (_) =>
        throw const BackendError(
          ForgeDevtoolsProtocol.control,
          'mode must be online',
        );
    await tester.tap(_segment('Offline'));
    await tester.pumpAndSettle();

    expect(find.text('mode must be online'), findsOneWidget);
  });

  testWidgets('resets on an isolate change: what the old app was on is not '
      'shown for the new one', (tester) async {
    final fake = FakeForgeBackend()
      ..mode = 'offline'
      ..latencyMs = 1000;
    await pumpPanel(tester, fake);
    expect(_selectedMode(tester), {'offline'});
    expect(find.text('+1000ms'), findsOneWidget);

    // A hot restart: a new app, online, no delay.
    fake
      ..mode = 'online'
      ..latencyMs = 0;
    fake.swapIsolate();
    await tester.pump(const Duration(seconds: 1));
    await tester.pumpAndSettle();

    expect(_selectedMode(tester), {'online'});
    expect(find.text('+1000ms'), findsNothing);
    expect(find.text('no delay'), findsOneWidget);
  });

  testWidgets('an isolate change to an app with no simulator removes the '
      'rail', (tester) async {
    final fake = await pumpPanel(tester);
    expect(_mode, findsOneWidget);

    fake.controlsWired = false;
    fake.swapIsolate();
    await tester.pump(const Duration(seconds: 1));
    await tester.pumpAndSettle();

    expect(_mode, findsNothing);
  });

  testWidgets('a principal change reads the rail again and keeps what the app '
      'reports', (tester) async {
    // The mode and the latency are the developer's network: the app keeps
    // them across a principal change, and so does what the rail shows.
    final fake = FakeForgeBackend()
      ..mode = 'offline'
      ..latencyMs = 250;
    await pumpPanel(tester, fake);
    final before = fake.callsTo(ForgeDevtoolsProtocol.control).length;

    fake.session = 1;
    fake.emit({
      'cache': '1',
      'skipped': 0,
      'entries': [_marker(5, 1)],
    });
    await tester.pump(const Duration(seconds: 1));
    await tester.pumpAndSettle();

    expect(
      fake.callsTo(ForgeDevtoolsProtocol.control).length,
      greaterThan(before),
    );
    expect(_selectedMode(tester), {'offline'});
    expect(find.text('+250ms'), findsOneWidget);
  });

  testWidgets('shows the revalidation toggles the app registered and flips '
      'one', (tester) async {
    final fake = FakeForgeBackend();
    fake.revalidation['focus'] = false;
    fake.revalidation['poll'] = true;
    await pumpPanel(tester, fake);

    FilterChip chip(String source) => tester.widget<FilterChip>(
      find.byKey(ValueKey('control-toggle-$source')),
    );

    expect(chip('focus').selected, isFalse);
    expect(chip('poll').selected, isTrue);
    expect(
      find.byKey(const ValueKey('control-toggle-reconnect')),
      findsNothing,
    );

    await tester.tap(find.byKey(const ValueKey('control-toggle-focus')));
    await tester.pumpAndSettle();

    expect(fake.revalidation['focus'], isTrue);
    expect(chip('focus').selected, isTrue);
    expect(fake.callsTo(ForgeDevtoolsProtocol.control).last['toggle'], 'focus');
    expect(fake.callsTo(ForgeDevtoolsProtocol.control).last['session'], '0');
  });

  testWidgets('the toggles stay when there is no simulator, and the network '
      'switches do not', (tester) async {
    final fake = FakeForgeBackend()..controlsWired = false;
    fake.revalidation['reconnect'] = false;
    await pumpPanel(tester, fake);

    expect(
      find.byKey(const ValueKey('control-toggle-reconnect')),
      findsOneWidget,
    );
    expect(_mode, findsNothing);
  });
}
