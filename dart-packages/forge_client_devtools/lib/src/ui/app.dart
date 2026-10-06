import 'dart:async';

import 'package:flutter/material.dart';
import 'package:forge_client/devtools_protocol.dart';

import '../backend/backend.dart';
import '../state/connection.dart';
import 'queries_panel.dart';
import 'widgets.dart';

/// One tab of the workspace.
typedef PanelTab = ({
  String label,
  Widget Function(ForgeConnection connection) build,
});

/// The tabs, in order. Each task that adds a panel appends here.
final List<PanelTab> _panels = [
  (label: 'Queries', build: (c) => QueriesPanel(connection: c)),
];

/// The whole forge extension UI, over any [ForgeBackend].
class ForgeDevtoolsPanel extends StatefulWidget {
  /// Creates the panel.
  const ForgeDevtoolsPanel({super.key, required this.backend});

  /// Where calls go.
  final ForgeBackend backend;

  @override
  State<ForgeDevtoolsPanel> createState() => _ForgeDevtoolsPanelState();
}

class _ForgeDevtoolsPanelState extends State<ForgeDevtoolsPanel> {
  late final ForgeConnection _connection = ForgeConnection(widget.backend);

  @override
  void dispose() {
    _connection.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => ListenableBuilder(
    listenable: _connection,
    builder: (context, _) => switch (_connection.phase) {
      ConnectionPhase.unavailable => const _Unavailable(),
      ConnectionPhase.connecting => const Center(
        child: Text('Connecting to forge_client...'),
      ),
      ConnectionPhase.incompatible || ConnectionPhase.failed => _Problem(
        message: _connection.error ?? 'Unknown error.',
        onRetry: () => unawaited(_connection.reload()),
      ),
      ConnectionPhase.ready when _connection.cacheId == null => const Center(
        child: Text(
          'No cache is attached. configureClient attaches one in debug builds; '
          'a QueryCache built directly needs registerForgeServiceExtensions(cache).',
        ),
      ),
      ConnectionPhase.ready => _Workspace(connection: _connection),
    },
  );
}

class _Unavailable extends StatelessWidget {
  const _Unavailable();

  @override
  Widget build(BuildContext context) => const Center(
    key: ValueKey('forge-unavailable'),
    child: Padding(
      padding: EdgeInsets.all(24),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text('forge_client is not running in the connected app.'),
          SizedBox(height: 12),
          Text('Check that:'),
          Text('  the app depends on forge_client;'),
          Text(
            '  it is a debug or profile build, not a release build, where the devtools are compiled out;',
          ),
          Text('  it was not built with --dart-define=forge.devtools=false;'),
          Text(
            '  a cache built with the QueryCache constructor was passed to registerForgeServiceExtensions.',
          ),
          SizedBox(height: 12),
          Text(
            'This tab connects by itself when forge_client appears, for example after a hot restart.',
          ),
        ],
      ),
    ),
  );
}

class _Problem extends StatelessWidget {
  const _Problem({required this.message, required this.onRetry});

  final String message;
  final VoidCallback onRetry;

  @override
  Widget build(BuildContext context) => Center(
    child: Column(
      mainAxisSize: MainAxisSize.min,
      children: [
        Padding(padding: const EdgeInsets.all(16), child: Text(message)),
        TextButton(onPressed: onRetry, child: const Text('Retry')),
      ],
    ),
  );
}

class _Workspace extends StatelessWidget {
  const _Workspace({required this.connection});

  final ForgeConnection connection;

  @override
  Widget build(BuildContext context) {
    // Every panel, and the status bar, is keyed on the cache and on the
    // connection's generation: when the principal changes, nothing a panel
    // read for the previous one survives the rebuild.
    final scope = '${connection.cacheId}-${connection.generation}';

    return DefaultTabController(
      length: _panels.length,
      child: Column(
        children: [
          Row(
            children: [
              if (connection.caches.length > 1)
                Padding(
                  padding: const EdgeInsets.symmetric(horizontal: 8),
                  child: DropdownButton<String>(
                    key: const ValueKey('cache-picker'),
                    value: connection.cacheId,
                    items: [
                      for (final cache in connection.caches)
                        DropdownMenuItem(
                          value: cache.str('id'),
                          child: Text(cache.str('label')),
                        ),
                    ],
                    onChanged: (id) {
                      if (id != null) connection.selectCache(id);
                    },
                  ),
                ),
              Expanded(
                child: _StatusBar(
                  key: ValueKey('status-$scope'),
                  connection: connection,
                ),
              ),
            ],
          ),
          TabBar(
            isScrollable: true,
            tabs: [for (final panel in _panels) Tab(text: panel.label)],
          ),
          Expanded(
            child: TabBarView(
              children: [
                for (final panel in _panels)
                  KeyedSubtree(
                    key: ValueKey('${panel.label}-$scope'),
                    child: panel.build(connection),
                  ),
              ],
            ),
          ),
        ],
      ),
    );
  }
}

class _StatusBar extends StatefulWidget {
  const _StatusBar({super.key, required this.connection});

  final ForgeConnection connection;

  @override
  State<_StatusBar> createState() => _StatusBarState();
}

class _StatusBarState extends State<_StatusBar>
    with ActivityRefresh<_StatusBar> {
  Json? _snapshot;

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
      final snapshot = await connection.call(ForgeDevtoolsProtocol.snapshot);
      if (mounted) setState(() => _snapshot = snapshot);
    } on BackendError {
      // The status bar is decoration; a panel shows the real error.
    }
  }

  @override
  Widget build(BuildContext context) {
    final snapshot = _snapshot;
    final statuses = snapshot?.obj('statuses') ?? const <String, Object?>{};
    final store = snapshot?.obj('store') ?? const <String, Object?>{};
    // A snapshot taken while the app changes account carries zeros, not an
    // empty cache: say so instead of showing them.
    final switching = snapshot?.flag('stale') ?? false;

    return SingleChildScrollView(
      scrollDirection: Axis.horizontal,
      child: Row(
        key: const ValueKey('status-bar'),
        children: [
          if (switching)
            const Padding(
              padding: EdgeInsets.symmetric(horizontal: 4),
              child: Chip(
                key: ValueKey('status-switching'),
                label: Text('switching account'),
              ),
            )
          else ...[
            for (final bucket in [
              'success',
              'error',
              'fetching',
              'stale',
              'unmounted',
            ])
              Padding(
                padding: const EdgeInsets.symmetric(horizontal: 4),
                child: Chip(label: Text('$bucket ${statuses.integer(bucket)}')),
              ),
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: 4),
              child: Chip(label: Text('records ${store.integer('records')}')),
            ),
          ],
          IconButton(
            key: const ValueKey('status-refresh'),
            tooltip: 'Refresh',
            icon: const Icon(Icons.refresh),
            onPressed: () => unawaited(refresh()),
          ),
        ],
      ),
    );
  }
}
