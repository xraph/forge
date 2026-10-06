import 'dart:convert';

import 'package:forge_client/forge_client.dart';

/// How a queued write is drawn after a restart. The app's own optimistic
/// callback cannot be stored, so this serialisable description is computed
/// when the write is queued and turned back into an [Optimistic] on restore.
sealed class OverlayIntent {
  /// Base constructor.
  const OverlayIntent();

  /// The optimistic spec to pass to `QueryCache.mutate`, or null for none.
  Optimistic<Object?>? toOptimistic();

  /// The stored form, or null for [NoOverlay].
  String? encode();

  /// Rebuilds an intent stored by [encode].
  static OverlayIntent decode(String? source) {
    if (source == null) return const NoOverlay();

    final json = jsonDecode(source);
    if (json is! Map<String, Object?>) {
      throw FormatException('overlay intent is not an object', source);
    }

    return switch (json['kind']) {
      'merge' => MergeOverlay(
        json['key']! as String,
        Map<String, Object?>.of(json['patch']! as Map<String, Object?>),
      ),
      'delete' => DeleteOverlay(json['key']! as String),
      final kind => throw FormatException('unknown overlay intent', kind),
    };
  }
}

/// Draw nothing.
final class NoOverlay extends OverlayIntent {
  /// Creates the intent.
  const NoOverlay();

  @override
  Optimistic<Object?>? toOptimistic() => null;

  @override
  String? encode() => null;
}

/// Merge [patch] over the record [key].
final class MergeOverlay extends OverlayIntent {
  /// Merges [patch] into [key].
  const MergeOverlay(this.key, this.patch);

  /// The entity drawn over.
  final EntityKey key;

  /// The top-level fields replaced.
  final Map<String, Object?> patch;

  @override
  Optimistic<Object?> toOptimistic() => OptimisticUpdate<Object?>(
    (previous) => previous is Map<String, Object?>
        ? <String, Object?>{...previous, ...patch}
        : previous,
    key: key,
  );

  @override
  String encode() => jsonEncode({'kind': 'merge', 'key': key, 'patch': patch});
}

/// Hide the record [key].
final class DeleteOverlay extends OverlayIntent {
  /// Hides [key].
  const DeleteOverlay(this.key);

  /// The entity hidden.
  final EntityKey key;

  @override
  Optimistic<Object?> toOptimistic() => OptimisticDelete<Object?>(key: key);

  @override
  String encode() => jsonEncode({'kind': 'delete', 'key': key});
}

/// The default intent: a PATCH or PUT with a map body merges the body into the
/// entity the write targets, a DELETE hides it, and anything else (a create,
/// a write with no single target) draws nothing.
OverlayIntent deriveOverlayIntent(OperationMeta meta, TagContext args) {
  final EntityKey? key;
  try {
    key = targetOf(meta, args);
  } on AmbiguousTargetError {
    return const NoOverlay();
  }
  if (key == null) return const NoOverlay();

  final body = args.body;
  return switch (meta.method.toUpperCase()) {
    'DELETE' => DeleteOverlay(key),
    'PATCH' || 'PUT' when body is Map<String, Object?> => MergeOverlay(
      key,
      Map<String, Object?>.of(body),
    ),
    _ => const NoOverlay(),
  };
}
