package dart

import "strings"

// renderSupport renders lib/src/support.dart: the value types and decode
// helpers every model shares.
//
// With hooks the package depends on forge_client, and Json, Value, Unchanged,
// Assign and WireCodec are re-exported from it so a model built here is the
// type the runtime's bindings expect. Without hooks the package depends on
// package:http alone, so the same five are declared here instead.
func renderSupport(hooks bool) string {
	var b strings.Builder

	b.WriteString(generatedHeader)
	b.WriteString("\nimport 'dart:convert';\nimport 'dart:typed_data';\n")

	if hooks {
		b.WriteString(supportForgeClient)
	} else {
		b.WriteString(supportStandalone)
	}

	b.WriteString(supportHelpers)

	return b.String()
}

const supportForgeClient = `
import 'package:forge_client/forge_client.dart' show Assign, Unchanged, Value;

export 'package:forge_client/forge_client.dart'
    show Assign, Json, Unchanged, Value, WireCodec, decodeCached;
`

const supportStandalone = `
/// A client-shaped JSON object: wire keys already renamed by a codec.
typedef Json = Map<String, Object?>;

/// Converts between the server's JSON and the client-shaped JSON models read.
abstract interface class WireCodec {
  /// Server JSON to client-shaped.
  Object? decode(Object? wire);

  /// Client-shaped to server JSON.
  Object? encode(Object? client);
}

/// An optional field that can be left alone or explicitly set, including to
/// null.
sealed class Value<T> {
  /// Const base constructor.
  const Value();
}

/// Leave the field as it is.
final class Unchanged<T> extends Value<T> {
  /// Const constructor.
  const Unchanged();

  @override
  bool operator ==(Object other) => other is Unchanged;

  @override
  int get hashCode => (Unchanged).hashCode;
}

/// Set the field to [value], which may be null.
final class Assign<T> extends Value<T> {
  /// Wraps [value].
  const Assign(this.value);

  /// The new value.
  final T? value;

  @override
  bool operator ==(Object other) => other is Assign && other.value == value;

  @override
  int get hashCode => Object.hash(Assign, value);
}
`

const supportHelpers = `
/// A 64-bit integer carried as its decimal string, so it survives a web build
/// where Dart's ` + "`int`" + ` is a JavaScript double.
extension type const Int64(String value) implements Object {
  /// Parses [value] as an arbitrary-precision integer.
  BigInt toBigInt() => BigInt.parse(value);

  /// Parses [value] as a native ` + "`int`" + `. Loses precision above 2^53 on the web.
  int toInt() => int.parse(value);
}

/// Reads a client-shaped JSON object.
Map<String, Object?> decodeObject(Object? value) =>
    (value as Map<Object?, Object?>).cast<String, Object?>();

/// Reads an unmodifiable list, decoding each element with [item].
List<T> decodeList<T>(Object? value, T Function(Object?) item) =>
    List<T>.unmodifiable((value as List<Object?>).map(item));

/// Reads an unmodifiable string-keyed map, decoding each value with [item].
Map<String, T> decodeMap<T>(Object? value, T Function(Object?) item) =>
    Map<String, T>.unmodifiable(
      decodeObject(value).map((key, v) => MapEntry(key, item(v))),
    );

/// Applies [decode] unless [value] is null.
T? decodeNullable<T>(Object? value, T Function(Object?) decode) =>
    value == null ? null : decode(value);

/// Applies [encode] unless [value] is null.
Object? encodeNullable<T extends Object>(T? value, Object? Function(T) encode) =>
    value == null ? null : encode(value);

/// Reads a JSON number as a ` + "`double`" + `.
double decodeDouble(Object? value) => (value as num).toDouble();

/// Reads a JSON number as an ` + "`int`" + `.
int decodeInt(Object? value) => (value as num).toInt();

/// Reads an int64 carried as a string or a number.
Int64 decodeInt64(Object? value) => switch (value) {
  final String text => Int64(text),
  final int number => Int64(number.toString()),
  final num number => Int64(number.toStringAsFixed(0)),
  _ => throw FormatException('not an int64: $value'),
};

/// Reads an int64 carried as a decimal string or a number, as a native ` + "`int`" + `.
int decodeIntOrString(Object? value) => switch (value) {
  final String text => int.parse(text),
  final num number => number.toInt(),
  _ => throw FormatException('not an int64: $value'),
};

/// Reads an ISO-8601 timestamp.
DateTime decodeDateTime(Object? value) => DateTime.parse(value as String);

/// Reads base64-encoded bytes.
Uint8List decodeBytes(Object? value) => base64Decode(value as String);

/// Writes a calendar date as ` + "`yyyy-mm-dd`" + `.
String encodeDate(DateTime value) => value.toIso8601String().substring(0, 10);

/// Writes bytes as base64.
String encodeBytes(Uint8List value) => base64Encode(value);

/// Structural equality over lists, maps and scalars.
bool deepEquals(Object? a, Object? b) {
  if (identical(a, b)) return true;
  if (a is List<Object?> && b is List<Object?>) {
    if (a.length != b.length) return false;
    for (var i = 0; i < a.length; i++) {
      if (!deepEquals(a[i], b[i])) return false;
    }
    return true;
  }
  if (a is Map<Object?, Object?> && b is Map<Object?, Object?>) {
    if (a.length != b.length) return false;
    for (final key in a.keys) {
      if (!b.containsKey(key) || !deepEquals(a[key], b[key])) return false;
    }
    return true;
  }
  return a == b;
}

/// A hash consistent with [deepEquals].
int deepHash(Object? value) => switch (value) {
  final List<Object?> list => Object.hashAll(list.map(deepHash)),
  final Map<Object?, Object?> map => Object.hashAllUnordered(
    map.entries.map((e) => Object.hash(e.key, deepHash(e.value))),
  ),
  _ => value.hashCode,
};

/// Equality for [Value] fields of generated argument classes.
bool valueEquals<T>(Value<T> a, Value<T> b) => switch ((a, b)) {
  (Unchanged<T>(), Unchanged<T>()) => true,
  (Assign<T>(value: final x), Assign<T>(value: final y)) => deepEquals(x, y),
  _ => false,
};

/// A hash consistent with [valueEquals].
int valueHash<T>(Value<T> value) => switch (value) {
  Unchanged<T>() => 0,
  Assign<T>(value: final v) => Object.hash(1, deepHash(v)),
};
`
