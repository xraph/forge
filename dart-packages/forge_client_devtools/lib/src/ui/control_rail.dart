import 'dart:async';

import 'package:flutter/material.dart';
import 'package:forge_client/devtools_protocol.dart';

import '../backend/backend.dart';
import '../state/connection.dart';
import 'widgets.dart';

/// Online, slow, offline, extra latency, fail-next and the revalidation
/// toggles.
///
/// Every switch is read back from the app, and the rail holds nothing the app
/// did not just say: the mode and the latency are the app's network, they
/// survive a principal change there, and they are gone with an isolate, so
/// the rail drops what it shows when either the principal or the isolate
/// changes and reads again. Each change is aimed at the session the panel last
/// saw, so a click made against an account the app has left is refused.
///
/// Fail next fails the next request once with the status picked beside it:
/// 408, 429, 500, 503 or 409, so a retry policy, a backoff or a conflict path
/// can each be tried by hand.
///
/// The network switches are absent when the app wired no simulator: a row of
/// switches that do nothing would be worse than no row. The revalidation
/// toggles belong to the app and work without one.
class ControlRail extends StatefulWidget {
  /// Creates the rail.
  const ControlRail({super.key, required this.connection});

  /// The app connection.
  final ForgeConnection connection;

  @override
  State<ControlRail> createState() => _ControlRailState();
}

class _ControlRailState extends State<ControlRail>
    with ActivityRefresh<ControlRail>, GenerationFence<ControlRail> {
  static const _latencies = [0, 250, 1000, 3000];

  /// The statuses Fail next can arm, in the order the picker lists them.
  static const _failStatuses = [408, 429, 500, 503, 409];

  Json? _state;

  /// The status the next Fail next arms. A choice in the rail, not the app's
  /// state, so it is kept across a refresh.
  int _failStatus = 500;

  /// Bumped by [forget], so an answer that was on its way is not kept.
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
    unawaited(refresh());
  }

  @override
  Future<void> refresh() => _send(const {}, quiet: true);

  /// Sends [params] and shows what the app says it is now. A read that fails
  /// says nothing: the rail is a control, and a panel shows the real error.
  Future<void> _send(Map<String, String> params, {bool quiet = false}) async {
    final epoch = _epoch;
    // The messenger outlives this rail, which a refused session replaces.
    final messenger = quiet ? null : ScaffoldMessenger.maybeOf(context);

    try {
      final state = await connection.call(
        ForgeDevtoolsProtocol.control,
        params,
      );
      if (mounted && epoch == _epoch) setState(() => _state = state);
    } on BackendError catch (failure) {
      messenger?.showSnackBar(SnackBar(content: Text(failure.message)));
    }
  }

  @override
  Widget build(BuildContext context) {
    final state = _state;
    if (state == null) return const SizedBox.shrink();

    final wired = state.flag('wired');
    final toggles = [
      for (final MapEntry(key: source, value: toggle)
          in state.obj('revalidation').entries)
        if (toggle is Map<String, Object?> && toggle.flag('registered'))
          (source: source, enabled: toggle.flag('enabled')),
    ];

    if (!wired && toggles.isEmpty) return const SizedBox.shrink();

    final latency = state.integer('latencyMs');

    return SingleChildScrollView(
      scrollDirection: Axis.horizontal,
      child: Row(
        key: const ValueKey('control-rail'),
        mainAxisSize: MainAxisSize.min,
        children: [
          if (wired) ...[
            SegmentedButton<String>(
              key: const ValueKey('control-mode'),
              segments: const [
                ButtonSegment(value: 'online', label: Text('Online')),
                ButtonSegment(value: 'slow', label: Text('Slow')),
                ButtonSegment(value: 'offline', label: Text('Offline')),
              ],
              selected: {state.str('mode')},
              onSelectionChanged: (selection) =>
                  unawaited(_send({'mode': selection.first})),
            ),
            const SizedBox(width: 8),
            DropdownButton<int>(
              key: const ValueKey('control-latency'),
              // A latency the app set that is not on the list is still the
              // one in force, so it is shown rather than rounded to a choice.
              value: latency,
              items: [
                for (final ms in {..._latencies, latency}.toList()..sort())
                  DropdownMenuItem(
                    value: ms,
                    child: Text(ms == 0 ? 'no delay' : '+${ms}ms'),
                  ),
              ],
              onChanged: (ms) => unawaited(_send({'latencyMs': '${ms ?? 0}'})),
            ),
            const SizedBox(width: 8),
            if (state.flag('armed')) ...[
              Chip(label: Text('armed ${state.integer('armedStatus')}')),
              TextButton(
                key: const ValueKey('control-disarm'),
                onPressed: () => unawaited(_send({'disarm': 'true'})),
                child: const Text('Disarm'),
              ),
            ] else ...[
              DropdownButton<int>(
                key: const ValueKey('control-fail-status'),
                value: _failStatus,
                items: [
                  for (final status in _failStatuses)
                    DropdownMenuItem(value: status, child: Text('$status')),
                ],
                onChanged: (status) =>
                    setState(() => _failStatus = status ?? 500),
              ),
              const SizedBox(width: 4),
              OutlinedButton(
                key: const ValueKey('control-fail-next'),
                onPressed: () => unawaited(_send({'failNext': '$_failStatus'})),
                child: const Text('Fail next'),
              ),
            ],
          ],
          for (final (:source, :enabled) in toggles)
            Padding(
              padding: const EdgeInsets.only(left: 6),
              child: FilterChip(
                key: ValueKey('control-toggle-$source'),
                label: Text(source),
                selected: enabled,
                onSelected: (_) => unawaited(_send({'toggle': source})),
              ),
            ),
        ],
      ),
    );
  }
}
