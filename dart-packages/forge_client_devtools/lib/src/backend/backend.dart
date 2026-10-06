import 'package:flutter/foundation.dart';

/// A decoded JSON object.
typedef Json = Map<String, Object?>;

/// Where the panel's calls go: the VM service in DevTools, a fake in tests.
abstract interface class ForgeBackend {
  /// True while the connected isolate has `ext.forge.hello` registered.
  ValueListenable<bool> get available;

  /// Calls one `ext.forge.*` method. Throws [BackendError] on failure.
  Future<Json> call(String method, [Map<String, String> params = const {}]);

  /// Every `forge:event` payload the app posts.
  Stream<Json> get events;
}

/// A failed extension call.
final class BackendError implements Exception {
  /// Creates the error.
  const BackendError(this.method, this.message);

  /// The method that failed.
  final String method;

  /// What the app or the VM said.
  final String message;

  @override
  String toString() => message;
}

/// Lenient readers over decoded JSON, so a missing field renders as empty
/// rather than throwing inside a build method.
extension JsonRead on Json {
  /// A string, or `''`.
  String str(String key) => switch (this[key]) {
    null => '',
    final String value => value,
    final Object other => '$other',
  };

  /// A string, or null.
  String? strOrNull(String key) => switch (this[key]) {
    null => null,
    final String value => value,
    final Object other => '$other',
  };

  /// An integer, or 0.
  int integer(String key) => switch (this[key]) {
    final num value => value.toInt(),
    _ => 0,
  };

  /// True only for a JSON `true`.
  bool flag(String key) => this[key] == true;

  /// A nested object, or an empty one.
  Json obj(String key) => objOrNull(key) ?? const {};

  /// A nested object, or null.
  Json? objOrNull(String key) => switch (this[key]) {
    final Map<String, Object?> value => value,
    _ => null,
  };

  /// A list of objects, or an empty list.
  List<Json> objs(String key) => switch (this[key]) {
    final List<Object?> value => [
      for (final item in value)
        if (item is Map<String, Object?>) item,
    ],
    _ => const [],
  };

  /// A list of strings, or an empty list.
  List<String> strings(String key) => switch (this[key]) {
    final List<Object?> value => [for (final item in value) '$item'],
    _ => const [],
  };
}
