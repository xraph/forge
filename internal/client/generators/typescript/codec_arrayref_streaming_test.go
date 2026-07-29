package typescript

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/xraph/forge/internal/client"
)

// arrayRefWSSpec returns a minimal spec with exactly one WebSocket endpoint
// whose SendSchema and ReceiveSchema are both `{type: array, items: $ref
// User}` -- the single most common OpenAPI "list of X" wire shape
// (arrayRefCodecID's own doc comment, codecs.go), and the shape Finding 2
// (task 5c review) identified as silently un-registered for WebSocket/SSE/
// WebTransport endpoints.
//
// When includeUnrelatedListEndpoint is true, an unrelated REST endpoint
// (GET /users -> User[]) is added -- one that has NOTHING to do with the
// WebSocket endpoint above, beyond sharing the User schema. Before this
// task's fix, THIS unrelated endpoint was the only thing that could ever
// cause registerEndpointArrayBodyCodecs to register the "[]User" entry (it
// only ever walked spec.Endpoints), so whether the WebSocket payload actually
// got renamed at runtime depended entirely on whether some unrelated REST
// endpoint happened to also return/accept an array of the same item schema --
// exactly the non-obvious, spooky-action-at-a-distance bug this test pins
// closed.
func arrayRefWSSpec(includeUnrelatedListEndpoint bool) *client.APISpec {
	user := &client.Schema{
		Type:     "object",
		Required: []string{"id"},
		Properties: map[string]*client.Schema{
			"id":      {Type: "string"},
			"user_id": {Type: "string"},
		},
	}

	spec := &client.APISpec{
		Info:    client.APIInfo{Title: "Array Ref WS API", Version: "1.0.0"},
		Schemas: map[string]*client.Schema{"User": user},
		WebSockets: []client.WebSocketEndpoint{
			{
				ID:            "userList",
				Path:          "/ws/users",
				SendSchema:    &client.Schema{Type: "array", Items: &client.Schema{Ref: "#/components/schemas/User"}},
				ReceiveSchema: &client.Schema{Type: "array", Items: &client.Schema{Ref: "#/components/schemas/User"}},
			},
		},
	}

	if includeUnrelatedListEndpoint {
		spec.Endpoints = []client.Endpoint{
			{
				Method: "GET", Path: "/users", OperationID: "users.list",
				Responses: map[int]*client.Response{200: {Content: map[string]*client.MediaType{
					"application/json": {Schema: &client.Schema{Type: "array", Items: &client.Schema{Ref: "#/components/schemas/User"}}}}}},
			},
		}
	}

	return spec
}

// TestArrayRefWSSchemaCodecRegisteredRegardlessOfUnrelatedRESTEndpoint is the
// regression test for Finding 2 (task 5c review): schemaCodecRef (rest.go)
// resolves `{type: array, items: $ref User}` to the id "[]User" for
// messageCodecRef (websocket.go) exactly as it does for an ordinary REST
// endpoint body/response, but registerEndpointArrayBodyCodecs (codecs.go)
// previously only ever walked spec.Endpoints to actually REGISTER that id in
// the codec table -- spec.WebSockets/spec.SSEs were never walked. That left
// "[]User" resolving to a real id with NO warning (schemaCodecRef legitimately
// recognises the shape) and NO table entry either, so decode()/encode()
// silently found nothing under "[]User" and passed the array through
// completely unrenamed -- a defect invisible from websocket.ts's own source
// (byte-identical either way) and only reachable by checking codecs.ts and
// actual runtime behaviour.
//
// Measured BEFORE this task's fix (quoting the task brief's own reproduction):
//
//	WITHOUT an unrelated GET /users -> User[]:  codecs.ts has "[]User"=false, warnings=[]
//	  runtime: {"decoded":[{"user_id":"x"}],"encoded":[{"userId":"y"}]}    <- no rename
//	WITH    an unrelated GET /users -> User[]:  codecs.ts has "[]User"=true,  warnings=[]
//	  runtime: {"decoded":[{"userId":"x"}],"encoded":[{"user_id":"y"}]}    <- renamed
//
// This test drives BOTH variants and asserts they now behave IDENTICALLY --
// both renamed, regardless of whether the unrelated REST endpoint exists.
func TestArrayRefWSSchemaCodecRegisteredRegardlessOfUnrelatedRESTEndpoint(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		includeUnrelatedList bool
	}{
		{"without unrelated REST endpoint", false},
		{"with unrelated REST endpoint", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spec := arrayRefWSSpec(tc.includeUnrelatedList)
			config := baseConfig()

			out, err := NewGenerator().Generate(context.Background(), spec, config)
			require.NoError(t, err)

			require.Contains(t, out.Files, "src/codecs.ts")
			require.Contains(t, out.Files, "src/websocket.ts")
			assert.Empty(t, out.Warnings, "the array-of-$ref shape resolves cleanly; no warning should fire either way")

			codecs := out.Files["src/codecs.ts"]
			assert.Contains(t, codecs, `"[]User":`,
				"the \"[]User\" entry must be registered in the codec table regardless of whether an unrelated REST endpoint also references User[]")

			ws := out.Files["src/websocket.ts"]
			assert.Contains(t, ws, `decode(JSON.parse(data), "[]User")`)
			assert.Contains(t, ws, `encode(message, "[]User")`)

			dir := t.TempDir()
			writeTree(t, dir, out.Files)
			writeTree(t, dir, map[string]string{"src/__setup_ws_sse.ts": wsSSEFakeSetup})

			if errs := typeCheck(t, dir); len(errs) != 0 {
				t.Fatalf("generated array-ref WS client must type-check with zero errors, got:\n%s", strings.Join(errs, "\n"))
			}

			driver := `
import './__setup_ws_sse';
import { UserListWSClient } from './websocket';

async function main() {
  const wsClient = new UserListWSClient({ baseURL: 'http://example.invalid' });
  await wsClient.connect();

  const fakeWS = (globalThis as any).__lastFakeWS;

  const received: any[] = [];
  wsClient.onMessage((msg) => received.push(msg));

  // Wire frame: an ARRAY of snake_case items.
  fakeWS.onmessage({
    data: JSON.stringify([{ id: 'i', user_id: 'x' }]),
  });

  // Outgoing: an ARRAY of camelCase items.
  await wsClient.send([{ id: 'i', userId: 'y' }] as any);

  console.log(JSON.stringify({
    decoded: received,
    encoded: fakeWS.sent.map((s: string) => JSON.parse(s)),
  }));

  wsClient.close();
}

main().catch((err) => {
  console.error(err);
  throw err;
});
`
			writeTree(t, dir, map[string]string{"src/__driver_arrayref_ws.ts": driver})

			stdout := runNodeDriver(t, dir, "src/__driver_arrayref_ws.ts")

			var result struct {
				// Each of these is a []Message, where a Message is itself an
				// ARRAY of User items (SendSchema/ReceiveSchema are both
				// `{type: array, items: $ref User}`) -- one message was sent
				// on each side, so exactly one element is expected at the
				// outer level, itself wrapping the one-item array payload.
				Decoded [][]map[string]any `json:"decoded"`
				Encoded [][]map[string]any `json:"encoded"`
			}
			decodeLastLine(t, stdout, &result)

			require.Len(t, result.Decoded, 1, "stdout:\n%s", stdout)
			require.Len(t, result.Decoded[0], 1, "the decoded message must contain exactly one item; stdout:\n%s", stdout)
			assert.Equal(t, "x", result.Decoded[0][0]["userId"],
				"each item of an array-of-$ref WS message must decode (user_id -> userId), regardless of an unrelated REST endpoint; stdout:\n%s", stdout)

			require.Len(t, result.Encoded, 1, "stdout:\n%s", stdout)
			require.Len(t, result.Encoded[0], 1, "the encoded message must contain exactly one item; stdout:\n%s", stdout)
			assert.Equal(t, "y", result.Encoded[0][0]["user_id"],
				"each item of an array-of-$ref WS message must encode (userId -> user_id), regardless of an unrelated REST endpoint; stdout:\n%s", stdout)
		})
	}
}
