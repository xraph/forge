import 'ref.dart';
import 'types.dart';

/// Splits a response into a flat entity store and a skeleton of references.
///
/// [rootType] is the typename of [value] (or of its elements, when [value] is
/// a list) and comes from the generated `OperationMeta.rootType`. It is the
/// only way this runtime learns a typename: JSON carries none, and inferring
/// one from the presence of an `id` property is the guess the Go side refuses.
/// Descending past the root uses `schema[type].fields`; where that is absent
/// the subtree is left inline.
///
/// Pure. [value] is never mutated, and subtrees containing no entity are
/// returned by reference rather than copied. Cyclic graphs terminate.
NormalizeResult normalize(
  Object? value,
  String? rootType,
  EntitySchema schema,
) {
  final records = <EntityKey, Json>{};
  final deps = <EntityKey>{};

  // Source node to its skeleton, registered before the node's children are
  // walked, which is what makes a cyclic graph terminate.
  final seen = Map<Object, Object?>.identity();

  late final Object? Function(Object? node, String? type) walk;

  Object? walkList(List<Object?> node, String? type) {
    // A list does not change the typename: `[]Order` is a list of `Order`.
    final out = List<Object?>.filled(node.length, null);
    seen[node] = out;

    var changed = false;

    for (var i = 0; i < node.length; i++) {
      out[i] = walk(node[i], type);
      if (!identical(out[i], node[i])) changed = true;
    }

    // Nothing beneath this list referenced an entity, so the input is already
    // its own skeleton. A cycle back to this node would have produced `out`
    // somewhere below and flipped `changed`.
    if (!changed) {
      seen[node] = node;

      return node;
    }

    return markRewritten(out);
  }

  Object? walkMap(Map<String, Object?> node, String? type) {
    final meta = type == null ? null : schema[type];

    // A type with no idField is a signpost, walked for its fields and never
    // keyed.
    final idField = meta?.idField;
    final id = idField == null ? null : node[idField];
    final key = idField != null && isIdentity(id)
        ? entityKey(type!, id!)
        : null;

    final out = <String, Object?>{};
    EntityRef? ref;

    if (key == null) {
      seen[node] = out;
    } else {
      ref = makeRef(key);
      seen[node] = ref;
      deps.add(key);
    }

    var changed = false;

    for (final MapEntry(key: field, value: child) in node.entries) {
      final walked = walk(child, meta?.fields[field]);
      out[field] = walked;
      if (!identical(walked, child)) changed = true;
    }

    if (key != null) {
      final prev = records[key];

      // The same entity can occur twice in one response with different field
      // sets. Merging rather than replacing is the rule the store applies
      // across responses too.
      records[key] = prev == null ? out : {...prev, ...out};

      return ref;
    }

    if (!changed) {
      seen[node] = node;

      return node;
    }

    return markRewritten(out);
  }

  walk = (Object? node, String? type) => switch (node) {
    List<Object?>() ||
    Map<String, Object?>() when seen.containsKey(node) => seen[node],
    List<Object?>() => walkList(node, type),
    Map<String, Object?>() => walkMap(node, type),
    _ => node,
  };

  return NormalizeResult(
    skeleton: walk(value, rootType),
    records: records,
    deps: deps,
  );
}
