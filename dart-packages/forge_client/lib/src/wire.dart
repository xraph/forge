/// The JSON encoding a snapshot uses, and its inverse. Port of
/// `packages/client-core/src/wire.ts`.
///
/// `__ref` is the wire form of a reference. A response may contain an object
/// of exactly that shape, so the encoder escapes colliding keys on the way out
/// (`__ref` becomes `___ref`, and so on) and [revive] unescapes them on the
/// way in. Both are one walk; the encoder's walk also collects references for
/// the reachability closure and notices cycles.
///
/// The document must be the one the TS encoder writes, because a snapshot
/// hydrates in either runtime. `JSON.stringify` makes four choices that Dart's
/// `jsonEncode` does not, so [encode] makes them itself: own keys come out
/// integer-like first and ascending, then in insertion order; an integral
/// double is written as an integer; negative zero is written as `0`; and a
/// non-finite number is written as `null`.
library;

import 'dart:collection';

import 'ref.dart' show EntityRef, markRewritten;
import 'types.dart' show EntityKey;

/// A key that would be read back as the marker, or as an escape of one.
final RegExp _collides = RegExp(r'^_*__ref$');

/// A key the encoder escaped on the way out.
final RegExp _escaped = RegExp(r'^_+__ref$');

/// Where an encode is happening, for the cycle error.
final class EncodeContext {
  /// Encoding for query [query], and record [entity] when there is one.
  const EncodeContext({required this.query, this.entity});

  /// The query whose payload this is.
  final String query;

  /// The record being encoded, when this is a record rather than a skeleton.
  final String? entity;
}

/// The JSON-safe copy, and every reference found in encounter order, not
/// deduplicated: the caller is walking a closure and already tracks keys.
typedef EncodeResult = ({Object? value, List<EntityKey> refs});

/// Copy [node] into a JSON-safe form, escaping keys and lifting references.
///
/// A route rather than a set of everything seen detects cycles, so an object
/// reached twice through different fields (a DAG) is accepted.
EncodeResult encode(Object? node, EncodeContext context) {
  final refs = <EntityKey>[];
  final route = HashSet<Object>.identity();

  Object? walk(Object? value, String path) {
    if (value is EntityRef) {
      refs.add(value.key);

      return {'__ref': value.key};
    }

    if (value is Map<Object?, Object?>) {
      if (!route.add(value)) throw _cyclic(context, path);

      final out = <String, Object?>{};

      for (final MapEntry(:key, value: child) in _inJsKeyOrder(value).entries) {
        final name = '$key';

        out[_collides.hasMatch(name) ? '_$name' : name] = walk(
          child,
          '$path.$name',
        );
      }

      route.remove(value);

      return out;
    }

    if (value is List<Object?>) {
      if (!route.add(value)) throw _cyclic(context, path);

      final out = [
        for (var i = 0; i < value.length; i++) walk(value[i], '$path[$i]'),
      ];

      route.remove(value);

      return out;
    }

    if (value is double) return _jsonNumber(value);

    return value;
  }

  return (value: walk(node, _rootPath(context)), refs: refs);
}

/// [value] with its entries in the order `Object.keys` lists them: keys that
/// are canonical array indices (`0` to `4294967294`, no sign, no leading zero)
/// ascending, then every other key in insertion order.
Map<Object?, Object?> _inJsKeyOrder(Map<Object?, Object?> value) {
  final indices = <int, MapEntry<Object?, Object?>>{};
  final rest = <MapEntry<Object?, Object?>>[];

  for (final entry in value.entries) {
    final key = entry.key;
    final index = key is String ? _arrayIndex(key) : null;

    if (index == null) {
      rest.add(entry);
    } else {
      indices[index] = entry;
    }
  }

  if (indices.isEmpty) return value;

  return {
    for (final index in indices.keys.toList()..sort())
      indices[index]!.key: indices[index]!.value,
    for (final entry in rest) entry.key: entry.value,
  };
}

int? _arrayIndex(String key) {
  if (key.isEmpty || key.length > 10) return null;

  final index = int.tryParse(key);

  final canonical = index != null && index >= 0 && index < 4294967295;

  return canonical && '$index' == key ? index : null;
}

/// The value `JSON.stringify` would write for [number], as something Dart's
/// `jsonEncode` writes the same way.
Object? _jsonNumber(double number) {
  if (!number.isFinite) return null;

  // `-0.0` and `7.0` both pass the integral test; `toInt` drops the sign and
  // the fraction digit. Beyond 2^63 a double stays a double, which parses back
  // to the same number in either runtime though the text differs.
  if (number == number.truncateToDouble() &&
      number.abs() < 9223372036854775808.0) {
    return number.toInt();
  }

  return number;
}

/// Throw if [node] is cyclic, without copying it. The denormalized mode needs
/// the check and none of the escaping.
void assertAcyclic(Object? node, EncodeContext context) {
  final route = HashSet<Object>.identity();

  void walk(Object? value, String path) {
    if (value is Map<Object?, Object?>) {
      if (!route.add(value)) throw _cyclic(context, path);

      for (final MapEntry(:key, value: child) in value.entries) {
        walk(child, '$path.$key');
      }

      route.remove(value);
    } else if (value is List<Object?>) {
      if (!route.add(value)) throw _cyclic(context, path);

      for (var i = 0; i < value.length; i++) {
        walk(value[i], '$path[$i]');
      }

      route.remove(value);
    }
  }

  walk(node, _rootPath(context));
}

/// Turn a decoded payload back into a skeleton the runtime recognises.
///
/// A container is marked rewritten only where a reference occurs beneath it,
/// and a container where nothing changed is returned by identity, which keeps
/// the store's "not rewritten means no walk" fast path for hydrated data.
Object? revive(Object? node) => _revive(node).value;

/// A revived node, whether a reference was minted at or beneath it, and
/// whether anything beneath it differs from the input. The change is tracked
/// as a flag rather than by comparing the result with the input, so no value
/// is ever compared by identity.
typedef _Revived = ({Object? value, bool refs, bool changed});

_Revived _revive(Object? node) {
  if (node is Map<Object?, Object?>) {
    if (_isMarker(node)) {
      return (
        value: EntityRef(node['__ref']! as String),
        refs: true,
        changed: true,
      );
    }

    final out = <String, Object?>{};
    var refs = false;
    var changed = false;

    for (final MapEntry(:key, :value) in node.entries) {
      final text = '$key';
      final name = _escaped.hasMatch(text) ? text.substring(1) : text;
      final child = _revive(value);

      out[name] = child.value;

      if (child.refs) refs = true;
      if (name != text || child.changed) changed = true;
    }

    // Nothing moved, so nothing was minted either.
    if (!changed) return (value: node, refs: false, changed: false);

    return (value: refs ? markRewritten(out) : out, refs: refs, changed: true);
  }

  if (node is List<Object?>) {
    final out = <Object?>[];
    var refs = false;
    var changed = false;

    for (final element in node) {
      final child = _revive(element);

      out.add(child.value);

      if (child.refs) refs = true;
      if (child.changed) changed = true;
    }

    if (!changed) return (value: node, refs: false, changed: false);

    return (value: refs ? markRewritten(out) : out, refs: refs, changed: true);
  }

  return (value: node, refs: false, changed: false);
}

/// Exactly one key, named `__ref`, holding a string: the only shape the
/// encoder emits unescaped.
bool _isMarker(Map<Object?, Object?> node) =>
    node.length == 1 && node.containsKey('__ref') && node['__ref'] is String;

String _rootPath(EncodeContext context) =>
    context.entity == null ? 'skeleton' : 'data';

StateError _cyclic(EncodeContext context, String path) {
  final entity = context.entity;
  final headline = entity == null
      ? 'cannot serialize a cyclic value'
      : 'cannot serialize a cycle within one record';
  final record = entity == null ? '' : '\n  entity  $entity';

  return StateError(
    '[forge] dehydrate: $headline\n  query   ${context.query}$record\n  path    $path',
  );
}
