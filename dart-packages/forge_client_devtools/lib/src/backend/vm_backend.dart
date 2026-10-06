import 'dart:async';

import 'package:devtools_extensions/devtools_extensions.dart';
import 'package:flutter/foundation.dart';
import 'package:forge_client/devtools_protocol.dart';
import 'package:vm_service/vm_service.dart';

import 'backend.dart';
import 'isolate_watch.dart';

/// [ServiceHooks] over DevTools' `serviceManager`.
final class _DevtoolsHooks implements ServiceHooks {
  const _DevtoolsHooks();

  @override
  ValueListenable<Object?> get mainIsolate =>
      serviceManager.isolateManager.mainIsolate;

  @override
  ValueListenable<Object?> get connection => serviceManager.connectedState;

  @override
  ValueListenable<bool> hasServiceExtension(String name) =>
      serviceManager.serviceExtensionManager.hasServiceExtension(name);
}

/// The real backend: `serviceManager` from `devtools_extensions`, which talks
/// to the connected app's main isolate. The only file in this package that
/// imports `devtools_extensions`, so widget tests never load it.
final class VmForgeBackend implements ForgeBackend {
  /// Creates the backend. Construct it inside `DevToolsExtension`, after the
  /// globals exist.
  VmForgeBackend()
    : _watch = IsolateWatch(
        const _DevtoolsHooks(),
        ForgeDevtoolsProtocol.hello,
      ) {
    serviceManager.connectedState.addListener(_onConnection);
  }

  /// Follows `ext.forge.hello` across hot restarts and isolate changes.
  final IsolateWatch _watch;

  @override
  ValueListenable<bool> get available => _watch.available;

  @override
  ValueListenable<int> get isolate => _watch.isolate;

  late final StreamController<Json> _events = StreamController<Json>.broadcast(
    onListen: _listen,
    onCancel: _stop,
  );
  StreamSubscription<Event>? _subscription;

  @override
  Stream<Json> get events => _events.stream;

  @override
  Future<Json> call(
    String method, [
    Map<String, String> params = const {},
  ]) async {
    try {
      final response = await serviceManager.callServiceExtensionOnMainIsolate(
        method,
        args: params,
      );
      return <String, Object?>{...?response.json};
    } on RPCError catch (error) {
      throw BackendError(method, error.details ?? error.message);
    } on Object catch (error) {
      throw BackendError(method, '$error');
    }
  }

  void _onConnection() {
    if (!_events.hasListener) return;
    unawaited(_stop().then((_) => _listen()));
  }

  Future<void> _listen() async {
    final service = serviceManager.service;
    if (service == null || _subscription != null) return;

    try {
      await service.streamListen(EventStreams.kExtension);
    } on RPCError catch (error) {
      // 103 is "stream already subscribed": DevTools itself usually is.
      if (error.code != 103) rethrow;
    }

    _subscription = service.onExtensionEvent
        .where(
          (event) => event.extensionKind == ForgeDevtoolsProtocol.eventKind,
        )
        .listen(
          (event) =>
              _events.add(<String, Object?>{...?event.extensionData?.data}),
        );
  }

  Future<void> _stop() async {
    await _subscription?.cancel();
    _subscription = null;
  }
}
