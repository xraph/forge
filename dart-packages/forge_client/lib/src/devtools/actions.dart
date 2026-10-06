/// The half that writes. Port of `client-devtools/src/actions.ts`. Each call
/// does one thing the runtime already does and records itself in the log
/// before it acts, so the trace shows a panel click as a panel click.
///
/// Three rules sit on top of the port.
///
/// A sync source owns its entity types: only the source writes their records,
/// so a hand edit would leave the replica and the store disagreeing. Every
/// action that would write, evict, promote or invalidate an owned entity (or a
/// tag naming one) throws a [StateError] that names it, before anything is
/// recorded or moved. Reads of owned entities are unaffected.
///
/// Keys resolve against the cache as it is now, and only then. While the cache
/// is changing principal (or once the inspector is detached) every action
/// throws a [StateError] instead, so a click that was aimed at one principal's
/// data cannot land on the next.
///
/// An action that changes data goes through the cache's own public write
/// paths where one exists (`refetch`, `invalidate`, `dropKey`, `clear`). The
/// four that have none (`evict`, `rollback`, `promote`, `patchEntity`) use the
/// `DevCache` seam's store and overlay-stack calls, the same ones the TS
/// actions reach, followed by `notifyChanged` so every watcher hears of it.
library;

import 'log.dart';
import 'seams.dart';
import 'types.dart';

/// Panel actions over one cache.
final class DevtoolsActions {
  /// Creates the action layer. [session] is read at each call, because an
  /// identity change increments it. [answerable] is the inspector's own
  /// check that the cache can be asked something right now: it is false while
  /// the cache changes principal and after the inspector is disposed.
  DevtoolsActions(this._cache, this._log, this._session, this._answerable);

  final DevCache _cache;
  final EventLog _log;
  final int Function() _session;
  final bool Function() _answerable;

  /// Throws unless the cache can be acted on now.
  void _ready() {
    if (_answerable()) return;

    throw StateError(
      '[forge] devtools actions are unavailable right now: the cache is '
      'changing principal, or the inspector was detached. Nothing was changed.',
    );
  }

  /// Refuses an action on an entity (or tag) a sync source owns.
  Never _refuse(String action, String target) => throw StateError(
    '[forge] cannot $action $target from the devtools: a sync source owns '
    'that entity, and an edit made here would leave the replica and the store '
    'out of step. Change it through the source. Nothing was changed.',
  );

  DevTracked? _find(String key) {
    for (final record in _cache.trackedRecords()) {
      if (record.key == key) return record;
    }

    return null;
  }

  void _record(ActionKind action, String target) => _log.push(
    (seq, at) => ActionLog(
      seq: seq,
      at: at,
      session: _session(),
      action: action,
      target: target,
    ),
  );

  /// Runs this query again whatever the cache holds. Fails when nothing
  /// tracks [key], and while the cache is changing principal.
  Future<Object?> refetch(String key) async {
    _ready();

    final found = _find(key);

    if (found == null) throw StateError('[forge] nothing is tracking $key');

    _record(ActionKind.refetch, key);

    // The cache's own refetch: a REST response never writes an owned record,
    // so this cannot move a sync source's replica.
    return _cache.cache.refetch(found.meta, found.args);
  }

  /// Raises the tags this query carries. Refused when any of them names an
  /// owned entity type.
  bool invalidate(String key) {
    _ready();

    final entry = _cache.query(key);

    if (entry == null) return false;

    final tags = [...entry.tags];

    for (final tag in tags) {
      if (_cache.ownsTag(tag)) _refuse('invalidate', tag);
    }

    _record(ActionKind.invalidate, key);
    _cache.cache.invalidate(tags);

    return true;
  }

  /// Raises one tag by hand. Refused when it names an owned entity type.
  void invalidateTag(String tag) {
    _ready();

    if (_cache.ownsTag(tag)) _refuse('invalidate', tag);

    _record(ActionKind.invalidateTag, tag);
    _cache.cache.invalidate([tag]);
  }

  /// Drops one entity record. False when the store does not hold it. Refused
  /// for an owned entity.
  bool evict(String entityKey) {
    _ready();

    if (_cache.ownsKey(entityKey)) _refuse('evict', entityKey);
    if (!_cache.hasEntity(entityKey)) return false;

    _record(ActionKind.evict, entityKey);

    final dropped = _cache.evictEntity(entityKey);

    _cache.notifyChanged();

    return dropped;
  }

  /// Forgets this query, or resets it if watched.
  bool drop(String key) {
    _ready();

    if (_find(key) == null) return false;

    _record(ActionKind.drop, key);

    return _cache.cache.dropKey(key);
  }

  /// Drops every entity, skeleton and registry entry. The cache keeps a sync
  /// source's records through a clear, so owned entities survive this.
  void clear() {
    _ready();
    _record(ActionKind.clear, '*');
    _cache.cache.clear();
  }

  /// Takes one optimistic layer off the stack. Removal, not an inverse: it
  /// writes nothing to the store, so it is allowed for any layer.
  bool rollback(int id) {
    _ready();

    if (!_cache.takeOverlay(id)) return false;

    _record(ActionKind.rollback, 'overlay #$id');
    _cache.notifyChanged();

    return true;
  }

  /// Commits one optimistic layer to the base store by hand. Refused when the
  /// layer touches an owned entity; the layer stays on the stack.
  bool promote(int id) {
    _ready();

    for (final layer in _cache.overlays()) {
      if (layer.id != id) continue;

      for (final patch in layer.patches) {
        if (_cache.ownsKey(patch.key)) {
          _refuse('promote a change to', patch.key);
        }
      }
    }

    if (!_cache.promoteOverlay(id)) return false;

    _record(ActionKind.rollback, 'promote overlay #$id');
    _cache.notifyChanged();

    return true;
  }

  /// Marks exactly one query stale without raising its tags.
  bool forceStale(String key) {
    _ready();

    final entry = _cache.query(key);

    if (entry == null) return false;

    _record(ActionKind.stale, key);
    _cache.markStale(entry);
    _cache.notifyChanged();

    return true;
  }

  /// Pushes a hand-written field change onto the overlay stack and returns
  /// the layer id, for the undo. Refused for an owned entity.
  int patchEntity(String key, Map<String, Object?> fields) {
    _ready();

    if (_cache.ownsKey(key)) _refuse('patch', key);

    _record(ActionKind.rollback, 'patch $key');

    final id = _cache.pushMerge(key, fields);

    _cache.notifyChanged();

    return id;
  }

  /// Records that the panel is holding a query in a state. Nothing moves.
  void hold(String key, String state) {
    _ready();
    _record(ActionKind.hold, '$key in $state');
  }

  /// Records the release of a held query.
  void release(String key) {
    _ready();
    _record(ActionKind.release, key);
  }
}
