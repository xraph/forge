# Specification compliance

The oRPC extension speaks [JSON-RPC 2.0](https://www.jsonrpc.org/specification) on its endpoint (`POST /rpc` by default) and describes its methods with an [OpenRPC 1.3.2](https://spec.open-rpc.org/) document (`GET /rpc/schema`). This page lists where it follows each spec and where it doesn't, as the code stands today. The request handling is in `extension.go` (`handleJSONRPC`, `parseBatchRequest`) and `server.go` (`HandleRequest`, `HandleBatch`, `OpenRPCDocument`), and the wire types are in `types.go`.

It is not fully compliant with either spec. The gaps that matter most are notifications, which still get a response, and the OpenRPC `params` description, which doesn't match the shape the server expects.

## JSON-RPC 2.0

### What follows the spec

| Spec requirement | What the extension does |
| --- | --- |
| `jsonrpc` must be exactly `"2.0"` | Anything else gets `-32600` with the message `Invalid JSON-RPC version` |
| `method` must be a string | A missing or empty method gets `-32600`. A non-string method fails to decode and gets `-32600` (`Invalid request format`) |
| `id` may be a string, number or null | Stored as `any` and echoed back unchanged in the response |
| A response has `result` on success and `error` on failure, never both | `NewSuccessResponse` and `NewErrorResponse` build one or the other |
| Error objects have an integer `code`, a string `message` and optional `data` | `Error{Code, Message, Data}` |
| Reserved error codes | `ErrParseError` (-32700), `ErrInvalidRequest` (-32600), `ErrMethodNotFound` (-32601), `ErrInvalidParams` (-32602), `ErrInternalError` (-32603) and `ErrServerError` (-32000) |
| Invalid JSON gets `-32700` with a null `id` | Yes, for a single request or a batch |
| A batch is an array, answered with an array | Requests run one after another, so responses come back in request order |
| An empty batch array is an invalid request | Answered with a single `-32600` error object (`empty batch`) |

### Where it doesn't

| Spec requirement | What the extension does instead |
| --- | --- |
| The server must not reply to a notification (a request without `id`) | Every request gets a response. A notification is answered with `"id": null` |
| A batch made only of notifications gets no response at all, and notifications inside a batch get no response object | Every element of a batch gets a response object |
| An invalid element in a batch (for example `[1, {...}]`) gets its own `-32600` response while the valid ones still run | One bad element rejects the whole batch with a single `-32600` error object |
| `result` is required on success | `Result` is tagged `omitempty`, so a method that returns a nil result (or a route that answers with JSON `null`) produces a response with neither `result` nor `error` |
| `params` may be an array (by position) or an object (by name) | Route-backed methods accept only an object. An array fails when it's converted to a request and comes back as `-32603`, not `-32602`. Methods you register yourself get whatever was decoded, so they can accept arrays |
| `-32602` for invalid parameters | Only returned when `params` can't be decoded as JSON, which the earlier parse step normally catches first. Bad params for a route surface as `-32603` |

Errors from a route or an interceptor are returned as `-32603` with the error text as the message. A route that answers with an HTTP status of 400 or more becomes `-32603` with `route returned status <code>`.

A few things the spec leaves to the server:

- Request too large, batches turned off (`enable_batch: false`), and a batch over `batch_limit` are answered with `-32000`. Turned-off batches get a single error object, an oversized batch gets an array with one error in it.
- The HTTP status is 200 for every JSON-RPC response, including errors. The only exception is a 500 when the oRPC service can't be resolved from the container.
- `Request.IsNotification()` exists and reports whether `id` is absent or null, but the request path doesn't use it.

## OpenRPC 1.3.2

`GET /rpc/schema` returns an `OpenRPCDocument`. Methods are sorted by name, so the document is stable between builds.

### What follows the spec

| Spec requirement | What the extension does |
| --- | --- |
| `openrpc` version string | `"1.3.2"` |
| `info` with `title` and `version` | From `server_name` and `server_version` (`WithServerInfo`). `description` is always `JSON-RPC 2.0 API` |
| `methods` array | One entry per registered method |
| Method `name` | The JSON-RPC method name |
| Method `summary`, `description`, `tags`, `deprecated` | `summary` and `description` both carry the route summary. Tags are Tag Objects with a `name` |
| Content Descriptors have `name` and `schema` | The params and result descriptors always have both |

### Where it doesn't

| Spec requirement | What the extension does instead |
| --- | --- |
| A method's `params` describe what the client sends | Each method gets one Content Descriptor named `params` whose schema is the whole params object. With OpenRPC's default `paramStructure` of `either`, a client reading the document would send `[{...}]` or `{"params": {...}}`, but the server expects the object itself (`{"id": "42", "body": {...}}`) |
| `params` is required on every method (it may be empty) | Tagged `omitempty`, so it's left out when a method has no params schema, which can happen for methods you register yourself |
| A Server Object has a `name` | `servers` holds one entry with only `url`, set to the endpoint path (for example `/rpc`) |
| `errors` lists the errors a method can return | Never filled in |
| `components` for reusable schemas | Never filled in. Schemas are inlined |

Where the params and result schemas come from is covered in the [README](README.md#openrpc-schema): `forge.WithORPCParams` and `forge.WithORPCResult` first, then the route's OpenAPI schemas, then a generated fallback.

## Tests

`server_test.go`, `comprehensive_test.go`, `types_test.go`, `errors_test.go` and `determinism_test.go` cover request validation, error codes, batches, method naming, OpenRPC generation and the sorted method order. Run them from this directory with `GOWORK=off go test -race ./...`. None of them assert the notification or batch-element behaviour described above.
