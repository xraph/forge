package dart

import (
	"regexp"
	"strings"
	"testing"

	"github.com/xraph/forge/internal/client"
)

func TestTypedStreamingClients(t *testing.T) {
	out := generate(t, fixture(t, "default"))

	assertContains(t, "chat_socket.dart", file(t, out, "lib/src/streaming/chat_socket.dart"),
		"final class ChatSocket {",
		"Future<ChatSocketSession> connect({required String roomId}) async {",
		"'$base/ws/chat/${Uri.encodeComponent(roomId)}'",
		"'https' => 'wss', 'http' => 'ws'",
		"StreamConnectContext(url: url.replace(scheme: scheme), endpoint: '/ws/chat/{roomId}', headers: headers),",
		"Stream<LineItem> get messages =>",
		"_connection.messages.map((m) => LineItem.fromClient(lineItemCodec.decode(m)));",
		"void send(LineItem message) => _connection.send(lineItemCodec.encode(message.toClient()));",
	)

	// The runtime delivers only the events it is told to listen for, so the
	// generated factory names every event the endpoint declares.
	assertContains(t, "notifications_events.dart", file(t, out, "lib/src/streaming/notifications_events.dart"),
		"connect ?? eventSourceConnection(events: ['created'])", "Stream<Customer> get messages =>",
		"StreamConnectContext(url: url, endpoint: '/sse/notifications', headers: headers),",
		"Customer.fromClient(customerCodec.decode((m! as Map<Object?, Object?>)['data']))")

	assertContains(t, "telemetry_transport.dart", file(t, out, "lib/src/streaming/telemetry_transport.dart"),
		"connect ?? webTransportConnection()", "Stream<LineItem> get messages =>")
}

func TestStreamsConnectPerEndpointAndNeverTouchTheCache(t *testing.T) {
	out := generate(t, fixture(t, "default"))

	for name, content := range out.Files {
		if !strings.HasPrefix(name, "lib/src/streaming/") {
			continue
		}

		for _, banned := range []string{"SubscriptionManager", "EntityStore", "QueryCache", "principal:"} {
			if strings.Contains(content, banned) {
				t.Errorf("%s names %s: the typed sockets connect per endpoint and leave the cache to the adapters", name, banned)
			}
		}
	}
}

func TestMultiplexedDirectionIsUntypedAndReported(t *testing.T) {
	f := fixture(t, "default")
	f.Spec.WebSockets = append(f.Spec.WebSockets, client.WebSocketEndpoint{
		ID: "mux", Path: "/ws/mux",
		SendSchema:   ref("LineItem"),
		SendMessages: map[string]*client.Schema{"a": ref("LineItem"), "b": ref("Customer")},
	})

	out := generate(t, f)

	assertContains(t, "mux_socket.dart", file(t, out, "lib/src/streaming/mux_socket.dart"),
		"void send(Object? message) => _connection.send(message);")
	assertContains(t, "warnings", strings.Join(out.Warnings, "\n"), "stream /ws/mux: the send direction carries 2 message types")
}

func TestMessagesNamingOneTypeStayTyped(t *testing.T) {
	f := fixture(t, "default")
	f.Spec.WebSockets = append(f.Spec.WebSockets, client.WebSocketEndpoint{
		ID: "pair", Path: "/ws/pair",
		SendMessages:    map[string]*client.Schema{"say": ref("LineItem"), "shout": ref("LineItem")},
		ReceiveMessages: map[string]*client.Schema{"said": ref("Customer")},
	})

	out := generate(t, f)
	pair := file(t, out, "lib/src/streaming/pair_socket.dart")

	assertContains(t, "pair_socket.dart", pair, "void send(LineItem message)", "Stream<Customer> get messages")

	if strings.Contains(strings.Join(out.Warnings, "\n"), "stream /ws/pair") {
		t.Errorf("two messages of one type are one type, got warnings:\n%s", strings.Join(out.Warnings, "\n"))
	}
}

func TestSeveralSseEventsDeliverTheRawFrames(t *testing.T) {
	f := fixture(t, "default")
	f.Spec.SSEs = append(f.Spec.SSEs, client.SSEEndpoint{
		ID: "mixed", Path: "/sse/mixed",
		EventSchemas: map[string]*client.Schema{"line": ref("LineItem"), "who": ref("Customer")},
	})

	out := generate(t, f)

	assertContains(t, "mixed_events.dart", file(t, out, "lib/src/streaming/mixed_events.dart"),
		"Stream<Object?> get messages =>", "eventSourceConnection(events: ['line', 'who'])")
	assertContains(t, "warnings", strings.Join(out.Warnings, "\n"), "stream /sse/mixed: the event direction carries 2 message types")
}

func TestFeatureClientsFollowTheStreamingConfig(t *testing.T) {
	out := generate(t, fixture(t, "default"))

	assertContains(t, "rooms.dart", file(t, out, "lib/src/streaming/rooms.dart"),
		"final class RoomClient {", "Future<List<RoomMessage>> history(", "this.maxRooms = 50,", "'$base/ws'", "endpoint: '/ws',")
	assertContains(t, "presence.dart", file(t, out, "lib/src/streaming/presence.dart"),
		"static const List<String> statuses = ['online', 'away', 'busy', 'offline'];")
	assertContains(t, "typing.dart", file(t, out, "lib/src/streaming/typing.dart"),
		"this.timeout = const Duration(milliseconds: 3000),")
	assertContains(t, "channels.dart", file(t, out, "lib/src/streaming/channels.dart"),
		"'action': 'publish',")
	assertContains(t, "streaming_client.dart", file(t, out, "lib/src/streaming/streaming_client.dart"),
		"final class StreamingClient {", "rooms = RoomClient(baseUrl: baseUrl, connect: connect, headers: headers),")

	f := fixture(t, "default")
	f.Config.Streaming.EnableHistory = false

	rooms := file(t, generate(t, f), "lib/src/streaming/rooms.dart")
	if strings.Contains(rooms, "history(") {
		t.Errorf("history must follow EnableHistory:\n%s", rooms)
	}
}

func TestFeatureClientsTakeTheirPathsAndLimitsFromTheDocument(t *testing.T) {
	f := fixture(t, "default")
	f.Spec.Streaming = &client.StreamingSpec{
		Rooms:    &client.RoomOperations{Path: "/realtime/rooms"},
		Presence: &client.PresenceOperations{Path: "/realtime/presence", Statuses: []string{"here", "gone"}},
		Typing:   &client.TypingOperations{Path: "/realtime/typing"},
		Channels: &client.ChannelOperations{Path: "/realtime/channels"},
	}
	f.Config.Streaming.RoomConfig.MaxRoomsPerUser = 7
	f.Config.Streaming.PresenceConfig.HeartbeatIntervalMs = 1500
	f.Config.Streaming.TypingConfig.TimeoutMs = 2500
	f.Config.Streaming.TypingConfig.DebounceMs = 120
	f.Config.Streaming.ChannelConfig.MaxChannelsPerUser = 9

	out := generate(t, f)

	assertContains(t, "rooms.dart", file(t, out, "lib/src/streaming/rooms.dart"),
		"endpoint: '/realtime/rooms',", "this.maxRooms = 7,")
	assertContains(t, "presence.dart", file(t, out, "lib/src/streaming/presence.dart"),
		"endpoint: '/realtime/presence',", "statuses = ['here', 'gone'];", "Duration(milliseconds: 1500)")
	assertContains(t, "typing.dart", file(t, out, "lib/src/streaming/typing.dart"),
		"endpoint: '/realtime/typing',", "Duration(milliseconds: 2500)", "Duration(milliseconds: 120)")
	assertContains(t, "channels.dart", file(t, out, "lib/src/streaming/channels.dart"),
		"endpoint: '/realtime/channels',", "this.maxChannels = 9,")
}

func TestOnlyTheEnabledFeaturesAreGenerated(t *testing.T) {
	f := fixture(t, "default")
	f.Config.Streaming.EnablePresence = false
	f.Config.Streaming.EnableTyping = false

	out := generate(t, f)

	for _, gone := range []string{"presence", "typing"} {
		if _, ok := out.Files["lib/src/streaming/"+gone+".dart"]; ok {
			t.Errorf("%s generated while its feature is off", gone)
		}
	}

	hub := file(t, out, "lib/src/streaming/streaming_client.dart")
	assertContains(t, "streaming_client.dart", hub, "RoomClient", "ChannelClient")

	for _, gone := range []string{"PresenceClient", "TypingClient"} {
		if strings.Contains(hub, gone) {
			t.Errorf("StreamingClient names %s while its feature is off:\n%s", gone, hub)
		}
	}

	f.Config.Streaming.GenerateUnifiedClient = false

	if _, ok := generate(t, f).Files["lib/src/streaming/streaming_client.dart"]; ok {
		t.Error("StreamingClient generated with the unified client off")
	}

	f.Config.Streaming.GenerateModularClients = false

	for name := range generate(t, f).Files {
		if name == "lib/src/streaming/rooms.dart" || name == "lib/src/streaming/channels.dart" {
			t.Errorf("%s generated with the modular clients off", name)
		}
	}
}

func TestStreamingOffKeepsTheStreamsTable(t *testing.T) {
	f := fixture(t, "default")
	f.Config.IncludeStreaming = false

	out := generate(t, f)

	for name := range out.Files {
		if strings.HasPrefix(name, "lib/src/streaming/") {
			t.Errorf("%s emitted with streaming off", name)
		}
	}

	ops := file(t, out, "lib/src/ops.dart")
	assertContains(t, "ops.dart", ops, "const List<StreamBinding> streams = [", "order.updated")

	if strings.Contains(strings.Join(out.Warnings, "\n"), "streaming clients are generated only with --hooks") {
		t.Error("a package with streaming off must not warn about streaming clients")
	}
}

func TestStreamingWithoutHooksIsSkippedWithAWarning(t *testing.T) {
	out := generate(t, fixture(t, "no-hooks"))

	for name := range out.Files {
		if strings.HasPrefix(name, "lib/src/streaming/") {
			t.Errorf("%s emitted without hooks", name)
		}
	}

	assertContains(t, "warnings", strings.Join(out.Warnings, "\n"), "streaming clients are generated only with --hooks")
}

func TestStreamingBarrelExportsTheClients(t *testing.T) {
	out := generate(t, fixture(t, "default"))
	barrel := file(t, out, "lib/orders_forge_client.dart")

	assertContains(t, "barrel", barrel,
		"export 'src/streaming/chat_socket.dart';",
		"export 'src/streaming/rooms.dart';",
		"export 'src/streaming/streaming_client.dart';")
}

func TestPathParametersDoNotShadowGeneratedLocals(t *testing.T) {
	out := generate(t, streamingFixture())
	socket := file(t, out, "lib/src/streaming/clash_socket.dart")

	// A parameter named like a local or a field would shadow it, and the
	// body would then read the parameter's String where it means the field.
	assertContains(t, "clash_socket.dart", socket,
		"required String base$", "required String url$", "required String headers$",
		"required String heartbeat$", "required String connection$", "required String scheme$")
	assertContains(t, "clash_socket.dart", socket,
		"${Uri.encodeComponent(base$)}", "${Uri.encodeComponent(headers$)}")
}

func TestWebSocketHeartbeatFollowsTheFeatureFlag(t *testing.T) {
	f := fixture(t, "default")
	on := file(t, generate(t, f), "lib/src/streaming/chat_socket.dart")
	assertContains(t, "chat_socket.dart", on, "this.heartbeat = const Duration(milliseconds: 30000)")

	f.Config.Features.Heartbeat = false
	off := file(t, generate(t, f), "lib/src/streaming/chat_socket.dart")
	assertContains(t, "chat_socket.dart", off, "this.heartbeat,")

	if strings.Contains(off, "milliseconds: 30000") {
		t.Errorf("heartbeat must be off without the feature:\n%s", off)
	}
}

func TestAnonymousEndpointsAreNamedFromTheirPathWords(t *testing.T) {
	out := generate(t, streamingFixture())

	assertContains(t, "ws_anonymous_socket.dart", file(t, out, "lib/src/streaming/ws_anonymous_socket.dart"),
		"final class WsAnonymousSocket {")
	assertContains(t, "sse_ticks_events.dart", file(t, out, "lib/src/streaming/sse_ticks_events.dart"),
		"final class SseTicksEvents {", "Stream<Object?> get messages => _connection.messages;")
}

func TestStreamingTypeNamesAreReserved(t *testing.T) {
	out := generate(t, streamingFixture())
	reserved := ReservedIdentifiers()
	declared := regexp.MustCompile(`(?m)^(?:final|sealed) class (\w+)`)

	for _, name := range []string{
		"lib/src/streaming/rooms.dart", "lib/src/streaming/presence.dart", "lib/src/streaming/typing.dart",
		"lib/src/streaming/channels.dart", "lib/src/streaming/streaming_client.dart", "lib/src/streaming/live_connection.dart",
	} {
		for _, m := range declared.FindAllStringSubmatch(file(t, out, name), -1) {
			if !reserved[m[1]] {
				t.Errorf("%s declares %s, which a schema could take: add it to generatedTypeNames", name, m[1])
			}
		}
	}
}

func TestSchemaNamedLikeAStreamingTypeIsRenamed(t *testing.T) {
	f := streamingFixture()
	f.Spec.Schemas["LiveConnection"] = &client.Schema{Type: "object", Properties: map[string]*client.Schema{"a": {Type: "string"}}}
	f.Spec.Schemas["UserPresence"] = &client.Schema{Type: "object", Properties: map[string]*client.Schema{"a": {Type: "string"}}}

	out := generate(t, f)

	assertContains(t, "warnings", strings.Join(out.Warnings, "\n"),
		`schema "LiveConnection" is generated as LiveConnectionModel`, `schema "UserPresence" is generated as UserPresenceModel`)
}
