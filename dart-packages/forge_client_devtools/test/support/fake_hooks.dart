import 'package:flutter/foundation.dart';
import 'package:forge_client_devtools/src/backend/isolate_watch.dart';

/// [ServiceHooks] that behave as devtools_app_shared 0.5.1's service manager
/// does: `hasServiceExtension` hands out one notifier per name until the main
/// isolate closes, when every notifier is set to false and dropped, so a
/// later call returns a new one.
final class FakeServiceHooks implements ServiceHooks {
  /// Starts with a main isolate that has [registered] extensions.
  ///
  /// The extension manager listens to the main isolate too, and the order of
  /// listeners is not fixed. By default it drops its notifiers before the
  /// watch hears the isolate close; with [closeLate], after.
  FakeServiceHooks({Set<String> registered = const {}, this.closeLate = false})
    : _registered = {...registered};

  /// Whether the notifiers are dropped after the watch heard the close.
  final bool closeLate;

  final ValueNotifier<Object?> _isolate = ValueNotifier<Object?>('isolate-1');
  final ValueNotifier<Object?> _connection = ValueNotifier<Object?>(true);
  final Map<String, ValueNotifier<bool>> _notifiers = {};
  final Set<String> _registered;

  @override
  ValueListenable<Object?> get mainIsolate => _isolate;

  @override
  ValueListenable<Object?> get connection => _connection;

  @override
  ValueListenable<bool> hasServiceExtension(String name) => _notifiers
      .putIfAbsent(name, () => ValueNotifier(_registered.contains(name)));

  void _closed() {
    _registered.clear();
    for (final notifier in _notifiers.values) {
      notifier.value = false;
    }
    _notifiers.clear();
  }

  /// The main isolate closes, as at the start of a hot restart.
  void closeIsolate() {
    if (!closeLate) _closed();
    _isolate.value = null;
    if (closeLate) _closed();
  }

  /// A new main isolate starts, with nothing registered yet.
  void openIsolate(Object id) => _isolate.value = id;

  /// The main isolate is replaced without closing first. The extension
  /// manager keeps its notifiers, as 0.5.1 does for a non-null change.
  void replaceIsolate(Object id) => _isolate.value = id;

  /// The app registers [name] on the current isolate.
  void register(String name) {
    _registered.add(name);
    _notifiers[name]?.value = true;
  }

  /// The VM service connection drops and comes back, dropping every
  /// notifier as `vmServiceClosed` does.
  void reconnect() {
    _closed();
    _connection.value = !(_connection.value! as bool);
  }
}
