import 'dart:async';
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:forge_client/devtools_protocol.dart';

import '../backend/backend.dart';
import '../state/connection.dart';
import 'widgets.dart';

/// The tag graph on the left; the two questions and the invalidation preview
/// on the right.
///
/// The list re-reads in place when the app reports activity, so the reader's
/// scroll position survives. Tags, query keys and operation ids are sent back
/// to the app exactly as it gave them.
///
/// The runtime's explain answers are empty, not marked `stale`, while the
/// app changes account: the list shows no tags and a report says the query is
/// not tracked. The workspace rebuilds this panel as soon as the connection
/// sees the principal move, so nothing read for the previous account stays
/// on screen.
class ExplainPanel extends StatefulWidget {
  /// Creates the panel.
  const ExplainPanel({super.key, required this.connection});

  /// The app connection.
  final ForgeConnection connection;

  @override
  State<ExplainPanel> createState() => _ExplainPanelState();
}

class _ExplainPanelState extends State<ExplainPanel>
    with ActivityRefresh<ExplainPanel> {
  int _refresh = 0;
  String _filter = '';
  String? _selected;

  @override
  ForgeConnection get connection => widget.connection;

  @override
  Future<void> refresh() async {
    if (mounted) setState(() => _refresh++);
  }

  Future<({int total, List<Json> items})> _fetch(int offset, int limit) async {
    final page = await connection.call(ForgeDevtoolsProtocol.tags, {
      'offset': '$offset',
      'limit': '$limit',
      if (_filter.isNotEmpty) 'filter': _filter,
    });
    return (total: page.integer('total'), items: page.objs('items'));
  }

  @override
  Widget build(BuildContext context) {
    final selected = _selected;

    return Row(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Expanded(
          flex: 2,
          child: Column(
            children: [
              Padding(
                padding: const EdgeInsets.all(8),
                child: TextField(
                  key: const ValueKey('tags-filter'),
                  decoration: const InputDecoration(
                    hintText: 'Tag contains, then Enter',
                  ),
                  onSubmitted: (value) =>
                      setState(() => _filter = value.trim()),
                ),
              ),
              Expanded(
                child: PagedList(
                  key: const ValueKey('tags-list'),
                  reloadToken: _filter,
                  refreshToken: _refresh,
                  itemExtent: 64,
                  totalLabel: (total) => '$total tags',
                  emptyText: 'No tags match.',
                  fetch: _fetch,
                  itemBuilder: (context, row) {
                    final tag = row.str('tag');
                    return ListTile(
                      key: ValueKey('tag-$tag'),
                      dense: true,
                      selected: tag == selected,
                      title: Text(
                        tag,
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                      ),
                      subtitle: Text(
                        'carried by ${_count(row, 'carriers')}, mounted ${_count(row, 'mounted')}',
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                      ),
                      onTap: () => setState(() => _selected = tag),
                    );
                  },
                ),
              ),
              if (selected != null)
                _TagDetail(
                  key: ValueKey('tag-detail-$selected'),
                  connection: connection,
                  tag: selected,
                ),
            ],
          ),
        ),
        const VerticalDivider(width: 1),
        Expanded(
          flex: 3,
          child: ListView(
            padding: const EdgeInsets.all(12),
            children: [
              _Explain(connection: connection),
              const Divider(height: 32),
              _WouldInvalidate(connection: connection),
            ],
          ),
        ),
      ],
    );
  }
}

/// How many queries a row lists under [name]: the app's own total, which
/// counts past the cap on the list it sends, or the list when it sent none.
int _count(Json row, String name) {
  final total = row.integer('${name}Total');
  return total > 0 ? total : row.strings(name).length;
}

/// The selected tag, with who carries it and who an invalidation would
/// reach, and the button that invalidates it.
///
/// The tags call has no per-tag form, so the detail reads the tag back
/// through the filter and picks the exact match, and follows activity so it
/// is never older than the list.
class _TagDetail extends StatefulWidget {
  const _TagDetail({super.key, required this.connection, required this.tag});

  final ForgeConnection connection;
  final String tag;

  @override
  State<_TagDetail> createState() => _TagDetailState();
}

class _TagDetailState extends State<_TagDetail>
    with ActivityRefresh<_TagDetail> {
  Json? _row;
  bool _gone = false;

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
      final page = await connection.call(ForgeDevtoolsProtocol.tags, {
        'filter': widget.tag,
        'limit': '${ForgeDevtoolsProtocol.maxPage}',
      });
      if (!mounted) return;

      final match = page
          .objs('items')
          .where((row) => row.str('tag') == widget.tag)
          .firstOrNull;

      setState(() {
        if (match != null) {
          _row = match;
          _gone = false;
        } else if (!page.flag('truncated')) {
          // Every tag containing this one was read and it is not among them.
          _gone = true;
        }
      });
    } on BackendError {
      // The list shows the real error; this pane keeps what it last read.
    }
  }

  Future<void> _invalidate() async {
    final messenger = ScaffoldMessenger.of(context);
    try {
      await connection.call(ForgeDevtoolsProtocol.action, {
        'action': 'invalidateTag',
        'target': widget.tag,
      });
      messenger.showSnackBar(
        SnackBar(content: Text('Invalidated ${widget.tag}')),
      );
    } on BackendError catch (failure) {
      // The app's refusal, as it was said: a disposed cache, or a session
      // the cache has left.
      messenger.showSnackBar(SnackBar(content: Text(failure.message)));
    }
  }

  @override
  Widget build(BuildContext context) {
    final row = _row;

    // A tag carried by a thousand queries lists them all: scroll, so the
    // pane never pushes the list off the screen.
    return ConstrainedBox(
      constraints: const BoxConstraints(maxHeight: 240),
      child: SingleChildScrollView(
        padding: const EdgeInsets.all(8),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(widget.tag, style: Theme.of(context).textTheme.titleSmall),
            if (_gone)
              const Text('No query carries this tag any more.')
            else if (row != null) ...[
              KeyValue('carried by', row.strings('carriers').join(', ')),
              if (_count(row, 'carriers') > row.strings('carriers').length)
                Text('showing the first ${row.strings('carriers').length}'),
              KeyValue(
                'an invalidation reaches',
                row.strings('mounted').join(', '),
              ),
              if (_count(row, 'mounted') > row.strings('mounted').length)
                Text('showing the first ${row.strings('mounted').length}'),
            ],
            OutlinedButton(
              key: const ValueKey('tag-invalidate'),
              onPressed: () => unawaited(_invalidate()),
              child: const Text('Invalidate this tag'),
            ),
          ],
        ),
      ),
    );
  }
}

class _Explain extends StatefulWidget {
  const _Explain({required this.connection});

  final ForgeConnection connection;

  @override
  State<_Explain> createState() => _ExplainState();
}

class _ExplainState extends State<_Explain> {
  static const _questions = {
    'explain': 'pick for me',
    'whyNotRefetched': 'why not refetched',
    'whyRefetched': 'why refetched',
  };

  final _key = TextEditingController();
  String _question = 'explain';
  Json? _result;
  String? _error;

  @override
  void dispose() {
    _key.dispose();
    super.dispose();
  }

  Future<void> _run() async {
    // Sent as typed: a query key is an identifier, never trimmed or folded.
    final key = _key.text;
    if (key.trim().isEmpty) {
      setState(() => _error = 'enter a query key');
      return;
    }

    try {
      final result = await widget.connection.call(
        ForgeDevtoolsProtocol.explain,
        {'key': key, 'question': _question},
      );
      if (mounted) {
        setState(() {
          _result = result;
          _error = null;
        });
      }
    } on BackendError catch (failure) {
      if (mounted) {
        setState(() {
          _result = null;
          _error = failure.message;
        });
      }
    }
  }

  @override
  Widget build(BuildContext context) => Column(
    crossAxisAlignment: CrossAxisAlignment.start,
    children: [
      Text(
        'Why did this query (not) refetch?',
        style: Theme.of(context).textTheme.titleMedium,
      ),
      TextField(
        key: const ValueKey('explain-key'),
        controller: _key,
        decoration: const InputDecoration(
          hintText: 'Query key, from the Queries tab',
        ),
      ),
      Row(
        children: [
          DropdownButton<String>(
            key: const ValueKey('explain-question'),
            value: _question,
            items: [
              for (final MapEntry(:key, :value) in _questions.entries)
                DropdownMenuItem(value: key, child: Text(value)),
            ],
            onChanged: (value) =>
                setState(() => _question = value ?? 'explain'),
          ),
          const SizedBox(width: 8),
          FilledButton(
            key: const ValueKey('explain-run'),
            onPressed: () => unawaited(_run()),
            child: const Text('Explain'),
          ),
        ],
      ),
      if (_error != null) Text(_error!),
      if (_result case final result?)
        _Report(report: result.objOrNull('report')),
    ],
  );
}

class _Report extends StatelessWidget {
  const _Report({required this.report});

  final Json? report;

  @override
  Widget build(BuildContext context) {
    final report = this.report;
    if (report == null) {
      return const Text('The log holds no request for this query.');
    }

    final cause = report.obj('cause');

    if (report.str('kind') == 'refetch') {
      return Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Chip(
            key: const ValueKey('report-outcome'),
            label: Text(report.str('reason')),
          ),
          Text(report.str('summary')),
          if (cause.isNotEmpty) KeyValue('cause', cause.str('label')),
          KeyValue('matched', report.strings('matched').join(', ')),
        ],
      );
    }

    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Chip(
          key: const ValueKey('report-outcome'),
          label: Text(report.str('outcome')),
        ),
        Text(report.str('reason')),
        KeyValue('cause', cause.str('label')),
        KeyValue('invalidated', report.strings('invalidated').join(', ')),
        KeyValue('carried', report.strings('carried').join(', ')),
        KeyValue('matched', report.strings('matched').join(', ')),
        if (cause.strings('unresolved').isNotEmpty)
          KeyValue('unresolved', cause.strings('unresolved').join(', ')),
        if (report.objs('nearest').isNotEmpty)
          const SectionTitle('nearest misses'),
        for (final miss in report.objs('nearest'))
          ListTile(
            dense: true,
            title: Text('${miss.str('invalidated')} vs ${miss.str('carried')}'),
            subtitle: Text('${miss.str('relation')}: ${miss.str('hint')}'),
          ),
        if (report.strings('suggestions').isNotEmpty)
          const SectionTitle('what to change'),
        for (final suggestion in report.strings('suggestions'))
          Text(suggestion),
      ],
    );
  }
}

class _WouldInvalidate extends StatefulWidget {
  const _WouldInvalidate({required this.connection});

  final ForgeConnection connection;

  @override
  State<_WouldInvalidate> createState() => _WouldInvalidateState();
}

class _WouldInvalidateState extends State<_WouldInvalidate> {
  final _args = TextEditingController(text: '{}');
  final _response = TextEditingController();
  List<Json> _operations = const [];
  int _operationsTotal = 0;
  String? _operation;
  Json? _preview;
  String? _error;

  @override
  void initState() {
    super.initState();
    unawaited(_load());
  }

  @override
  void dispose() {
    _args.dispose();
    _response.dispose();
    super.dispose();
  }

  Future<void> _load() async {
    try {
      final result = await widget.connection.call(
        ForgeDevtoolsProtocol.operations,
      );
      if (!mounted) return;
      setState(() {
        _operations = result.objs('operations');
        _operationsTotal = result.integer('total');
        _operation = _operations.isEmpty ? null : _operations.first.str('id');
      });
    } on BackendError catch (failure) {
      if (mounted) setState(() => _error = failure.message);
    }
  }

  String? _invalidJson(String name, String text) {
    if (text.trim().isEmpty) return null;
    try {
      jsonDecode(text);
      return null;
    } on FormatException {
      return '$name is not valid JSON';
    }
  }

  Future<void> _run() async {
    final operation = _operation;
    if (operation == null) return;

    final problem =
        _invalidJson('args', _args.text) ??
        _invalidJson('response', _response.text);
    if (problem != null) {
      setState(() => _error = problem);
      return;
    }

    try {
      final result = await widget.connection.call(
        ForgeDevtoolsProtocol.wouldInvalidate,
        {
          'operation': operation,
          if (_args.text.trim().isNotEmpty) 'args': _args.text.trim(),
          if (_response.text.trim().isNotEmpty)
            'response': _response.text.trim(),
        },
      );
      if (mounted) {
        setState(() {
          _preview = result.objOrNull('preview');
          _error = null;
        });
      }
    } on BackendError catch (failure) {
      if (mounted) {
        setState(() {
          _preview = null;
          _error = failure.message;
        });
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    final preview = _preview;

    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text(
          'What would this operation invalidate?',
          style: Theme.of(context).textTheme.titleMedium,
        ),
        if (_operations.isEmpty)
          const Text(
            'No operations known yet. Pass the generated operations table to registerForgeServiceExtensions.',
          )
        else ...[
          DropdownButton<String>(
            key: const ValueKey('would-operation'),
            isExpanded: true,
            value: _operation,
            items: [
              for (final op in _operations)
                DropdownMenuItem(
                  value: op.str('id'),
                  child: Text(
                    '${op.str('method')} ${op.str('path')}  (${op.str('id')})',
                    overflow: TextOverflow.ellipsis,
                  ),
                ),
            ],
            onChanged: (value) => setState(() => _operation = value),
          ),
          if (_operationsTotal > _operations.length)
            Text(
              'showing the first ${_operations.length} of $_operationsTotal operations',
            ),
        ],
        TextField(
          key: const ValueKey('would-args'),
          controller: _args,
          decoration: const InputDecoration(
            labelText:
                'args JSON: {"path": {...}, "query": {...}, "body": ...}',
          ),
        ),
        TextField(
          key: const ValueKey('would-response'),
          controller: _response,
          decoration: const InputDecoration(
            labelText: 'a representative response JSON, for {res.x} templates',
          ),
        ),
        FilledButton(
          key: const ValueKey('would-run'),
          onPressed: () => unawaited(_run()),
          child: const Text('Preview'),
        ),
        if (_error != null) Text(_error!),
        if (preview != null) ...[
          KeyValue('tags', preview.strings('tags').join(', ')),
          KeyValue('unresolved', preview.strings('unresolved').join(', ')),
          Text(
            'missed: ${preview.strings('missed').isEmpty ? 'none' : preview.strings('missed').join(', ')}',
          ),
          for (final hit in preview.objs('hits'))
            Text(
              '${hit.str('tag')} reaches ${hit.strings('queries').isEmpty ? 'nothing mounted' : hit.strings('queries').join(', ')}',
            ),
        ],
      ],
    );
  }
}
