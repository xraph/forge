package dart

import (
	"fmt"
	"strings"
)

// errorClass is one subclass of the sealed ApiError.
type errorClass struct {
	name   string
	status int
}

// errorClasses are the statuses the spec names, in the order the factory
// switches over them. Any other non-2xx status becomes UnexpectedStatus.
var errorClasses = []errorClass{
	{"BadRequest", 400},
	{"Unauthorized", 401},
	{"Forbidden", 403},
	{"NotFound", 404},
	{"Conflict", 409},
	{"UnprocessableEntity", 422},
	{"TooManyRequests", 429},
	{"InternalServerError", 500},
	{"ServiceUnavailable", 503},
	{"GatewayTimeout", 504},
}

// generatedTypeNames are the type names the generated package itself
// declares outside its models.
var generatedTypeNames = []string{
	"ApiError", "UnexpectedStatus", "RestClient", "CredentialsProvider", "RestClientPagination",
	"Page", "PageParams", "Scope", "Role", "Permission", "OperationName",
	"Int64", "Json", "Value", "Unchanged", "Assign", "WireCodec",
	"RoomClient", "RoomMessage", "RoomEvent", "RoomMessageReceived", "RoomMemberJoined",
	"RoomMemberLeft", "RoomFailure", "PresenceClient", "UserPresence", "TypingClient",
	"TypingEvent", "ChannelClient", "ChannelMessage", "StreamingClient", "LiveConnection",
}

// forgeClientTypeNames are the types package:forge_client exports. An app
// imports that package and the generated one together, so a model with one of
// these names would make every use of it ambiguous.
var forgeClientTypeNames = []string{
	"EntityMeta", "EntityRef", "EntityRecord", "NormalizeResult", "OperationMeta", "TagContext",
	"OperationArgs", "NoArgs", "ResolvedTags", "Scheduler", "ManualScheduler", "Transport",
	"TransportRequest", "AuthProvider", "Clock", "ManualClock", "RetryPolicy", "HttpStatusError",
	"MissingPathParamsError", "RequestEvent", "RestTransport", "QueryState", "QueryIdle",
	"QueryLoading", "QuerySuccess", "QueryFailure", "MutationState", "MutationIdle",
	"MutationPending", "MutationSuccess", "MutationFailure", "Optimistic", "OptimisticUpdate",
	"OptimisticDelete", "OptimisticMany", "QueryCache", "RequestOptions", "MutateOptions",
	"CommitScheduler", "QueryBinding", "QueryRef", "MutationBinding", "FocusSignal",
	"ConnectivitySignal", "StreamIntent", "StreamBinding", "EntityStreamBinding",
	"DuplexStreamBinding", "StreamConnection", "StreamConnectContext", "TransportUnavailable",
	"BackoffPolicy", "SubscriptionManager", "StreamBinder", "SnapshotMode", "Snapshot",
	"HydrationFailure", "SyncSource", "SyncContext", "PendingMutation", "MutationOutcome",
	"Applied", "Queued", "Rejected", "SyncStatus", "Synced", "Pending", "Offline", "SyncFailed",
	"StorageAdapter", "StorageSession", "PendingMutationRecord", "KeyValueStore",
	"KeyValueBatch", "CacheEvent", "CacheObserver", "SecurityScheme", "SyncDeclaration",
	"EntityKey", "EntitySchema", "FromClient", "ToClient", "Sleep", "RequestObserver",
	"StreamConnect", "Placement", "FrameDecoder", "OptimisticCreate", "RequestReport",
	"RequestStarted", "RequestAttempt", "RequestRefresh", "RequestRefreshed", "RequestRetried",
	"RequestSettled", "OutboxInspector", "DevtoolsInspectable", "AmbiguousTargetError",
	"StreamFrame", "QueryStatus", "TrackedRecord", "RequestAbandoned", "QueryRegistry",
	"Invalidator", "EntityStore", "OverlayStack", "CachedQuery", "RestoreInput",
	"BinderSnapshot", "ChannelBindings", "ChannelSnapshot", "CommitOptions", "CreatePatch",
	"DecodedFrame", "DeletePatch", "EntityPatch", "FrameHandler", "FramesCommitted", "Keepalive",
	"LiveBinding", "LiveQuerySnapshot", "MergePatch", "MergeSource", "MutationCommitted",
	"MutationSettled", "OutboxEnqueued", "OutboxFailed", "OutboxFailureSource", "OutboxReplayed",
	"OverlayEntry", "OverlayHost", "OverlayLayer", "QueryEntry", "QueryInvalidated", "QueryPlaced",
	"QuerySpec", "QueryTransition", "ReceiveOnlyConnection", "ResolvedPatches", "SettleResult",
	"SocketSnapshot", "StagedWrite", "SubscribeOptions", "SyncStatusChanged", "Unmount",
}

// dartCoreTypeNames are dart:core and dart:async types the generated code
// names, which a model must not shadow.
var dartCoreTypeNames = []string{
	"Object", "String", "List", "Map", "Set", "Iterable", "Future", "Stream", "Duration",
	"DateTime", "Uri", "Type", "Symbol", "Null", "Never", "Record", "Enum", "Error",
	"Exception", "Comparable", "Pattern", "RegExp", "BigInt", "MapEntry", "StackTrace",
	"Uint8List", "Timer", "Completer", "StreamController", "StreamSubscription",
	"TimeoutException", "FormatException", "StateError",
}

// ReservedIdentifiers returns the type names a schema must not take in a
// generated Dart package: its own declarations, forge_client's exports and the
// core types its code names. StripPrefix leaves a schema prefixed rather than
// strip it onto one of these, and the model registry renames a schema that
// already has one by adding Model.
func ReservedIdentifiers() map[string]bool {
	reserved := map[string]bool{}

	for _, list := range [][]string{generatedTypeNames, forgeClientTypeNames, dartCoreTypeNames} {
		for _, name := range list {
			reserved[name] = true
		}
	}

	for _, ec := range errorClasses {
		reserved[ec.name] = true
	}

	return reserved
}

// renderErrors renders lib/src/errors.dart. With hooks it also maps
// forge_client's HttpStatusError, which is what a binding's failure carries.
func renderErrors(hooks bool) string {
	var b strings.Builder

	b.WriteString(generatedHeader)

	if hooks {
		b.WriteString("\nimport 'package:forge_client/forge_client.dart' show HttpStatusError;\n")
	}

	b.WriteString(`
/// A non-2xx response, typed by status so a ` + "`switch`" + ` over it is exhaustive.
sealed class ApiError implements Exception {
  /// Const base constructor.
  const ApiError(this.status, this.body, {this.headers = const {}});

  /// Builds the subclass matching [status].
  factory ApiError.fromResponse(
    int status,
    Object? body, {
    Map<String, String> headers = const {},
  }) => switch (status) {
`)

	for _, ec := range errorClasses {
		fmt.Fprintf(&b, "    %d => %s(body, headers: headers),\n", ec.status, ec.name)
	}

	b.WriteString(`    _ => UnexpectedStatus(status, body, headers: headers),
  };

  /// The HTTP status code.
  final int status;

  /// The decoded response body: JSON when it parsed, otherwise the text.
  final Object? body;

  /// The response headers, lower-cased.
  final Map<String, String> headers;

  /// The server's ` + "`message`" + ` or ` + "`error`" + ` field, else the ` + "`detail`" + ` or
  /// ` + "`title`" + ` of an RFC 7807 problem, when the body carries one.
  String? get message => switch (body) {
    {'message': final String text} => text,
    {'error': final String text} => text,
    {'detail': final String text} => text,
    {'title': final String text} => text,
    final String text when text.isNotEmpty => text,
    _ => null,
  };

  @override
  String toString() => 'ApiError($status${message == null ? '' : ': $message'})';
}
`)

	for _, ec := range errorClasses {
		fmt.Fprintf(&b, "\n/// HTTP %d.\n", ec.status)
		fmt.Fprintf(&b, "final class %s extends ApiError {\n", ec.name)
		fmt.Fprintf(&b, "  /// Creates a %d error.\n", ec.status)
		fmt.Fprintf(&b, "  const %s(Object? body, {super.headers}) : super(%d, body);\n", ec.name, ec.status)
		b.WriteString("}\n")
	}

	b.WriteString(`
/// Any other non-2xx status.
final class UnexpectedStatus extends ApiError {
  /// Creates an error for [status].
  const UnexpectedStatus(super.status, super.body, {super.headers});
}
`)

	if hooks {
		b.WriteString(`
/// Maps a failure from ` + "`forge_client`" + ` (an ` + "`HttpStatusError`" + `) or from the
/// generated REST client to an [ApiError], or returns null for anything else.
ApiError? apiErrorOf(Object error) => switch (error) {
  final ApiError typed => typed,
  HttpStatusError(:final status, :final body, :final headers) =>
    ApiError.fromResponse(status, body, headers: headers),
  _ => null,
};
`)
	}

	return b.String()
}
