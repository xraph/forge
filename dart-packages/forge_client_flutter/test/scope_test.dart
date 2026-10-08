import 'dart:async';

import 'package:flutter/widgets.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:forge_client/forge_client.dart';
import 'package:forge_client_flutter/forge_client_flutter.dart';
import 'package:forge_client_flutter/testing.dart';

import 'support/harness.dart';

/// Records the cache [ForgeScope.of] resolves, the way a builder resolves it.
final class _Probe extends StatelessWidget {
  const _Probe({required this.onResolve, this.explicit});

  final void Function(QueryCache client) onResolve;
  final QueryCache? explicit;

  @override
  Widget build(BuildContext context) {
    onResolve(ForgeScope.of(context, client: explicit));
    return const SizedBox();
  }
}

void main() {
  tearDown(() => setClient(null));

  group('client resolution', () {
    testWidgets(
      'falls back to the module-level client when no provider is rendered',
      (tester) async {
        final h = harness((_, _) => null);
        setClient(h.cache);
        QueryCache? resolved;

        // No scope anywhere. Generated bindings live at module scope and must
        // not require the app to adopt a dependency injection style first.
        await tester.pumpWidget(
          _Probe(onResolve: (client) => resolved = client),
        );

        expect(resolved, same(h.cache));
      },
    );

    testWidgets('prefers a provided client over the module-level one', (
      tester,
    ) async {
      final global = harness((_, _) => null);
      final scoped = harness((_, _) => null);
      setClient(global.cache);
      QueryCache? resolved;

      await tester.pumpWidget(
        scope(scoped, _Probe(onResolve: (client) => resolved = client)),
      );

      expect(resolved, same(scoped.cache));
    });

    testWidgets('prefers a per-call client over a provided one', (
      tester,
    ) async {
      final provided = harness((_, _) => null);
      final explicit = harness((_, _) => null);
      QueryCache? resolved;

      await tester.pumpWidget(
        scope(
          provided,
          _Probe(
            explicit: explicit.cache,
            onResolve: (client) => resolved = client,
          ),
        ),
      );

      expect(resolved, same(explicit.cache));
    });

    testWidgets(
      'reports the missing configuration rather than fetching into a scratch cache',
      (tester) async {
        await tester.pumpWidget(_Probe(onResolve: (_) {}));

        // getClient throws a named StateError rather than minting a cache
        // nobody else can see.
        expect(tester.takeException(), isA<StateError>());
      },
    );

    testWidgets(
      'exposes the resolved client for work that reaches past the hooks',
      (tester) async {
        final h = harness((_, _) => null);
        QueryCache? resolved;

        await tester.pumpWidget(
          scope(
            h,
            Builder(
              builder: (context) {
                resolved = context.forgeClient;
                return const SizedBox();
              },
            ),
          ),
        );

        expect(resolved, same(h.cache));
      },
    );

    // Flutter-only: React's useContext has no maybe variant to port.
    testWidgets(
      'maybeOf answers the nearest scope only, never the module-level client',
      (tester) async {
        final global = harness((_, _) => null);
        final scoped = harness((_, _) => null);
        setClient(global.cache);
        QueryCache? outside = global.cache;
        QueryCache? inside;

        await tester.pumpWidget(
          Column(
            textDirection: .ltr,
            children: [
              Builder(
                builder: (context) {
                  outside = ForgeScope.maybeOf(context);
                  return const SizedBox();
                },
              ),
              scope(
                scoped,
                Builder(
                  builder: (context) {
                    inside = ForgeScope.maybeOf(context);
                    return const SizedBox();
                  },
                ),
              ),
            ],
          ),
        );

        expect(outside, isNull);
        expect(inside, same(scoped.cache));
      },
    );
  });

  group('getServerSnapshot', () {
    test(
      'renders the loading branch on the server and issues no request',
      () {},
      skip: 'Flutter has no server rendering; there is no getServerSnapshot to port.',
    );
    test(
      'returns the same object on every call, for a query no cache has opened',
      () {},
      skip: 'Flutter has no server rendering; there is no getServerSnapshot to port.',
    );
  });

  group('seams', () {
    testWidgets(
      'installs the seams for its client and removes them when it goes away',
      (tester) async {
        final h = harness((_, _) => null);
        final focus = FakeFocusSignal();

        await tester.pumpWidget(scope(h, const SizedBox(), focus: focus));
        expect(flutterSeamsInstalled(h.cache), isTrue);
        expect(focus.hasListener, isTrue);

        await tester.pumpWidget(const SizedBox());
        expect(flutterSeamsInstalled(h.cache), isFalse);
        expect(focus.hasListener, isFalse);
      },
    );

    testWidgets(
      'moves the seams to the new client when the client is swapped',
      (tester) async {
        final a = harness((_, _) => null);
        final b = harness((_, _) => null);
        final focus = FakeFocusSignal();
        final connectivity = FakeConnectivitySignal();
        final resolved = <QueryCache>[];
        // One probe instance across both pumps, so it can only rebuild because
        // the scope told it the client changed.
        final probe = _Probe(onResolve: resolved.add);

        Widget tree(Harness h) =>
            scope(h, probe, focus: focus, connectivity: connectivity);

        await tester.pumpWidget(tree(a));
        await tester.pumpWidget(tree(b));

        // The signals are shared, so the client is the only thing that changed.
        expect(flutterSeamsInstalled(a.cache), isFalse);
        expect(flutterSeamsInstalled(b.cache), isTrue);
        expect(focus.hasListener, isTrue);
        expect(resolved.last, same(b.cache));
      },
    );

    testWidgets('keeps one installation when two scopes share a client', (
      tester,
    ) async {
      final h = harness((_, _) => null);

      await tester.pumpWidget(scope(h, scope(h, const SizedBox())));
      expect(flutterSeamsInstalled(h.cache), isTrue);

      await tester.pumpWidget(scope(h, const SizedBox()));
      expect(flutterSeamsInstalled(h.cache), isTrue);
    });

    testWidgets('installs nothing when asked not to', (tester) async {
      final h = harness((_, _) => null);

      await tester.pumpWidget(
        ForgeScope(
          client: h.cache,
          installSeams: false,
          child: const SizedBox(),
        ),
      );

      expect(flutterSeamsInstalled(h.cache), isFalse);
    });

    testWidgets('installs and removes the seams when installSeams is toggled', (
      tester,
    ) async {
      final h = harness((_, _) => null);
      final focus = FakeFocusSignal();
      final connectivity = FakeConnectivitySignal();

      Widget tree({required bool install}) => ForgeScope(
        client: h.cache,
        focus: focus,
        connectivity: connectivity,
        installSeams: install,
        child: const SizedBox(),
      );

      await tester.pumpWidget(tree(install: false));
      expect(flutterSeamsInstalled(h.cache), isFalse);

      await tester.pumpWidget(tree(install: true));
      expect(flutterSeamsInstalled(h.cache), isTrue);
      expect(focus.hasListener, isTrue);

      await tester.pumpWidget(tree(install: false));
      expect(flutterSeamsInstalled(h.cache), isFalse);
      expect(focus.hasListener, isFalse);
    });

    testWidgets(
      'takes a swapped focus signal into effect and releases the old one',
      (tester) async {
        final clock = ManualClock();
        final h = harness(
          (request, call) => order(idOf(request), 10 + call),
          clock: clock,
        );
        final first = FakeFocusSignal();
        final second = FakeFocusSignal();
        final connectivity = FakeConnectivitySignal();

        Widget tree(FakeFocusSignal focus) => scope(
          h,
          const SizedBox(),
          focus: focus,
          connectivity: connectivity,
        );

        await tester.pumpWidget(tree(first));
        final subscription = getOrder(const OrderArgs(1))
            .watch(h.cache, staleTime: const Duration(minutes: 1))
            .listen((_) {});
        await settle(tester);
        expect(h.transport.countOf(opGetOrder), 1);

        await tester.pumpWidget(tree(second));

        expect(first.hasListener, isFalse);
        expect(second.hasListener, isTrue);
        // The same cache stayed installed, with the connectivity signal untouched
        // in value but re-subscribed by the reinstall.
        expect(flutterSeamsInstalled(h.cache), isTrue);

        // The released signal revalidates nothing, the new one does.
        clock.advance(const Duration(minutes: 5));
        first.focus();
        await settle(tester);
        expect(h.transport.countOf(opGetOrder), 1);

        second.focus();
        await settle(tester);
        expect(h.transport.countOf(opGetOrder), 2);

        unawaited(subscription.cancel());
      },
    );

    testWidgets(
      'takes a swapped connectivity signal into effect and releases the old one',
      (tester) async {
        final clock = ManualClock();
        final h = harness(
          (request, call) => order(idOf(request), 10 + call),
          clock: clock,
        );
        final focus = FakeFocusSignal();
        final first = FakeConnectivitySignal();
        final second = FakeConnectivitySignal();

        Widget tree(FakeConnectivitySignal connectivity) => scope(
          h,
          const SizedBox(),
          focus: focus,
          connectivity: connectivity,
        );

        await tester.pumpWidget(tree(first));
        final subscription = getOrder(const OrderArgs(1))
            .watch(h.cache, staleTime: const Duration(minutes: 1))
            .listen((_) {});
        await settle(tester);
        expect(h.transport.countOf(opGetOrder), 1);

        await tester.pumpWidget(tree(second));

        expect(first.hasListener, isFalse);
        expect(second.hasListener, isTrue);

        clock.advance(const Duration(minutes: 5));
        first.goOnline();
        await settle(tester);
        expect(h.transport.countOf(opGetOrder), 1);

        second.goOnline();
        await settle(tester);
        expect(h.transport.countOf(opGetOrder), 2);

        unawaited(subscription.cancel());
      },
    );

    // Installation is ref-counted per cache and never keyed on the signals:
    // keying it on them would install two revalidators on one cache, so one
    // focus would refetch twice, and the Riverpod adapter shares one
    // installation per cache too.
    testWidgets(
      'keeps the first install\'s signals while any scope sharing the client holds it',
      (tester) async {
        final clock = ManualClock();
        final h = harness(
          (request, call) => order(idOf(request), 10 + call),
          clock: clock,
        );
        final firstFocus = FakeFocusSignal();
        final secondFocus = FakeFocusSignal();
        final swappedFocus = FakeFocusSignal();
        final firstConnectivity = FakeConnectivitySignal();
        final secondConnectivity = FakeConnectivitySignal();

        Widget second(FakeFocusSignal focus) => KeyedSubtree(
          key: const ValueKey('second'),
          child: scope(
            h,
            const SizedBox(),
            focus: focus,
            connectivity: secondConnectivity,
          ),
        );

        Widget tree({
          required bool withFirst,
          required FakeFocusSignal secondSignal,
        }) => Column(
          textDirection: .ltr,
          children: [
            if (withFirst)
              KeyedSubtree(
                key: const ValueKey('first'),
                child: scope(
                  h,
                  const SizedBox(),
                  focus: firstFocus,
                  connectivity: firstConnectivity,
                ),
              ),
            second(secondSignal),
          ],
        );

        await tester.pumpWidget(
          tree(withFirst: true, secondSignal: secondFocus),
        );
        final subscription = getOrder(const OrderArgs(1))
            .watch(h.cache, staleTime: const Duration(minutes: 1))
            .listen((_) {});
        await settle(tester);
        expect(h.transport.countOf(opGetOrder), 1);

        // The second scope joined the first one's installation.
        expect(firstFocus.hasListener, isTrue);
        expect(firstConnectivity.hasListener, isTrue);
        expect(secondFocus.hasListener, isFalse);
        expect(secondConnectivity.hasListener, isFalse);

        // A swap by the scope that did not install rejoins the same installation.
        await tester.pumpWidget(
          tree(withFirst: true, secondSignal: swappedFocus),
        );
        expect(swappedFocus.hasListener, isFalse);
        expect(firstFocus.hasListener, isTrue);

        // The installing scope goes away, but the second still holds the cache,
        // so the first scope's signals stay in effect.
        await tester.pumpWidget(
          tree(withFirst: false, secondSignal: swappedFocus),
        );
        expect(flutterSeamsInstalled(h.cache), isTrue);
        expect(firstFocus.hasListener, isTrue);
        expect(swappedFocus.hasListener, isFalse);

        clock.advance(const Duration(minutes: 5));
        swappedFocus.focus();
        await settle(tester);
        expect(h.transport.countOf(opGetOrder), 1);

        firstFocus.focus();
        await settle(tester);
        expect(h.transport.countOf(opGetOrder), 2);

        // The last holder releases, and the first scope's signals with it.
        await tester.pumpWidget(const SizedBox());
        expect(flutterSeamsInstalled(h.cache), isFalse);
        expect(firstFocus.hasListener, isFalse);
        expect(firstConnectivity.hasListener, isFalse);

        unawaited(subscription.cancel());
      },
    );
  });
}
