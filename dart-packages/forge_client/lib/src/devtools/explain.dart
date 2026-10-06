/// The two questions: why did this query refetch, and why did it not. Port of
/// `client-devtools/src/explain.ts`. Nothing here runs anything: every answer
/// is map reads over the registry and the log.
library;

import 'dart:convert';

import '../operation.dart';
import '../tags.dart';
import '../types.dart';
import 'log.dart';
import 'seams.dart';
import 'tag.dart';
import 'types.dart';

/// The JSON form of [args] the devtools keep: path, query and body. Headers
/// are always left out.
Json tagContextJson(TagContext args) => {
  if (args.path.isNotEmpty) 'path': args.path,
  if (args.query.isNotEmpty) 'query': args.query,
  if (args.body != null) 'body': args.body,
};

/// A truncated argument key, so one enormous body cannot fill the ring.
String argsKey(Object? args, {int limit = 200}) {
  final value = args is TagContext ? tagContextJson(args) : args;
  String text;

  try {
    text = value == null
        ? ''
        : jsonEncode(value, toEncodable: (Object? other) => other.toString());
  } on Object {
    // A cyclic or unserialisable argument. Not worth throwing over.
    text = '[unserialisable]';
  }

  return text.length > limit ? '${text.substring(0, limit)}...' : text;
}

/// What a caller may hand `whyNotRefetched` as the thing that should have hit.
sealed class MissCause {
  /// Const base.
  const MissCause();
}

/// An operation, resolved exactly as the invalidator would resolve it.
final class OperationCause extends MissCause {
  /// Creates the cause.
  const OperationCause(
    this.meta, {
    this.args = TagContext.empty,
    this.response,
  });

  /// The operation.
  final OperationMeta meta;

  /// Its arguments.
  final TagContext args;

  /// A representative response, for `{res.x}` templates.
  final Object? response;
}

/// Tags already resolved: a recorded cause or a hand-written hypothesis.
final class TagsCause extends MissCause {
  /// Creates the cause.
  const TagsCause(
    this.tags, {
    this.unresolved = const [],
    this.label,
    this.seq,
  });

  /// The tags raised.
  final List<String> tags;

  /// Templates that resolved to nothing.
  final List<String> unresolved;

  /// How to name it in a report.
  final String? label;

  /// The log entry it came from.
  final int? seq;
}

/// What [meta] would invalidate and which mounted queries each tag reaches.
InvalidationPreview wouldInvalidate(
  DevCache cache,
  OperationMeta meta, [
  TagContext args = TagContext.empty,
  Object? response,
]) {
  final resolved = resolveTags(meta.invalidates, args, response);
  final hits = <TagHit>[];
  final missed = <String>[];

  for (final tag in resolved.tags) {
    final queries = cache.mountedKeysFor(tag)..sort();
    hits.add(TagHit(tag: tag, queries: queries));
    if (queries.isEmpty) missed.add(tag);
  }

  return InvalidationPreview(
    operation: operationName(meta),
    templates: [...meta.invalidates],
    tags: [...resolved.tags],
    unresolved: [...resolved.unresolved],
    hits: hits,
    missed: missed,
  );
}

CauseSummary _summarise(MissCause cause) {
  switch (cause) {
    case OperationCause(:final meta, :final args, :final response):
      final resolved = resolveTags(meta.invalidates, args, response);

      return CauseSummary(
        label: operationName(meta),
        seq: null,
        tags: [...resolved.tags],
        unresolved: [...resolved.unresolved],
      );
    case TagsCause(:final tags, :final unresolved, :final label, :final seq):
      return CauseSummary(
        label: label ?? 'tags',
        seq: seq,
        tags: [...tags],
        unresolved: [...unresolved],
      );
  }
}

/// A recorded mutation or frame batch, as a cause an explanation can use.
CauseSummary? causeOf(LogEntry entry) => switch (entry) {
  MutationLog(:final operation, :final seq, :final tags, :final unresolved) =>
    CauseSummary(
      label: 'mutation $operation',
      seq: seq,
      tags: tags,
      unresolved: unresolved,
    ),
  FramesLog(:final frames, :final seq, :final tags) => CauseSummary(
    label: '$frames stream frame${frames == 1 ? '' : 's'}',
    seq: seq,
    tags: tags,
    unresolved: const [],
  ),
  _ => null,
};

/// Why [key] did not refetch when [cause] went past it.
MissReport whyNotRefetched(
  DevCache cache,
  String key,
  MissCause cause, [
  EventLog? log,
]) {
  final summary = _summarise(cause);
  final entry = cache.query(key);

  if (entry == null) {
    return MissReport(
      query: key,
      outcome: MissOutcome.notTracked,
      reason:
          'the cache has never heard of `$key`. A query key is its operation plus its '
          'arguments, so a key that looks right but was assembled by hand often is not; list '
          'the queries and copy one.',
      cause: summary,
      mounts: 0,
      settled: false,
      invalidated: summary.tags,
      carried: const [],
      matched: const [],
      nearest: const [],
      suggestions: const [],
    );
  }

  final carried = [...entry.tags]..sort();
  final matched = [
    for (final tag in summary.tags)
      if (entry.tags.contains(tag)) tag,
  ];
  final nearest = matched.isNotEmpty
      ? const <NearMiss>[]
      : nearMisses(summary.tags, carried);
  final causeSeq = summary.seq;
  final placed =
      causeSeq != null &&
      log?.last((e) => e is PlacedLog && e.query == key && e.seq > causeSeq) !=
          null;

  final outcome = matched.isEmpty
      ? MissOutcome.missed
      : placed
      ? MissOutcome.placed
      : entry.mounts == 0
      ? MissOutcome.staleWhileUnmounted
      : MissOutcome.refetched;

  final suggestions = <String>[];

  if (outcome == MissOutcome.missed) {
    if (summary.unresolved.isNotEmpty) {
      suggestions.add(
        "${summary.unresolved.length} of this operation's Invalidates templates "
        'resolved to nothing and were skipped: ${summary.unresolved.join(', ')}. A template '
        'naming `{res.x}` needs the response to carry `x`; one naming `{req.x}` needs the '
        'request to. This is the most common cause of an invalidation that silently did not '
        'happen.',
      );
    }

    for (final miss in nearest) {
      suggestions.add(miss.hint);
    }

    if (!entry.settled) {
      suggestions.add(
        '`$key` has never settled, so it carries only its `provides` templates and none of '
        'the entity keys a response would have added. Until it loads, an invalidation of a '
        'specific entity cannot reach it.',
      );
    }

    if (suggestions.isEmpty) {
      suggestions.add(
        'nothing the cause raised resembles anything this query carries. Check that the '
        'operation declares Invalidates at all: an empty list invalidates nothing and reports '
        'nothing.',
      );
    }
  }

  return MissReport(
    query: key,
    outcome: outcome,
    reason: _reasonFor(outcome, key, summary, matched, entry.mounts),
    cause: summary,
    mounts: entry.mounts,
    settled: entry.settled,
    invalidated: summary.tags,
    carried: carried,
    matched: matched,
    nearest: nearest,
    suggestions: suggestions,
  );
}

String _reasonFor(
  MissOutcome outcome,
  String key,
  CauseSummary cause,
  List<String> matched,
  int mounts,
) => switch (outcome) {
  MissOutcome.missed =>
    '`$key` did not refetch because none of the ${cause.tags.length} tag(s) '
        '${cause.label} raised are tags it carries. The two sets are disjoint.',
  MissOutcome.staleWhileUnmounted =>
    '`$key` was reached by ${matched.join(', ')} and is marked stale, but nothing has it '
        'mounted, so no request was made. It refetches the moment it mounts again. This is the '
        'cache declining to fetch data nobody is looking at, not a missed invalidation.',
  MissOutcome.placed =>
    '`$key` was reached by ${matched.join(', ')}, and a placement callback answered for '
        'it, so no refetch was owed. If what it is showing is wrong, the callback is what is '
        'wrong: return null from it to fall back to a refetch.',
  MissOutcome.refetched =>
    '`$key` was reached by ${matched.join(', ')} with $mounts mount(s), so it '
        'did refetch. Whatever is wrong is downstream of the cache.',
  MissOutcome.notTracked => '`$key` is not a query this cache is tracking.',
};

/// Why [key] refetched, from the log.
RefetchReport? whyRefetched(EventLog log, String key) {
  final dispatch = log.last((entry) => entry is FetchLog && entry.query == key);

  if (dispatch is! FetchLog) return null;

  final hit = log.last(
    (entry) =>
        entry is InvalidatedLog &&
        entry.query == key &&
        entry.seq < dispatch.seq,
  );
  final matched = hit is InvalidatedLog ? hit.matched : const <String>[];
  final causeSeq = dispatch.cause;
  final causeEntry = causeSeq == null ? null : log.find(causeSeq);
  final cause = causeEntry == null ? null : causeOf(causeEntry);

  return RefetchReport(
    query: key,
    at: dispatch.at,
    reason: dispatch.reason,
    cause: cause,
    matched: matched,
    summary: _summaryFor(key, dispatch.reason, cause, matched),
  );
}

String _summaryFor(
  String key,
  FetchReason reason,
  CauseSummary? cause,
  List<String> matched,
) {
  if (reason == FetchReason.mount) {
    return '`$key` was fetched because it was mounted for the first time.';
  }

  if (reason == FetchReason.invalidation && cause != null) {
    return '`$key` refetched because ${cause.label} invalidated '
        '${cause.tags.join(', ')}, of which it carries ${matched.join(', ')}.';
  }

  if (reason == FetchReason.invalidation) {
    return '`$key` refetched because an invalidation reached it through ${matched.join(', ')}. '
        "The cause is older than the log's window.";
  }

  return '`$key` refetched without an invalidation behind it: an explicit refetch(), a '
      'remount onto a stale entry, or stream gap recovery after a reconnect.';
}
