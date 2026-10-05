/// Which entity keys a sync source owns: the one rule the cache, the frame
/// path and snapshots share, so that owned records only ever come from their
/// source. Internal; not exported from the package.
library;

import 'types.dart' show EntityKey;

/// The typename an entity key names: everything before its first colon.
String typenameOf(EntityKey key) {
  final colon = key.indexOf(':');

  return colon == -1 ? key : key.substring(0, colon);
}

/// [skip] plus every key among [keys] whose typename [owns], or [skip] itself
/// when none is owned.
Set<EntityKey>? withOwned(
  bool Function(String typename) owns,
  Set<EntityKey>? skip,
  Iterable<EntityKey> keys,
) {
  Set<EntityKey>? owned;

  for (final key in keys) {
    if (owns(typenameOf(key))) (owned ??= <EntityKey>{}).add(key);
  }

  if (owned == null) return skip;

  return {...?skip, ...owned};
}
