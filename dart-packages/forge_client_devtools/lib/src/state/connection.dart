import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:forge_client/devtools_protocol.dart';

import '../backend/backend.dart';

/// Where the panel is with the app.
enum ConnectionPhase {
  /// `ext.forge.hello` is not registered on the connected isolate.
  unavailable,

  /// Saying hello.
  connecting,

  /// Talking to a compatible app.
  ready,

  /// The app speaks another protocol version.
  incompatible,

  /// Hello failed.
  failed,
}

/// The panel's view of the app: which cache, which phase, the live events.
///
/// The panel runs in DevTools, not in the app, so it holds its own copies of
/// what the app sent. None of them may outlive the principal they were read
/// for. The connection tracks the cache's identity session (from a snapshot,
/// a log read or an event) and, as soon as it sees that session move or a
/// `principal` marker arrive, drops its event list and bumps [generation].
/// The workspace keys every panel on [generation], so whatever a panel held
/// is thrown away with it.
final class ForgeConnection extends ChangeNotifier {
  /// Starts watching [backend].
  ForgeConnection(this.backend) {
    backend.available.addListener(_onAvailability);
    backend.isolate.addListener(_onIsolate);
    _subscription = backend.events.listen(_onEvent);
    _onAvailability();
  }

  /// Most events kept for the Events panel.
  static const eventCapacity = 1000;

  /// The `ext.forge.control` parameters that change the network the app
  /// sees. A control call carrying any of them is aimed at a session.
  static const controlWrites = {
    'mode',
    'latencyMs',
    'failNext',
    'disarm',
    'toggle',
  };

  /// What a call says when the app changed principal while it was waiting.
  static const movedMessage =
      'The app changed account while this was running, so its answer was '
      'dropped and the view was reloaded. Nothing was changed.';

  /// Where calls go.
  final ForgeBackend backend;

  late final StreamSubscription<Json> _subscription;
  bool _disposed = false;

  /// Bumped on every isolate change and every time the app goes away, so a
  /// hello answered after either is ignored.
  int _isolateEpoch = 0;

  /// The current phase.
  ConnectionPhase phase = ConnectionPhase.unavailable;

  /// Why the phase is `incompatible` or `failed`.
  String? error;

  /// The attached caches, from hello.
  List<Json> caches = const [];

  /// The cache every call is scoped to.
  String? cacheId;

  /// The picked cache's identity session as last seen, or null before the
  /// panel has read one. Sent with every call that changes something, so the
  /// app refuses a click made against a principal it has left.
  int? session;

  /// Bumped whenever what the panel holds stops being valid: the principal
  /// changed, another cache was picked, or the app went away.
  int generation = 0;

  /// Bumped on every event batch, so panels can refresh.
  final ValueNotifier<int> activity = ValueNotifier<int>(0);

  /// Log entries posted since the cache was picked or the principal last
  /// changed, oldest first.
  final List<Json> events = [];

  /// Entries the app or this list had to drop.
  int eventsSkipped = 0;

  /// Calls [method] scoped to the picked cache.
  ///
  /// An `ext.forge.action`, an `ext.forge.outboxAction` and an
  /// `ext.forge.control` call that changes the network also carry [session],
  /// read first when the panel has not seen one. When such a call fails, the
  /// session is read again, so a refusal because the principal changed
  /// reloads the view. A call whose answer arrives after the panel saw the
  /// principal change throws a [BackendError] instead of returning it.
  Future<Json> call(
    String method, [
    Map<String, String> params = const {},
  ]) async {
    final started = generation;
    final aimed = _aimed(method, params);
    final scoped = <String, String>{'cache': ?cacheId};

    if (aimed && !params.containsKey('session')) {
      final current = session ?? await _readSession(method);
      if (generation != started) throw BackendError(method, movedMessage);
      scoped['session'] = '$current';
    }
    scoped.addAll(params);

    final Json result;

    try {
      result = await backend.call(method, scoped);
    } on BackendError {
      if (aimed && !_disposed) await _resync();
      rethrow;
    }

    if (_disposed) return result;
    if (generation != started) throw BackendError(method, movedMessage);

    if (method == ForgeDevtoolsProtocol.snapshot ||
        method == ForgeDevtoolsProtocol.log) {
      _note(result);
    }

    return result;
  }

  /// Says hello again.
  Future<void> reload() => _hello();

  /// Scopes everything to cache [id].
  void selectCache(String id) {
    if (id == cacheId) return;
    cacheId = id;
    // Each cache counts its own sessions.
    session = null;
    _forget();
    notifyListeners();
    activity.value++;
  }

  static bool _aimed(String method, Map<String, String> params) =>
      method == ForgeDevtoolsProtocol.action ||
      method == ForgeDevtoolsProtocol.outboxAction ||
      (method == ForgeDevtoolsProtocol.control &&
          params.keys.any(controlWrites.contains));

  Future<int> _readSession(String method) async {
    final snapshot = await backend.call(ForgeDevtoolsProtocol.snapshot, {
      'cache': ?cacheId,
    });
    if (_disposed) throw BackendError(method, movedMessage);
    _note(snapshot);
    return session ??
        (throw BackendError(
          method,
          'The app did not say which session the cache is on, so nothing was '
          'sent.',
        ));
  }

  /// Reads the session again after a refused change. A session that moved
  /// resets the panel; a failed read leaves it as it is.
  Future<void> _resync() async {
    try {
      final snapshot = await backend.call(ForgeDevtoolsProtocol.snapshot, {
        'cache': ?cacheId,
      });
      if (!_disposed) _note(snapshot);
    } on BackendError {
      // The refusal itself is what the panel shows.
    }
  }

  /// Takes the session from a snapshot or a log read for the picked cache.
  void _note(Json answer) {
    final cache = answer.strOrNull('cache');
    if (cache != null && cache != cacheId) return;
    if (answer['session'] case final num value) _saw(value.toInt());
  }

  void _saw(int value) {
    final known = session;
    if (known == null) {
      session = value;
    } else if (known != value) {
      _principalChanged(value);
    }
  }

  /// The principal changed: nothing the panel holds may be shown again.
  void _principalChanged(int? value) {
    session = value;
    _forget();
    notifyListeners();
  }

  /// Drops the event list and every panel's state.
  void _forget() {
    events.clear();
    eventsSkipped = 0;
    generation++;
  }

  /// The app may be another one now (a hot restart, a new isolate, a
  /// reconnect). It usually reuses cache id `1` and session `0`, so nothing
  /// the panel holds can be told apart from the new app's: drop the caches,
  /// the session, the events and every panel, as for a principal change, and
  /// say hello again.
  void _onIsolate() {
    if (_disposed) return;
    _isolateEpoch++;
    caches = const [];
    cacheId = null;
    session = null;
    error = null;
    _forget();
    _onAvailability();
  }

  void _onAvailability() {
    if (backend.available.value) {
      unawaited(_hello());
    } else {
      // The app went away. Whoever it is when it comes back, what this panel
      // read before is not theirs, and a hello still on its way belongs to
      // the app that left.
      _isolateEpoch++;
      session = null;
      _forget();
      _set(ConnectionPhase.unavailable);
    }
  }

  Future<void> _hello() async {
    _set(ConnectionPhase.connecting);
    final epoch = _isolateEpoch;
    try {
      final hello = await backend.call(ForgeDevtoolsProtocol.hello);
      if (_disposed || epoch != _isolateEpoch) return;

      final protocol = hello.integer('protocol');
      if (protocol != ForgeDevtoolsProtocol.version) {
        error =
            'The app speaks forge devtools protocol $protocol and this extension speaks '
            '${ForgeDevtoolsProtocol.version}. Update forge_client and forge_client_devtools together.';
        _set(ConnectionPhase.incompatible);
        return;
      }

      caches = hello.objs('caches');
      if (cacheId == null || !caches.any((c) => c.str('id') == cacheId)) {
        cacheId = caches.isEmpty ? null : caches.last.str('id');
        session = null;
        _forget();
      }
      error = null;
      _set(ConnectionPhase.ready);
    } on BackendError catch (failure) {
      if (_disposed || epoch != _isolateEpoch) return;
      error = failure.message;
      _set(ConnectionPhase.failed);
    }
  }

  void _onEvent(Json event) {
    if (_disposed || event.str('cache') != cacheId) return;

    final entries = event.objs('entries');
    var start = 0;

    for (var i = 0; i < entries.length; i++) {
      final entry = entries[i];
      final at = switch (entry['session']) {
        final num value => value.toInt(),
        _ => null,
      };

      if (entry.str('kind') == 'principal') {
        // P4: a marker always clears, even when a snapshot already showed the
        // new session. Only what follows it is kept.
        _principalChanged(at ?? session);
        start = i;
      } else if (at != null) {
        final before = generation;
        _saw(at);
        if (generation != before) start = i;
      }
    }

    events.addAll(entries.skip(start));
    eventsSkipped += event.integer('skipped');

    if (events.length > eventCapacity) {
      final excess = events.length - eventCapacity;
      eventsSkipped += excess;
      events.removeRange(0, excess);
    }

    activity.value++;
  }

  void _set(ConnectionPhase next) {
    phase = next;
    if (!_disposed) notifyListeners();
  }

  @override
  void dispose() {
    _disposed = true;
    backend.available.removeListener(_onAvailability);
    backend.isolate.removeListener(_onIsolate);
    unawaited(_subscription.cancel());
    activity.dispose();
    super.dispose();
  }
}
