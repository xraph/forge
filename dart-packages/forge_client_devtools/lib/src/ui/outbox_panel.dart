import 'dart:async';

import 'package:flutter/material.dart';
import 'package:forge_client/devtools_protocol.dart';

import '../backend/backend.dart';
import '../state/connection.dart';
import 'widgets.dart';

/// Writes waiting in the outbox, the ones that failed, and the two levers.
///
/// The app sends ids, operation names, states and a failure that names its
/// kind and status (`conflict 409`, `uncertain: <reason>`, `unreadable`),
/// never a write's arguments or a response body, so there is nothing more to
/// show of either. Replay and discard are aimed at the session the panel last
/// saw: when the app has changed account since, it refuses, and the panel
/// reloads. Nothing crosses principals: the panel drops what it holds when the
/// principal or the isolate changes.
class OutboxPanel extends StatefulWidget {
  /// Creates the panel.
  const OutboxPanel({super.key, required this.connection});

  /// The app connection.
  final ForgeConnection connection;

  @override
  State<OutboxPanel> createState() => _OutboxPanelState();
}

class _OutboxPanelState extends State<OutboxPanel>
    with ActivityRefresh<OutboxPanel>, GenerationFence<OutboxPanel> {
  Json? _state;
  String? _error;
  bool _acting = false;

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
    _acting = false;
    unawaited(refresh());
  }

  @override
  Future<void> refresh() async {
    final epoch = _epoch;
    try {
      final state = await connection.call(ForgeDevtoolsProtocol.outbox);
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

  Future<void> _act(String action, String id) async {
    if (_acting) return;
    final epoch = _epoch;
    // The messenger outlives this panel, which a refused session replaces.
    final messenger = ScaffoldMessenger.maybeOf(context);
    setState(() => _acting = true);

    try {
      await connection.call(ForgeDevtoolsProtocol.outboxAction, {
        'action': action,
        'id': id,
      });
    } on BackendError catch (failure) {
      messenger?.showSnackBar(SnackBar(content: Text(failure.message)));
    } finally {
      if (mounted && epoch == _epoch) setState(() => _acting = false);
    }

    // Refused or done, what the outbox holds is not what it was.
    if (mounted && epoch == _epoch) await refresh();
  }

  @override
  Widget build(BuildContext context) {
    final state = _state;
    if (state == null) return Center(child: Text(_error ?? 'Loading...'));

    // The app is changing account: its answer is empty on purpose, and says
    // nothing about the outbox, so neither "empty" nor "no storage session".
    if (state.flag('stale')) {
      return const Center(
        key: ValueKey('outbox-switching'),
        child: Text('switching account'),
      );
    }

    final wired = state.flag('wired');
    final entries = state.objs('entries');
    final total = state.integer('total');

    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        if (state.str('source') == 'events')
          const Padding(
            padding: EdgeInsets.all(8),
            child: Text(
              'This cache has no storage session, so only writes seen since '
              'DevTools attached are listed.',
            ),
          ),
        if (!wired)
          const Padding(
            padding: EdgeInsets.all(8),
            child: Text(
              'Replay and discard need an OutboxInspector: pass one to '
              'registerForgeServiceExtensions(cache, outbox: ...).',
            ),
          ),
        if (state.flag('truncated'))
          Padding(
            padding: const EdgeInsets.all(8),
            child: Text(
              'Showing the first ${entries.length} of $total writes.',
              key: const ValueKey('outbox-truncated'),
            ),
          ),
        if (_error != null)
          Padding(padding: const EdgeInsets.all(8), child: Text(_error!)),
        Expanded(
          child: entries.isEmpty
              ? const Center(child: Text('The outbox is empty.'))
              : ListView.builder(
                  itemCount: entries.length,
                  itemBuilder: (context, index) => _row(entries[index], wired),
                ),
        ),
      ],
    );
  }

  Widget _row(Json entry, bool wired) {
    if (entry.flag('oversized')) return OversizedRow(entry);

    final id = entry.str('id');
    final state = entry.str('state');
    final failure = entry.strOrNull('failure');
    // A write being sent right now, or already replayed, has nothing to act
    // on.
    final actionable =
        wired && !_acting && (state == 'queued' || state == 'failed');

    return ListTile(
      key: ValueKey('outbox-$id'),
      dense: true,
      title: Text('$id  ${entry.str('operation')}'),
      subtitle: Text('$state${failure == null ? '' : ': $failure'}'),
      trailing: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          TextButton(
            key: ValueKey('outbox-replay-$id'),
            onPressed: actionable ? () => unawaited(_act('replay', id)) : null,
            child: const Text('Replay'),
          ),
          TextButton(
            key: ValueKey('outbox-discard-$id'),
            onPressed: actionable ? () => unawaited(_act('discard', id)) : null,
            child: const Text('Discard'),
          ),
        ],
      ),
    );
  }
}
