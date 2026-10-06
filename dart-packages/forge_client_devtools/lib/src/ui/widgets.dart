import 'dart:async';
import 'dart:convert';

import 'package:flutter/material.dart';

import '../backend/backend.dart';
import '../state/connection.dart';

/// Runs [action] at once, then at most once per [interval], with one trailing
/// run for calls that arrived in between.
final class Throttle {
  /// Creates the throttle.
  Throttle(this.interval, this.action);

  /// The minimum gap between runs.
  final Duration interval;

  /// What to run.
  final void Function() action;

  Timer? _timer;
  bool _pending = false;

  /// Requests a run.
  void call() {
    if (_timer != null) {
      _pending = true;
      return;
    }
    action();
    _timer = Timer(interval, () {
      _timer = null;
      if (_pending) {
        _pending = false;
        call();
      }
    });
  }

  /// Cancels any pending run.
  void dispose() {
    _timer?.cancel();
    _timer = null;
  }
}

/// Calls [refresh] when the connection reports activity, at most every
/// 500ms. Does not call it on mount: a panel loads itself in `initState`.
mixin ActivityRefresh<T extends StatefulWidget> on State<T> {
  /// The connection whose activity to follow.
  ForgeConnection get connection;

  /// Re-reads whatever the panel shows.
  Future<void> refresh();

  late final Throttle _throttle = Throttle(
    const Duration(milliseconds: 500),
    () => unawaited(refresh()),
  );

  @override
  void initState() {
    super.initState();
    connection.activity.addListener(_throttle.call);
  }

  @override
  void dispose() {
    connection.activity.removeListener(_throttle.call);
    _throttle.dispose();
    super.dispose();
  }
}

/// Fetches one page: rows `[offset, offset + limit)` and the total.
typedef PageFetcher = Future<({int total, List<Json> items})> Function(
  int offset,
  int limit,
);

/// A lazily paged list: loads [pageSize] rows, then the next page when the
/// last row scrolls into view. Never holds more than it has been asked to show.
class PagedList extends StatefulWidget {
  /// Creates the list.
  const PagedList({
    super.key,
    required this.fetch,
    required this.itemBuilder,
    this.pageSize = 100,
    this.reloadToken,
    this.totalLabel,
    this.emptyText = 'Nothing here yet.',
  });

  /// Loads a page.
  final PageFetcher fetch;

  /// Builds one row.
  final Widget Function(BuildContext context, Json item) itemBuilder;

  /// Rows per request.
  final int pageSize;

  /// Reloads from the first page whenever this changes.
  final Object? reloadToken;

  /// The header text for a total, or no header.
  final String Function(int total)? totalLabel;

  /// Shown when there are no rows.
  final String emptyText;

  @override
  State<PagedList> createState() => _PagedListState();
}

class _PagedListState extends State<PagedList> {
  final List<Json> _items = [];
  int _total = 0;
  bool _loading = false;
  bool _loaded = false;
  String? _error;
  int _generation = 0;

  @override
  void initState() {
    super.initState();
    unawaited(_next());
  }

  @override
  void didUpdateWidget(PagedList oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.reloadToken != widget.reloadToken) {
      _generation++;
      _items.clear();
      _total = 0;
      _loading = false;
      _loaded = false;
      unawaited(_next());
    }
  }

  Future<void> _next() async {
    if (_loading) return;
    _loading = true;
    final generation = _generation;

    try {
      final page = await widget.fetch(_items.length, widget.pageSize);
      if (!mounted || generation != _generation) return;
      setState(() {
        _items.addAll(page.items);
        _total = page.total;
        _loaded = true;
        _error = null;
      });
    } on Object catch (error) {
      if (!mounted || generation != _generation) return;
      setState(() => _error = '$error');
    } finally {
      if (generation == _generation) _loading = false;
    }
  }

  @override
  Widget build(BuildContext context) {
    if (_error != null && _items.isEmpty) return Center(child: Text(_error!));
    if (!_loaded) return const Center(child: Text('Loading...'));

    final more = _items.length < _total;

    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        if (widget.totalLabel != null)
          Padding(
            padding: const EdgeInsets.all(8),
            child: Text(
              widget.totalLabel!(_total),
              key: const ValueKey('paged-total'),
            ),
          ),
        Expanded(
          child: _items.isEmpty
              ? Center(child: Text(widget.emptyText))
              : ListView.builder(
                  itemCount: _items.length + (more ? 1 : 0),
                  itemBuilder: (context, index) {
                    if (index >= _items.length) {
                      if (!_loading && _error == null) unawaited(_next());
                      return const Padding(
                        padding: EdgeInsets.all(12),
                        child: Text('Loading more...'),
                      );
                    }
                    return widget.itemBuilder(context, _items[index]);
                  },
                ),
        ),
      ],
    );
  }
}

/// A collapsible view of decoded JSON. Maps and lists fold; scalars print.
class JsonView extends StatelessWidget {
  /// Creates the view.
  const JsonView(
    this.value, {
    super.key,
    this.label = 'value',
    this.initiallyExpanded = false,
  });

  /// The decoded value.
  final Object? value;

  /// The key or index it sits under.
  final String label;

  /// Whether the top level starts open.
  final bool initiallyExpanded;

  @override
  Widget build(BuildContext context) => switch (value) {
    final Map<String, Object?> map => ExpansionTile(
      dense: true,
      initiallyExpanded: initiallyExpanded,
      title: Text('$label {${map.length}}'),
      children: [
        for (final MapEntry(:key, value: child) in map.entries)
          JsonView(child, label: key),
      ],
    ),
    final List<Object?> list => ExpansionTile(
      dense: true,
      initiallyExpanded: initiallyExpanded,
      title: Text('$label [${list.length}]'),
      children: [
        for (var i = 0; i < list.length; i++) JsonView(list[i], label: '$i'),
      ],
    ),
    final scalar => ListTile(
      dense: true,
      title: Text('$label: ${jsonEncode(scalar)}'),
    ),
  };
}

/// A small heading inside a detail pane.
class SectionTitle extends StatelessWidget {
  /// Creates the heading.
  const SectionTitle(this.text, {super.key});

  /// The heading text.
  final String text;

  @override
  Widget build(BuildContext context) => Padding(
    padding: const EdgeInsets.only(top: 12, bottom: 4),
    child: Text(text, style: Theme.of(context).textTheme.titleSmall),
  );
}

/// One `label: value` line.
class KeyValue extends StatelessWidget {
  /// Creates the line.
  const KeyValue(this.label, this.value, {super.key});

  /// The label.
  final String label;

  /// The value.
  final String value;

  @override
  Widget build(BuildContext context) => Padding(
    padding: const EdgeInsets.symmetric(vertical: 2),
    child: Text('$label: $value'),
  );
}
