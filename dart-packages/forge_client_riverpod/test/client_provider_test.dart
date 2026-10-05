import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_riverpod/misc.dart' show Override, ProviderException;
import 'package:flutter_test/flutter_test.dart';
import 'package:forge_client/forge_client.dart';
import 'package:forge_client_flutter/forge_client_flutter.dart';
import 'package:forge_client_flutter/testing.dart';
import 'package:forge_client_riverpod/forge_client_riverpod.dart';

import 'support/harness.dart';

Object _unwrapped(Object error) =>
    error is ProviderException ? _unwrapped(error.exception) : error;

void main() {
  tearDown(() => setClient(null));

  group('forgeClientProvider', () {
    test('serves the overridden client', () {
      final h = harness((_, _) => null);
      final container = containerFor(h);

      expect(container.read(forgeClientProvider), same(h.cache));
      expect(container.read(forgeInstalledClientProvider), same(h.cache));
    });

    test('falls back to the global client when not overridden', () {
      final h = harness((_, _) => null);
      setClient(h.cache);
      final container = ProviderContainer(retry: (_, _) => null);
      addTearDown(container.dispose);

      expect(container.read(forgeClientProvider), same(h.cache));
    });

    test('reports the missing configuration when nothing was configured', () {
      final container = ProviderContainer(retry: (_, _) => null);
      addTearDown(container.dispose);

      // getClient's StateError, possibly wrapped in Riverpod 3's
      // ProviderException.
      expect(
        () => container.read(forgeClientProvider),
        throwsA(
          isA<Object>().having(_unwrapped, 'unwrapped error', isA<StateError>()),
        ),
      );
    });

    testWidgets('does not retry a provider that throws', (tester) async {
      // An Exception, not an Error: Riverpod's default retry skips Errors, so a
      // StateError from getClient would prove nothing. No container-level retry.
      // Under testWidgets the clock is fake, so the wait below is long enough
      // for several of Riverpod's retries (200ms, doubling) and costs nothing.
      var calls = 0;
      final container = ProviderContainer(
        overrides: [
          forgeClientProvider.overrideWith((ref) {
            calls++;
            throw Exception('boom');
          }),
        ],
      );
      addTearDown(container.dispose);
      final sub = container.listen(forgeClientProvider, (_, _) {}, onError: (_, _) {});
      addTearDown(sub.close);

      await tester.pump(const Duration(seconds: 10));

      expect(calls, 1);
    });
  });

  group('forgeInstalledClientProvider', () {
    test('installs the seams for the overridden client while the container lives', () {
      final h = harness((_, _) => null);
      final focus = FakeFocusSignal();
      final connectivity = FakeConnectivitySignal();
      final container = ProviderContainer(
        overrides: [
          forgeClientProvider.overrideWithValue(h.cache),
          forgeFocusSignalProvider.overrideWithValue(focus),
          forgeConnectivitySignalProvider.overrideWithValue(connectivity),
        ],
        retry: (_, _) => null,
      );

      container.read(forgeInstalledClientProvider);
      expect(flutterSeamsInstalled(h.cache), isTrue);
      expect(focus.hasListener, isTrue);
      expect(connectivity.hasListener, isTrue);

      container.dispose();
      expect(flutterSeamsInstalled(h.cache), isFalse);
      expect(focus.hasListener, isFalse);
    });

    test('reinstalls when the focus signal changes', () {
      final h = harness((_, _) => null);
      final first = FakeFocusSignal();
      final second = FakeFocusSignal();
      final connectivity = FakeConnectivitySignal();
      List<Override> overrides(FakeFocusSignal focus) => [
        forgeClientProvider.overrideWithValue(h.cache),
        forgeFocusSignalProvider.overrideWithValue(focus),
        forgeConnectivitySignalProvider.overrideWithValue(connectivity),
      ];
      final container = ProviderContainer(overrides: overrides(first), retry: (_, _) => null);
      addTearDown(container.dispose);
      container.read(forgeInstalledClientProvider);
      expect(first.hasListener, isTrue);

      container.updateOverrides(overrides(second));
      container.read(forgeInstalledClientProvider);

      expect(first.hasListener, isFalse);
      expect(second.hasListener, isTrue);
      expect(flutterSeamsInstalled(h.cache), isTrue);
    });

    test('reinstalls when the connectivity signal changes', () {
      final h = harness((_, _) => null);
      final first = FakeConnectivitySignal();
      final second = FakeConnectivitySignal();
      final focus = FakeFocusSignal();
      List<Override> overrides(FakeConnectivitySignal connectivity) => [
        forgeClientProvider.overrideWithValue(h.cache),
        forgeFocusSignalProvider.overrideWithValue(focus),
        forgeConnectivitySignalProvider.overrideWithValue(connectivity),
      ];
      final container = ProviderContainer(overrides: overrides(first), retry: (_, _) => null);
      addTearDown(container.dispose);
      container.read(forgeInstalledClientProvider);
      expect(first.hasListener, isTrue);

      container.updateOverrides(overrides(second));
      container.read(forgeInstalledClientProvider);

      expect(first.hasListener, isFalse);
      expect(second.hasListener, isTrue);
      expect(flutterSeamsInstalled(h.cache), isTrue);
    });

    test('moves the seams to the new cache when the client changes', () {
      final oldHarness = harness((_, _) => null);
      final newHarness = harness((_, _) => null);
      final focus = FakeFocusSignal();
      final connectivity = FakeConnectivitySignal();
      List<Override> overrides(QueryCache cache) => [
        forgeClientProvider.overrideWithValue(cache),
        forgeFocusSignalProvider.overrideWithValue(focus),
        forgeConnectivitySignalProvider.overrideWithValue(connectivity),
      ];
      final container = ProviderContainer(
        overrides: overrides(oldHarness.cache),
        retry: (_, _) => null,
      );
      addTearDown(container.dispose);
      expect(container.read(forgeInstalledClientProvider), same(oldHarness.cache));
      expect(flutterSeamsInstalled(oldHarness.cache), isTrue);

      container.updateOverrides(overrides(newHarness.cache));

      expect(container.read(forgeInstalledClientProvider), same(newHarness.cache));
      expect(flutterSeamsInstalled(oldHarness.cache), isFalse);
      expect(flutterSeamsInstalled(newHarness.cache), isTrue);
      expect(focus.hasListener, isTrue);
      expect(connectivity.hasListener, isTrue);
    });

    test('shares one installation with a ForgeScope on the same cache', () {
      final h = harness((_, _) => null);
      final container = containerFor(h);

      // What ForgeScope does on mount, done by hand.
      final scope = installFlutterSeams(
        h.cache,
        focus: FakeFocusSignal(),
        connectivity: FakeConnectivitySignal(),
      );
      container.read(forgeInstalledClientProvider);

      scope();
      expect(flutterSeamsInstalled(h.cache), isTrue);
    });
  });
}
