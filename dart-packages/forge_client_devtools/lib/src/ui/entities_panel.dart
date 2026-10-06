import 'dart:async';

import 'package:flutter/material.dart';
import 'package:forge_client/devtools_protocol.dart';

import '../backend/backend.dart';
import '../state/connection.dart';
import 'widgets.dart';

/// The entity store, paged, with one entity in detail.
///
/// The table does not reload on activity: a store of ten thousand records is
/// read a page at a time, and reloading would throw away the reader's scroll
/// position on every frame batch. The refresh button re-reads the pages on
/// screen in place. The detail pane follows activity, so the open entity is
/// current.
///
/// Every identifier is sent back as the app gave it: a row is selected by its
/// key, verbatim, and that key is what `ext.forge.entity` and the evict
/// action receive.
class EntitiesPanel extends StatefulWidget {
  /// Creates the panel.
  const EntitiesPanel({super.key, required this.connection});

  /// The app connection.
  final ForgeConnection connection;

  @override
  State<EntitiesPanel> createState() => _EntitiesPanelState();
}

class _EntitiesPanelState extends State<EntitiesPanel> {
  /// Height of one row: a dense two-line tile.
  static const _rowExtent = 64.0;

  String _type = '';
  String _filter = '';
  int _refresh = 0;
  String? _selected;
  bool _switching = false;

  Future<({int total, List<Json> items})> _fetch(int offset, int limit) async {
    final page = await widget.connection.call(ForgeDevtoolsProtocol.entities, {
      'offset': '$offset',
      'limit': '$limit',
      if (_type.isNotEmpty) 'type': _type,
      if (_filter.isNotEmpty) 'filter': _filter,
    });
    final switching = page.flag('stale');
    if (mounted && switching != _switching) {
      setState(() => _switching = switching);
    }
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
              child: Row(
                children: [
                  Expanded(
                    child: TextField(
                      key: const ValueKey('entities-type'),
                      decoration: const InputDecoration(
                        hintText: 'Type, e.g. Order',
                      ),
                      onSubmitted: (value) =>
                          setState(() => _type = value.trim()),
                    ),
                  ),
                  const SizedBox(width: 8),
                  Expanded(
                    child: TextField(
                      key: const ValueKey('entities-filter'),
                      decoration: const InputDecoration(
                        hintText: 'Key contains',
                      ),
                      onSubmitted: (value) =>
                          setState(() => _filter = value.trim()),
                    ),
                  ),
                  IconButton(
                    key: const ValueKey('entities-refresh'),
                    tooltip: 'Reload',
                    icon: const Icon(Icons.refresh),
                    onPressed: () => setState(() => _refresh++),
                  ),
                ],
              ),
            ),
            if (_switching)
              const Padding(
                padding: EdgeInsets.symmetric(horizontal: 8),
                child: Chip(
                  key: ValueKey('entities-switching'),
                  label: Text('switching account'),
                ),
              ),
            Expanded(
              child: PagedList(
                key: const ValueKey('entities-list'),
                reloadToken: (_type, _filter),
                refreshToken: _refresh,
                itemExtent: _rowExtent,
                totalLabel: (total) => '$total entities',
                emptyText: 'No entities match.',
                fetch: _fetch,
                itemBuilder: (context, row) {
                  final key = row.str('key');
                  return ListTile(
                    key: ValueKey('entity-$key'),
                    dense: true,
                    selected: key == _selected,
                    title: Text(
                      key,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                    ),
                    subtitle: Text(
                      'v${row.integer('version')}  frame ${row.integer('frameAt')}  refs ${row.integer('refCount')}',
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
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
            ? const Center(child: Text('Pick an entity.'))
            : _EntityDetail(
                key: ValueKey(_selected),
                connection: widget.connection,
                entityKey: _selected!,
                onChanged: () => setState(() => _refresh++),
              ),
      ),
    ],
  );
}

class _EntityDetail extends StatefulWidget {
  const _EntityDetail({
    super.key,
    required this.connection,
    required this.entityKey,
    required this.onChanged,
  });

  final ForgeConnection connection;
  final String entityKey;

  /// Called after the store changed, so the table can re-read.
  final VoidCallback onChanged;

  @override
  State<_EntityDetail> createState() => _EntityDetailState();
}

class _EntityDetailState extends State<_EntityDetail>
    with ActivityRefresh<_EntityDetail> {
  Json? _result;
  String? _error;

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
      final result = await connection.call(ForgeDevtoolsProtocol.entity, {
        'key': widget.entityKey,
      });
      if (!mounted) return;
      setState(() {
        _result = result;
        _error = null;
      });
    } on BackendError catch (failure) {
      if (mounted) setState(() => _error = failure.message);
    }
  }

  Future<void> _evict() async {
    try {
      final result = await connection.call(ForgeDevtoolsProtocol.action, {
        'action': 'evict',
        'target': widget.entityKey,
      });
      if (!mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(
          content: Text(
            result.flag('ok')
                ? 'Evicted'
                : 'The store no longer holds this entity',
          ),
        ),
      );
      widget.onChanged();
      unawaited(refresh());
    } on BackendError catch (failure) {
      // The app's refusal is shown as it was said: a sync-owned entity, a
      // session the cache has left, a disposed cache.
      if (mounted) {
        ScaffoldMessenger.of(context)
            .showSnackBar(SnackBar(content: Text(failure.message)));
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    if (_error != null) return Center(child: Text(_error!));

    final result = _result;
    if (result == null) return const Center(child: Text('Loading...'));
    if (result.flag('stale')) {
      return const Center(child: Text('switching account'));
    }

    final entity = result.objOrNull('entity');
    if (entity == null) {
      return const Center(
        child: Text('The store no longer holds this entity.'),
      );
    }
    if (entity.flag('oversized')) return Center(child: OversizedRow(entity));

    final refs = entity.strings('refs');
    final dependents = entity.strings('dependents');

    return ListView(
      padding: const EdgeInsets.all(12),
      children: [
        Text(widget.entityKey, style: Theme.of(context).textTheme.titleMedium),
        Align(
          alignment: Alignment.centerLeft,
          child: OutlinedButton(
            key: const ValueKey('entity-evict'),
            onPressed: () => unawaited(_evict()),
            child: const Text('Evict'),
          ),
        ),
        KeyValue('version', '${entity.integer('version')}'),
        KeyValue('frame clock', '${entity.integer('frameAt')}'),
        const SectionTitle('fields, as the store holds them'),
        JsonView(entity['fields'], label: 'fields', initiallyExpanded: true),
        const SectionTitle('with pending optimistic writes folded in'),
        JsonView(result['folded'], label: 'folded', initiallyExpanded: true),
        SectionTitle('references (${entity.integer('refsTotal')})'),
        Text(refs.join(', ')),
        if (entity.integer('refsTotal') > refs.length)
          Text('showing the first ${refs.length}'),
        SectionTitle(
          'queries that reached it (${entity.integer('dependentsTotal')})',
        ),
        Text(dependents.join(', ')),
        if (entity.integer('dependentsTotal') > dependents.length)
          Text('showing the first ${dependents.length}'),
      ],
    );
  }
}
