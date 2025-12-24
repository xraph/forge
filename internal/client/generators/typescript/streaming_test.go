package typescript

import (
	"strings"
	"testing"

	"github.com/xraph/forge/internal/client"
)

func TestRoomsGenerator_Generate(t *testing.T) {
	spec := &client.APISpec{
		Info: client.APIInfo{
			Title:   "Test API",
			Version: "1.0.0",
		},
		Streaming: &client.StreamingSpec{
			EnableRooms:   true,
			EnableHistory: true,
			Rooms: &client.RoomOperations{
				Path: "/ws/rooms",
			},
		},
	}

	config := client.GeneratorConfig{
		Language:         "typescript",
		PackageName:      "test-client",
		IncludeStreaming: true,
		Features: client.Features{
			Reconnection:    true,
			Heartbeat:       true,
			StateManagement: true,
		},
		Streaming: client.StreamingConfig{
			EnableRooms:            true,
			EnableHistory:          true,
			GenerateModularClients: true,
			RoomConfig: client.RoomClientConfig{
				MaxRoomsPerUser:     50,
				IncludeMemberEvents: true,
			},
		},
	}

	gen := NewRoomsGenerator()
	code := gen.Generate(spec, config)

	// Check for expected exports
	expectedItems := []string{
		"export interface JoinOptions",
		"export type MessageHandler",
		"export type MemberHandler",
		"export interface RoomClientConfig",
		"export interface RoomState",
		"export class RoomClient",
		"async connect()",
		"disconnect(",
		"async join(roomId: string",
		"async leave(roomId: string)",
		"async send(roomId: string",
		"async broadcast(roomId: string",
		"getHistory(roomId: string",
		"getMembers(roomId: string)",
		"onMessage(roomId: string",
		"onMemberJoin(roomId: string",
		"onMemberLeave(roomId: string",
	}

	for _, item := range expectedItems {
		if !strings.Contains(code, item) {
			t.Errorf("Expected code to contain %q", item)
		}
	}

	// Check for reconnection support (now using scheduleReconnect)
	if !strings.Contains(code, "scheduleReconnect") {
		t.Error("Expected code to contain reconnection logic")
	}
}

func TestPresenceGenerator_Generate(t *testing.T) {
	spec := &client.APISpec{
		Info: client.APIInfo{
			Title:   "Test API",
			Version: "1.0.0",
		},
		Streaming: &client.StreamingSpec{
			EnablePresence: true,
			Presence: &client.PresenceOperations{
				Path:     "/ws/presence",
				Statuses: []string{"online", "away", "busy", "offline"},
			},
		},
	}

	config := client.GeneratorConfig{
		Language:         "typescript",
		PackageName:      "test-client",
		IncludeStreaming: true,
		Features: client.Features{
			Reconnection:    true,
			StateManagement: true,
		},
		Streaming: client.StreamingConfig{
			EnablePresence:         true,
			GenerateModularClients: true,
			PresenceConfig: client.PresenceClientConfig{
				Statuses:            []string{"online", "away", "busy", "offline"},
				HeartbeatIntervalMs: 30000,
			},
		},
	}

	gen := NewPresenceGenerator()
	code := gen.Generate(spec, config)

	// Check for expected exports
	expectedItems := []string{
		"export enum PresenceStatus",
		"ONLINE = 'online'",
		"AWAY = 'away'",
		"BUSY = 'busy'",
		"OFFLINE = 'offline'",
		"export type PresenceHandler",
		"export interface PresenceClientConfig",
		"export class PresenceClient",
		"async setStatus(status: PresenceStatus",
		"getStatus(userId: string)",
		"subscribe(userIds: string[])",
		"unsubscribe(userIds: string[])",
		"getOnlineUsers(",
		"onPresenceChange(",
		"startHeartbeat()",
	}

	for _, item := range expectedItems {
		if !strings.Contains(code, item) {
			t.Errorf("Expected code to contain %q", item)
		}
	}
}

func TestTypingGenerator_Generate(t *testing.T) {
	spec := &client.APISpec{
		Info: client.APIInfo{
			Title:   "Test API",
			Version: "1.0.0",
		},
		Streaming: &client.StreamingSpec{
			EnableTyping: true,
			Typing: &client.TypingOperations{
				Path:      "/ws/typing",
				TimeoutMs: 3000,
			},
		},
	}

	config := client.GeneratorConfig{
		Language:         "typescript",
		PackageName:      "test-client",
		IncludeStreaming: true,
		Features: client.Features{
			Reconnection:    true,
			StateManagement: true,
		},
		Streaming: client.StreamingConfig{
			EnableTyping:           true,
			GenerateModularClients: true,
			TypingConfig: client.TypingClientConfig{
				TimeoutMs:  3000,
				DebounceMs: 300,
			},
		},
	}

	gen := NewTypingGenerator()
	code := gen.Generate(spec, config)

	// Check for expected exports
	expectedItems := []string{
		"export interface TypingUser",
		"export type TypingHandler",
		"export interface TypingClientConfig",
		"export class TypingClient",
		"startTyping(roomId: string)",
		"stopTyping(roomId: string)",
		"getTypingUsers(roomId: string)",
		"onTypingStart(roomId: string",
		"onTypingStop(roomId: string",
		"debounceMs",
		"timeoutMs",
	}

	for _, item := range expectedItems {
		if !strings.Contains(code, item) {
			t.Errorf("Expected code to contain %q", item)
		}
	}
}

func TestChannelsGenerator_Generate(t *testing.T) {
	spec := &client.APISpec{
		Info: client.APIInfo{
			Title:   "Test API",
			Version: "1.0.0",
		},
		Streaming: &client.StreamingSpec{
			EnableChannels: true,
			Channels: &client.ChannelOperations{
				Path: "/ws/channels",
			},
		},
	}

	config := client.GeneratorConfig{
		Language:         "typescript",
		PackageName:      "test-client",
		IncludeStreaming: true,
		Features: client.Features{
			Reconnection:    true,
			StateManagement: true,
		},
		Streaming: client.StreamingConfig{
			EnableChannels:         true,
			GenerateModularClients: true,
			ChannelConfig: client.ChannelClientConfig{
				MaxChannelsPerUser: 100,
			},
		},
	}

	gen := NewChannelsGenerator()
	code := gen.Generate(spec, config)

	// Check for expected exports
	expectedItems := []string{
		"export interface ChannelMessage",
		"export type ChannelMessageHandler",
		"export interface SubscribeOptions",
		"export interface ChannelClientConfig",
		"export class ChannelClient",
		"subscribe(channelId: string",
		"unsubscribe(channelId: string)",
		"async publish<T = any>(channelId: string",
		"getSubscribedChannels()",
		"isSubscribed(channelId: string)",
		"onMessage<T = any>(channelId: string",
		"onAnyMessage<T = any>(",
	}

	for _, item := range expectedItems {
		if !strings.Contains(code, item) {
			t.Errorf("Expected code to contain %q", item)
		}
	}
}

func TestStreamingClientGenerator_Generate(t *testing.T) {
	spec := &client.APISpec{
		Info: client.APIInfo{
			Title:   "Test API",
			Version: "1.0.0",
		},
		Streaming: &client.StreamingSpec{
			EnableRooms:    true,
			EnablePresence: true,
			EnableTyping:   true,
			EnableChannels: true,
		},
	}

	config := client.GeneratorConfig{
		Language:         "typescript",
		PackageName:      "test-client",
		IncludeStreaming: true,
		Features: client.Features{
			Reconnection:    true,
			StateManagement: true,
		},
		Streaming: client.StreamingConfig{
			EnableRooms:            true,
			EnablePresence:         true,
			EnableTyping:           true,
			EnableChannels:         true,
			GenerateUnifiedClient:  true,
			GenerateModularClients: true,
		},
	}

	gen := NewStreamingClientGenerator()
	code := gen.Generate(spec, config)

	// Check for expected imports
	expectedImports := []string{
		"import { RoomClient",
		"import { PresenceClient",
		"import { TypingClient",
		"import { ChannelClient",
	}

	for _, item := range expectedImports {
		if !strings.Contains(code, item) {
			t.Errorf("Expected code to contain import %q", item)
		}
	}

	// Check for unified client
	expectedItems := []string{
		"export interface StreamingClientConfig",
		"export interface ConnectOptions",
		"export class StreamingClient",
		"public readonly rooms: RoomClient",
		"public readonly presence: PresenceClient",
		"public readonly typing: TypingClient",
		"public readonly channels: ChannelClient",
		"async connect(options?: ConnectOptions)",
		"disconnect()",
		"getState(): ConnectionState",
		"onConnectionStateChange(",
		"onError(",
		"getClientStates()",
		"setupEventForwarding()",
		"updateOverallState()",
	}

	for _, item := range expectedItems {
		if !strings.Contains(code, item) {
			t.Errorf("Expected code to contain %q", item)
		}
	}
}

func TestStreamingClientGenerator_PartialFeatures(t *testing.T) {
	spec := &client.APISpec{
		Info: client.APIInfo{
			Title:   "Test API",
			Version: "1.0.0",
		},
		Streaming: &client.StreamingSpec{
			EnableRooms:    true,
			EnableChannels: true,
			// Presence and Typing disabled
		},
	}

	config := client.GeneratorConfig{
		Language:         "typescript",
		PackageName:      "test-client",
		IncludeStreaming: true,
		Features: client.Features{
			Reconnection:    true,
			StateManagement: true,
		},
		Streaming: client.StreamingConfig{
			EnableRooms:            true,
			EnableChannels:         true,
			EnablePresence:         false,
			EnableTyping:           false,
			GenerateUnifiedClient:  true,
			GenerateModularClients: true,
		},
	}

	gen := NewStreamingClientGenerator()
	code := gen.Generate(spec, config)

	// Should include rooms and channels
	if !strings.Contains(code, "public readonly rooms: RoomClient") {
		t.Error("Expected code to contain rooms client")
	}
	if !strings.Contains(code, "public readonly channels: ChannelClient") {
		t.Error("Expected code to contain channels client")
	}

	// Should NOT include presence and typing
	if strings.Contains(code, "public readonly presence: PresenceClient") {
		t.Error("Expected code to NOT contain presence client")
	}
	if strings.Contains(code, "public readonly typing: TypingClient") {
		t.Error("Expected code to NOT contain typing client")
	}
}

func TestGenerator_GeneratesStreamingFiles(t *testing.T) {
	spec := &client.APISpec{
		Info: client.APIInfo{
			Title:   "Test API",
			Version: "1.0.0",
		},
		Streaming: &client.StreamingSpec{
			EnableRooms:    true,
			EnablePresence: true,
			EnableTyping:   true,
			EnableChannels: true,
			EnableHistory:  true,
		},
	}

	config := client.GeneratorConfig{
		Language:         "typescript",
		PackageName:      "test-client",
		OutputDir:        "./out",
		APIName:          "TestClient",
		IncludeStreaming: true,
		Version:          "1.0.0",
		Features: client.Features{
			Reconnection:    true,
			Heartbeat:       true,
			StateManagement: true,
			TypedErrors:     true,
		},
		Streaming: client.StreamingConfig{
			EnableRooms:            true,
			EnablePresence:         true,
			EnableTyping:           true,
			EnableChannels:         true,
			EnableHistory:          true,
			GenerateUnifiedClient:  true,
			GenerateModularClients: true,
		},
	}

	gen := NewGenerator()

	result, err := gen.Generate(t.Context(), spec, config)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	// Check that streaming files are generated
	expectedFiles := []string{
		"src/events.ts",
		"src/rooms.ts",
		"src/presence.ts",
		"src/typing.ts",
		"src/channels.ts",
		"src/streaming.ts",
	}

	for _, file := range expectedFiles {
		if _, ok := result.Files[file]; !ok {
			t.Errorf("Expected file %q to be generated", file)
		}
	}

	// Check that streaming types are in types.ts
	typesCode := result.Files["src/types.ts"]
	streamingTypes := []string{
		"export interface Message",
		"export interface Member",
		"export interface Room",
		"export interface RoomOptions",
		"export interface HistoryQuery",
		"export interface UserPresence",
	}

	for _, typ := range streamingTypes {
		if !strings.Contains(typesCode, typ) {
			t.Errorf("Expected types.ts to contain %q", typ)
		}
	}

	// Check index.ts exports streaming modules
	indexCode := result.Files["src/index.ts"]
	streamingExports := []string{
		"export * from './events'",
		"export * from './rooms'",
		"export * from './presence'",
		"export * from './typing'",
		"export * from './channels'",
		"export * from './streaming'",
	}

	for _, exp := range streamingExports {
		if !strings.Contains(indexCode, exp) {
			t.Errorf("Expected index.ts to contain %q", exp)
		}
	}
}

func TestGenerator_NoStreamingWhenDisabled(t *testing.T) {
	spec := &client.APISpec{
		Info: client.APIInfo{
			Title:   "Test API",
			Version: "1.0.0",
		},
	}

	config := client.GeneratorConfig{
		Language:         "typescript",
		PackageName:      "test-client",
		OutputDir:        "./out",
		APIName:          "TestClient",
		IncludeStreaming: false,
		Version:          "1.0.0",
		Features: client.Features{
			Reconnection:    false,
			StateManagement: false,
		},
		Streaming: client.StreamingConfig{
			EnableRooms:            false,
			EnablePresence:         false,
			EnableTyping:           false,
			EnableChannels:         false,
			GenerateUnifiedClient:  false,
			GenerateModularClients: false,
		},
	}

	gen := NewGenerator()

	result, err := gen.Generate(t.Context(), spec, config)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	// Check that streaming files are NOT generated
	unexpectedFiles := []string{
		"src/events.ts",
		"src/rooms.ts",
		"src/presence.ts",
		"src/typing.ts",
		"src/channels.ts",
		"src/streaming.ts",
	}

	for _, file := range unexpectedFiles {
		if _, ok := result.Files[file]; ok {
			t.Errorf("Unexpected file %q was generated", file)
		}
	}
}

func TestGenerator_ClientOnlyMode(t *testing.T) {
	spec := &client.APISpec{
		Info: client.APIInfo{
			Title:   "Test API",
			Version: "1.0.0",
		},
		Endpoints: []client.Endpoint{
			{
				Method: "GET",
				Path:   "/users",
			},
		},
	}

	config := client.GeneratorConfig{
		Language:         "typescript",
		PackageName:      "test-client",
		OutputDir:        "./out",
		APIName:          "TestClient",
		IncludeStreaming: false,
		Version:          "1.0.0",
		ClientOnly:       true, // Enable client-only mode
		GenerateTests:    true,
		GenerateLinting:  true,
		GenerateCI:       true,
	}

	gen := NewGenerator()

	result, err := gen.Generate(t.Context(), spec, config)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	// Check that package/config files are NOT generated
	unexpectedFiles := []string{
		"package.json",
		"tsconfig.json",
		"jest.config.js",
		"tests/client.test.ts",
		"tests/utils.ts",
		".eslintrc.js",
		".prettierrc",
		".prettierignore",
		".eslintignore",
		".github/workflows/ci.yml",
		".gitignore",
		".npmignore",
	}

	for _, file := range unexpectedFiles {
		if _, ok := result.Files[file]; ok {
			t.Errorf("Unexpected file %q was generated in client-only mode", file)
		}
	}

	// Check that source files ARE generated WITHOUT src/ prefix
	expectedFiles := []string{
		"fetch.ts",
		"errors.ts",
		"types.ts",
		"client.ts",
		"rest.ts",
		"index.ts",
	}

	for _, file := range expectedFiles {
		if _, ok := result.Files[file]; !ok {
			t.Errorf("Expected file %q to be generated in client-only mode", file)
		}
	}

	// Verify src/ prefixed files are NOT present
	unexpectedSrcFiles := []string{
		"src/fetch.ts",
		"src/errors.ts",
		"src/types.ts",
	}

	for _, file := range unexpectedSrcFiles {
		if _, ok := result.Files[file]; ok {
			t.Errorf("File %q should not have src/ prefix in client-only mode", file)
		}
	}
}

func TestGenerator_ClientOnlyWithStreaming(t *testing.T) {
	spec := &client.APISpec{
		Info: client.APIInfo{
			Title:   "Test API",
			Version: "1.0.0",
		},
		Streaming: &client.StreamingSpec{
			EnableRooms:    true,
			EnablePresence: true,
		},
	}

	config := client.GeneratorConfig{
		Language:         "typescript",
		PackageName:      "test-client",
		OutputDir:        "./out",
		APIName:          "TestClient",
		IncludeStreaming: true,
		Version:          "1.0.0",
		ClientOnly:       true, // Enable client-only mode
		Features: client.Features{
			Reconnection:    true,
			StateManagement: true,
		},
		Streaming: client.StreamingConfig{
			EnableRooms:            true,
			EnablePresence:         true,
			GenerateUnifiedClient:  true,
			GenerateModularClients: true,
		},
	}

	gen := NewGenerator()

	result, err := gen.Generate(t.Context(), spec, config)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	// Check that package/config files are NOT generated
	unexpectedFiles := []string{
		"package.json",
		"tsconfig.json",
	}

	for _, file := range unexpectedFiles {
		if _, ok := result.Files[file]; ok {
			t.Errorf("Unexpected file %q was generated in client-only mode", file)
		}
	}

	// Check that streaming source files ARE generated WITHOUT src/ prefix
	expectedFiles := []string{
		"events.ts",
		"rooms.ts",
		"presence.ts",
		"streaming.ts",
		"types.ts",
		"index.ts",
	}

	for _, file := range expectedFiles {
		if _, ok := result.Files[file]; !ok {
			t.Errorf("Expected file %q to be generated in client-only mode", file)
		}
	}

	// Verify src/ prefixed files are NOT present
	unexpectedSrcFiles := []string{
		"src/events.ts",
		"src/rooms.ts",
		"src/presence.ts",
	}

	for _, file := range unexpectedSrcFiles {
		if _, ok := result.Files[file]; ok {
			t.Errorf("File %q should not have src/ prefix in client-only mode", file)
		}
	}
}

func TestGenerator_AsyncAPIOnlyMode(t *testing.T) {
	spec := &client.APISpec{
		Info: client.APIInfo{
			Title:   "Test Streaming API",
			Version: "1.0.0",
		},
		Endpoints: []client.Endpoint{}, // No REST endpoints
		Streaming: &client.StreamingSpec{
			EnableRooms:    true,
			EnablePresence: true,
			EnableTyping:   true,
			EnableChannels: true,
		},
	}

	config := client.GeneratorConfig{
		Language:         "typescript",
		PackageName:      "@repo/api-client",
		OutputDir:        "./stream",
		APIName:          "StreamingClient",
		IncludeStreaming: true,
		Version:          "1.0.0",
		ClientOnly:       true,
		Features: client.Features{
			Reconnection:    true,
			Heartbeat:       true,
			StateManagement: true,
		},
		Streaming: client.StreamingConfig{
			EnableRooms:            true,
			EnablePresence:         true,
			EnableTyping:           true,
			EnableChannels:         true,
			GenerateUnifiedClient:  true,
			GenerateModularClients: true,
		},
	}

	gen := NewGenerator()

	result, err := gen.Generate(t.Context(), spec, config)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	// Check that REST-related files and config files are NOT generated (due to ClientOnly)
	unexpectedFiles := []string{
		"package.json",
		"tsconfig.json",
		"fetch.ts",
		"errors.ts",
		"client.ts",
		"rest.ts",
		"pagination.ts",
		"websocket.ts", // Generic websocket client
		"sse.ts",       // Generic SSE client
		// Also check src/ prefixed versions shouldn't exist
		"src/types.ts",
		"src/events.ts",
	}

	for _, file := range unexpectedFiles {
		if _, ok := result.Files[file]; ok {
			t.Errorf("Unexpected file %q was generated in AsyncAPI-only mode", file)
		}
	}

	// Check that ONLY streaming files ARE generated (without src/ prefix due to ClientOnly)
	expectedFiles := []string{
		"types.ts",
		"events.ts",
		"rooms.ts",
		"presence.ts",
		"typing.ts",
		"channels.ts",
		"streaming.ts",
		"index.ts",
	}

	for _, file := range expectedFiles {
		if _, ok := result.Files[file]; !ok {
			t.Errorf("Expected file %q to be generated in AsyncAPI-only mode", file)
		}
	}

	// Verify index.ts doesn't export REST modules
	indexCode := result.Files["index.ts"]
	unexpectedExports := []string{
		"export * from './fetch'",
		"export * from './errors'",
		"export * from './client'",
		"export * from './rest'",
	}

	for _, exp := range unexpectedExports {
		if strings.Contains(indexCode, exp) {
			t.Errorf("Index.ts should not contain %q in AsyncAPI-only mode", exp)
		}
	}

	// Verify index.ts DOES export streaming modules
	expectedExports := []string{
		"export * from './types'",
		"export * from './events'",
		"export * from './rooms'",
		"export * from './presence'",
		"export * from './typing'",
		"export * from './channels'",
		"export * from './streaming'",
	}

	for _, exp := range expectedExports {
		if !strings.Contains(indexCode, exp) {
			t.Errorf("Index.ts should contain %q in AsyncAPI-only mode", exp)
		}
	}
}

// Tests for production hardening features

func TestRoomsGenerator_ConnectionTimeout(t *testing.T) {
	spec := &client.APISpec{
		Info: client.APIInfo{
			Title:   "Test API",
			Version: "1.0.0",
		},
		Streaming: &client.StreamingSpec{
			EnableRooms: true,
			Rooms: &client.RoomOperations{
				Path: "/ws/rooms",
			},
		},
	}

	config := client.GeneratorConfig{
		Language:         "typescript",
		PackageName:      "test-client",
		IncludeStreaming: true,
		Features: client.Features{
			Reconnection:    true,
			StateManagement: true,
		},
		Streaming: client.StreamingConfig{
			EnableRooms:            true,
			GenerateModularClients: true,
		},
	}

	gen := NewRoomsGenerator()
	code := gen.Generate(spec, config)

	// Check for connection timeout configuration
	timeoutConfig := []string{
		"connectionTimeout?: number",
		"connectionTimeout: 30000",
		"connectionTimeoutId",
		"clearConnectionTimeout",
		"Connection timeout",
	}

	for _, item := range timeoutConfig {
		if !strings.Contains(code, item) {
			t.Errorf("Expected code to contain connection timeout config %q", item)
		}
	}
}

func TestRoomsGenerator_OfflineQueue(t *testing.T) {
	spec := &client.APISpec{
		Info: client.APIInfo{
			Title:   "Test API",
			Version: "1.0.0",
		},
		Streaming: &client.StreamingSpec{
			EnableRooms: true,
			Rooms: &client.RoomOperations{
				Path: "/ws/rooms",
			},
		},
	}

	config := client.GeneratorConfig{
		Language:         "typescript",
		PackageName:      "test-client",
		IncludeStreaming: true,
		Features: client.Features{
			Reconnection:    true,
			StateManagement: true,
		},
		Streaming: client.StreamingConfig{
			EnableRooms:            true,
			GenerateModularClients: true,
		},
	}

	gen := NewRoomsGenerator()
	code := gen.Generate(spec, config)

	// Check for offline queue functionality
	queueItems := []string{
		"QueuedMessage",
		"messageQueue",
		"enableOfflineQueue",
		"maxQueueSize",
		"queueMessageTTL",
		"flushQueue",
	}

	for _, item := range queueItems {
		if !strings.Contains(code, item) {
			t.Errorf("Expected code to contain offline queue item %q", item)
		}
	}
}

func TestRoomsGenerator_CrossPlatformEventEmitter(t *testing.T) {
	spec := &client.APISpec{
		Info: client.APIInfo{
			Title:   "Test API",
			Version: "1.0.0",
		},
		Streaming: &client.StreamingSpec{
			EnableRooms: true,
			Rooms: &client.RoomOperations{
				Path: "/ws/rooms",
			},
		},
	}

	config := client.GeneratorConfig{
		Language:         "typescript",
		PackageName:      "test-client",
		IncludeStreaming: true,
		Features: client.Features{
			Reconnection:    true,
			StateManagement: true,
		},
		Streaming: client.StreamingConfig{
			EnableRooms:            true,
			GenerateModularClients: true,
		},
	}

	gen := NewRoomsGenerator()
	code := gen.Generate(spec, config)

	// Check for cross-platform EventEmitter
	eventEmitterItems := []string{
		"class EventEmitter",
		"private listeners: Map<string, Set<Function>>",
		"on(event: string, handler: Function)",
		"off(event: string, handler: Function)",
		"emit(event: string, ...args: any[])",
		"removeAllListeners(event?: string)",
	}

	for _, item := range eventEmitterItems {
		if !strings.Contains(code, item) {
			t.Errorf("Expected code to contain EventEmitter item %q", item)
		}
	}
}

func TestRoomsGenerator_LazyWebSocketLoading(t *testing.T) {
	spec := &client.APISpec{
		Info: client.APIInfo{
			Title:   "Test API",
			Version: "1.0.0",
		},
		Streaming: &client.StreamingSpec{
			EnableRooms: true,
			Rooms: &client.RoomOperations{
				Path: "/ws/rooms",
			},
		},
	}

	config := client.GeneratorConfig{
		Language:         "typescript",
		PackageName:      "test-client",
		IncludeStreaming: true,
		Features: client.Features{
			Reconnection:    true,
			StateManagement: true,
		},
		Streaming: client.StreamingConfig{
			EnableRooms:            true,
			GenerateModularClients: true,
		},
	}

	gen := NewRoomsGenerator()
	code := gen.Generate(spec, config)

	// Check for lazy WebSocket loading
	lazyLoadItems := []string{
		"isBrowser",
		"window.WebSocket",
		"_WebSocketImpl",
		"getWebSocket()",
		"require('ws')",
	}

	for _, item := range lazyLoadItems {
		if !strings.Contains(code, item) {
			t.Errorf("Expected code to contain lazy WebSocket loading item %q", item)
		}
	}
}

func TestChannelsGenerator_OfflineQueue(t *testing.T) {
	spec := &client.APISpec{
		Info: client.APIInfo{
			Title:   "Test API",
			Version: "1.0.0",
		},
		Streaming: &client.StreamingSpec{
			EnableChannels: true,
			Channels: &client.ChannelOperations{
				Path: "/ws/channels",
			},
		},
	}

	config := client.GeneratorConfig{
		Language:         "typescript",
		PackageName:      "test-client",
		IncludeStreaming: true,
		Features: client.Features{
			Reconnection:    true,
			StateManagement: true,
		},
		Streaming: client.StreamingConfig{
			EnableChannels:         true,
			GenerateModularClients: true,
		},
	}

	gen := NewChannelsGenerator()
	code := gen.Generate(spec, config)

	// Check for offline queue functionality (channels uses publishQueue)
	queueItems := []string{
		"QueuedPublish",
		"publishQueue",
		"enableOfflineQueue",
		"maxQueueSize",
		"flushQueue",
	}

	for _, item := range queueItems {
		if !strings.Contains(code, item) {
			t.Errorf("Expected code to contain offline queue item %q", item)
		}
	}
}

func TestPresenceGenerator_ConnectionTimeout(t *testing.T) {
	spec := &client.APISpec{
		Info: client.APIInfo{
			Title:   "Test API",
			Version: "1.0.0",
		},
		Streaming: &client.StreamingSpec{
			EnablePresence: true,
			Presence: &client.PresenceOperations{
				Path: "/ws/presence",
			},
		},
	}

	config := client.GeneratorConfig{
		Language:         "typescript",
		PackageName:      "test-client",
		IncludeStreaming: true,
		Features: client.Features{
			Reconnection:    true,
			StateManagement: true,
		},
		Streaming: client.StreamingConfig{
			EnablePresence:         true,
			GenerateModularClients: true,
		},
	}

	gen := NewPresenceGenerator()
	code := gen.Generate(spec, config)

	// Check for connection timeout configuration
	timeoutConfig := []string{
		"connectionTimeout?: number",
		"connectionTimeout: 30000",
		"connectionTimeoutId",
		"clearConnectionTimeout",
	}

	for _, item := range timeoutConfig {
		if !strings.Contains(code, item) {
			t.Errorf("Expected code to contain connection timeout config %q", item)
		}
	}
}

func TestTypingGenerator_ConnectionTimeout(t *testing.T) {
	spec := &client.APISpec{
		Info: client.APIInfo{
			Title:   "Test API",
			Version: "1.0.0",
		},
		Streaming: &client.StreamingSpec{
			EnableTyping: true,
			Typing: &client.TypingOperations{
				Path: "/ws/typing",
			},
		},
	}

	config := client.GeneratorConfig{
		Language:         "typescript",
		PackageName:      "test-client",
		IncludeStreaming: true,
		Features: client.Features{
			Reconnection:    true,
			StateManagement: true,
		},
		Streaming: client.StreamingConfig{
			EnableTyping:           true,
			GenerateModularClients: true,
		},
	}

	gen := NewTypingGenerator()
	code := gen.Generate(spec, config)

	// Check for connection timeout configuration
	timeoutConfig := []string{
		"connectionTimeout?: number",
		"connectionTimeout: 30000",
		"connectionTimeoutId",
		"clearConnectionTimeout",
	}

	for _, item := range timeoutConfig {
		if !strings.Contains(code, item) {
			t.Errorf("Expected code to contain connection timeout config %q", item)
		}
	}
}

func TestPresenceGenerator_LazyWebSocketLoading(t *testing.T) {
	spec := &client.APISpec{
		Info: client.APIInfo{
			Title:   "Test API",
			Version: "1.0.0",
		},
		Streaming: &client.StreamingSpec{
			EnablePresence: true,
			Presence: &client.PresenceOperations{
				Path: "/ws/presence",
			},
		},
	}

	config := client.GeneratorConfig{
		Language:         "typescript",
		PackageName:      "test-client",
		IncludeStreaming: true,
		Features: client.Features{
			Reconnection:    true,
			StateManagement: true,
		},
		Streaming: client.StreamingConfig{
			EnablePresence:         true,
			GenerateModularClients: true,
		},
	}

	gen := NewPresenceGenerator()
	code := gen.Generate(spec, config)

	// Check for lazy WebSocket loading
	lazyLoadItems := []string{
		"isBrowser",
		"window.WebSocket",
		"_WebSocketImpl",
		"getWebSocket()",
	}

	for _, item := range lazyLoadItems {
		if !strings.Contains(code, item) {
			t.Errorf("Expected code to contain lazy WebSocket loading item %q", item)
		}
	}
}

func TestTypingGenerator_LazyWebSocketLoading(t *testing.T) {
	spec := &client.APISpec{
		Info: client.APIInfo{
			Title:   "Test API",
			Version: "1.0.0",
		},
		Streaming: &client.StreamingSpec{
			EnableTyping: true,
			Typing: &client.TypingOperations{
				Path: "/ws/typing",
			},
		},
	}

	config := client.GeneratorConfig{
		Language:         "typescript",
		PackageName:      "test-client",
		IncludeStreaming: true,
		Features: client.Features{
			Reconnection:    true,
			StateManagement: true,
		},
		Streaming: client.StreamingConfig{
			EnableTyping:           true,
			GenerateModularClients: true,
		},
	}

	gen := NewTypingGenerator()
	code := gen.Generate(spec, config)

	// Check for lazy WebSocket loading
	lazyLoadItems := []string{
		"isBrowser",
		"window.WebSocket",
		"_WebSocketImpl",
		"getWebSocket()",
	}

	for _, item := range lazyLoadItems {
		if !strings.Contains(code, item) {
			t.Errorf("Expected code to contain lazy WebSocket loading item %q", item)
		}
	}
}
