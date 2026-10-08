import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:forge_client/forge_client.dart';
import 'package:forge_client_flutter/forge_client_flutter.dart';

import 'internal.dart';

/// The cache every Forge provider reads from. Override it in `ProviderScope`:
///
/// ```dart
/// ProviderScope(
///   overrides: [forgeClientProvider.overrideWithValue(client)],
///   child: const App(),
/// )
/// ```
///
/// Not overridden, it falls back to the global client from
/// `configureClient`, and throws `getClient`'s `StateError` when nothing was
/// configured. Build the cache with `commitScheduler: frameCommitScheduler()`
/// for per-frame commits, as with `forge_client_flutter`.
final Provider<QueryCache> forgeClientProvider = Provider<QueryCache>(
  (ref) => getClient(),
  name: 'forgeClientProvider',
  retry: noRetry,
);

/// The focus signal [forgeInstalledClientProvider] installs. Defaults to
/// [AppLifecycleFocusSignal]; tests override it with a fake.
final Provider<FocusSignal> forgeFocusSignalProvider = Provider<FocusSignal>(
  (ref) => AppLifecycleFocusSignal(),
  name: 'forgeFocusSignalProvider',
  retry: noRetry,
);

/// The connectivity signal [forgeInstalledClientProvider] installs. Defaults
/// to [ConnectivityPlusSignal]; tests override it with a fake.
final Provider<ConnectivitySignal> forgeConnectivitySignalProvider =
    Provider<ConnectivitySignal>(
      (ref) => ConnectivityPlusSignal(),
      name: 'forgeConnectivitySignalProvider',
      retry: noRetry,
    );

/// [forgeClientProvider]'s cache with focus and reconnect revalidation
/// installed through `installFlutterSeams`, the same installer `ForgeScope`
/// uses, for as long as this provider lives.
///
/// Every query and mutation provider in this package reads this one, so an
/// override of [forgeClientProvider], by value or by function, always gets
/// the seams.
final Provider<QueryCache> forgeInstalledClientProvider = Provider<QueryCache>(
  (ref) {
    final client = ref.watch(forgeClientProvider);
    final uninstall = installFlutterSeams(
      client,
      focus: ref.watch(forgeFocusSignalProvider),
      connectivity: ref.watch(forgeConnectivitySignalProvider),
    );
    ref.onDispose(uninstall);
    return client;
  },
  name: 'forgeInstalledClientProvider',
  retry: noRetry,
);
