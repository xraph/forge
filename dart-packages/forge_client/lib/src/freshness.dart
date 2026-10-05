import 'dart:async';

import 'cache.dart';
import 'operation.dart';
import 'transport.dart';

/// The host telling the cache that focus came back. The core never imports
/// Flutter: the Flutter adapter implements this over `AppLifecycleListener`.
abstract interface class FocusSignal {
  /// Emits true when focus is regained and false when it is lost.
  Stream<bool> get focused;
}

/// The host telling the cache that the network came back. The Flutter adapter
/// implements this over `connectivity_plus`.
abstract interface class ConnectivitySignal {
  /// Emits true when the network comes back and false when it goes.
  Stream<bool> get online;
}

/// Revalidates stale queries when [signal] reports focus regained, at most
/// once per [throttle] on the cache's clock. A query that just refetched is
/// not expired, so staleTime remains the real rate limit. Returns the
/// uninstaller, which is idempotent.
void Function() revalidateOnFocus(
  QueryCache cache,
  FocusSignal signal, {
  Duration throttle = const Duration(seconds: 5),
}) {
  int? last;

  final subscription = signal.focused.listen((focused) {
    if (!focused) return;

    final now = cache.clock.now();

    if (last != null && now - last! < throttle.inMilliseconds) return;

    last = now;
    cache.revalidate();
  });

  return _once(subscription);
}

/// Revalidates stale queries when [signal] reports the network back. Returns
/// the uninstaller, which is idempotent.
void Function() revalidateOnReconnect(
  QueryCache cache,
  ConnectivitySignal signal,
) {
  final subscription = signal.online.listen((online) {
    if (online) cache.revalidate();
  });

  return _once(subscription);
}

/// Refetches one query every [every] until the returned stop is called or
/// the cache is disposed.
///
/// A loop over a [Sleep] rather than a periodic timer: the next delay begins
/// after the previous request settled, so a slow endpoint spreads its polls
/// out rather than queueing them. A failed request is already reported through
/// the cache's `onError` and does not stop the loop. TypeScript also pauses
/// while the document is hidden; the core has no document, so a host pauses
/// polling by calling stop when it loses focus.
void Function() poll(
  QueryCache cache,
  OperationMeta meta,
  TagContext args,
  Duration every, {
  Sleep sleep = realSleep,
}) {
  var stopped = false;

  Future<void> loop() async {
    while (!stopped && !cache.isDisposed) {
      await sleep(every);

      // A disposed cache refuses every refetch; looping on would only keep
      // reporting that.
      if (stopped || cache.isDisposed) break;

      try {
        await cache.refetch(meta, args);
      } on Object {
        // Reported through the cache's own onError already.
      }
    }
  }

  unawaited(loop());

  return () => stopped = true;
}

void Function() _once(StreamSubscription<bool> subscription) {
  var stopped = false;

  return () {
    if (stopped) return;

    stopped = true;
    unawaited(subscription.cancel());
  };
}
