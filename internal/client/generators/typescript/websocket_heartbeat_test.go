package typescript_test

import (
	"context"
	"strings"
	"testing"

	"github.com/xraph/forge/internal/client"
	"github.com/xraph/forge/internal/client/generators/typescript"
)

// What the generated client's heartbeat has to actually do.
//
// It used to do nothing in a browser. The body was guarded on `!isBrowser` and
// called the `ws` package's protocol-level `ping()`, with a comment saying
// browsers handle ping/pong automatically. Browsers do answer a server's
// control ping automatically, and they expose no way to send one, so on the
// only platform this client mostly runs on the interval fired and nothing
// happened.
//
// That would be harmless against a server that measured liveness with control
// frames. This one does not. `extensions/streaming` closes a connection once
// `time.Since(GetLastActivity()) > PingInterval + PongTimeout`, and
// UpdateActivity is called only from the read loop, on an inbound application
// message. So the protocol ping was doubly useless: absent in browsers, and
// invisible to the server in Node. The heartbeat has to send a real message on
// both platforms. See extensions/streaming/heartbeat_test.go.

func heartbeatClient(t *testing.T) string {
	t.Helper()

	spec := &client.APISpec{
		Info: client.APIInfo{Title: "Chat API", Version: "1.0.0"},
		WebSockets: []client.WebSocketEndpoint{
			{
				ID:            "chat",
				Path:          "/chat",
				Description:   "Chat WebSocket",
				SendSchema:    &client.Schema{Type: "object"},
				ReceiveSchema: &client.Schema{Type: "object"},
			},
		},
	}

	config := client.GeneratorConfig{
		Language:         "typescript",
		OutputDir:        "./chatclient",
		PackageName:      "@example/chatclient",
		APIName:          "ChatClient",
		BaseURL:          "https://api.example.com",
		Version:          "1.0.0",
		IncludeStreaming: true,
		Features:         client.Features{Reconnection: true, Heartbeat: true},
	}

	result, err := typescript.NewGenerator().Generate(context.Background(), spec, config)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	code, ok := result.Files["src/websocket.ts"]
	if !ok {
		t.Fatal("src/websocket.ts was not generated")
	}

	return code
}

// heartbeatBody returns just the startHeartbeat method, so an assertion about
// the heartbeat cannot be satisfied by an unrelated part of the file.
func heartbeatBody(t *testing.T, code string) string {
	t.Helper()

	const marker = "private startHeartbeat(): void {"

	start := strings.Index(code, marker)
	if start < 0 {
		t.Fatal("generated client has no startHeartbeat method")
	}

	rest := code[start:]

	end := strings.Index(rest, "private stopHeartbeat")
	if end < 0 {
		return rest
	}

	return rest[:end]
}

func TestGeneratedHeartbeat_SendsAnApplicationMessage(t *testing.T) {
	t.Parallel()

	body := heartbeatBody(t, heartbeatClient(t))

	// The server counts inbound application messages and nothing else, so the
	// heartbeat has to put one on the wire.
	if !strings.Contains(body, "this.ws.send(") {
		t.Error("heartbeat does not send anything on the socket")
	}

	if !strings.Contains(body, "'system'") || !strings.Contains(body, "'ping'") {
		t.Errorf("heartbeat does not send the system ping the server understands:\n%s", body)
	}
}

func TestGeneratedHeartbeat_IsNotGatedOnRunningOutsideABrowser(t *testing.T) {
	t.Parallel()

	body := heartbeatBody(t, heartbeatClient(t))

	// The browser is the platform this client mostly runs on. A heartbeat that
	// skips it is a heartbeat that does not exist.
	if strings.Contains(body, "!isBrowser") {
		t.Errorf("heartbeat is still skipped in browsers:\n%s", body)
	}
}

func TestGeneratedHeartbeat_SurvivesASendThatThrows(t *testing.T) {
	t.Parallel()

	body := heartbeatBody(t, heartbeatClient(t))

	// The send runs on a timer, so nothing is awaiting it and nothing would
	// catch a throw. A socket that died between the readyState check and the
	// send must not surface as an unhandled error.
	if !strings.Contains(body, "try {") || !strings.Contains(body, "catch") {
		t.Errorf("heartbeat send is not guarded against a throwing socket:\n%s", body)
	}
}
