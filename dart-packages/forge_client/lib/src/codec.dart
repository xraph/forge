/// The two codec layers a generated client supplies.
///
/// The wire codec renames keys between the server's JSON and the client-shaped
/// `Json` the store holds. The model codec turns client-shaped `Json` into a
/// typed model. The store only ever sees client-shaped values.
library;

/// Renames between the server's JSON and the client-shaped value.
///
/// Generated packages emit one `const` implementation per schema; the runtime
/// never looks inside it.
abstract interface class WireCodec {
  /// Server JSON to client-shaped.
  Object? decode(Object? wire);

  /// Client-shaped to server JSON.
  Object? encode(Object? client);
}

/// Builds a typed model from a client-shaped value.
typedef FromClient<T> = T Function(Object? client);

/// Turns a typed model back into its client-shaped value.
typedef ToClient<T> = Object? Function(T value);
