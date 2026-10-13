package typescript

import (
	"strings"
	"testing"

	"github.com/xraph/forge/internal/client"
)

func TestWebSocketInlineMessagesOmitUnusedCodecImports(t *testing.T) {
	spec := &client.APISpec{WebSockets: []client.WebSocketEndpoint{{ID: "updates", Path: "/updates", SendSchema: &client.Schema{Type: "object"}, ReceiveSchema: &client.Schema{Type: "object"}}}}
	cfg := client.DefaultConfig()
	cfg.FieldNaming = client.NamingCamel
	code, _ := NewWebSocketGenerator().Generate(spec, cfg)

	if strings.Contains(code, "import { decode, encode }") {
		t.Fatal("inline messages generated unused codec imports")
	}
}

func TestSSEInlineMessagesOmitUnusedCodecImports(t *testing.T) {
	spec := &client.APISpec{SSEs: []client.SSEEndpoint{{ID: "updates", Path: "/updates", EventSchemas: map[string]*client.Schema{"updated": {Type: "object"}}}}}
	cfg := client.DefaultConfig()
	cfg.FieldNaming = client.NamingCamel
	code, _ := NewSSEGenerator().Generate(spec, cfg)

	if strings.Contains(code, "import { decode }") {
		t.Fatal("inline SSE events generated unused codec imports")
	}
}
