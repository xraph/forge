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
///
/// The app posts a lifecycle event whenever a cache attaches or detaches, and
/// refuses a call about a cache that is gone with
/// [ForgeDevtoolsProtocol.cacheGone]. Either one makes the connection say
/// hello again: everything the panel holds is dropped first, as for a
/// principal change, and the panel moves to the newest cache when the one it
/// showed is no longer attached. A cache that was disposed and replaced in the
/// same isolate (an `OfflineClient.close` and `open` for the next user)
/// therefore never stays on screen beside the app that replaced it.
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

  /// What the panel says when the app speaks another protocol version.
  static const mismatchMessage =
      "This DevTools extension and the app's forge_client versions don't "
      'match; upgrade both.';

  /// What a call says when it is made while the panel says hello. The panel
  /// that made it is about to be replaced, so nothing is sent.
  static const reconnectingMessage =
      'The panel is reconnecting to the app, so nothing was sent.';

  /// Where calls go.
  final ForgeBackend backend;

  late final StreamSubscription<Json> _subscription;
  bool _disposed = false;

  /// Bumped on every isolate change and every time the app goes away, so a
  /// hello answered after either is ignored.
  int _isolateEpoch = 0;

  /// Bumped by every hello, so only the answer to the latest one is used.
  int _helloSequence = 0;

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

  /// Whether a call has handed a panel anything since the panel last dropped
  /// what it held. The first session the panel learns is adopted with a
  /// clear when it has: what was read before it is fenced by no session.
  bool _shown = false;

  /// Calls [method] scoped to the picked cache.
  ///
  /// An `ext.forge.action`, an `ext.forge.outboxAction`, an
  /// `ext.forge.capture` and an `ext.forge.control` call that changes the
  /// network also carry [session],
  /// read first when the panel has not seen one. When such a call fails, the
  /// session is read again, so a refusal because the principal changed
  /// reloads the view. A call whose answer arrives after the panel saw the
  /// principal change throws a [BackendError] instead of returning it. A call
  /// refused because its cache is gone says hello again, which drops
  /// everything the panel holds.
  Future<Json> call(
    String method, [
    Map<String, String> params = const {},
  ]) async {
    // Saying hello: the panel making this call is about to be replaced, and
    // the cache it would ask may be the one that is gone.
    if (phase == ConnectionPhase.connecting) {
      throw BackendError(method, reconnectingMessage);
    }
    // An app that speaks another protocol version is asked nothing more.
    if (phase == ConnectionPhase.incompatible) {
      throw BackendError(method, mismatchMessage);
    }

    final started = generation;
    final aimed = _aimed(method, params);
    final cache = cacheId;
    final scoped = <String, String>{'cache': ?cache};

    if (aimed && !params.containsKey('session')) {
      final int current;
      try {
        current = session ?? await _readSession(method);
      } on BackendError catch (failure) {
        _refused(failure, cache);
        rethrow;
      }
      if (generation != started) throw BackendError(method, movedMessage);
      scoped['session'] = '$current';
    }
    scoped.addAll(params);

    final Json result;

    try {
      result = await backend.call(method, scoped);
    } on BackendError catch (failure) {
      if (_refused(failure, cache)) rethrow;
      if (aimed && !_disposed) await _resync();
      rethrow;
    }

    if (_disposed) return result;
    if (generation != started) throw BackendError(method, movedMessage);

    if (method == ForgeDevtoolsProtocol.snapshot ||
        method == ForgeDevtoolsProtocol.log) {
      _note(result);
    }

    _shown = true;
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
      method == ForgeDevtoolsProtocol.capture ||
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
  /// resets the panel; a failed read leaves it as it is, unless the cache is
  /// gone.
  Future<void> _resync() async {
    final cache = cacheId;
    try {
      final snapshot = await backend.call(ForgeDevtoolsProtocol.snapshot, {
        'cache': ?cache,
      });
      if (!_disposed) _note(snapshot);
    } on BackendError catch (failure) {
      // The refusal itself is what the panel shows.
      _refused(failure, cache);
    }
  }

  /// Says hello again when [failure] refused a call about [cache], the cache
  /// the panel still shows, because it is gone. Returns whether the refusal
  /// was about a gone cache.
  ///
  /// Not while a hello is already on its way: the panels it cleared read
  /// again at once, still aimed at the gone cache until the hello answers,
  /// and each refusal starting another hello would never let one finish.
  bool _refused(BackendError failure, String? cache) {
    if (_disposed || failure.code != ForgeDevtoolsProtocol.cacheGone) {
      return false;
    }
    if (cache == cacheId && phase != ConnectionPhase.connecting) {
      unawaited(_hello(again: true));
    }
    return true;
  }

  /// Takes the session from a snapshot or a log read for the picked cache.
  void _note(Json answer) {
    final cache = answer.strOrNull('cache');
    if (cache != null && cache != cacheId) return;
    if (answer['session'] case final num value) _saw(value.toInt());
  }

  void _saw(int value) {
    final known = session;
    if (known == null && (_shown || events.isNotEmpty)) {
      // M1: the first session seen, after something was already read. That
      // read is fenced by no session (a page from before a switch whose first
      // snapshot already says the new session), so it is dropped first.
      _principalChanged(value);
    } else if (known == null) {
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
    _shown = false;
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

  /// Says hello. [again] is a hello because a cache attached, detached or
  /// was refused as gone: what the panel holds may be a cache that is gone,
  /// or one another cache replaced, so all of it is dropped first, as for a
  /// principal change.
  Future<void> _hello({bool again = false}) async {
    if (_disposed) return;
    if (again) _forget();
    _set(ConnectionPhase.connecting);
    final epoch = _isolateEpoch;
    final sequence = ++_helloSequence;
    bool stale() =>
        _disposed || epoch != _isolateEpoch || sequence != _helloSequence;
    try {
      final hello = await backend.call(ForgeDevtoolsProtocol.hello);
      if (stale()) return;

      final protocol = hello.integer('protocol');
      if (protocol != ForgeDevtoolsProtocol.version) {
        // Nothing else is asked of an app that speaks another version: its
        // answers would be read with the wrong shapes.
        caches = const [];
        cacheId = null;
        session = null;
        error =
            '$mismatchMessage The app speaks forge devtools protocol '
            '$protocol, this extension speaks ${ForgeDevtoolsProtocol.version}.';
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
      if (stale()) return;
      error = failure.message;
      _set(ConnectionPhase.failed);
    }
  }

  void _onEvent(Json event) {
    if (_disposed) return;

    // An app that speaks another protocol version is asked nothing more,
    // and what it posts is not read.
    if (phase == ConnectionPhase.incompatible) return;

    // A cache attached or detached, this one or another: say hello again.
    if (event.containsKey(ForgeDevtoolsProtocol.lifecycle)) {
      unawaited(_hello(again: true));
      return;
    }

    if (event.str('cache') != cacheId) return;

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
