import 'package:flutter/foundation.dart';

/// The parts of DevTools' service manager the backend watches. Implemented
/// over `serviceManager` in DevTools and by a fake in tests, so the isolate
/// tracking is tested without a VM.
abstract interface class ServiceHooks {
  /// The connected app's main isolate, or null while there is none.
  ValueListenable<Object?> get mainIsolate;

  /// The VM service connection state.
  ValueListenable<Object?> get connection;

  /// Whether the main isolate has registered [name]. DevTools may hand out a
  /// new notifier for the same name after an isolate closes, so this is asked
  /// again rather than kept.
  ValueListenable<bool> hasServiceExtension(String name);
}

/// Follows one service extension across isolates.
///
/// DevTools' `hasServiceExtension` notifier is not stable: when the main
/// isolate closes (a hot restart) it is set to false and dropped, and a later
/// call returns a new one. A notifier captured once would stay false for good.
/// This keeps its own [available] and re-resolves the DevTools notifier on
/// every isolate or connection change, and whenever the one it holds fires.
/// [isolate] changes on every isolate or connection change, so the panel can
/// drop everything it read from the previous app.
final class IsolateWatch {
  /// Starts following [extension] through [hooks].
  IsolateWatch(this.hooks, this.extension) {
    _lastIsolate = hooks.mainIsolate.value;
    hooks.mainIsolate.addListener(_onMainIsolate);
    hooks.connection.addListener(_onConnection);
    _resolve();
  }

  /// Where the service state comes from.
  final ServiceHooks hooks;

  /// The extension that says forge_client is there.
  final String extension;

  final ValueNotifier<bool> _available = ValueNotifier<bool>(false);
  final ValueNotifier<int> _isolate = ValueNotifier<int>(0);
  ValueListenable<bool>? _held;
  Object? _lastIsolate;
  bool _disposed = false;

  /// True while the current main isolate has [extension] registered.
  ValueListenable<bool> get available => _available;

  /// Bumped on every isolate or connection change.
  ValueListenable<int> get isolate => _isolate;

  void _onMainIsolate() {
    final next = hooks.mainIsolate.value;
    if (identical(next, _lastIsolate)) return;
    _lastIsolate = next;
    _changed();
  }

  void _onConnection() => _changed();

  void _changed() {
    if (_disposed) return;
    _isolate.value++;
    // DevTools may drop its notifiers in a listener that runs after this one.
    // That is harmless: the next isolate or connection change asks again,
    // and no extension can register before there is a new isolate.
    _resolve();
  }

  /// Takes the notifier DevTools hands out now, and its value.
  void _resolve() {
    if (_disposed) return;
    final next = hooks.hasServiceExtension(extension);
    if (!identical(next, _held)) {
      _held?.removeListener(_resolve);
      _held = next..addListener(_resolve);
    }
    _available.value = next.value;
  }

  /// Stops watching.
  void dispose() {
    _disposed = true;
    hooks.mainIsolate.removeListener(_onMainIsolate);
    hooks.connection.removeListener(_onConnection);
    _held?.removeListener(_resolve);
    _held = null;
    _available.dispose();
    _isolate.dispose();
  }
}
