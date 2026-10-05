import 'dart:async';

import 'package:flutter/scheduler.dart';
import 'package:flutter/widgets.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:forge_client_flutter/forge_client_flutter.dart';

import 'support/harness.dart';

/// Stands in for plan 04's OfflineClient, which implements the same
/// interface, re-exported here from forge_client.
final class _FakeOutbox implements OutboxFailureSource {
  final StreamController<Object> controller = StreamController<Object>.broadcast(sync: true);

  @override
  Stream<Object> get failures => controller.stream;
}

void main() {
  group('ForgeOutboxListener', () {
    testWidgets('hands each failure to onFailure with a live context', (tester) async {
      final h = harness((_, _) => null);
      final outbox = _FakeOutbox();
      final seen = <String>[];

      await tester.pumpWidget(scope(
        h,
        ForgeOutboxListener(
          source: outbox,
          onFailure: (context, failure) {
            expect(context.mounted, isTrue);
            seen.add('$failure');
          },
          child: const SizedBox(),
        ),
      ));

      outbox.controller
        ..add('conflict on Order:1')
        ..add('validation on Order:2');

      expect(seen, ['conflict on Order:1', 'validation on Order:2']);
    });

    testWidgets('calls the latest onFailure, not the first', (tester) async {
      final h = harness((_, _) => null);
      final outbox = _FakeOutbox();
      final first = <Object>[];
      final second = <Object>[];

      Widget listen(List<Object> into) => scope(
        h,
        ForgeOutboxListener(
          source: outbox,
          onFailure: (context, failure) => into.add(failure),
          child: const SizedBox(),
        ),
      );

      await tester.pumpWidget(listen(first));
      await tester.pumpWidget(listen(second));
      outbox.controller.add('gone');

      expect(first, isEmpty);
      expect(second, ['gone']);
    });

    testWidgets('moves to a new source and cancels the old one', (tester) async {
      final h = harness((_, _) => null);
      final a = _FakeOutbox();
      final b = _FakeOutbox();
      final seen = <Object>[];

      Widget listen(_FakeOutbox source) => scope(
        h,
        ForgeOutboxListener(
          source: source,
          onFailure: (context, failure) => seen.add(failure),
          child: const SizedBox(),
        ),
      );

      await tester.pumpWidget(listen(a));
      await tester.pumpWidget(listen(b));

      expect(a.controller.hasListener, isFalse);
      expect(b.controller.hasListener, isTrue);

      b.controller.add('from b');
      expect(seen, ['from b']);
    });

    testWidgets('cancels its subscription when removed', (tester) async {
      final h = harness((_, _) => null);
      final outbox = _FakeOutbox();

      await tester.pumpWidget(scope(
        h,
        ForgeOutboxListener(
          source: outbox,
          onFailure: (context, failure) {},
          child: const SizedBox(),
        ),
      ));
      expect(outbox.controller.hasListener, isTrue);

      await tester.pumpWidget(scope(h, const SizedBox()));
      expect(outbox.controller.hasListener, isFalse);
    });
  });

  group('while the tree is being built', () {
    // A source can emit from inside a build: the cache notifies synchronously,
    // and an outbox that rolls back an optimistic write on a failure does so
    // in that notification. onFailure typically shows a snack bar, which
    // marks widgets dirty, so it must not run until the frame is done.

    /// A listener over [outbox] whose child emits [emitted] while it builds.
    Widget emitting(
      _FakeOutbox outbox,
      List<Object> emitted,
      void Function(BuildContext context, Object failure) onFailure,
    ) => ForgeOutboxListener(
      source: outbox,
      onFailure: onFailure,
      child: Builder(builder: (context) {
        for (final failure in emitted) {
          outbox.controller.add(failure);
        }
        emitted.clear();
        return const SizedBox();
      }),
    );

    testWidgets('holds failures until the frame is done and delivers them in order', (tester) async {
      final h = harness((_, _) => null);
      final outbox = _FakeOutbox();
      final seen = <String>[];
      final phases = <SchedulerPhase>[];

      await tester.pumpWidget(scope(
        h,
        emitting(outbox, ['first', 'second'], (context, failure) {
          phases.add(SchedulerBinding.instance.schedulerPhase);
          expect(context.mounted, isTrue);
          seen.add('$failure');
        }),
      ));

      expect(seen, ['first', 'second']);
      expect(phases, isNot(contains(SchedulerPhase.persistentCallbacks)));
    });

    testWidgets('keeps a failure that arrives after a held one behind it', (tester) async {
      final h = harness((_, _) => null);
      final outbox = _FakeOutbox();
      final seen = <String>[];

      await tester.pumpWidget(scope(
        h,
        ForgeOutboxListener(
          source: outbox,
          onFailure: (context, failure) => seen.add('$failure'),
          child: Builder(builder: (context) {
            // Registered first, so it runs before the listener's own
            // post-frame callback, with 'held' still waiting. A failure
            // arriving there must not overtake it.
            SchedulerBinding.instance.addPostFrameCallback(
              (_) => outbox.controller.add('late'),
            );
            outbox.controller.add('held');
            return const SizedBox();
          }),
        ),
      ));

      expect(seen, ['held', 'late']);
    });

    testWidgets('delivers the rest of a held batch when onFailure throws', (tester) async {
      final h = harness((_, _) => null);
      final outbox = _FakeOutbox();
      final seen = <String>[];

      await tester.pumpWidget(scope(
        h,
        emitting(outbox, ['bad', 'good'], (context, failure) {
          if (failure == 'bad') throw const Boom('handler failed');
          seen.add('$failure');
        }),
      ));

      expect(seen, ['good']);
      expect(tester.takeException(), isA<Boom>());
    });

    testWidgets('drops a held failure when the listener is removed in the same frame', (tester) async {
      final h = harness((_, _) => null);
      final outbox = _FakeOutbox();
      final seen = <String>[];

      Widget tree({required bool listening, required List<Object> emitted}) => scope(
        h,
        Stack(children: [
          // Builds before the listener is unmounted, which happens when the
          // frame's build scope finishes.
          Builder(builder: (context) {
            for (final failure in emitted) {
              outbox.controller.add(failure);
            }
            return const SizedBox();
          }),
          if (listening)
            ForgeOutboxListener(
              source: outbox,
              onFailure: (context, failure) => seen.add('$failure'),
              child: const SizedBox(),
            ),
        ]),
      );

      await tester.pumpWidget(tree(listening: true, emitted: []));
      await tester.pumpWidget(tree(listening: false, emitted: ['too late']));

      expect(seen, isEmpty);
      expect(tester.takeException(), isNull);
    });
  });
}
