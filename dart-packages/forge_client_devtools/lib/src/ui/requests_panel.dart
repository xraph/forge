import 'dart:async';

import 'package:flutter/material.dart';
import 'package:forge_client/devtools_protocol.dart';

import '../backend/backend.dart';
import '../state/connection.dart';
import 'widgets.dart';

/// The request log: the policy each request met, never its headers or bodies.
///
/// The app never sends a header or a body, so there is nothing here to show
/// of either: the panel shows the path, the query values (each already cut
/// short), the status, the timing, the retries and the wait on the auth
/// refresh. Nothing crosses principals: when the app changes principal the
/// log is a single marker, and the panel drops what it held.
class RequestsPanel extends StatefulWidget {
  /// Creates the panel.
  const RequestsPanel({super.key, required this.connection});

  /// The app connection.
  final ForgeConnection connection;

  @override
  State<RequestsPanel> createState() => _RequestsPanelState();
}

class _RequestsPanelState extends State<RequestsPanel>
    with ActivityRefresh<RequestsPanel>, GenerationFence<RequestsPanel> {
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
      final state = await connection.call(ForgeDevtoolsProtocol.requests);
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

    if (!state.flag('watching')) {
      return const Center(
        child: Text(
          'Nothing is recording requests. Pass a RestTransport to configureClient, or call '
          'registerForgeServiceExtensions(cache, transport: rest).',
        ),
      );
    }

    // The newest first, and never more than the panel is willing to hold.
    final held = state.objs('entries');
    final entries =
        (held.length > ForgeConnection.eventCapacity
                ? held.sublist(held.length - ForgeConnection.eventCapacity)
                : held)
            .reversed
            .toList();

    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Padding(
          padding: const EdgeInsets.all(8),
          child: Row(
            children: [
              Expanded(
                child: Text(
                  'No headers or bodies are recorded.  ${state.integer('dropped')} older requests dropped.',
                ),
              ),
              IconButton(
                tooltip: 'Reload',
                icon: const Icon(Icons.refresh),
                onPressed: () => unawaited(refresh()),
              ),
            ],
          ),
        ),
        if (_error != null) Text(_error!),
        Expanded(
          child: entries.isEmpty
              ? const Center(child: Text('No requests yet.'))
              : ListView.builder(
                  itemCount: entries.length,
                  itemBuilder: (context, index) => _row(entries[index]),
                ),
        ),
      ],
    );
  }

  Widget _row(Json entry) {
    if (entry.flag('oversized')) return OversizedRow(entry);

    if (entry.flag('marker')) {
      return const ListTile(
        key: ValueKey('request-marker'),
        dense: true,
        title: Text('identity changed'),
        subtitle: Text('earlier requests were cleared'),
      );
    }

    final retries = [
      for (final retry in entry.objs('retries'))
        '${retry.strOrNull('status') ?? 'network'} after ${retry.integer('delayMs')}ms',
    ];
    final status = entry.strOrNull('status');
    final args = entry.str('args');

    return ListTile(
      key: ValueKey('request-${entry.integer('id')}'),
      dense: true,
      title: Text(
        '${entry.str('operation')}  ${entry.str('outcome')}${status == null ? '' : ' $status'}'
        '${args.isEmpty ? '' : '  $args'}',
        maxLines: 2,
        overflow: TextOverflow.ellipsis,
      ),
      subtitle: Text(
        'attempts ${entry.integer('attempts')} of ${entry.integer('limit')}'
        '${entry['duration'] == null ? '  in flight' : '  ${entry.integer('duration')}ms'}'
        '${retries.isEmpty ? '' : '  retries: ${retries.join(', ')}'}'
        '${entry.integer('refreshes') == 0 ? '' : '  auth refresh ${entry.integer('authMs')}ms${entry.flag('joined') ? ' (joined)' : ''}'}',
      ),
    );
  }
}
