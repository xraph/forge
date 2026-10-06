import 'dart:async';

import 'package:flutter/material.dart';
import 'package:forge_client/devtools_protocol.dart';

import '../backend/backend.dart';
import '../state/connection.dart';
import 'widgets.dart';

/// Every remembered query on the left, the picked one in detail on the right.
class QueriesPanel extends StatefulWidget {
  /// Creates the panel.
  const QueriesPanel({super.key, required this.connection});

  /// The app connection.
  final ForgeConnection connection;

  @override
  State<QueriesPanel> createState() => _QueriesPanelState();
}

class _QueriesPanelState extends State<QueriesPanel>
    with ActivityRefresh<QueriesPanel> {
  String _filter = '';
  int _generation = 0;
  String? _selected;

  @override
  ForgeConnection get connection => widget.connection;

  @override
  Future<void> refresh() async {
    if (mounted) setState(() => _generation++);
  }

  Future<({int total, List<Json> items})> _fetch(int offset, int limit) async {
    final page = await connection.call(ForgeDevtoolsProtocol.queries, {
      'offset': '$offset',
      'limit': '$limit',
      if (_filter.isNotEmpty) 'filter': _filter,
    });
    return (total: page.integer('total'), items: page.objs('items'));
  }

  @override
  Widget build(BuildContext context) => Row(
    crossAxisAlignment: CrossAxisAlignment.stretch,
    children: [
      Expanded(
        flex: 2,
        child: Column(
          children: [
            Padding(
              padding: const EdgeInsets.all(8),
              child: TextField(
                key: const ValueKey('queries-filter'),
                decoration: const InputDecoration(
                  hintText: 'Filter by key, then Enter',
                ),
                onSubmitted: (value) => setState(() => _filter = value),
              ),
            ),
            Expanded(
              child: PagedList(
                reloadToken: _filter,
                refreshToken: _generation,
                totalLabel: (total) => '$total queries',
                fetch: _fetch,
                itemBuilder: (context, row) {
                  final key = row.str('key');
                  return ListTile(
                    key: ValueKey('query-$key'),
                    dense: true,
                    selected: key == _selected,
                    title: Text(key),
                    subtitle: Text(
                      '${row.str('status')}  mounts ${row.integer('mounts')}  '
                      'tags ${row.integer('tagCount')}  deps ${row.integer('depCount')}'
                      '${row.flag('stale') ? '  stale' : ''}${row.flag('fetching') ? '  fetching' : ''}',
                    ),
                    onTap: () => setState(() => _selected = key),
                  );
                },
              ),
            ),
          ],
        ),
      ),
      const VerticalDivider(width: 1),
      Expanded(
        flex: 3,
        child: _selected == null
            ? const Center(child: Text('Pick a query.'))
            : _QueryDetail(
                key: ValueKey(_selected),
                connection: connection,
                queryKey: _selected!,
              ),
      ),
    ],
  );
}

class _QueryDetail extends StatefulWidget {
  const _QueryDetail({
    super.key,
    required this.connection,
    required this.queryKey,
  });

  final ForgeConnection connection;
  final String queryKey;

  @override
  State<_QueryDetail> createState() => _QueryDetailState();
}

class _QueryDetailState extends State<_QueryDetail>
    with ActivityRefresh<_QueryDetail> {
  Json? _detail;
  String? _error;
  bool _loaded = false;

  static const _actions = {
    'refetch': 'Refetch',
    'invalidate': 'Invalidate',
    'stale': 'Mark stale',
    'drop': 'Drop',
  };

  @override
  ForgeConnection get connection => widget.connection;

  @override
  void initState() {
    super.initState();
    unawaited(refresh());
  }

  @override
  Future<void> refresh() async {
    try {
      final result = await connection.call(ForgeDevtoolsProtocol.query, {
        'key': widget.queryKey,
      });
      if (!mounted) return;
      setState(() {
        _detail = result.objOrNull('detail');
        _error = null;
        _loaded = true;
      });
    } on BackendError catch (failure) {
      if (mounted) setState(() => _error = failure.message);
    }
  }

  Future<void> _act(String action) async {
    try {
      final result = await connection.call(ForgeDevtoolsProtocol.action, {
        'action': action,
        'target': widget.queryKey,
      });
      if (!mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(
          content: Text(
            result.flag('ok')
                ? '$action sent'
                : '$action refused: nothing tracks this query',
          ),
        ),
      );
    } on BackendError catch (failure) {
      // The app's refusal is shown as it was said: a stale session, a
      // sync-owned entity, a disposed cache.
      if (mounted) {
        ScaffoldMessenger.of(context)
            .showSnackBar(SnackBar(content: Text(failure.message)));
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    if (_error != null) return Center(child: Text(_error!));
    if (!_loaded) return const Center(child: Text('Loading...'));

    final detail = _detail;
    if (detail == null) {
      return const Center(child: Text('This query is no longer tracked.'));
    }
    if (detail.flag('oversized')) {
      return Center(child: OversizedRow(detail));
    }

    final tags = detail.strings('tags');
    final deps = detail.strings('deps');

    return ListView(
      padding: const EdgeInsets.all(12),
      children: [
        Text(widget.queryKey, style: Theme.of(context).textTheme.titleMedium),
        Wrap(
          spacing: 8,
          children: [
            for (final MapEntry(key: action, value: label) in _actions.entries)
              OutlinedButton(
                key: ValueKey('query-action-$action'),
                onPressed: () => unawaited(_act(action)),
                child: Text(label),
              ),
          ],
        ),
        KeyValue('status', detail.str('status')),
        if (detail.strOrNull('error') case final error?)
          KeyValue('error', error),
        KeyValue('mounts', '${detail.integer('mounts')}'),
        KeyValue('stale', '${detail.flag('stale')}'),
        KeyValue('fetching', '${detail.flag('fetching')}'),
        KeyValue('settled at', '${detail.integer('settledAt')}'),
        KeyValue('frame restarts', '${detail.integer('frameRestarts')}'),
        const SectionTitle('provides'),
        Text(detail.strings('provides').join(', ')),
        SectionTitle('tags (${detail.integer('tagsTotal')})'),
        Text(tags.join(', ')),
        if (detail.integer('tagsTotal') > tags.length)
          Text('showing the first ${tags.length}'),
        SectionTitle('deps (${detail.integer('depsTotal')})'),
        Text(deps.join(', ')),
        if (detail.integer('depsTotal') > deps.length)
          Text('showing the first ${deps.length}'),
        const SectionTitle('last settled value'),
        JsonView(detail['value']),
      ],
    );
  }
}
