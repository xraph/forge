import 'dart:async';

import 'package:flutter/material.dart';
import 'package:forge_client/devtools_protocol.dart';

import '../backend/backend.dart';
import '../state/connection.dart';
import 'widgets.dart';

/// One log entry as a line of text.
String describeLogEntry(Json entry) {
  String list(String key) => entry.strings(key).join(', ');
  final cause = entry['cause'] == null
      ? ''
      : ' (cause #${entry.integer('cause')})';

  return switch (entry.str('kind')) {
    'mutation' =>
      '${entry.str('operation')} raised ${list('tags')}'
          '${entry.strings('unresolved').isEmpty ? '' : '; unresolved ${list('unresolved')}'}',
    'frames' => '${entry.integer('frames')} frame(s) raised ${list('tags')}',
    'invalidated' => '${entry.str('query')} hit by ${list('matched')}$cause',
    'placed' => '${entry.str('query')} answered by a placement callback$cause',
    'fetch' => '${entry.str('query')} fetched (${entry.str('reason')})$cause',
    'settle' =>
      '${entry.str('query')} settled, store v${entry.integer('version')}',
    'error' => '${entry.str('query')} failed: ${entry.str('message')}',
    'principal' => 'identity changed, session ${entry.integer('session')}',
    'action' => 'panel ${entry.str('action')} ${entry.str('target')}',
    'outbox' =>
      'outbox ${entry.str('phase')} ${entry.str('mutationId')}'
          '${entry.strOrNull('failure') == null ? '' : ': ${entry.str('failure')}'}',
    'sync' =>
      'sync ${entry.str('entity')} ${entry.str('status')}'
          '${entry.strOrNull('detail') == null ? '' : ' (${entry.str('detail')})'}',
    final other => other,
  };
}

/// The causal event log: a backfill from `ext.forge.log`, then live events.
///
/// Nothing crosses principals. The panel holds at most
/// [ForgeConnection.eventCapacity] entries, the runtime's own cap. A
/// principal-change marker, whether it arrives live or inside a log read,
/// drops every entry before it: what is left is the marker and what came
/// after, and the marker carries no ids and no payloads.
class EventsPanel extends StatefulWidget {
  /// Creates the panel.
  const EventsPanel({super.key, required this.connection});

  /// The app connection.
  final ForgeConnection connection;

  @override
  State<EventsPanel> createState() => _EventsPanelState();
}

class _EventsPanelState extends State<EventsPanel>
    with GenerationFence<EventsPanel> {
  static const _kinds = [
    'mutation',
    'frames',
    'invalidated',
    'placed',
    'fetch',
    'settle',
    'error',
    'principal',
    'action',
    'outbox',
    'sync',
  ];

  List<Json> _backfill = const [];
  int _dropped = 0;
  bool _truncated = false;
  String? _kind;
  String? _error;

  /// Bumped by [forget], so a read that was on its way is not kept.
  int _epoch = 0;

  @override
  ForgeConnection get connection => widget.connection;

  @override
  void initState() {
    super.initState();
    unawaited(_load());
  }

  @override
  void forget() {
    _epoch++;
    _backfill = const [];
    _dropped = 0;
    _truncated = false;
    _error = null;
    unawaited(_load());
  }

  Future<void> _load() async {
    final epoch = _epoch;
    try {
      final log = await connection.call(ForgeDevtoolsProtocol.log, {
        'limit': '${ForgeDevtoolsProtocol.maxPage}',
      });
      if (!mounted || epoch != _epoch) return;

      final entries = log.objs('entries');
      // A marker ends the previous principal's entries. Keep it and what
      // follows, and nothing before it.
      final marker = entries.lastIndexWhere(
        (entry) => entry.str('kind') == 'principal',
      );

      setState(() {
        _backfill = marker > 0 ? entries.sublist(marker) : entries;
        _dropped = log.integer('dropped');
        _truncated = log.flag('truncated');
        _error = null;
      });
    } on BackendError catch (failure) {
      if (mounted && epoch == _epoch) setState(() => _error = failure.message);
    }
  }

  List<Json> _entries() {
    final last = _backfill.isEmpty ? 0 : _backfill.last.integer('seq');
    var all = [
      ..._backfill,
      for (final entry in connection.events)
        if (entry.integer('seq') > last) entry,
    ];
    if (all.length > ForgeConnection.eventCapacity) {
      all = all.sublist(all.length - ForgeConnection.eventCapacity);
    }
    return _kind == null
        ? all
        : [
            for (final entry in all)
              if (entry.str('kind') == _kind) entry,
          ];
  }

  @override
  Widget build(BuildContext context) => ValueListenableBuilder<int>(
    valueListenable: connection.activity,
    builder: (context, _, _) {
      final entries = _entries();

      return Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Padding(
            padding: const EdgeInsets.all(8),
            child: Wrap(
              spacing: 6,
              crossAxisAlignment: WrapCrossAlignment.center,
              children: [
                for (final kind in _kinds)
                  FilterChip(
                    key: ValueKey('events-kind-$kind'),
                    label: Text(kind),
                    selected: _kind == kind,
                    onSelected: (on) =>
                        setState(() => _kind = on ? kind : null),
                  ),
                Text(
                  'dropped $_dropped  skipped ${connection.eventsSkipped}'
                  '${_truncated ? '  older entries not loaded' : ''}',
                ),
                IconButton(
                  tooltip: 'Reload',
                  icon: const Icon(Icons.refresh),
                  onPressed: () => unawaited(_load()),
                ),
              ],
            ),
          ),
          if (_error != null) Text(_error!),
          Expanded(
            child: entries.isEmpty
                ? const Center(child: Text('No events yet.'))
                : ListView.builder(
                    itemCount: entries.length,
                    itemBuilder: (context, index) {
                      final entry = entries[index];
                      if (entry.flag('oversized')) return OversizedRow(entry);

                      return ListTile(
                        key: ValueKey('event-${entry.integer('seq')}'),
                        dense: true,
                        leading: Text('#${entry.integer('seq')}'),
                        title: Text(describeLogEntry(entry)),
                        subtitle: Text(
                          '${entry.str('kind')}  session ${entry.integer('session')}  at ${entry.integer('at')}',
                        ),
                      );
                    },
                  ),
          ),
        ],
      );
    },
  );
}
