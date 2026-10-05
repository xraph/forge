/// One entry of the generated `securitySchemes` table: an OpenAPI security
/// scheme, reduced to what an `AuthProvider` dispatches on.
final class SecurityScheme {
  /// Creates a scheme row.
  const SecurityScheme({
    required this.type,
    this.scheme,
    this.name,
    this.location,
  });

  /// The OpenAPI scheme type: `http`, `apiKey`, `oauth2` or `openIdConnect`.
  final String type;

  /// For `http`: the authorization scheme, e.g. `bearer` or `basic`.
  final String? scheme;

  /// For `apiKey`: the header, query or cookie name that carries the key.
  final String? name;

  /// For `apiKey`: where the key goes, `header`, `query` or `cookie`.
  final String? location;
}
