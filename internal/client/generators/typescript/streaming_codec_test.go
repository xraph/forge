package typescript

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// wsSSEFakeSetup is written as its own module (src/__setup_ws_sse.ts) and
// imported FIRST, before './websocket'/'./sse', by every driver in this
// file. This ordering matters: websocket.ts/sse.ts each compute
// `const isBrowser = typeof window !== 'undefined' && typeof
// window.WebSocket !== 'undefined'` (or ...EventSource...) at MODULE-EVAL
// time, once, the moment the module is first evaluated. ES module
// evaluation order is depth-first over the static import graph, in source
// order at each level, so a sibling import with no dependencies of its own
// (this setup module) is fully evaluated before the next sibling import
// runs -- which is what lets this module install a fake window.WebSocket/
// window.EventSource global before websocket.ts/sse.ts ever read it. Without
// this ordering, isBrowser would be computed false (no `window` global
// exists in a bare Node process), sending both generated clients down the
// `require('ws')`/`require('eventsource')` Node branch instead, which is not
// what this test wants to exercise.
const wsSSEFakeSetup = `
class FakeWebSocket {
  static OPEN = 1;
  static CLOSED = 3;
  readyState = 1;
  onopen: ((ev?: any) => void) | null = null;
  onmessage: ((ev: any) => void) | null = null;
  onerror: ((ev: any) => void) | null = null;
  onclose: ((ev: any) => void) | null = null;
  sent: string[] = [];

  constructor(public url: string) {
    (globalThis as any).__lastFakeWS = this;
    setTimeout(() => { if (this.onopen) this.onopen(); }, 0);
  }

  send(data: string): void {
    this.sent.push(data);
  }

  close(): void {
    if (this.onclose) this.onclose({});
  }
}

class FakeEventSource {
  static CONNECTING = 0;
  static OPEN = 1;
  static CLOSED = 2;
  readyState = 1;
  onopen: ((ev?: any) => void) | null = null;
  onmessage: ((ev: any) => void) | null = null;
  onerror: ((ev: any) => void) | null = null;
  private listeners = new Map<string, Set<(ev: any) => void>>();

  constructor(public url: string, public opts?: any) {
    (globalThis as any).__lastFakeES = this;
    setTimeout(() => { if (this.onopen) this.onopen(); }, 0);
  }

  addEventListener(type: string, handler: (ev: any) => void): void {
    if (!this.listeners.has(type)) this.listeners.set(type, new Set());
    this.listeners.get(type)!.add(handler);
  }

  removeEventListener(type: string, handler: (ev: any) => void): void {
    this.listeners.get(type)?.delete(handler);
  }

  emit(type: string, data: string): void {
    const ev = { data, lastEventId: '' };
    this.listeners.get(type)?.forEach((h) => h(ev));
  }

  close(): void {}
}

(globalThis as any).window = {
  WebSocket: FakeWebSocket,
  EventSource: FakeEventSource,
};
`

// TestWebSocketAndSSEDecodeEncodeWirePayloads is the runtime execution proof
// that commit 5139609's regression is fixed: websocket.ts/sse.ts previously
// cast an incoming/outgoing payload straight through JSON.parse/
// JSON.stringify with no rename at all, so a spec-derived message type
// declaring camelCase properties (e.g. types.User's userId/createdAt) lied
// about what was actually on the wire (snake_case: user_id/created_at).
//
// This drives REAL generated code (websocket.ts's ChatWSClient and sse.ts's
// NotificationsSSEClient, from wsSSESpec()'s User-typed WS/SSE endpoints)
// under Node via esbuild, exactly as runtime_test.go's own e2e test drives
// RESTClient -- not encode()/decode() called directly, and not a hand-built
// config.
//
// Covers, in one driver:
//  1. an incoming WS frame {"user_id":"x"} surfaces to the handler as
//     {userId:'x'};
//  2. an outgoing WS send({userId:'x'}) puts {"user_id":"x"} on the wire;
//  3. an SSE event {"user_id":"x"} surfaces as {userId:'x'};
//  4. an unknown key not declared anywhere on User survives untouched in
//     both directions (WS in/out) and on the SSE side.
func TestWebSocketAndSSEDecodeEncodeWirePayloads(t *testing.T) {
	spec := wsSSESpec()
	config := baseConfig() // NamingCamel by default -- codecsNeeded(config) == true

	out, err := NewGenerator().Generate(context.Background(), spec, config)
	require.NoError(t, err)

	require.Contains(t, out.Files, "src/websocket.ts")
	require.Contains(t, out.Files, "src/sse.ts")
	require.Contains(t, out.Files, "src/codecs.ts")

	// Sanity checks on the fix's shape before ever running anything: the
	// raw, unrenamed casts from the regression must be gone, replaced by a
	// decode()/encode() call referencing the User codec id.
	ws := out.Files["src/websocket.ts"]
	assert.Contains(t, ws, "import { decode, encode } from './codecs';")
	assert.NotContains(t, ws, "JSON.parse(data);", "the raw, un-decoded cast from the regression must be gone")
	assert.Contains(t, ws, `decode(JSON.parse(data), "User")`)
	assert.Contains(t, ws, `encode(message, "User")`)

	sseCode := out.Files["src/sse.ts"]
	assert.Contains(t, sseCode, "import { decode } from './codecs';")
	assert.Contains(t, sseCode, `decode(JSON.parse(event.data), "User")`)
	// The reserved replay control events are the only listeners allowed to parse
	// without a codec, because no schema declares them and there is therefore no
	// codec id to route them through. Counted on the bare assignment rather than
	// on a particular declared type, so the guard holds whatever this fixture
	// happens to $ref: any un-decoded parse the generator reintroduces — typed,
	// unknown, or otherwise — pushes the count past two.
	assert.Equal(t, 2, strings.Count(sseCode, "= JSON.parse(event.data);"),
		"only forge.resumed and forge.gap may parse without a codec")

	dir := t.TempDir()
	writeTree(t, dir, out.Files)
	writeTree(t, dir, map[string]string{"src/__setup_ws_sse.ts": wsSSEFakeSetup})

	if errs := typeCheck(t, dir); len(errs) != 0 {
		t.Fatalf("generated ws-sse client must type-check with zero errors, got:\n%s", strings.Join(errs, "\n"))
	}

	driver := `
import './__setup_ws_sse';
import { ChatWSClient } from './websocket';
import { NotificationsSSEClient } from './sse';

async function main() {
  // --- WebSocket: incoming frame decodes, outgoing message encodes, ---
  // --- unknown keys survive both directions.                        ---
  const wsClient = new ChatWSClient({ baseURL: 'http://example.invalid' });
  await wsClient.connect();

  const fakeWS = (globalThis as any).__lastFakeWS;

  const receivedWS: any[] = [];
  wsClient.onMessage((msg) => receivedWS.push(msg));

  // Wire frame: snake_case, plus an unknown key User never declares.
  fakeWS.onmessage({
    data: JSON.stringify({ user_id: 'x', unknown_wire_key: 'w1' }),
  });

  // Outgoing: camelCase message, plus an unknown key.
  await wsClient.send({ userId: 'y', unknownClientKey: 'w2' } as any);

  // --- SSE: incoming event decodes, unknown key survives. ---
  const sseClient = new NotificationsSSEClient({ baseURL: 'http://example.invalid' });
  await sseClient.connect();

  const fakeES = (globalThis as any).__lastFakeES;

  const receivedSSE: any[] = [];
  sseClient.onCreated((data) => receivedSSE.push(data));

  fakeES.emit('created', JSON.stringify({ user_id: 'z', unknown_wire_key: 'w3' }));

  console.log(JSON.stringify({
    wsReceived: receivedWS,
    wsSentRaw: fakeWS.sent.map((s: string) => JSON.parse(s)),
    sseReceived: receivedSSE,
  }));

  // Both clients default to Features.Heartbeat/Reconnection on (baseConfig()
  // -> DefaultConfig()), which means connect() started a setInterval
  // heartbeat that is never unref'd. Without closing both clients here,
  // Node never runs out of pending timers and this driver process hangs
  // forever -- runNodeDriver would then block indefinitely waiting for it
  // to exit.
  wsClient.close();
  sseClient.close();
}

main().catch((err) => {
  console.error(err);
  throw err;
});
`
	writeTree(t, dir, map[string]string{"src/__driver_ws_sse.ts": driver})

	stdout := runNodeDriver(t, dir, "src/__driver_ws_sse.ts")

	var result struct {
		WSReceived  []map[string]any `json:"wsReceived"`
		WSSentRaw   []map[string]any `json:"wsSentRaw"`
		SSEReceived []map[string]any `json:"sseReceived"`
	}
	decodeLastLine(t, stdout, &result)

	// 1. Incoming WS frame decodes wire -> camelCase.
	require.Len(t, result.WSReceived, 1, "stdout:\n%s", stdout)
	assert.Equal(t, "x", result.WSReceived[0]["userId"], "wire user_id must decode to camelCase userId; stdout:\n%s", stdout)
	assert.NotContains(t, result.WSReceived[0], "user_id", "the wire-cased key must not survive decode; stdout:\n%s", stdout)
	// 4a. Unknown key survives decode verbatim.
	assert.Equal(t, "w1", result.WSReceived[0]["unknown_wire_key"], "an unrecognized key must pass through decode untouched; stdout:\n%s", stdout)

	// 2. Outgoing WS send encodes camelCase -> wire.
	require.Len(t, result.WSSentRaw, 1, "stdout:\n%s", stdout)
	assert.Equal(t, "y", result.WSSentRaw[0]["user_id"], "camelCase userId must encode to wire user_id; stdout:\n%s", stdout)
	assert.NotContains(t, result.WSSentRaw[0], "userId", "the camelCase key must not leak onto the wire; stdout:\n%s", stdout)
	// 4b. Unknown key survives encode verbatim.
	assert.Equal(t, "w2", result.WSSentRaw[0]["unknownClientKey"], "an unrecognized key must pass through encode untouched; stdout:\n%s", stdout)

	// 3. SSE event decodes wire -> camelCase.
	require.Len(t, result.SSEReceived, 1, "stdout:\n%s", stdout)
	assert.Equal(t, "z", result.SSEReceived[0]["userId"], "wire user_id must decode to camelCase userId over SSE; stdout:\n%s", stdout)
	assert.NotContains(t, result.SSEReceived[0], "user_id", "the wire-cased key must not survive decode over SSE; stdout:\n%s", stdout)
	assert.Equal(t, "w3", result.SSEReceived[0]["unknown_wire_key"], "an unrecognized key must pass through SSE decode untouched; stdout:\n%s", stdout)
}

// TestPreserveNamingSkipsCodecsInStreamingWSAndSSE is the "preserve" gate's
// WS/SSE counterpart: under NamingPreserve with no FieldOverrides,
// codecsNeeded(config) is false, src/codecs.ts is never emitted at all, and
// websocket.ts/sse.ts must not import from it or reference encode()/
// decode() -- otherwise generated output fails tsc with TS2307 ("Cannot
// find module './codecs'"). This proves the gating this task's brief
// specifically called out (codecs.go's codecsNeeded doc comment) actually
// holds for the two files this task touches, not just fetch.ts (already
// covered by the "preserve" gate fixture in fixtures_test.go).
func TestPreserveNamingSkipsCodecsInStreamingWSAndSSE(t *testing.T) {
	spec := wsSSESpec()
	config := preserveConfig()
	require.False(t, codecsNeeded(config), "sanity check: preserveConfig() must not need codecs")

	out, err := NewGenerator().Generate(context.Background(), spec, config)
	require.NoError(t, err)

	require.NotContains(t, out.Files, "src/codecs.ts", "sanity check: codecs.ts must not be emitted under NamingPreserve")

	ws := out.Files["src/websocket.ts"]
	sseCode := out.Files["src/sse.ts"]

	for name, code := range map[string]string{"websocket.ts": ws, "sse.ts": sseCode} {
		assert.NotContains(t, code, "./codecs", "%s must not import from ./codecs under NamingPreserve", name)
		assert.NotContains(t, code, "decode(", "%s must not call decode() under NamingPreserve", name)
		assert.NotContains(t, code, "encode(", "%s must not call encode() under NamingPreserve", name)
	}

	// The payload casts must be exactly the pre-codec raw JSON.parse/
	// JSON.stringify shape -- passthrough, not renamed (NamingPreserve means
	// no renaming, not "renamed to the same name").
	assert.Contains(t, ws, "const message: types.User = JSON.parse(data);")
	assert.Contains(t, ws, "this.ws.send(JSON.stringify(message));")
	assert.Contains(t, sseCode, "const data: types.User = JSON.parse(event.data);")

	dir := t.TempDir()
	writeTree(t, dir, out.Files)

	if errs := typeCheck(t, dir); len(errs) != 0 {
		t.Fatalf("preserve-naming ws-sse client must type-check with zero errors, got:\n%s", strings.Join(errs, "\n"))
	}
}

// TestHardcodedStreamingTypesUntouchedByCodecFix pins the exact,
// byte-identical hardcoded interfaces generateStreamingTypes emits for
// Message, Member, Room, RoomOptions, HistoryQuery, and UserPresence --
// including their snake_case (room_id, display_name, etc.) field names.
// These are string literals in generator.go, not derived from any
// client.Schema, have no codec-table entry at all, and an earlier
// investigation (recorded in this task's brief) confirmed renaming only
// their TypeScript declaration -- without also changing every generator
// that hand-builds an object literal keyed by these same wire names
// (rooms.go, presence.go, channels.go, typing.go) -- would break wire
// correctness. This task's fix deliberately never touches generator.go's
// generateStreamingTypes, nor rooms.go/presence.go/channels.go/typing.go,
// so this asserts that boundary explicitly rather than leaving it as an
// unstated assumption.
func TestHardcodedStreamingTypesUntouchedByCodecFix(t *testing.T) {
	spec := wsSSESpec()
	config := baseConfig() // EnableRooms/EnablePresence/EnableHistory all default true

	out, err := NewGenerator().Generate(context.Background(), spec, config)
	require.NoError(t, err)

	types := out.Files["src/types.ts"]

	for _, want := range []string{
		"export interface Message {",
		"  room_id: string;",
		"  user_id?: string;",
		"export interface Member {",
		"  user_id: string;",
		"  display_name?: string;",
		"  avatar_url?: string;",
		"  joined_at?: string;",
		"export interface Room {",
		"  created_by?: string;",
		"  created_at?: string;",
		"export interface RoomOptions {",
		"  max_members?: number;",
		"  is_private?: boolean;",
		"export interface HistoryQuery {",
		"  before_id?: string;",
		"  after_id?: string;",
		"export interface UserPresence {",
		"  userId: string;",
		"  customMessage?: string;",
		"  lastSeen?: string;",
		"  roomId?: string;",
	} {
		assert.Contains(t, types, want, "hardcoded streaming type field must be byte-identical to the pre-fix generator output")
	}
}
