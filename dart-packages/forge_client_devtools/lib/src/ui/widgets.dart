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

/// Throws away what the panel holds when the connection's [generation] moves:
/// the principal changed, another cache was picked, or the app went away.
///
/// The workspace already keys every panel on the generation, so a panel it
/// builds is replaced outright. This is the same rule for a panel that lives
/// on its own, and the reason a panel can say that nothing it read for one
/// principal outlives the switch: [forget] is the one place that drops it.
mixin GenerationFence<T extends StatefulWidget> on State<T> {
  /// The connection whose generation to follow.
  ForgeConnection get connection;

  /// Drops everything held for the previous principal, and reads again. Runs
  /// inside `setState`.
  void forget();

  late int _heldGeneration;

  @override
  void initState() {
    super.initState();
    _heldGeneration = connection.generation;
    connection.addListener(_checkGeneration);
  }

  void _checkGeneration() {
    if (connection.generation == _heldGeneration) return;
    _heldGeneration = connection.generation;
    if (mounted) setState(forget);
  }

  @override
  void dispose() {
    connection.removeListener(_checkGeneration);
    super.dispose();
  }
}

/// Fetches one page: rows `[offset, offset + limit)` and the total.
typedef PageFetcher = Future<({int total, List<Json> items})> Function(
  int offset,
  int limit,
);

/// A windowed list over a store of any size: it knows the total, builds only
/// the rows in view, and holds at most [maxPages] pages of [pageSize] rows.
///
/// A page is read when a row of it is first built, so a scroll to row 9,000
/// reads the pages around row 9,000 and none of those in between. A page
/// that has not arrived shows a placeholder row. When more than [maxPages]
/// are held, the ones least recently on screen are let go and read again if
/// the reader scrolls back, so memory follows the window and not the store. A
/// page with a row in the list's viewport (or its cache extent) is never let
/// go, so a viewport that shows more than [maxPages] pages holds them all and
/// scrolling it reads only the pages that come into view, never the ones
/// already on screen. Rows say whether they are in the list by being mounted,
/// not by being built: a list builds only the rows that scroll in, so the
/// rows already on screen are built in no recent frame.
///
/// A new [reloadToken] (other inputs, such as a filter) starts again from the
/// first page. A new [refreshToken] (the app reported activity, or the
/// reader asked) re-reads the pages held, in place: the rows neither flicker
/// nor lose their scroll position, and the cost is at most [maxPages] plus
/// the first page however large the store is. A failed refresh is shown above the
/// rows and does not stop other pages from loading. A row the app replaced
/// with an `oversized` marker shows as [OversizedRow] rather than through
/// [itemBuilder].
class PagedList extends StatefulWidget {
  /// Creates the list.
  const PagedList({
    super.key,
    required this.fetch,
    required this.itemBuilder,
    this.pageSize = 100,
    this.maxPages = 6,
    this.itemExtent,
    this.reloadToken,
    this.refreshToken,
    this.totalLabel,
    this.emptyText = 'Nothing here yet.',
  }) : assert(maxPages >= 2, 'a window needs room for the pages in view');

  /// Loads a page.
  final PageFetcher fetch;

  /// Builds one row.
  final Widget Function(BuildContext context, Json item) itemBuilder;

  /// Rows per request.
  final int pageSize;

  /// Most pages held at once, not counting those on screen.
  final int maxPages;

  /// A fixed row height. A list that sets it scrolls to any row without
  /// laying out the rows before it; one that does not has a scrollbar that
  /// follows an estimate.
  final double? itemExtent;

  /// Reloads from the first page whenever this changes.
  final Object? reloadToken;

  /// Re-reads the pages held, in place, whenever this changes.
  final Object? refreshToken;

  /// The header text for a total, or no header.
  final String Function(int total)? totalLabel;

  /// Shown when there are no rows.
  final String emptyText;

  @override
  State<PagedList> createState() => _PagedListState();
}

class _PagedListState extends State<PagedList> {
  /// The pages held, by page number, least recently on screen first.
  final Map<int, List<Json>> _pages = {};
  final Set<int> _inflight = {};
  final Map<int, String> _failed = {};

  /// How many mounted rows each page has: the pages in the viewport.
  final Map<int, int> _visible = {};
  int _total = 0;
  bool _loaded = false;
  String? _error;
  String? _refreshError;
  int _epoch = 0;
  bool _refreshing = false;
  bool _refreshAgain = false;

  @override
  void initState() {
    super.initState();
    _request(0);
  }

  @override
  void didUpdateWidget(PagedList oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.reloadToken != widget.reloadToken) {
      // Whatever is still on its way was asked for the old inputs.
      _epoch++;
      _pages.clear();
      _inflight.clear();
      _failed.clear();
      _total = 0;
      _loaded = false;
      _error = null;
      _refreshError = null;
      _refreshing = false;
      _refreshAgain = false;
      _request(0);
    } else if (oldWidget.refreshToken != widget.refreshToken) {
      unawaited(_refresh());
    }
  }

  /// Asks for [page] unless it is held, on its way, or failed (a failed page
  /// is asked for again by the reader, not by the next frame).
  void _request(int page) {
    if (_pages.containsKey(page) ||
        _inflight.contains(page) ||
        _failed.containsKey(page)) {
      return;
    }
    _inflight.add(page);
    final epoch = _epoch;
    // Not inline: a row is requested while the list lays out.
    scheduleMicrotask(() => unawaited(_read(page, epoch)));
  }

  Future<void> _read(int page, int epoch) async {
    if (!mounted || epoch != _epoch) return;

    try {
      final result = await widget.fetch(
        page * widget.pageSize,
        widget.pageSize,
      );
      if (!mounted || epoch != _epoch) return;
      setState(() {
        _total = result.total;
        _loaded = true;
        _error = null;
        _failed.remove(page);
        _hold(page, result.items);
        _dropBeyondTotal();
      });
    } on Object catch (error) {
      if (!mounted || epoch != _epoch) return;
      setState(() {
        if (_loaded) {
          _failed[page] = '$error';
        } else {
          _error = '$error';
        }
      });
    } finally {
      if (epoch == _epoch) _inflight.remove(page);
    }
  }

  /// Keeps [rows] as the most recent page and lets the oldest ones go.
  void _hold(int page, List<Json> rows) {
    _pages.remove(page);
    _pages[page] = rows;
    while (_pages.length > widget.maxPages) {
      // The oldest page that is not on screen. When every page is, the
      // window is as wide as the viewport and nothing goes.
      final victim = _pages.keys.cast<int?>().firstWhere(
        (held) => held != page && (_visible[held] ?? 0) == 0,
        orElse: () => null,
      );
      if (victim == null) break;
      _pages.remove(victim);
    }
  }

  /// A row of [page] was mounted in the list.
  void _shown(int page) => _visible[page] = (_visible[page] ?? 0) + 1;

  /// A row of [page] left the list.
  void _hidden(int page) {
    final left = (_visible[page] ?? 0) - 1;
    if (left > 0) {
      _visible[page] = left;
    } else {
      _visible.remove(page);
    }
  }

  /// Lets go of pages past the total.
  void _dropBeyondTotal() {
    _pages.removeWhere((page, _) => page * widget.pageSize >= _total);
    _failed.removeWhere((page, _) => page * widget.pageSize >= _total);
  }

  /// Re-reads the pages held, one request each, then swaps them in together.
  Future<void> _refresh() async {
    if (!_loaded) {
      // The first read never arrived (it failed, or is still on its way):
      // reading again is the refresh.
      _request(0);
      return;
    }
    if (_refreshing) {
      _refreshAgain = true;
      return;
    }
    _refreshing = true;
    final epoch = _epoch;
    final fresh = <int, List<Json>>{};
    var total = _total;

    try {
      // The first page is always asked, held or not: with no rows the total
      // is all there is to learn, and with a window far down it is how a
      // total that fell to zero (or rose from it) is found.
      for (final page in {0, ..._pages.keys}.toList()..sort()) {
        final result = await widget.fetch(
          page * widget.pageSize,
          widget.pageSize,
        );
        if (!mounted || epoch != _epoch) return;
        fresh[page] = result.items;
        total = result.total;
      }
      setState(() {
        _total = total;
        for (final MapEntry(:key, :value) in fresh.entries) {
          // A page that scrolled away while this ran stays away.
          if (_pages.containsKey(key)) _pages[key] = value;
        }
        _dropBeyondTotal();
        _failed.clear();
        _refreshError = null;
      });
    } on Object catch (error) {
      if (!mounted || epoch != _epoch) return;
      setState(() => _refreshError = '$error');
    } finally {
      if (epoch == _epoch) {
        _refreshing = false;
        if (_refreshAgain && mounted) {
          _refreshAgain = false;
          unawaited(_refresh());
        }
      }
    }
  }

  void _retry(int page) {
    setState(() => _failed.remove(page));
    _request(page);
  }

  /// The stand-in for row [index] of a page that is not held. A page that
  /// failed says so once, on its first row, and keeps the others quiet.
  Widget _placeholder(int index, int page) {
    final failed = _failed[page];
    if (failed == null) {
      return const ListTile(
        dense: true,
        enabled: false,
        title: Text('Loading...'),
      );
    }
    if (index % widget.pageSize != 0) {
      return const ListTile(dense: true, enabled: false);
    }
    final first = page * widget.pageSize + 1;
    return ListTile(
      key: ValueKey('paged-failed-$page'),
      dense: true,
      title: Text(
        'Could not load rows from $first: $failed',
        maxLines: 2,
        overflow: TextOverflow.ellipsis,
      ),
      subtitle: const Text('Tap to retry', maxLines: 1),
      onTap: () => _retry(page),
    );
  }

  Widget _row(BuildContext context, int index) {
    final page = index ~/ widget.pageSize;

    // Mounted while the row is in the list, so the page is the last to be
    // let go however long ago the row was built.
    return _PageMark(
      page: page,
      onShown: _shown,
      onHidden: _hidden,
      child: _rowContent(context, index, page),
    );
  }

  Widget _rowContent(BuildContext context, int index, int page) {
    final rows = _pages[page];

    if (rows == null) {
      _request(page);
      return _placeholder(index, page);
    }

    if (page != _pages.keys.last) {
      _pages
        ..remove(page)
        ..[page] = rows;
    }

    final at = index - page * widget.pageSize;
    if (at >= rows.length) return const SizedBox.shrink();

    final item = rows[at];
    if (item.flag('oversized')) return OversizedRow(item);
    return widget.itemBuilder(context, item);
  }

  @override
  Widget build(BuildContext context) {
    if (!_loaded) {
      final error = _error;
      if (error == null) return const Center(child: Text('Loading...'));
      return Center(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Padding(
              padding: const EdgeInsets.all(16),
              child: Text(error, key: const ValueKey('paged-error')),
            ),
            TextButton(
              key: const ValueKey('paged-retry'),
              onPressed: () => _retry(0),
              child: const Text('Retry'),
            ),
          ],
        ),
      );
    }

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
        if (_refreshError != null)
          Padding(
            padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
            child: Text(
              'Could not refresh: $_refreshError',
              key: const ValueKey('paged-refresh-error'),
            ),
          ),
        Expanded(
          child: _total == 0
              ? Center(child: Text(widget.emptyText))
              : ListView.builder(
                  itemCount: _total,
                  itemExtent: widget.itemExtent,
                  itemBuilder: _row,
                ),
        ),
      ],
    );
  }
}

/// Counts one row of [page] as in the list for as long as it is mounted.
class _PageMark extends StatefulWidget {
  const _PageMark({
    required this.page,
    required this.onShown,
    required this.onHidden,
    required this.child,
  });

  final int page;
  final void Function(int page) onShown;
  final void Function(int page) onHidden;
  final Widget child;

  @override
  State<_PageMark> createState() => _PageMarkState();
}

class _PageMarkState extends State<_PageMark> {
  @override
  void initState() {
    super.initState();
    widget.onShown(widget.page);
  }

  @override
  void didUpdateWidget(_PageMark oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.page != widget.page) {
      oldWidget.onHidden(oldWidget.page);
      widget.onShown(widget.page);
    }
  }

  @override
  void dispose() {
    widget.onHidden(widget.page);
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => widget.child;
}

/// Stands in for a row the app would not send because one of its
/// identifiers is over 64 KB: the app replaces such a row with
/// `{oversized: true, field: ...}`.
class OversizedRow extends StatelessWidget {
  /// Creates the placeholder for [row].
  const OversizedRow(this.row, {super.key});

  /// The marker row.
  final Json row;

  @override
  Widget build(BuildContext context) => ListTile(
    dense: true,
    enabled: false,
    title: Text('too large to show (${row.str('field')} is over 64 KB)'),
  );
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
    // The app marks what it left out of a bounded copy with a string: how many
    // more, a cycle, a level cut for depth or size. Show it as a marker, not
    // as if it were the application's own text.
    final String text when _isMarker(text) => ListTile(
      dense: true,
      title: Text(
        '$label: $text',
        style: const TextStyle(fontStyle: FontStyle.italic),
      ),
    ),
    final scalar => ListTile(
      dense: true,
      title: Text('$label: ${jsonEncode(scalar)}'),
    ),
  };
}

final _marker = RegExp(r'^\[(\d+ more|more|cycle|deeper|truncated)\]$');

bool _isMarker(String text) => _marker.hasMatch(text);

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
