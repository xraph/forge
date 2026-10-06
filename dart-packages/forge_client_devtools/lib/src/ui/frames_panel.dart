import 'dart:async';

import 'package:flutter/material.dart';
import 'package:forge_client/devtools_protocol.dart';

import '../backend/backend.dart';
import '../state/connection.dart';
import 'widgets.dart';

/// The `intent` of the marker the runtime leaves when the principal changes.
const _principalIntent = 'principal';

/// The frame ring: off by default, the only view that shows payloads.
///
/// The app bounds every payload before it sends one, and the panel shows
/// what it got: a list cut short ends in an `[N more]` marker, a value that
/// contains itself is shown as `[cycle]`, and a level cut for depth or size
/// is `[deeper]` or `[truncated]`. Nothing crosses principals: when the app
/// changes principal the ring is a single marker, and the panel drops the
/// frame it had open along with the rest.
class FramesPanel extends StatefulWidget {
  /// Creates the panel.
  const FramesPanel({super.key, required this.connection});

  /// The app connection.
  final ForgeConnection connection;

  @override
  State<FramesPanel> createState() => _FramesPanelState();
}

class _FramesPanelState extends State<FramesPanel>
    with ActivityRefresh<FramesPanel>, GenerationFence<FramesPanel> {
  Json? _state;

  /// The frame open in the detail pane: its batch `seq` and its place among
  /// the frames of that batch. The ring slides while capture runs, so a
  /// position in the list would move the pane to another frame.
  ({int seq, int ordinal})? _open;
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
    _open = null;
    _error = null;
    unawaited(refresh());
  }

  @override
  Future<void> refresh() async {
    final epoch = _epoch;
    try {
      final state = await connection.call(ForgeDevtoolsProtocol.frames, {
        'limit': '${ForgeDevtoolsProtocol.maxPage}',
      });
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

  Future<void> _toggle(bool on) async {
    try {
      await connection.call(ForgeDevtoolsProtocol.capture, {
        'enabled': '$on',
        if (on) 'limit': '200',
      });
      await refresh();
    } on BackendError catch (failure) {
      if (mounted) setState(() => _error = failure.message);
    }
  }

  int? _openIndex(List<Json> frames) {
    final open = _open;
    if (open == null) return null;

    var ordinal = 0;
    for (var i = 0; i < frames.length; i++) {
      if (frames[i].integer('seq') != open.seq) continue;
      if (ordinal == open.ordinal) return i;
      ordinal++;
    }
    return null;
  }

  void _select(List<Json> frames, int index) {
    final seq = frames[index].integer('seq');
    var ordinal = 0;
    for (var i = 0; i < index; i++) {
      if (frames[i].integer('seq') == seq) ordinal++;
    }
    setState(() => _open = (seq: seq, ordinal: ordinal));
  }

  @override
  Widget build(BuildContext context) {
    final state = _state;
    if (state == null) return Center(child: Text(_error ?? 'Loading...'));

    final capturing = state.flag('capturing');
    final frames = state.objs('entries');
    final open = _openIndex(frames);

    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Padding(
          padding: const EdgeInsets.symmetric(horizontal: 8),
          child: Wrap(
            spacing: 16,
            crossAxisAlignment: WrapCrossAlignment.center,
            children: [
              Switch(
                key: const ValueKey('frames-capture'),
                value: capturing,
                onChanged: (on) => unawaited(_toggle(on)),
              ),
              const Text('Capture frames'),
              Text('capacity ${state.integer('capacity')}'),
              Text(
                '${state.integer('dropped')} dropped',
                key: const ValueKey('frames-dropped'),
              ),
            ],
          ),
        ),
        if (_error != null) Text(_error!),
        if (!capturing)
          const Expanded(
            child: Center(
              child: Text(
                'Frame capture is off. It keeps payloads, so it is opt in: switch it on to record the next frames.',
              ),
            ),
          )
        else if (frames.isEmpty)
          const Expanded(child: Center(child: Text('No frames yet.')))
        else
          Expanded(
            child: Row(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                Expanded(
                  child: ListView.builder(
                    itemCount: frames.length,
                    itemBuilder: (context, index) =>
                        _row(frames, index, selected: open == index),
                  ),
                ),
                const VerticalDivider(width: 1),
                Expanded(
                  child: open == null
                      ? const Center(
                          child: Text('Pick a frame to read its payload.'),
                        )
                      : _Detail(frame: frames[open]),
                ),
              ],
            ),
          ),
      ],
    );
  }

  Widget _row(List<Json> frames, int index, {required bool selected}) {
    final frame = frames[index];
    if (frame.flag('oversized')) return OversizedRow(frame);

    final key = ValueKey('frame-${frame.integer('seq')}-$index');

    if (frame.str('intent') == _principalIntent) {
      return ListTile(
        key: key,
        dense: true,
        selected: selected,
        title: const Text('identity changed'),
        subtitle: const Text('earlier frames were cleared'),
        onTap: () => _select(frames, index),
      );
    }

    return ListTile(
      key: key,
      dense: true,
      selected: selected,
      title: Text('${frame.str('channel')}  ${frame.str('message')}'),
      subtitle: Text(
        '${frame.str('intent')} ${frame.str('entity')}  batch #${frame.integer('seq')}',
      ),
      onTap: () => _select(frames, index),
    );
  }
}

class _Detail extends StatelessWidget {
  const _Detail({required this.frame});

  final Json frame;

  @override
  Widget build(BuildContext context) {
    if (frame.flag('oversized')) return OversizedRow(frame);

    if (frame.str('intent') == _principalIntent) {
      return const Center(
        child: Text('The app changed identity. Nothing before this was kept.'),
      );
    }

    return SingleChildScrollView(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Padding(
            padding: const EdgeInsets.all(8),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                KeyValue('channel', frame.str('channel')),
                KeyValue('message', frame.str('message')),
                KeyValue('intent', frame.str('intent')),
                KeyValue('entity', frame.str('entity')),
                const Text(
                  'A payload is cut short before it is sent. A marker such '
                  'as [3 more] or [cycle] stands for what was left out.',
                ),
              ],
            ),
          ),
          JsonView(frame['payload'], label: 'payload', initiallyExpanded: true),
        ],
      ),
    );
  }
}
