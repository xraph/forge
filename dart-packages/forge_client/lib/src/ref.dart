import 'types.dart';

/// A placeholder standing where an entity was lifted out of the tree.
///
/// References are recognised by type, never by inspecting properties: a JSON
/// decoder can never produce an [EntityRef], so a response that happens to
/// contain an object shaped like `{"__ref": "Order:7"}` round-trips untouched.
/// The `__ref` spelling exists only in the snapshot wire format (plan 01b).
/// Named `EntityRef` rather than `Ref` because Riverpod 3 exports `Ref`.
final class EntityRef {
  /// Creates a reference to [key]. Prefer [makeRef] in runtime code.
  const EntityRef(this.key);

  /// The entity key this reference stands for.
  final EntityKey key;

  @override
  String toString() => 'EntityRef($key)';
}

/// Containers that `normalize` had to rebuild because a reference appears
/// somewhere beneath them. A skeleton node without the mark contains no
/// references at all, so rehydrating it is the identity function.
final Expando<bool> _rewritten = Expando<bool>('forge.rewritten');

/// Mints a reference to [key].
EntityRef makeRef(EntityKey key) => EntityRef(key);

/// Whether [value] is a reference minted by this runtime.
bool isRef(Object? value) => value is EntityRef;

/// Marks a rebuilt skeleton container. Returns [node] for chaining.
T markRewritten<T extends Object>(T node) {
  _rewritten[node] = true;

  return node;
}

/// Whether [node] was marked by [markRewritten].
bool isRewritten(Object node) => _rewritten[node] ?? false;

/// `Order` + `7` becomes `Order:7`.
///
/// The id is rendered the way JavaScript's `String()` renders it, so numeric
/// `7`, `7.0` and string `'7'` are one key, and a key minted in Dart equals the
/// key the TypeScript runtime mints for the same record.
EntityKey entityKey(String typename, Object id) => '$typename:${jsString(id)}';

/// Whether a value can identify a record: a non-empty string, a finite
/// number, or a [BigInt]. Booleans, null, NaN and containers cannot.
bool isIdentity(Object? value) => switch (value) {
  String() => value.isNotEmpty,
  int() => true,
  double() => value.isFinite,
  BigInt() => true,
  _ => false,
};

/// Renders a scalar the way JavaScript's `String(value)` does.
///
/// Dart prints `7.0` for an integral double where JavaScript prints `7`, and a
/// cache key, a tag or a URL that differs between the two runtimes would split
/// one entity into two entries. Everything that renders a value into a key goes
/// through here.
String jsString(Object? value) {
  if (value is double &&
      value.isFinite &&
      value == value.truncateToDouble() &&
      value.abs() < 1e21) {
    return BigInt.from(value).toString();
  }

  return '$value';
}

/// Value equality that sees through references: two normalization passes mint
/// two [EntityRef] objects for `Order:7`, and they are the same value.
bool sameValue(Object? a, Object? b) {
  if (identical(a, b)) return true;
  if (a is EntityRef && b is EntityRef) return a.key == b.key;
  if (a is num && b is num) return a == b;
  if (a is String && b is String) return a == b;
  if (a is bool && b is bool) return a == b;
  if (a is BigInt && b is BigInt) return a == b;

  return false;
}
