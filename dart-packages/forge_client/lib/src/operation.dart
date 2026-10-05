import 'codec.dart';

/// One operation, exactly as the generated `ops.dart` declares it.
final class OperationMeta {
  /// Creates an operation row. Generated code declares these as `const`.
  const OperationMeta({
    required this.id,
    required this.method,
    required this.path,
    this.entity,
    this.rootType,
    this.staleTime,
    this.provides = const [],
    this.invalidates = const [],
    this.security = const [],
    this.bodyCodec,
    this.responseCodec,
    this.idempotent = false,
    this.requestContentType,
  });

  /// The generated table key, e.g. `op_get_order`. Used by the outbox to turn
  /// a persisted operation back into its row. The cache keys queries by
  /// [method] and [path], as TypeScript does, so cache keys match across
  /// runtimes.
  final String id;

  /// The HTTP method, upper case.
  final String method;

  /// The path template, e.g. `/orders/{id}`.
  final String path;

  /// The entity this operation's cache contract is about.
  final String? entity;

  /// The typename of the response document (of its elements for a bare
  /// list). Indexes the entities table when normalizing a response; falls back
  /// to [entity] when absent.
  final String? rootType;

  /// How long this operation's result stays fresh. Null means "use the cache
  /// default".
  final Duration? staleTime;

  /// Tag templates this operation provides.
  final List<String> provides;

  /// Tag templates this operation invalidates.
  final List<String> invalidates;

  /// The security scheme names this operation declares.
  final List<String> security;

  /// Renames the request body from client shape to wire shape.
  final WireCodec? bodyCodec;

  /// Renames the response from wire shape to client shape.
  final WireCodec? responseCodec;

  /// Whether the route uses Forge's idempotency middleware
  /// (`x-forge-idempotent`). Read by the outbox; the transport does not retry
  /// on it.
  final bool idempotent;

  /// The content type the request body is sent as. Null means JSON, which is
  /// what every row without a body, or with a plain `application/json` body,
  /// leaves it as.
  ///
  /// The transport encodes the body by the one content-type rule every Forge
  /// Dart client applies: a JSON type (`application/json`, `text/json`, any
  /// `+json`) is JSON-encoded; `application/x-www-form-urlencoded` sends a
  /// map's fields urlencoded; a text type sends the string as it is; anything
  /// else sends the `Uint8List` untouched. Each goes out under this type.
  final String? requestContentType;
}

/// The arguments of one call, in the vocabulary the tag resolver, the URL
/// builder and the cache key all speak. Every value is client-shaped.
final class TagContext {
  /// Creates a context.
  const TagContext({
    this.path = const {},
    this.query = const {},
    this.headers = const {},
    this.body,
  });

  /// Path parameters by name.
  final Map<String, Object?> path;

  /// Query parameters by name. A null value is skipped.
  final Map<String, Object?> query;

  /// Header parameters by name.
  final Map<String, String> headers;

  /// The request body, client-shaped. Null means no body.
  final Object? body;

  /// The context of an operation that takes no arguments.
  static const empty = TagContext();

  /// Whether this context carries nothing at all. An empty context keys a
  /// query exactly as the TypeScript runtime keys a call with no arguments.
  bool get isEmpty =>
      path.isEmpty && query.isEmpty && headers.isEmpty && body == null;
}

/// What every generated `*Args` class implements.
abstract interface class OperationArgs {
  /// Gathers path, query, header and body parameters into a [TagContext].
  TagContext toTagContext();
}

/// The arguments of an operation that takes none.
final class NoArgs implements OperationArgs {
  /// Creates the empty arguments.
  const NoArgs();

  @override
  TagContext toTagContext() => TagContext.empty;
}

/// Wraps an optional PATCH field so "unchanged" and "set to null" differ.
///
/// Generated PATCH args default every optional field to `const Unchanged()`;
/// `toTagContext()` omits an unchanged field from the body and writes null for
/// `Assign(null)`. Equality is by variant and value, because generated args
/// compare by value and adapters key on them.
sealed class Value<T> {
  /// Const base constructor.
  const Value();
}

/// Leave the field as the server has it.
final class Unchanged<T> extends Value<T> {
  /// Creates the "leave unchanged" marker.
  const Unchanged();

  @override
  bool operator ==(Object other) => other is Unchanged;

  @override
  int get hashCode => (Unchanged).hashCode;
}

/// Set the field, possibly to null.
final class Assign<T> extends Value<T> {
  /// Creates an assignment of [value].
  const Assign(this.value);

  /// The value to write. Null clears the field.
  final T? value;

  @override
  bool operator ==(Object other) => other is Assign && other.value == value;

  @override
  int get hashCode => Object.hash(Assign, value);
}
