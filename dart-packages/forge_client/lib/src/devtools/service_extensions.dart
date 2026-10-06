/// The `ext.forge.*` VM service extensions and the `forge:event` stream.
///
/// Everything here is reached only from `registerForgeServiceExtensions`,
/// whose first line returns when `kForgeDevtools` is false, so a release
/// compiler drops this file and everything only it reaches.
///
/// Nothing crosses principals, and nothing outlives its cache. The devtools
/// behind each extension answer empty while the cache changes principal; this
/// host adds the other half: a cache that was disposed or detached is refused
/// by every extension that takes a cache id, and the host detaches it as soon
/// as it notices.
library;

import 'dart:async';
import 'dart:convert';
import 'dart:developer' as developer;

import 'package:meta/meta.dart';

import '../cache.dart';
import '../observe.dart';
import '../operation.dart';
import '../transport.dart';
import 'control.dart';
import 'devtools.dart' as dt;
import 'explain.dart' show MissCause, TagsCause;
import 'frames.dart' show bounded;
import 'inspect.dart' show EntityFilter;
import 'types.dart' show EntitySnapshot, LogEntry;
import 'protocol.dart';
import 'release.dart';
import 'requests.dart';
import 'seams.dart';

/// How the host registers a VM service extension. `developer.registerExtension`
/// in an app; a recording function in tests.
typedef ExtensionRegistrar = void Function(
  String method,
  developer.ServiceExtensionHandler handler,
);

/// How the host posts an event. `developer.postEvent` in an app.
typedef EventPoster = void Function(String kind, Map<String, Object?> data);

/// Attaches the devtools to [cache] and exposes it to Flutter DevTools.
///
/// `configureClient` calls this in debug and profile builds. Call it yourself
/// for a cache you built with the `QueryCache` constructor. [transport] feeds
/// the request log through its debug observer slot; [controls] is the
/// offline and latency simulator wrapped around the cache's transport;
/// [operations] is the generated `operations` table, which lets the panel
/// preview any operation; [outbox] lets the panel replay and discard.
///
/// On a cache that is already attached (every `configureClient` cache in a
/// debug build), a later call registers nothing again and needs no unregister
/// first. It sets [outbox] on the existing devtools when one is given, and
/// fills what is still empty: [transport] when no request log is wired,
/// [controls] and [revalidation] when none is, and [operations] by id. A slot
/// that is filled is never overwritten, so the first registration wins.
///
/// Throws a [StateError] for a disposed cache. Returns null, and does nothing,
/// when `kForgeDevtools` is false.
dt.Devtools? registerForgeServiceExtensions(
  QueryCache cache, {
  RestTransport? transport,
  ControlledTransport? controls,
  Map<String, OperationMeta> operations = const {},
  OutboxInspector? outbox,
  Revalidation? revalidation,
}) {
  if (!kForgeDevtools) return null;
  return ForgeDevtoolsHost.instance.attach(
    cache,
    transport: transport,
    controls: controls,
    operations: operations,
    outbox: outbox,
    revalidation: revalidation,
  );
}

/// Detaches the devtools from [cache], restoring its observer.
void unregisterForgeDevtools(QueryCache cache) {
  if (!kForgeDevtools) return;
  ForgeDevtoolsHost.instance.detach(cache);
}

final class _BadParams implements Exception {
  const _BadParams(this.message);

  final String message;
}

final class _Attached {
  _Attached(this.id, this.devtools, this.operations);

  final String id;
  final dt.Devtools devtools;
  final Map<String, OperationMeta> operations;
  RestTransport? transport;
  RequestObserver? requestObserver;
  final List<Map<String, Object?>> buffer = [];
  int skipped = 0;
  bool scheduled = false;
  void Function() unsubscribe = _noop;
  void Function() stopChanging = _noop;
  StreamSubscription<String?>? disposal;

  static void _noop() {}
}

/// The longest identifier (a query key, an entity key, a tag, an outbox id)
/// served whole. Identifiers must round-trip into later `ext.forge.*` calls, so
/// they are never cut; a row that carries a longer one is replaced by a marker
/// row instead of being truncated.
const _maxIdentifier = 64 * 1024;

/// The most operations one response lists.
const _maxOperations = 5000;

/// The most bytes one response may encode to. Larger is refused, not cut.
const _maxResponseBytes = 8 * 1024 * 1024;

/// Width at which an application-owned payload is bounded.
const _payloadWidth = 100;

/// Whether [value] is an identifier too long to serve.
bool _oversized(Object? value) =>
    value is String && value.length > _maxIdentifier;

/// [row] as it is, or a marker row when any identifier in it is too long. The
/// marker names the field and keeps the position (`seq`, `kind`) so a client
/// paging by sequence moves on.
Map<String, Object?> _guard(Map<String, Object?> row) {
  for (final MapEntry(:key, :value) in row.entries) {
    final long =
        _oversized(value) || (value is List<Object?> && value.any(_oversized));

    if (long) {
      return {
        'oversized': true,
        'field': key,
        if (row['seq'] case final int seq) 'seq': seq,
        if (row['kind'] case final String kind) 'kind': kind,
      };
    }
  }

  return row;
}

List<Map<String, Object?>> _guarded(Iterable<Map<String, Object?>> rows) => [
  for (final row in rows) _guard(row),
];

/// The first [limit] of [items], so a list inside a response never grows with
/// the cache. The total is sent beside it.
List<T> _head<T>(List<T> items, int limit) =>
    items.length <= limit ? items : items.sublist(0, limit);

/// [json] with the application-owned [fields] bounded: payload values are
/// capped in depth, width, size and string length. Everything else in [json]
/// is an identifier or a counter and is sent as it is.
Map<String, Object?> _payload(Map<String, Object?> json, List<String> fields) =>
    {
      ...json,
      for (final field in fields)
        if (json.containsKey(field)) field: bounded(json[field], _payloadWidth),
    };

/// How many detached ids the host remembers, to say why a call failed.
const _retiredLimit = 32;

/// Owns the extension registrations for the isolate and the attached caches.
final class ForgeDevtoolsHost {
  ForgeDevtoolsHost._(this._registrar, this._poster);

  static ForgeDevtoolsHost? _instance;

  /// The isolate's host, registering through `dart:developer`.
  static ForgeDevtoolsHost get instance => _instance ??= ForgeDevtoolsHost._(
    developer.registerExtension,
    (kind, data) => developer.postEvent(kind, data),
  );

  /// Replaces the host with one that registers and posts through the given
  /// functions. Tests only.
  @visibleForTesting
  static ForgeDevtoolsHost debugOverride({
    required ExtensionRegistrar registrar,
    required EventPoster poster,
  }) {
    _instance?._detachAll();
    return _instance = ForgeDevtoolsHost._(registrar, poster);
  }

  /// Detaches everything and forgets the host. Tests only.
  @visibleForTesting
  static void debugReset() {
    _instance?._detachAll();
    _instance = null;
  }

  /// Caches kept attached at once; attaching another detaches the oldest.
  static const int maxAttached = 8;

  final ExtensionRegistrar _registrar;
  final EventPoster _poster;
  final Map<String, _Attached> _attached = {};
  final Map<String, String> _retired = {};
  bool _registered = false;
  int _nextId = 1;

  /// Attached cache ids, oldest first. Tests only.
  @visibleForTesting
  List<String> get debugCacheIds => [..._attached.keys];

  /// Attached caches, oldest first. Tests only.
  @visibleForTesting
  List<QueryCache> get debugCaches => [
    for (final a in _attached.values) a.devtools.cache,
  ];

  _Attached? _find(QueryCache cache) {
    for (final attached in _attached.values) {
      if (identical(attached.devtools.cache, cache)) return attached;
    }

    return null;
  }

  /// Attaches [cache], or returns the devtools already attached to it after
  /// setting [outbox] on them when one is given and filling the slots that are
  /// still empty. See [registerForgeServiceExtensions].
  dt.Devtools attach(
    QueryCache cache, {
    RestTransport? transport,
    ControlledTransport? controls,
    Map<String, OperationMeta> operations = const {},
    OutboxInspector? outbox,
    Revalidation? revalidation,
  }) {
    if (cache.isDisposed) {
      throw StateError(
        '[forge] cannot attach the devtools to a cache that was disposed',
      );
    }

    _pruneDisposed();

    final existing = _find(cache);

    if (existing != null) {
      // Already attached, usually by configureClient. Nothing registers again;
      // what is empty is filled and what is filled stays as it is.
      _fill(
        existing,
        transport: transport,
        controls: controls,
        operations: operations,
        outbox: outbox,
        revalidation: revalidation,
      );

      return existing.devtools;
    }

    _register();

    while (_attached.length >= maxAttached) {
      detach(_attached.values.first.devtools.cache);
    }

    final requests = transport == null ? null : RequestLog();
    final RequestObserver? observer = requests?.observe;
    if (transport != null) transport.debugObserver = observer;

    final devtools = dt.attach(
      cache,
      requests: requests,
      controls: controls,
      revalidation: revalidation,
      outbox: outbox,
    );
    final attached = _Attached('${_nextId++}', devtools, {...operations})
      ..transport = transport
      ..requestObserver = observer;
    attached.unsubscribe = devtools.subscribe(
      (entry) => _queue(attached, entry),
    );
    // What was queued for the previous principal and not yet posted is that
    // principal's. The marker that follows is queued after the change settles.
    attached.stopChanging = cache.watchPrincipalChanging((_) {
      attached.buffer.clear();
      attached.skipped = 0;
    });
    // `dispose` closes this stream. It closes it late (after the sources stop),
    // so every call also checks `isDisposed`; this just does the detaching.
    attached.disposal = cache.principalChanges.listen(
      null,
      onDone: () => _detachAttached(attached, 'disposed'),
    );
    _attached[attached.id] = attached;

    return devtools;
  }

  void _fill(
    _Attached attached, {
    required RestTransport? transport,
    required ControlledTransport? controls,
    required Map<String, OperationMeta> operations,
    required OutboxInspector? outbox,
    required Revalidation? revalidation,
  }) {
    final devtools = attached.devtools;

    if (outbox != null) devtools.outboxInspector = outbox;

    for (final MapEntry(:key, :value) in operations.entries) {
      attached.operations.putIfAbsent(key, () => value);
    }

    if (transport != null && devtools.requestLog == null) {
      final log = RequestLog();
      final RequestObserver observer = log.observe;

      transport.debugObserver = observer;
      devtools.fillEmptySlots(requestLog: log);
      attached
        ..transport = transport
        ..requestObserver = observer;
    }

    devtools.fillEmptySlots(controls: controls, revalidation: revalidation);
  }

  /// Detaches [cache], giving back its observer and the transport's debug slot.
  void detach(QueryCache cache) {
    final found = _find(cache);

    if (found != null) _detachAttached(found, 'detached');
  }

  void _detachAttached(_Attached found, String reason) {
    if (_attached.remove(found.id) == null) return;

    _retired[found.id] = reason;
    if (_retired.length > _retiredLimit) _retired.remove(_retired.keys.first);

    found.unsubscribe();
    found.stopChanging();
    final disposal = found.disposal;
    if (disposal != null) unawaited(disposal.cancel());
    found.buffer.clear();
    found.skipped = 0;

    final transport = found.transport;
    if (transport != null &&
        identical(transport.debugObserver, found.requestObserver)) {
      transport.debugObserver = null;
    }

    found.devtools.dispose();
  }

  /// Detaches every attached cache that has been disposed.
  void _pruneDisposed() {
    for (final attached in [..._attached.values]) {
      if (attached.devtools.cache.isDisposed) {
        _detachAttached(attached, 'disposed');
      }
    }
  }

  void _detachAll() {
    for (final attached in [..._attached.values]) {
      _detachAttached(attached, 'detached');
    }
  }

  void _register() {
    if (_registered) return;
    _registered = true;

    for (final method in ForgeDevtoolsProtocol.methods) {
      try {
        _registrar(method, handle);
      } on ArgumentError {
        // Already registered in this isolate by an earlier host (a test that
        // reset the host). The earlier registration keeps answering.
      }
    }
  }

  /// Answers one extension call. Public for the registrar and for tests.
  ///
  /// A value the application owns (a record's fields, a frame payload, a query
  /// value, a source's description) is `bounded()` before it is sent. An
  /// identifier is sent whole, because the panel sends it back; a row whose
  /// identifier is over 64 KB is replaced by a marker row. A result that still
  /// encodes to more than 8 MB is refused.
  Future<developer.ServiceExtensionResponse> handle(
    String method,
    Map<String, String> params,
  ) async {
    try {
      final result = await _dispatch(method, params);
      final encoded = jsonEncode({
        'type': '_extensionType',
        'method': method,
        ...result,
      }, toEncodable: (Object? other) => '$other');

      if (encoded.length > _maxResponseBytes) {
        throw StateError(
          '[forge] the answer is larger than ${_maxResponseBytes ~/ (1024 * 1024)} MB; '
          'ask for a smaller page',
        );
      }

      return developer.ServiceExtensionResponse.result(encoded);
    } on _BadParams catch (error) {
      return developer.ServiceExtensionResponse.error(
        developer.ServiceExtensionResponse.invalidParams,
        error.message,
      );
    } on Object catch (error) {
      return developer.ServiceExtensionResponse.error(
        developer.ServiceExtensionResponse.extensionError,
        shortMessage(error is StateError ? error.message : '$error'),
      );
    }
  }

  Future<Map<String, Object?>> _dispatch(
    String method,
    Map<String, String> params,
  ) async {
    if (method == ForgeDevtoolsProtocol.hello) return _hello();

    final attached = _target(params);
    final devtools = attached.devtools;

    switch (method) {
      case ForgeDevtoolsProtocol.snapshot:
        return _snapshot(attached);
      case ForgeDevtoolsProtocol.queries:
        return _queries(devtools, params);
      case ForgeDevtoolsProtocol.query:
        return _query(devtools, params);
      case ForgeDevtoolsProtocol.entities:
        return _entities(devtools, params);
      case ForgeDevtoolsProtocol.entity:
        final key = _required(params, 'key');
        return {
          'entity': _entity(devtools.entity(key)),
          'folded': bounded(devtools.foldedRecord(key), _payloadWidth),
        };
      case ForgeDevtoolsProtocol.tags:
        return _tags(devtools, params);
      case ForgeDevtoolsProtocol.explain:
        return _explain(devtools, params);
      case ForgeDevtoolsProtocol.operations:
        final known = _knownOperations(attached).values.toList();
        return {
          'operations': _guarded([
            for (final meta in _head(known, _maxOperations))
              {
                'id': meta.id,
                'method': meta.method,
                'path': meta.path,
                'provides': meta.provides,
                'invalidates': meta.invalidates,
              },
          ]),
          'total': known.length,
          'truncated': known.length > _maxOperations,
        };
      case ForgeDevtoolsProtocol.wouldInvalidate:
        return _wouldInvalidate(attached, params);
      case ForgeDevtoolsProtocol.log:
        return _log(devtools, params);
      case ForgeDevtoolsProtocol.frames:
        return _frames(devtools, params);
      case ForgeDevtoolsProtocol.capture:
        final enabled = _bool(params, 'enabled');
        final limit = _int(params, 'limit', fallback: 200, min: 1, max: 2000);
        devtools.setCapture(enabled ? dt.FrameOptions(limit: limit) : null);
        return _framesState(devtools);
      case ForgeDevtoolsProtocol.requests:
        return {
          'watching': devtools.watchingRequests,
          'dropped': devtools.requestsDropped,
          'entries': _guarded([
            for (final entry in devtools.requests()) entry.toJson(),
          ]),
        };
      case ForgeDevtoolsProtocol.overlays:
        final layers = devtools.overlays();
        return {
          'overlays': _guarded([
            for (final layer in _head(
              layers,
              ForgeDevtoolsProtocol.maxListInDetail,
            ))
              layer.toJson(),
          ]),
          'total': layers.length,
          'truncated': layers.length > ForgeDevtoolsProtocol.maxListInDetail,
        };
      case ForgeDevtoolsProtocol.action:
        return _action(devtools, params);
      case ForgeDevtoolsProtocol.control:
        return _control(devtools, params);
      case ForgeDevtoolsProtocol.outbox:
        final outbox = await devtools.outbox();
        _alive(attached);
        // `stale: true`, when the devtools say so, passes through unchanged.
        return {
          ...outbox,
          'entries': _guarded((outbox['entries']! as List<Object?>).cast()),
        };
      case ForgeDevtoolsProtocol.outboxAction:
        final id = _required(params, 'id');
        _checkSession(devtools, params);
        switch (_required(params, 'action')) {
          case 'replay':
            await devtools.replayOutbox(id);
          case 'discard':
            await devtools.discardOutbox(id);
          default:
            throw const _BadParams('action must be replay or discard');
        }
        return {'ok': true};
      case ForgeDevtoolsProtocol.sync:
        final sync = await devtools.sync();
        _alive(attached);
        return {
          ...sync,
          'sources': _guarded([
            for (final source in sync['sources']! as List<Object?>)
              _payload(source! as Map<String, Object?>, ['detail']),
          ]),
          'entities': _guarded((sync['entities']! as List<Object?>).cast()),
        };
      default:
        throw _BadParams('unknown method $method');
    }
  }

  Map<String, Object?> _hello() {
    _pruneDisposed();

    return {
      'protocol': ForgeDevtoolsProtocol.version,
      'caches': [
        for (final attached in _attached.values)
          {
            'id': attached.id,
            'principal': attached.devtools.cache.principal,
            'label': 'cache ${attached.id}',
          },
      ],
    };
  }

  /// The cache a call is about. A disposed or detached cache is refused, never
  /// served: not from the store, the log, the frames, the requests or the
  /// outbox.
  _Attached _target(Map<String, String> params) {
    _pruneDisposed();

    final id = params['cache'];

    if (id == null || id.isEmpty) {
      if (_attached.isEmpty) throw StateError('[forge] no cache is attached');
      return _attached.values.last;
    }

    final found = _attached[id];

    if (found != null) return found;

    final reason = _retired[id];

    throw StateError(
      reason == null
          ? '[forge] no attached cache has id $id'
          : '[forge] cache $id was $reason, so the devtools no longer serve it',
    );
  }

  /// Throws when the cache went away while a call was waiting on it.
  void _alive(_Attached attached) {
    if (_attached[attached.id] == attached &&
        !attached.devtools.cache.isDisposed) {
      return;
    }

    _pruneDisposed();

    throw StateError(
      '[forge] cache ${attached.id} was disposed or detached while the call '
      'was running, so nothing is returned',
    );
  }

  Map<String, Object?> _snapshot(_Attached attached) {
    final devtools = attached.devtools;

    return {
      'cache': attached.id,
      'principal': devtools.cache.principal,
      // While the cache changes principal the counters are zero and the answer
      // says so, as the outbox and the sync state do.
      if (!devtools.answering) 'stale': true,
      'store': devtools.store().toJson(),
      'statuses': devtools.statusCounts(),
      'session': devtools.session,
      'capacity': devtools.capacity,
      'dropped': devtools.dropped,
      'sequence': devtools.eventLog.sequence,
      'capturing': devtools.capturing,
      'watchingRequests': devtools.watchingRequests,
      'controls': devtools.controls?.toJson(),
      'revalidation': devtools.revalidation?.toJson(),
      'outboxWired': devtools.outboxInspector != null,
    };
  }

  Map<String, Object?> _queries(
    dt.Devtools devtools,
    Map<String, String> params,
  ) {
    final offset = _int(params, 'offset', fallback: 0);
    final limit = _int(
      params,
      'limit',
      fallback: ForgeDevtoolsProtocol.defaultPage,
      min: 1,
      max: ForgeDevtoolsProtocol.maxPage,
    );
    final filter = params['filter'];
    final matching = [
      for (final row in devtools.querySummaries())
        if (filter == null ||
            filter.isEmpty ||
            (row['key']! as String).contains(filter))
          row,
    ]..sort((a, b) => (a['key']! as String).compareTo(b['key']! as String));

    final page = matching.skip(offset).take(limit).toList();

    return {
      'total': matching.length,
      'offset': offset,
      'truncated': offset + page.length < matching.length,
      'items': _guarded(page),
    };
  }

  Map<String, Object?> _query(
    dt.Devtools devtools,
    Map<String, String> params,
  ) {
    final detail = devtools.detail(_required(params, 'key'));
    if (detail == null) return {'detail': null};

    const cap = ForgeDevtoolsProtocol.maxListInDetail;
    return {
      'detail': _guard({
        ..._payload(detail.toJson(), ['value', 'args']),
        'tags': detail.tags.take(cap).toList(),
        'tagsTotal': detail.tags.length,
        'deps': detail.deps.take(cap).toList(),
        'depsTotal': detail.deps.length,
      }),
    };
  }

  /// One entity's JSON: the fields are the application's, so they are bounded;
  /// the keys it points at and the queries that reach it are capped, with the
  /// totals beside them.
  Map<String, Object?>? _entity(EntitySnapshot? entity) {
    if (entity == null) return null;

    const cap = ForgeDevtoolsProtocol.maxListInDetail;

    return _guard({
      ..._payload(entity.toJson(), ['fields']),
      'refs': entity.refs.take(cap).toList(),
      'refsTotal': entity.refs.length,
      'dependents': entity.dependents.take(cap).toList(),
      'dependentsTotal': entity.dependents.length,
    });
  }

  Map<String, Object?> _entities(
    dt.Devtools devtools,
    Map<String, String> params,
  ) {
    final offset = _int(params, 'offset', fallback: 0);
    final limit = _int(
      params,
      'limit',
      fallback: ForgeDevtoolsProtocol.defaultPage,
      min: 1,
      max: ForgeDevtoolsProtocol.maxPage,
    );
    final type = params['type'];
    final contains = params['filter'];
    final match = EntityFilter(
      type: type == null || type.isEmpty ? null : type,
      contains: contains == null || contains.isEmpty ? null : contains,
    );

    final total = devtools.countEntities(match);
    final page = [
      for (final entity in devtools.entities(
        EntityFilter(
          type: match.type,
          contains: match.contains,
          offset: offset,
          limit: limit,
        ),
      ))
        {
          'key': entity.key,
          'type': entity.type,
          'id': entity.id,
          'version': entity.version,
          'frameAt': entity.frameAt,
          'refCount': entity.refs.length,
        },
    ];

    return {
      'total': total,
      'offset': offset,
      'truncated': offset + page.length < total,
      'items': _guarded(page),
    };
  }

  Map<String, Object?> _tags(dt.Devtools devtools, Map<String, String> params) {
    final offset = _int(params, 'offset', fallback: 0);
    final limit = _int(
      params,
      'limit',
      fallback: ForgeDevtoolsProtocol.defaultPage,
      min: 1,
      max: ForgeDevtoolsProtocol.maxPage,
    );
    final filter = params['filter'];
    final matching = [
      for (final tag in devtools.tags())
        if (filter == null || filter.isEmpty || tag.tag.contains(filter)) tag,
    ];

    final page = matching.skip(offset).take(limit).toList();
    const cap = ForgeDevtoolsProtocol.maxListInDetail;

    return {
      'total': matching.length,
      'offset': offset,
      'truncated': offset + page.length < matching.length,
      'items': _guarded([
        for (final tag in page)
          {
            'tag': tag.tag,
            'carriers': tag.carriers.take(cap).toList(),
            'carriersTotal': tag.carriers.length,
            'mounted': tag.mounted.take(cap).toList(),
            'mountedTotal': tag.mounted.length,
          },
      ]),
    };
  }

  Map<String, Object?> _explain(
    dt.Devtools devtools,
    Map<String, String> params,
  ) {
    final key = _required(params, 'key');
    final rawCause = _json(params, 'cause');
    MissCause? cause;

    if (rawCause != null) {
      if (rawCause is! Map<String, Object?> ||
          rawCause['tags'] is! List<Object?>) {
        throw const _BadParams(
          'cause must be {"tags": [...], "unresolved": [...], "label": "..."}',
        );
      }
      cause = TagsCause(
        [for (final tag in rawCause['tags']! as List<Object?>) '$tag'],
        unresolved: [
          for (final tag
              in (rawCause['unresolved'] as List<Object?>?) ??
                  const <Object?>[])
            '$tag',
        ],
        label: rawCause['label'] as String?,
      );
    }

    final report = switch (params['question'] ?? 'explain') {
      'explain' => devtools.explain(key).toJson(),
      'whyNotRefetched' => devtools.whyNotRefetched(key, cause).toJson(),
      'whyRefetched' => devtools.whyRefetched(key)?.toJson(),
      final other => throw _BadParams(
        'question must be explain, whyNotRefetched or whyRefetched, got $other',
      ),
    };

    return {'report': report};
  }

  Map<String, OperationMeta> _knownOperations(_Attached attached) {
    final known = <String, OperationMeta>{...attached.operations};
    for (final record in DevCache(attached.devtools.cache).trackedRecords()) {
      known.putIfAbsent(record.meta.id, () => record.meta);
    }
    return Map.fromEntries(
      known.entries.toList()..sort((a, b) => a.key.compareTo(b.key)),
    );
  }

  Map<String, Object?> _wouldInvalidate(
    _Attached attached,
    Map<String, String> params,
  ) {
    final id = _required(params, 'operation');
    final meta =
        _knownOperations(attached)[id] ??
        (throw _BadParams('unknown operation $id'));
    final args = _json(params, 'args');

    if (args != null && args is! Map<String, Object?>) {
      throw const _BadParams('args must be a JSON object');
    }

    final map = (args as Map<String, Object?>?) ?? const <String, Object?>{};
    final context = TagContext(
      path: (map['path'] as Map<String, Object?>?) ?? const {},
      query: (map['query'] as Map<String, Object?>?) ?? const {},
      body: map['body'],
    );

    return {
      'preview': attached.devtools
          .wouldInvalidate(meta, context, _json(params, 'response'))
          .toJson(),
    };
  }

  Map<String, Object?> _log(dt.Devtools devtools, Map<String, String> params) {
    final after = _int(params, 'after', fallback: 0);
    final limit = _int(
      params,
      'limit',
      fallback: ForgeDevtoolsProtocol.defaultPage,
      min: 1,
      max: ForgeDevtoolsProtocol.maxPage,
    );
    final newer = [
      for (final entry in devtools.log())
        if (entry.seq > after) entry,
    ];
    final kept = newer.length > limit
        ? newer.sublist(newer.length - limit)
        : newer;

    return {
      'entries': _guarded([for (final entry in kept) entry.toJson()]),
      'dropped': devtools.dropped,
      'sequence': devtools.eventLog.sequence,
      'session': devtools.session,
      'truncated': newer.length > limit,
    };
  }

  Map<String, Object?> _framesState(dt.Devtools devtools) => {
    'capturing': devtools.capturing,
    'capacity': devtools.framesCapacity,
    'dropped': devtools.framesDropped,
  };

  Map<String, Object?> _frames(
    dt.Devtools devtools,
    Map<String, String> params,
  ) {
    final after = _int(params, 'after', fallback: 0);
    final limit = _int(
      params,
      'limit',
      fallback: ForgeDevtoolsProtocol.defaultPage,
      min: 1,
      max: ForgeDevtoolsProtocol.maxPage,
    );
    final newer = [
      for (final frame in devtools.frames())
        if (frame.seq > after) frame,
    ];
    final kept = newer.length > limit
        ? newer.sublist(newer.length - limit)
        : newer;

    return {
      ..._framesState(devtools),
      'entries': _guarded([
        for (final frame in kept) _payload(frame.toJson(), ['payload']),
      ]),
    };
  }

  /// Refuses an action aimed at an earlier session than the cache is on.
  ///
  /// The panel passes the `session` it last read. After a principal change the
  /// cache is on a later one, so a click made against the previous principal's
  /// view is refused instead of landing on the next principal's data.
  void _checkSession(dt.Devtools devtools, Map<String, String> params) {
    final raw = params['session'];

    if (raw == null || raw.isEmpty) return;

    final aimed = int.tryParse(raw);

    if (aimed == null || aimed < 0) {
      throw _BadParams('session must be a non-negative integer, got "$raw"');
    }

    final current = devtools.session;

    if (aimed != current) {
      throw StateError(
        '[forge] this action was aimed at session $aimed, but the cache is on '
        'session $current now (the principal changed). Nothing was changed.',
      );
    }
  }

  Map<String, Object?> _action(
    dt.Devtools devtools,
    Map<String, String> params,
  ) {
    final action = _required(params, 'action');
    final target = params['target'] ?? '';
    final actions = devtools.actions;

    _checkSession(devtools, params);

    switch (action) {
      case 'refetch':
        // Refused here, not inside the unawaited work, so a refusal reaches
        // the panel.
        actions.ensureReady();
        if (!devtools.records().any((record) => record.key == target)) {
          return {'ok': false};
        }
        // Not awaited: the panel must not hang on a slow or offline request.
        unawaited(
          actions.refetch(target).then<void>((_) {}, onError: (Object _) {}),
        );
        return {'ok': true};
      case 'invalidate':
        return {'ok': actions.invalidate(target)};
      case 'invalidateTag':
        actions.invalidateTag(_required(params, 'target'));
        return {'ok': true};
      case 'evict':
        return {'ok': actions.evict(target)};
      case 'drop':
        return {'ok': actions.drop(target)};
      case 'clear':
        actions.clear();
        return {'ok': true};
      case 'rollback':
        return {'ok': actions.rollback(_int(params, 'id', fallback: -1))};
      case 'promote':
        return {'ok': actions.promote(_int(params, 'id', fallback: -1))};
      case 'stale':
        return {'ok': actions.forceStale(target)};
      case 'patch':
        final fields = _json(params, 'fields');
        if (fields is! Map<String, Object?>) {
          throw const _BadParams('fields must be a JSON object');
        }
        return {
          'ok': true,
          'id': actions.patchEntity(_required(params, 'target'), fields),
        };
      default:
        throw _BadParams('unknown action $action');
    }
  }

  static const _controlWrites = [
    'mode',
    'latencyMs',
    'failNext',
    'disarm',
    'toggle',
  ];

  Map<String, Object?> _control(
    dt.Devtools devtools,
    Map<String, String> params,
  ) {
    // Every parameter here but `cache` changes the network the panel imposes,
    // so a session the cache has left is refused as it is for an action.
    if (_controlWrites.any(params.containsKey)) _checkSession(devtools, params);

    final controls = devtools.controls;
    final revalidation = devtools.revalidation;

    if (params['toggle'] case final toggle?) {
      final source = RevalidationSource.values
          .where((s) => s.name == toggle)
          .firstOrNull;
      if (source == null) {
        throw _BadParams(
          'toggle must be one of focus, reconnect, poll, got $toggle',
        );
      }
      revalidation?.toggle(source);
    }

    if (controls == null) {
      return {'wired': false, 'revalidation': revalidation?.toJson()};
    }

    if (params['mode'] case final mode?) {
      controls.mode =
          NetworkMode.values.where((m) => m.name == mode).firstOrNull ??
          (throw _BadParams('mode must be online, slow or offline, got $mode'));
    }
    if (params.containsKey('latencyMs')) {
      controls.latency = Duration(
        milliseconds: _int(params, 'latencyMs', fallback: 0, max: 60000),
      );
    }
    if (params.containsKey('failNext')) {
      controls.failNext(
        _int(params, 'failNext', fallback: 500, min: 100, max: 599),
      );
    }
    if (params['disarm'] == 'true') controls.disarm();

    return {
      'wired': true,
      ...controls.toJson(),
      'revalidation': revalidation?.toJson(),
    };
  }

  void _queue(_Attached attached, LogEntry entry) {
    attached.buffer.add(entry.toJson());
    if (attached.buffer.length > ForgeDevtoolsProtocol.maxEventsPerPost) {
      attached.buffer.removeAt(0);
      attached.skipped++;
    }
    if (attached.scheduled) return;
    attached.scheduled = true;
    Timer.run(() => _flush(attached));
  }

  void _flush(_Attached attached) {
    attached.scheduled = false;

    // A disposed cache posts nothing, even before the host has heard.
    _pruneDisposed();

    if (attached.buffer.isEmpty || _attached[attached.id] != attached) return;

    final entries = [...attached.buffer];
    final skipped = attached.skipped;
    attached.buffer.clear();
    attached.skipped = 0;

    _poster(ForgeDevtoolsProtocol.eventKind, {
      'cache': attached.id,
      'entries': _guarded(entries),
      'skipped': skipped,
    });
  }
}

String _required(Map<String, String> params, String name) {
  final value = params[name];
  if (value == null || value.isEmpty) throw _BadParams('$name is required');
  return value;
}

int _int(
  Map<String, String> params,
  String name, {
  required int fallback,
  int min = 0,
  int? max,
}) {
  final raw = params[name];
  if (raw == null || raw.isEmpty) return fallback;
  final value = int.tryParse(raw);
  if (value == null || value < min) {
    throw _BadParams('$name must be an integer of at least $min, got "$raw"');
  }
  return max != null && value > max ? max : value;
}

bool _bool(Map<String, String> params, String name) => switch (params[name]) {
  'true' => true,
  'false' => false,
  final other => throw _BadParams('$name must be true or false, got "$other"'),
};

Object? _json(Map<String, String> params, String name) {
  final raw = params[name];
  if (raw == null || raw.isEmpty) return null;
  try {
    return jsonDecode(raw);
  } on FormatException {
    throw _BadParams('$name is not valid JSON');
  }
}
