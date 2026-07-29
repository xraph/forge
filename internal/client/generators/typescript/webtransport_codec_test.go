package typescript

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/xraph/forge/internal/client"
)

// wtSpec returns a fresh spec with one WebTransport endpoint declaring all
// three schema kinds (bidirectional stream, unidirectional stream, and
// datagram), every one of them a direct $ref to User -- the same schema
// wsSSESpec() uses for its WebSocket/SSE endpoints, so this exercises the
// identical regression (a spec-derived, camelCase-declared type cast straight
// over a snake_case wire) across the third streaming transport this package
// generates. Built from baseSpec() so it shares the REST/schema shape every
// other fixture in this package uses.
func wtSpec() *client.APISpec {
	spec := baseSpec()
	spec.WebTransports = []client.WebTransportEndpoint{
		{
			ID:      "data",
			Path:    "/wt/data",
			Summary: "Data WebTransport",
			BiStreamSchema: &client.StreamSchema{
				SendSchema:    &client.Schema{Ref: "#/components/schemas/User"},
				ReceiveSchema: &client.Schema{Ref: "#/components/schemas/User"},
			},
			UniStreamSchema: &client.StreamSchema{
				SendSchema:    &client.Schema{Ref: "#/components/schemas/User"},
				ReceiveSchema: &client.Schema{Ref: "#/components/schemas/User"},
			},
			DatagramSchema: &client.Schema{Ref: "#/components/schemas/User"},
		},
	}

	return spec
}

// wtFakeSetup installs a fake global WebTransport class before webtransport.ts
// is ever imported, mirroring wsSSEFakeSetup's own module-eval-order
// reasoning (streaming_codec_test.go): webtransport.go computes
// `const isWebTransportSupported = typeof WebTransport !== 'undefined'` at
// MODULE-EVAL time, once, the moment the module is first evaluated, so the
// fake must exist as a global before that happens. Unlike WebSocket/
// EventSource (attached to a faked `window`), WebTransport is checked as a
// bare global identifier, so the fake is assigned directly to globalThis.
//
// FakeWebTransport implements just enough of the real WebTransport surface
// for the driver below to exercise every one of this task's Finding-1
// scenarios without a real network: `datagrams` (readable+writable, with
// test hooks to push an incoming datagram and capture outgoing ones),
// `createBidirectionalStream()` (a readable+writable pair, with test hooks to
// push incoming bytes and capture outgoing ones), and
// `incomingUnidirectionalStreams` (a readable stream of readable streams, so
// the driver can push one server-initiated uni-stream and read what
// processIncomingUniStream emits from it).
const wtFakeSetup = `
class FakeWebTransport {
  ready: Promise<void>;
  closed: Promise<void>;
  datagrams: { readable: ReadableStream<Uint8Array>; writable: WritableStream<Uint8Array> };
  incomingUnidirectionalStreams: ReadableStream<ReadableStream<Uint8Array>>;
  incomingBidirectionalStreams: ReadableStream<any>;

  sentDatagrams: Uint8Array[] = [];
  sentBidiData: Uint8Array[] = [];

  private datagramController!: ReadableStreamDefaultController<Uint8Array>;
  private uniStreamController!: ReadableStreamDefaultController<ReadableStream<Uint8Array>>;
  bidiReadController!: ReadableStreamDefaultController<Uint8Array>;

  constructor(public url: string) {
    (globalThis as any).__lastFakeWT = this;

    this.ready = Promise.resolve();
    this.closed = new Promise(() => {}); // never resolves during the test

    const sentDatagrams = this.sentDatagrams;
    this.datagrams = {
      writable: new WritableStream<Uint8Array>({
        write: (chunk) => {
          sentDatagrams.push(chunk);
        },
      }),
      readable: new ReadableStream<Uint8Array>({
        start: (controller) => {
          this.datagramController = controller;
        },
      }),
    };

    this.incomingUnidirectionalStreams = new ReadableStream<ReadableStream<Uint8Array>>({
      start: (controller) => {
        this.uniStreamController = controller;
      },
    });

    this.incomingBidirectionalStreams = new ReadableStream<any>({ start: () => {} });
  }

  pushDatagram(bytes: Uint8Array): void {
    this.datagramController.enqueue(bytes);
  }

  pushUniStream(bytes: Uint8Array): void {
    const stream = new ReadableStream<Uint8Array>({
      start(controller) {
        controller.enqueue(bytes);
        controller.close();
      },
    });
    this.uniStreamController.enqueue(stream);
  }

  createBidirectionalStream(): Promise<any> {
    const sentBidiData = this.sentBidiData;
    let readController!: ReadableStreamDefaultController<Uint8Array>;
    const readable = new ReadableStream<Uint8Array>({
      start(controller) {
        readController = controller;
      },
    });
    this.bidiReadController = readController;
    const writable = new WritableStream<Uint8Array>({
      write(chunk) {
        sentBidiData.push(chunk);
      },
    });
    return Promise.resolve({ readable, writable });
  }

  createUnidirectionalStream(): Promise<WritableStream<Uint8Array>> {
    return Promise.resolve(new WritableStream<Uint8Array>({ write: () => {} }));
  }

  close(): void {}
}

(globalThis as any).WebTransport = FakeWebTransport;
`

// TestWebTransportDecodeEncodeWirePayloads is the runtime execution proof
// that Finding 1 (task 5c) is fixed: webtransport.go previously cast every
// incoming/outgoing payload straight through JSON.parse/JSON.stringify with
// no rename at all -- across datagrams (sendDatagram/receiveDatagram), the
// bidirectional stream wrapper (BiDiStream.send/receive), and incoming
// unidirectional streams (the 'incomingUniStream' event) -- so a spec-derived
// message type declaring camelCase properties (types.User's userId) lied
// about what was actually on the wire (snake_case: user_id).
//
// Drives REAL generated code (webtransport.ts's DataWTClient, from
// wtSpec()'s User-typed WebTransport endpoint) under Node via esbuild,
// exactly as streaming_codec_test.go's own WS/SSE driver does -- not
// encode()/decode() called directly.
func TestWebTransportDecodeEncodeWirePayloads(t *testing.T) {
	spec := wtSpec()
	config := baseConfig() // NamingCamel by default -- codecsNeeded(config) == true

	out, err := NewGenerator().Generate(context.Background(), spec, config)
	require.NoError(t, err)

	require.Contains(t, out.Files, "src/webtransport.ts")
	require.Contains(t, out.Files, "src/codecs.ts")
	assert.Empty(t, out.Warnings, "every schema here is a direct $ref, so no warning should fire")

	wt := out.Files["src/webtransport.ts"]

	// Sanity checks on the fix's shape before ever running anything: the
	// raw, unrenamed casts from the regression must be gone, replaced by
	// decode()/encode() calls referencing the User codec id.
	assert.Contains(t, wt, "import { decode, encode } from './codecs';")
	assert.NotContains(t, wt, "JSON.parse(text);", "the raw, un-decoded datagram cast from the regression must be gone")
	assert.NotContains(t, wt, "JSON.parse(result);", "the raw, un-decoded BiDiStream.receive cast from the regression must be gone")
	assert.NotContains(t, wt, "JSON.parse(data));", "the raw, un-decoded incomingUniStream cast from the regression must be gone")
	assert.Contains(t, wt, `decode(JSON.parse(text), "User")`, "receiveDatagram/receiveDatagrams must decode via the User codec")
	assert.Contains(t, wt, `encode(msg, "User")`, "sendDatagram/sendDatagramSync/BiDiStream.send/UniStream.send must encode via the User codec")
	assert.Contains(t, wt, `decode(JSON.parse(result), "User")`, "BiDiStream.receive must decode via the User codec")
	assert.Contains(t, wt, `decode(JSON.parse(line), "User")`, "BiDiStream.receiveIterator must decode via the User codec")
	assert.Contains(t, wt, `decode(JSON.parse(data), "User")`, "processIncomingUniStream must decode via the User codec")

	dir := t.TempDir()
	writeTree(t, dir, out.Files)
	writeTree(t, dir, map[string]string{"src/__setup_wt.ts": wtFakeSetup})

	if errs := typeCheck(t, dir); len(errs) != 0 {
		t.Fatalf("generated webtransport client must type-check with zero errors, got:\n%s", strings.Join(errs, "\n"))
	}

	driver := `
import './__setup_wt';
import { DataWTClient } from './webtransport';

async function main() {
  const wt = new DataWTClient({ baseURL: 'http://example.invalid' });
  await wt.connect();

  const fake = (globalThis as any).__lastFakeWT;

  // --- Datagram: incoming decodes, outgoing encodes, unknown keys survive both directions. ---
  fake.pushDatagram(new TextEncoder().encode(JSON.stringify({ user_id: 'x', unknown_wire_key: 'w1' })));
  const receivedDatagram = await wt.receiveDatagram();

  await wt.sendDatagram({ userId: 'y', unknownClientKey: 'w2' } as any);
  const sentDatagramRaw = JSON.parse(new TextDecoder().decode(fake.sentDatagrams[0]));

  // --- BiDiStream: outgoing send encodes, incoming receive decodes, unknown keys survive. ---
  const bidi = await wt.openBidiStream();

  await bidi.send({ userId: 'y2', unknownClientKey: 'w4' } as any);
  const sentBidiRaw = JSON.parse(new TextDecoder().decode(fake.sentBidiData[0]));

  fake.bidiReadController.enqueue(new TextEncoder().encode(JSON.stringify({ user_id: 'z', unknown_wire_key: 'w3' })));
  fake.bidiReadController.close();
  const receivedBidi = await bidi.receive();

  // --- incomingUniStream: server-initiated uni-stream decodes, unknown key survives. ---
  const uniPromise = new Promise<any>((resolve) => {
    wt.onIncomingUniStream((data: any) => resolve(data));
  });
  fake.pushUniStream(new TextEncoder().encode(JSON.stringify({ user_id: 'u', unknown_wire_key: 'w5' })));
  const receivedUni = await uniPromise;

  console.log(JSON.stringify({
    receivedDatagram,
    sentDatagramRaw,
    sentBidiRaw,
    receivedBidi,
    receivedUni,
  }));

  wt.close();
}

main().catch((err) => {
  console.error(err);
  throw err;
});
`
	writeTree(t, dir, map[string]string{"src/__driver_wt.ts": driver})

	stdout := runNodeDriver(t, dir, "src/__driver_wt.ts")

	var result struct {
		ReceivedDatagram map[string]any `json:"receivedDatagram"`
		SentDatagramRaw  map[string]any `json:"sentDatagramRaw"`
		SentBidiRaw      map[string]any `json:"sentBidiRaw"`
		ReceivedBidi     map[string]any `json:"receivedBidi"`
		ReceivedUni      map[string]any `json:"receivedUni"`
	}
	decodeLastLine(t, stdout, &result)

	// 1. Incoming datagram decodes wire -> camelCase; unknown key survives.
	assert.Equal(t, "x", result.ReceivedDatagram["userId"], "wire user_id must decode to camelCase userId; stdout:\n%s", stdout)
	assert.NotContains(t, result.ReceivedDatagram, "user_id", "the wire-cased key must not survive decode; stdout:\n%s", stdout)
	assert.Equal(t, "w1", result.ReceivedDatagram["unknown_wire_key"], "an unrecognized key must pass through decode untouched; stdout:\n%s", stdout)

	// 2. Outgoing datagram encodes camelCase -> wire; unknown key survives.
	assert.Equal(t, "y", result.SentDatagramRaw["user_id"], "camelCase userId must encode to wire user_id; stdout:\n%s", stdout)
	assert.NotContains(t, result.SentDatagramRaw, "userId", "the camelCase key must not leak onto the wire; stdout:\n%s", stdout)
	assert.Equal(t, "w2", result.SentDatagramRaw["unknownClientKey"], "an unrecognized key must pass through encode untouched; stdout:\n%s", stdout)

	// 3. BiDiStream.send encodes camelCase -> wire; unknown key survives.
	assert.Equal(t, "y2", result.SentBidiRaw["user_id"], "camelCase userId must encode to wire user_id over BiDiStream.send; stdout:\n%s", stdout)
	assert.NotContains(t, result.SentBidiRaw, "userId", "the camelCase key must not leak onto the wire over BiDiStream.send; stdout:\n%s", stdout)
	assert.Equal(t, "w4", result.SentBidiRaw["unknownClientKey"], "an unrecognized key must pass through BiDiStream.send encode untouched; stdout:\n%s", stdout)

	// 4. BiDiStream.receive decodes wire -> camelCase; unknown key survives.
	assert.Equal(t, "z", result.ReceivedBidi["userId"], "wire user_id must decode to camelCase userId over BiDiStream.receive; stdout:\n%s", stdout)
	assert.NotContains(t, result.ReceivedBidi, "user_id", "the wire-cased key must not survive BiDiStream.receive decode; stdout:\n%s", stdout)
	assert.Equal(t, "w3", result.ReceivedBidi["unknown_wire_key"], "an unrecognized key must pass through BiDiStream.receive decode untouched; stdout:\n%s", stdout)

	// 5. incomingUniStream decodes wire -> camelCase; unknown key survives.
	assert.Equal(t, "u", result.ReceivedUni["userId"], "wire user_id must decode to camelCase userId over incomingUniStream; stdout:\n%s", stdout)
	assert.NotContains(t, result.ReceivedUni, "user_id", "the wire-cased key must not survive incomingUniStream decode; stdout:\n%s", stdout)
	assert.Equal(t, "w5", result.ReceivedUni["unknown_wire_key"], "an unrecognized key must pass through incomingUniStream decode untouched; stdout:\n%s", stdout)
}

// TestPreserveNamingSkipsCodecsInWebTransport is the "preserve" gate's
// WebTransport counterpart to streaming_codec_test.go's
// TestPreserveNamingSkipsCodecsInStreamingWSAndSSE: under NamingPreserve with
// no FieldOverrides, codecsNeeded(config) is false, src/codecs.ts is never
// emitted at all, and webtransport.ts must not import from it or reference
// encode()/decode() -- otherwise generated output fails tsc with TS2307
// ("Cannot find module './codecs'"). Proves the gating this task's brief
// specifically called out (codecs.go's codecsNeeded doc comment) holds for
// webtransport.ts too, not just websocket.ts/sse.ts (already covered) or
// fetch.ts (covered by the "preserve" gate fixture).
func TestPreserveNamingSkipsCodecsInWebTransport(t *testing.T) {
	spec := wtSpec()
	config := preserveConfig()
	require.False(t, codecsNeeded(config), "sanity check: preserveConfig() must not need codecs")

	out, err := NewGenerator().Generate(context.Background(), spec, config)
	require.NoError(t, err)

	require.NotContains(t, out.Files, "src/codecs.ts", "sanity check: codecs.ts must not be emitted under NamingPreserve")

	wt := out.Files["src/webtransport.ts"]
	assert.NotContains(t, wt, "./codecs", "webtransport.ts must not import from ./codecs under NamingPreserve")
	// Note: a blanket NotContains(wt, "decode(")/NotContains(wt, "encode(")
	// would false-positive on the Web APIs this file legitimately calls --
	// `new TextEncoder().encode(...)` and `new TextDecoder().decode(...)` --
	// which have nothing to do with this package's own encode()/decode()
	// codec functions. Checking for the codec table's own call shape
	// (`decode(<expr>, "<id>")` / `encode(<expr>, "<id>")`) is what actually
	// distinguishes "codec machinery referenced" from "the Encoding API used
	// as always".
	assert.NotContains(t, wt, `decode(JSON.parse`, "webtransport.ts must not call this package's decode() under NamingPreserve")
	assert.NotContains(t, wt, `encode(msg,`, "webtransport.ts must not call this package's encode() under NamingPreserve")

	// The payload casts must be exactly the pre-codec raw JSON.parse/
	// JSON.stringify shape -- passthrough, not renamed (NamingPreserve means
	// no renaming, not "renamed to the same name").
	assert.Contains(t, wt, "const data = encoder.encode(JSON.stringify(msg));")
	assert.Contains(t, wt, "return JSON.parse(text);")
	assert.Contains(t, wt, "return JSON.parse(result);")
	assert.Contains(t, wt, "this.emit('incomingUniStream', JSON.parse(data));")

	dir := t.TempDir()
	writeTree(t, dir, out.Files)

	if errs := typeCheck(t, dir); len(errs) != 0 {
		t.Fatalf("preserve-naming webtransport client must type-check with zero errors, got:\n%s", strings.Join(errs, "\n"))
	}
}

// TestPreserveNamingWithFieldOverridesUsesCodecsInWebTransport proves the
// gate is codecsNeeded(config), not a raw effectiveFieldNaming(config) ==
// NamingPreserve check: a config that sets NamingPreserve but ALSO
// configures a FieldOverrides entry still needs the codec machinery live
// (see codecsNeeded's doc comment, fieldname.go), so webtransport.ts must
// import './codecs' and reference encode()/decode() in this configuration --
// mirroring the same proof point tests already establish for websocket.ts/
// sse.ts and fetch.ts elsewhere in this package.
func TestPreserveNamingWithFieldOverridesUsesCodecsInWebTransport(t *testing.T) {
	spec := wtSpec()
	config := preserveConfig()
	config.FieldOverrides = map[string]string{"User.user_id": "userId"}
	require.True(t, codecsNeeded(config), "sanity check: a FieldOverrides entry must keep codecsNeeded true even under NamingPreserve")

	out, err := NewGenerator().Generate(context.Background(), spec, config)
	require.NoError(t, err)

	require.Contains(t, out.Files, "src/codecs.ts", "sanity check: codecs.ts must be emitted when FieldOverrides is non-empty")

	wt := out.Files["src/webtransport.ts"]
	assert.Contains(t, wt, "import { decode, encode } from './codecs';")
	assert.Contains(t, wt, `decode(JSON.parse(text), "User")`)
	assert.Contains(t, wt, `encode(msg, "User")`)

	dir := t.TempDir()
	writeTree(t, dir, out.Files)

	if errs := typeCheck(t, dir); len(errs) != 0 {
		t.Fatalf("preserve-with-overrides webtransport client must type-check with zero errors, got:\n%s", strings.Join(errs, "\n"))
	}
}

// TestHardcodedStreamingTypesUntouchedByWebTransportCodecFix is
// streaming_codec_test.go's TestHardcodedStreamingTypesUntouchedByCodecFix
// re-run against a spec that ALSO declares a WebTransport endpoint (wtSpec()
// builds on baseSpec(), same as wsSSESpec()), pinning that this task's
// WebTransport-specific fix touches none of generateStreamingTypes' hardcoded
// Message/Member/Room/RoomOptions/HistoryQuery/UserPresence interfaces --
// exactly the boundary the task brief calls out as deliberately untouched.
func TestHardcodedStreamingTypesUntouchedByWebTransportCodecFix(t *testing.T) {
	spec := wtSpec()
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
		assert.Contains(t, types, want, "hardcoded streaming type field must be byte-identical to the pre-fix generator output, even with a WebTransport endpoint present")
	}
}
