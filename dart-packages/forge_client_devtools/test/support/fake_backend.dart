import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:forge_client/devtools_protocol.dart';
import 'package:forge_client_devtools/forge_client_devtools.dart';

typedef Call = ({String method, Map<String, String> params});

/// A backend that answers every ext.forge method from in-memory state, the
/// way the service host in forge_client does, without a VM.
final class FakeForgeBackend implements ForgeBackend {
  FakeForgeBackend({
    bool available = true,
    this.entityCount = 3,
    this.protocol = ForgeDevtoolsProtocol.version,
  }) : _available = ValueNotifier<bool>(available);

  final ValueNotifier<bool> _available;
  final StreamController<Json> _events = StreamController<Json>.broadcast();

  int entityCount;
  int protocol;

  /// The cache's identity session, as `ext.forge.snapshot` and `ext.forge.log`
  /// report it. Calls that change something and carry an older one are
  /// refused, the way the real host refuses them.
  int session = 0;

  /// The cache's principal.
  String? principal;

  /// Whether the snapshot answers as it does while the cache changes account:
  /// zero counters and `stale: true`.
  bool switching = false;
  List<Json> caches = [
    {'id': '1', 'principal': null, 'label': 'cache 1'},
  ];
  String mode = 'online';
  int latencyMs = 0;
  int? armedStatus;
  bool controlsWired = true;
  bool capturing = false;
  int frameCapacity = 0;
  int framesDropped = 0;
  List<Json> frames = [];
  bool watchingRequests = true;
  bool outboxWired = true;
  String outboxSource = 'session';
  final List<String> actions = [];
  final List<Call> calls = [];
  final Map<String, FutureOr<Json> Function(Map<String, String> params)>
  overrides = {};

  @override
  ValueListenable<bool> get available => _available;

  set isAvailable(bool value) => _available.value = value;

  @override
  Stream<Json> get events => _events.stream;

  /// Delivers one forge:event payload.
  void emit(Json event) => _events.add(event);

  /// The params of every call to [method], in order.
  List<Map<String, String>> callsTo(String method) => [
    for (final c in calls)
      if (c.method == method) c.params,
  ];

  static const _queryRows = <Json>[
    {
      'key': 'GET /orders',
      'operation': 'GET /orders',
      'mounts': 1,
      'stale': false,
      'settled': true,
      'settledAt': 3,
      'status': 'success',
      'fetching': false,
      'tagCount': 3,
      'depCount': 2,
    },
    {
      'key': 'GET /orders/{id}|{"path":{"id":1}}',
      'operation': 'GET /orders/{id}',
      'mounts': 0,
      'stale': true,
      'settled': true,
      'settledAt': 2,
      'status': 'idle',
      'fetching': false,
      'tagCount': 1,
      'depCount': 1,
    },
  ];

  static const _tagRows = <Json>[
    {
      'tag': 'Order:1',
      'carriers': ['GET /orders'],
      'carriersTotal': 1,
      'mounted': <String>[],
      'mountedTotal': 0,
    },
    {
      'tag': 'Order[]',
      'carriers': ['GET /orders'],
      'carriersTotal': 1,
      'mounted': ['GET /orders'],
      'mountedTotal': 1,
    },
  ];

  int _int(Map<String, String> params, String name, int fallback) =>
      int.tryParse(params[name] ?? '') ?? fallback;

  /// Refuses a change aimed at a session the cache has left, with the real
  /// host's wording.
  void _checkSession(String method, Map<String, String> params) {
    final raw = params['session'];
    if (raw == null || raw.isEmpty) return;
    final aimed = int.parse(raw);
    if (aimed != session) {
      throw BackendError(
        method,
        '[forge] this action was aimed at session $aimed, but the cache is on '
        'session $session now (the principal changed). Nothing was changed.',
      );
    }
  }

  Json _page(List<Json> rows, Map<String, String> params) {
    final offset = _int(params, 'offset', 0);
    final limit = _int(
      params,
      'limit',
      ForgeDevtoolsProtocol.defaultPage,
    ).clamp(1, ForgeDevtoolsProtocol.maxPage);
    final items = rows.skip(offset).take(limit).toList();
    return {
      'total': rows.length,
      'offset': offset,
      'truncated': offset + items.length < rows.length,
      'items': items,
    };
  }

  Json _entities(Map<String, String> params) {
    final offset = _int(params, 'offset', 0);
    final limit = _int(
      params,
      'limit',
      ForgeDevtoolsProtocol.defaultPage,
    ).clamp(1, ForgeDevtoolsProtocol.maxPage);
    final type = params['type'];
    final total = type == null || type.isEmpty || type == 'Order'
        ? entityCount
        : 0;
    final end = (offset + limit).clamp(0, total);
    return {
      'total': total,
      'offset': offset,
      'truncated': end < total,
      'items': [
        for (var i = offset; i < end; i++)
          {
            'key': 'Order:$i',
            'type': 'Order',
            'id': '$i',
            'version': 1,
            'frameAt': 0,
            'refCount': 1,
          },
      ],
    };
  }

  Json _control(Map<String, String> params) {
    if (params.keys.any(
      const {'mode', 'latencyMs', 'failNext', 'disarm', 'toggle'}.contains,
    )) {
      _checkSession(ForgeDevtoolsProtocol.control, params);
    }
    if (!controlsWired) return {'wired': false, 'revalidation': null};
    if (params['mode'] case final value?) mode = value;
    if (params['latencyMs'] case final value?) latencyMs = int.parse(value);
    if (params['failNext'] case final value?) armedStatus = int.parse(value);
    if (params['disarm'] == 'true') armedStatus = null;
    return {
      'wired': true,
      'mode': mode,
      'latencyMs': latencyMs,
      'slowMs': 400,
      'armed': armedStatus != null,
      'armedStatus': armedStatus,
      'revalidation': null,
    };
  }

  Json _frames() => {
    'capturing': capturing,
    'capacity': frameCapacity,
    'dropped': framesDropped,
    'entries': frames,
  };

  @override
  Future<Json> call(
    String method, [
    Map<String, String> params = const {},
  ]) async {
    calls.add((method: method, params: params));
    await Future<void>.value();

    final override = overrides[method];
    if (override != null) return override(params);

    return switch (method) {
      ForgeDevtoolsProtocol.hello => {'protocol': protocol, 'caches': caches},
      ForgeDevtoolsProtocol.snapshot => {
        'cache': params['cache'] ?? '1',
        'principal': principal,
        if (switching) 'stale': true,
        'store': switching
            ? {
                'records': 0,
                'version': 0,
                'tracked': 0,
                'remembered': 0,
                'mounted': 0,
              }
            : {
                'records': entityCount,
                'version': 3,
                'tracked': 2,
                'remembered': 2,
                'mounted': 1,
              },
        'statuses': switching
            ? {
                'idle': 0,
                'pending': 0,
                'success': 0,
                'error': 0,
                'fetching': 0,
                'stale': 0,
                'unmounted': 0,
              }
            : {
                'idle': 1,
                'pending': 0,
                'success': 1,
                'error': 0,
                'fetching': 0,
                'stale': 1,
                'unmounted': 1,
              },
        'session': session,
        'capacity': 500,
        'dropped': 0,
        'sequence': 3,
        'capturing': capturing,
        'watchingRequests': watchingRequests,
        'controls': controlsWired ? _control(const {}) : null,
        'revalidation': null,
        'outboxWired': outboxWired,
      },
      ForgeDevtoolsProtocol.queries => _page([
        for (final row in _queryRows)
          if ((row['key']! as String).contains(params['filter'] ?? '')) row,
      ], params),
      ForgeDevtoolsProtocol.query => {
        'detail': params['key'] == 'GET /orders'
            ? {
                'key': 'GET /orders',
                'operation': 'GET /orders',
                'status': 'success',
                'error': null,
                'mounts': 1,
                'stale': false,
                'settled': true,
                'settledAt': 3,
                'fetching': false,
                'inflight': false,
                'restart': false,
                'frameRestarts': 0,
                'args': <String, Object?>{},
                'provides': ['Order[]'],
                'tags': ['Order:1', 'Order[]'],
                'tagsTotal': 2,
                'deps': ['Order:1'],
                'depsTotal': 1,
                'value': [
                  {'id': 1, 'total': 10},
                ],
              }
            : null,
      },
      ForgeDevtoolsProtocol.entities => _entities(params),
      ForgeDevtoolsProtocol.entity => {
        'entity': {
          'key': params['key'],
          'type': 'Order',
          'id': (params['key'] ?? '').split(':').last,
          'version': 1,
          'frameAt': 0,
          'fields': {
            'id': 1,
            'total': 10,
            'customer': {'__ref': 'Customer:c1'},
          },
          'refs': ['Customer:c1'],
          'refsTotal': 1,
          'dependents': ['GET /orders'],
          'dependentsTotal': 1,
        },
        'folded': {'id': 1, 'total': 12},
      },
      ForgeDevtoolsProtocol.tags => _page(_tagRows, params),
      ForgeDevtoolsProtocol.explain => {
        'report': {
          'kind': 'miss',
          'query': params['key'],
          'outcome': 'missed',
          'reason': '`GET /orders` did not refetch because none of the 1 tag(s) mutation POST /orders raised are tags it carries. The two sets are disjoint.',
          'cause': {
            'label': 'mutation POST /orders',
            'seq': 4,
            'tags': ['Order:9'],
            'unresolved': <String>[],
          },
          'mounts': 1,
          'settled': true,
          'invalidated': ['Order:9'],
          'carried': ['Order:1', 'Order[]'],
          'matched': <String>[],
          'nearest': [
            {
              'invalidated': 'Order:9',
              'carried': 'Order[]',
              'relation': 'instance-vs-collection',
              'hint': "Add `Order[]` to the operation's Invalidates.",
            },
          ],
          'suggestions': ["Add `Order[]` to the operation's Invalidates."],
        },
      },
      ForgeDevtoolsProtocol.operations => {
        'operations': [
          {
            'id': 'op_order_create',
            'method': 'POST',
            'path': '/orders',
            'provides': <String>[],
            'invalidates': ['Order:{res.id}'],
          },
        ],
        'total': 1,
        'truncated': false,
      },
      ForgeDevtoolsProtocol.wouldInvalidate => {
        'preview': {
          'operation': 'POST /orders',
          'templates': ['Order:{res.id}'],
          'tags': ['Order:9'],
          'unresolved': <String>[],
          'hits': [
            {'tag': 'Order:9', 'queries': <String>[]},
          ],
          'missed': ['Order:9'],
        },
      },
      ForgeDevtoolsProtocol.log => {
        'entries': [
          {
            'kind': 'fetch',
            'seq': 1,
            'at': 1,
            'session': 0,
            'query': 'GET /orders',
            'reason': 'mount',
            'cause': null,
          },
          {
            'kind': 'settle',
            'seq': 2,
            'at': 2,
            'session': 0,
            'query': 'GET /orders',
            'version': 3,
          },
        ],
        'dropped': 0,
        'sequence': 3,
        'session': session,
        'truncated': false,
      },
      ForgeDevtoolsProtocol.frames => _frames(),
      ForgeDevtoolsProtocol.capture => () {
        capturing = params['enabled'] == 'true';
        frameCapacity = capturing ? _int(params, 'limit', 200) : 0;
        return {
          'capturing': capturing,
          'capacity': frameCapacity,
          'dropped': framesDropped,
        };
      }(),
      ForgeDevtoolsProtocol.requests => {
        'watching': watchingRequests,
        'dropped': 0,
        'entries': watchingRequests
            ? [
                {
                  'id': 1,
                  'operation': 'GET /orders',
                  'method': 'GET',
                  'args': '',
                  'at': 1,
                  'duration': 12,
                  'attempts': 2,
                  'limit': 3,
                  'status': null,
                  'outcome': 'ok',
                  'retries': [
                    {'attempt': 0, 'delayMs': 200, 'status': 503},
                  ],
                  'refreshes': 0,
                  'joined': false,
                  'authMs': 0,
                  'marker': false,
                },
              ]
            : <Json>[],
      },
      ForgeDevtoolsProtocol.overlays => {
        'overlays': <Json>[],
        'total': 0,
        'truncated': false,
      },
      ForgeDevtoolsProtocol.action => () {
        _checkSession(method, params);
        actions.add(
          '${params['action']} ${params['target'] ?? params['id'] ?? ''}'
              .trim(),
        );
        return <String, Object?>{'ok': true};
      }(),
      ForgeDevtoolsProtocol.control => _control(params),
      ForgeDevtoolsProtocol.outbox => {
        'wired': outboxWired,
        'source': outboxSource,
        'entries': [
          {
            'id': 'm1',
            'operation': 'op_order_create',
            'createdAt': 0,
            'state': 'queued',
            'failure': null,
            'since': null,
            'at': null,
          },
          // Failure text is body-free: kind and status, or an uncertain
          // write's reason. Never a response body.
          {
            'id': 'm2',
            'operation': 'op_order_create',
            'createdAt': 0,
            'state': 'failed',
            'failure': 'conflict 409',
            'since': null,
            'at': null,
          },
          {
            'id': 'm3',
            'operation': 'op_order_create',
            'createdAt': 0,
            'state': 'sending',
            'failure': null,
            'since': 5,
            'at': null,
          },
        ],
        'total': 3,
        'truncated': false,
      },
      ForgeDevtoolsProtocol.outboxAction => () {
        _checkSession(method, params);
        actions.add('${params['action']} ${params['id']}');
        return <String, Object?>{'ok': true};
      }(),
      ForgeDevtoolsProtocol.sync => {
        'sources': [
          {
            'type': 'GroveSyncSource',
            'entities': ['Doc'],
            // The shape GroveSyncSource.describeForDevtools returns.
            'detail': {
              'protocol': 'grove-crdt',
              'nodeId': 'replica-a',
              'hlc': {
                'ts': '1700000000000',
                'counter': 42,
                'nodeId': 'replica-a',
              },
              'entities': {
                'Doc': {
                  'table': 'docs',
                  'pending': 3,
                  'status': 'pending',
                  'lastPull': null,
                  'datasets': <Object?>[],
                },
              },
              'peers': ['replica-b', 'replica-c'],
            },
          },
        ],
        'entities': [
          {
            'entity': 'Doc',
            'status': 'pending',
            'pending': 3,
            'error': null,
            'at': 5,
          },
        ],
      },
      _ => throw BackendError(method, 'the fake has no answer for $method'),
    };
  }
}
