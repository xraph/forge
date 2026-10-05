import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:forge_client/forge_client.dart';
import 'package:forge_client_flutter/forge_client_flutter.dart';
import 'package:forge_client_flutter/testing.dart';
import 'package:forge_client_riverpod/forge_client_riverpod.dart';

import 'support/harness.dart';

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
      // ProviderException, whose message includes the original.
      expect(
        () => container.read(forgeClientProvider),
        throwsA(predicate((Object error) => '$error'.contains('Bad state'))),
      );
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
