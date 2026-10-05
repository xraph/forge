import 'dart:convert';

import 'operation.dart';
import 'ref.dart';

/// What [resolveTags] produces: the tags that resolved, and the templates
/// that did not.
final class ResolvedTags {
  /// Creates a result.
  const ResolvedTags({required this.tags, required this.unresolved});

  /// Resolved tags, deduplicated, in declaration order.
  final List<String> tags;

  /// Templates that resolved to nothing.
  final List<String> unresolved;
}

final RegExp _placeholder = RegExp(r'\{([^{}]*)\}');
final RegExp _responseScoped = RegExp(r'\{(?!\s*req\.)');

/// Marks a lookup that found no property at all, which is different from a
/// property holding null: a source holding null has answered the question.
const Object _missing = _Missing();

final class _Missing {
  const _Missing();
}

/// Substitutes a tag template against one operation's arguments and response.
///
/// `Order[]` has no placeholder and is returned unchanged. `Order:{id}`,
/// `Customer:{req.customerId}` and `Shipment:{res.shipment.id}` substitute.
///
/// Returns null, never a partially substituted string, when any placeholder
/// names nothing usable. A tag that silently becomes `Customer:` matches no
/// query and reports nothing.
///
/// Header parameters are not searched, exactly as TypeScript has none to
/// search.
String? resolveTag(String template, TagContext args, [Object? response]) {
  if (!template.contains('{')) return template;

  var resolved = true;

  final tag = template.replaceAllMapped(_placeholder, (match) {
    final value = _lookup(match.group(1)!.trim(), args, response);

    if (!_usable(value)) {
      resolved = false;

      return '';
    }

    return jsString(value);
  });

  return resolved ? tag : null;
}

/// Resolves a whole `provides` or `invalidates` list, deduplicated.
///
/// Unresolvable templates are reported rather than thrown. A list response
/// answers a response template once per element: `GET /orders` declaring
/// `Order:{id}` provides one tag per record returned.
ResolvedTags resolveTags(
  List<String> templates,
  TagContext args, [
  Object? response,
]) {
  final tags = <String>[];
  final unresolved = <String>[];
  final seen = <String>{};

  void push(String tag) {
    if (seen.add(tag)) tags.add(tag);
  }

  for (final template in templates) {
    final tag = resolveTag(template, args, response);

    if (tag != null) {
      push(tag);
      continue;
    }

    if (response is List<Object?> && _responseScoped.hasMatch(template)) {
      var answered = response.isEmpty;

      for (final item in response) {
        final each = resolveTag(template, args, item);

        if (each != null) {
          answered = true;
          push(each);
        }
      }

      if (answered) continue;
    }

    unresolved.add(template);
  }

  return ResolvedTags(tags: tags, unresolved: unresolved);
}

/// Explicit-first lookup: `{req.x}` searches the request only (path, query,
/// body), `{res.a.b}` the response only, and a bare `{x}` the request then the
/// response, first source that has the property at all wins.
Object? _lookup(String expr, TagContext args, Object? response) {
  final dot = expr.indexOf('.');

  if (dot > 0) {
    final head = expr.substring(0, dot);
    final rest = expr.substring(dot + 1);

    if (head == 'req') return _fromRequest(rest, args);
    if (head == 'res') return _dig(response, rest);
  }

  final request = _fromRequest(expr, args);

  return identical(request, _missing) ? _dig(response, expr) : request;
}

Object? _fromRequest(String expr, TagContext args) {
  final fromPath = _dig(args.path, expr);

  if (!identical(fromPath, _missing)) return fromPath;

  final fromQuery = _dig(args.query, expr);

  return identical(fromQuery, _missing) ? _dig(args.body, expr) : fromQuery;
}

/// Walks a dotted path. Numeric segments index lists. A segment that names no
/// key is retried under its other spellings (`customer_id` finds
/// `customerId`), and an exact key always wins.
Object? _dig(Object? root, String path) {
  Object? node = root;

  for (final segment in path.split('.')) {
    switch (node) {
      case Map<String, Object?>():
        final key = node.containsKey(segment)
            ? segment
            : (_spelledAs(node, segment) ?? segment);

        if (!node.containsKey(key)) return _missing;

        node = node[key];
      case List<Object?>():
        final index = int.tryParse(segment);

        if (index == null || index < 0 || index >= node.length) return _missing;

        node = node[index];
      default:
        return _missing;
    }
  }

  return node;
}

String? _spelledAs(Map<String, Object?> record, String name) {
  String fold(String key) => key.replaceAll(RegExp('[_-]'), '').toLowerCase();
  final wanted = fold(name);

  for (final key in record.keys) {
    if (fold(key) == wanted) return key;
  }

  return null;
}

/// The identity rule widened by booleans, because a tag may partition a list
/// by a flag (`Order[]:{req.archived}`).
bool _usable(Object? value) => value is bool || isIdentity(value);

/// The name a query is keyed under: method plus path, as in TypeScript, so a
/// Dart cache key and a TypeScript cache key for the same call are equal.
String operationName(OperationMeta meta) => '${meta.method} ${meta.path}';

/// The cache key for one mounted query: its operation plus its arguments.
///
/// An empty [args] keys as the operation alone, matching a TypeScript call
/// with no arguments.
String queryKey(OperationMeta meta, TagContext args) =>
    operationQueryKey(operationName(meta), args.isEmpty ? null : args);

/// The cache key for [operation] called with [args], or with no arguments when
/// [args] is null. The TypeScript `queryKey(operation, args)`.
///
/// Map keys are sorted, so argument order never splits one query in two. A
/// null path, query or header value is dropped, because an optional parameter
/// left off and one passed null are the same request; a null inside the body
/// is kept.
String operationQueryKey(String operation, [TagContext? args]) {
  if (args == null) return operation;

  return '$operation|${_stable(<String, Object?>{if (args.path.isNotEmpty) 'path': _withoutNulls(args.path), if (args.query.isNotEmpty) 'query': _withoutNulls(args.query), if (args.headers.isNotEmpty) 'headers': args.headers, 'body': ?args.body})}';
}

Map<String, Object?> _withoutNulls(Map<String, Object?> source) => {
  for (final MapEntry(:key, :value) in source.entries) key: ?value,
};

String _stable(Object? value) => switch (value) {
  null => 'null',
  double() when !value.isFinite => 'null',
  num() || BigInt() => jsString(value),
  bool() => '$value',
  String() => jsonEncode(value),
  DateTime() => jsonEncode(value.toUtc().toIso8601String()),
  List<Object?>() => '[${value.map(_stable).join(',')}]',
  Map<String, Object?>() =>
    '{${(value.keys.toList()..sort()).map((key) => '${jsonEncode(key)}:${_stable(value[key])}').join(',')}}',
  _ => jsonEncode('$value'),
};
