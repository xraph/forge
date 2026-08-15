package golang

import (
	"fmt"
	"strings"

	"github.com/xraph/forge/internal/client"
)

// StreamingGenerator emits streaming.go: the declarations that more than one
// streaming transport's file refers to but none of them owns.
//
// ConnectionState and the reconnect-backoff helpers used to be emitted by
// websocket.go, which sse.go then referred to. That compiled only for a spec
// carrying both transports: generator.go emits websocket.go only when the
// spec declares a WebSocket endpoint, so an API that streams over SSE alone
// produced a client whose sse.go named half a dozen identifiers nothing
// declared. Hoisting them here decides the question once -- these belong to
// the package, not to whichever transport happened to be emitted first --
// rather than leaving each file to borrow from a neighbour that may not be
// there.
type StreamingGenerator struct{}

// NewStreamingGenerator creates a new shared streaming generator.
func NewStreamingGenerator() *StreamingGenerator {
	return &StreamingGenerator{}
}

// Generate generates the streaming.go file. It takes no spec: nothing it
// emits varies with the endpoints, only with the features that gate which
// declarations the transport files will reach for.
func (s *StreamingGenerator) Generate(config client.GeneratorConfig) string {
	var buf strings.Builder

	buf.WriteString(fmt.Sprintf("package %s\n\n", config.PackageName))

	// time's only users here are the reconnect helpers below, so the import
	// block goes away entirely along with them -- an unused import is a
	// compile error, and this file has nothing else to spend one on.
	if config.Features.Reconnection {
		buf.WriteString("import (\n")
		buf.WriteString("\t\"time\"\n")
		buf.WriteString(")\n\n")
	}

	buf.WriteString(s.generateConnectionStateType())

	if config.Features.Reconnection {
		buf.WriteString(s.generateReconnectHelpers())
	}

	return buf.String()
}

// generateConnectionStateType generates the ConnectionState type.
//
// Emitted whether or not StateManagement is on, which is what websocket.go
// did when it owned this. Only the state-management code paths refer to it,
// so with the feature off this is a declared-and-unused type -- legal Go,
// unlike the unused import above, and cheaper than a gate that would have to
// agree with every transport file's separate view of when it needs the type.
func (s *StreamingGenerator) generateConnectionStateType() string {
	var buf strings.Builder

	buf.WriteString("// ConnectionState represents the state of a streaming connection\n")
	buf.WriteString("type ConnectionState string\n\n")
	buf.WriteString("const (\n")
	buf.WriteString("\tConnectionStateDisconnected  ConnectionState = \"disconnected\"\n")
	buf.WriteString("\tConnectionStateConnecting    ConnectionState = \"connecting\"\n")
	buf.WriteString("\tConnectionStateConnected     ConnectionState = \"connected\"\n")
	buf.WriteString("\tConnectionStateReconnecting  ConnectionState = \"reconnecting\"\n")
	buf.WriteString("\tConnectionStateClosed        ConnectionState = \"closed\"\n")
	buf.WriteString("\tConnectionStateError         ConnectionState = \"error\"\n")
	buf.WriteString(")\n\n")

	return buf.String()
}

// generateReconnectHelpers generates the shared reconnection configuration
// and backoff calculation. Both websocket.go's and sse.go's reconnect methods
// call calculateBackoff and hold a reconnectConfig.
func (s *StreamingGenerator) generateReconnectHelpers() string {
	var buf strings.Builder

	buf.WriteString("// reconnectConfig holds reconnection configuration\n")
	buf.WriteString("type reconnectConfig struct {\n")
	buf.WriteString("\tinitialDelay  time.Duration\n")
	buf.WriteString("\tmaxDelay      time.Duration\n")
	buf.WriteString("\tmaxAttempts   int\n")
	buf.WriteString("\tbackoffFactor float64\n")
	buf.WriteString("}\n\n")

	buf.WriteString("func defaultReconnectConfig() reconnectConfig {\n")
	buf.WriteString("\treturn reconnectConfig{\n")
	buf.WriteString("\t\tinitialDelay:  time.Second,\n")
	buf.WriteString("\t\tmaxDelay:      30 * time.Second,\n")
	buf.WriteString("\t\tmaxAttempts:   10,\n")
	buf.WriteString("\t\tbackoffFactor: 2.0,\n")
	buf.WriteString("\t}\n")
	buf.WriteString("}\n\n")

	buf.WriteString("func calculateBackoff(attempt int, config reconnectConfig) time.Duration {\n")
	buf.WriteString("\tdelay := float64(config.initialDelay)\n")
	buf.WriteString("\tfor i := 0; i < attempt; i++ {\n")
	buf.WriteString("\t\tdelay *= config.backoffFactor\n")
	buf.WriteString("\t}\n")
	buf.WriteString("\tif time.Duration(delay) > config.maxDelay {\n")
	buf.WriteString("\t\treturn config.maxDelay\n")
	buf.WriteString("\t}\n")
	buf.WriteString("\treturn time.Duration(delay)\n")
	buf.WriteString("}\n\n")

	return buf.String()
}
