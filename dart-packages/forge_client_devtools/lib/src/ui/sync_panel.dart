import 'dart:async';

import 'package:flutter/material.dart';
import 'package:forge_client/devtools_protocol.dart';

import '../backend/backend.dart';
import '../state/connection.dart';
import 'widgets.dart';

/// Sync sources, what they describe of themselves, and each entity's status.
///
/// A source describes itself in whatever shape it likes, so the panel shows
/// the description as a folding tree. A description that has the shape the
/// Grove source gives (a replica clock, a node id, peers) also gets a line
/// for each, so the useful numbers do not hide inside folds. Nothing crosses
/// principals: the panel drops what it holds when the principal or the isolate
/// changes.
class SyncPanel extends StatefulWidget {
  /// Creates the panel.
  const SyncPanel({super.key, required this.connection});

  /// The app connection.
  final ForgeConnection connection;

  @override
  State<SyncPanel> createState() => _SyncPanelState();
}

class _SyncPanelState extends State<SyncPanel>
    with ActivityRefresh<SyncPanel>, GenerationFence<SyncPanel> {
  Json? _state;
  String? _error;

  /// Bumped by [forget], so a read that was on its way is not kept.
  int _epoch = 0;

  @override
  ForgeConnection get connection => widget.connection;

  @override
  void initState() {
    super.initState();
    unawaited(refresh());
  }

  @override
  void forget() {
    _epoch++;
    _state = null;
    _error = null;
    unawaited(refresh());
  }

  @override
  Future<void> refresh() async {
    final epoch = _epoch;
    try {
      final state = await connection.call(ForgeDevtoolsProtocol.sync);
      if (mounted && epoch == _epoch) {
        setState(() {
          _state = state;
          _error = null;
        });
      }
    } on BackendError catch (failure) {
      if (mounted && epoch == _epoch) setState(() => _error = failure.message);
    }
  }

  @override
  Widget build(BuildContext context) {
    final state = _state;
    if (state == null) return Center(child: Text(_error ?? 'Loading...'));

    // The app is changing account: its answer is empty on purpose, so it must
    // not read as a cache with no sync sources.
    if (state.flag('stale')) {
      return const Center(
        key: ValueKey('sync-switching'),
        child: Text('switching account'),
      );
    }

    final sources = state.objs('sources');
    final entities = state.objs('entities');

    if (sources.isEmpty && entities.isEmpty) {
      return const Center(
        child: Text(
          'No sync sources on this cache. Plain REST entities use the outbox '
          'instead.',
        ),
      );
    }

    return ListView(
      padding: const EdgeInsets.all(12),
      children: [
        const SectionTitle('sources'),
        for (final source in sources) _source(context, source),
        const SectionTitle('status per entity'),
        if (entities.isEmpty) const Text('No status reported yet.'),
        for (final entity in entities) _entity(entity),
      ],
    );
  }

  Widget _source(BuildContext context, Json source) {
    final detail = source.objOrNull('detail');
    final type = source.str('type');

    return Padding(
      key: ValueKey('sync-source-$type'),
      padding: const EdgeInsets.only(bottom: 8),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(type, style: Theme.of(context).textTheme.titleSmall),
          KeyValue('entities', source.strings('entities').join(', ')),
          if (source['detail'] == null)
            const Text(
              'This source does not describe itself. Implement '
              'DevtoolsInspectable to show its replica clock and peers.',
            )
          else if (detail == null)
            JsonView(source['detail'], label: 'detail', initiallyExpanded: true)
          else ...[
            ..._summary(detail),
            JsonView(detail, label: 'detail', initiallyExpanded: true),
          ],
        ],
      ),
    );
  }

  /// A line for each of the fields a replica describes itself with. A field
  /// that is missing, or is not the shape expected, gets no line: the tree
  /// below shows it as it came.
  List<Widget> _summary(Json detail) {
    final hlc = detail.objOrNull('hlc');
    final peers = detail['peers'];

    return [
      if (detail['protocol'] case final String protocol)
        KeyValue('protocol', protocol),
      if (detail.containsKey('nodeId'))
        // A source that is not running has no replica yet.
        KeyValue('node', detail.strOrNull('nodeId') ?? 'not running'),
      if (hlc != null)
        KeyValue(
          'hlc',
          '${hlc.str('ts')}:${hlc.integer('counter')}:${hlc.str('nodeId')}',
        ),
      if (peers is List<Object?>)
        KeyValue(
          'peers',
          peers.isEmpty ? '0' : '${peers.length} (${peers.join(', ')})',
        ),
    ];
  }

  Widget _entity(Json entity) {
    final status = entity.str('status');
    final pending = entity.integer('pending');
    final error = entity.strOrNull('error');

    return ListTile(
      key: ValueKey('sync-entity-${entity.str('entity')}'),
      dense: true,
      title: Text(entity.str('entity')),
      subtitle: Text(
        '$status'
        '${status == 'pending' ? ', $pending ${pending == 1 ? 'change' : 'changes'}' : ''}'
        '${error == null ? '' : ': $error'}',
      ),
    );
  }
}
