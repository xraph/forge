package typescript

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xraph/forge/internal/client"
	"github.com/xraph/forge/internal/client/generators"
)

// The fixtures under internal/client/testdata/injection carry specification
// text written to break out of the context each generator prints it into:
// a summary that closes its JSDoc block, a path that closes its template
// literal, a query parameter name that closes its string, and a schema name
// that closes its interface. Each one used to run as code in the generated
// client. These tests assert the payloads now arrive inert.

func injectionFixture(t *testing.T, name string) *client.APISpec {
	t.Helper()

	spec, err := client.NewSpecParser().ParseFile(context.Background(),
		filepath.Join("..", "..", "testdata", "injection", name))
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}

	return spec
}

func injectionConfig() client.GeneratorConfig {
	cfg := client.DefaultConfig()
	cfg.Language = "typescript"
	cfg.PackageName = "probe"

	return cfg
}

func generateInjectionClient(t *testing.T, spec *client.APISpec) *generators.GeneratedClient {
	t.Helper()

	out, err := NewGenerator().Generate(context.Background(), spec, injectionConfig())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	return out
}

// codeLines returns the lines of src that are not inside a comment, with
// string literals left in place. It is deliberately simple: generated code
// opens and closes block comments on their own lines, so a payload that
// escaped its comment shows up here as a code line.
func codeLines(src string) []string {
	var (
		out       []string
		inComment bool
	)

	for line := range strings.SplitSeq(src, "\n") {
		trimmed := strings.TrimSpace(line)

		switch {
		case inComment:
			if strings.Contains(trimmed, "*/") {
				inComment = false
			}

			continue
		case strings.HasPrefix(trimmed, "/**") || strings.HasPrefix(trimmed, "/*"):
			if !strings.Contains(trimmed, "*/") {
				inComment = true
			}

			continue
		case strings.HasPrefix(trimmed, "//"):
			continue
		}

		out = append(out, line)
	}

	return out
}

func TestRESTSpecTextCannotEscapeItsContext(t *testing.T) {
	out := generateInjectionClient(t, injectionFixture(t, "evil.json"))
	rest := out.Files["src/rest.ts"]

	// The summary keeps its text but cannot close the comment.
	if !strings.Contains(rest, `* S1 *\/ static __p = (globalThis.PWN_SUMMARY = 1); /*`) {
		t.Errorf("summary was not escaped inside its JSDoc block:\n%s", rest)
	}

	// Every line of a multi-line description stays inside the comment.
	if !strings.Contains(rest, "   * func init() { panic(\"PWN_GO_DESC\") }\n") {
		t.Errorf("second description line lost its comment prefix:\n%s", rest)
	}

	// The path's backticks are escaped inside the template literal.
	if !strings.Contains(rest, "`/items/${encodeURIComponent(String(id))}\\`+(globalThis.PWN_PATH=1)+\\``") {
		t.Errorf("path was not escaped inside its template literal:\n%s", rest)
	}

	// The query parameter name is a JSON string key, and the variable holding
	// its value is an identifier.
	if !strings.Contains(rest, `queryParams["q'] = (globalThis.PWN_QUERY=1); queryParams['z"] = qglobalThisPwnQuery1QueryParamsz;`) {
		t.Errorf("query parameter name was not quoted and sanitised:\n%s", rest)
	}

	for _, line := range codeLines(rest) {
		for _, payload := range []string{"static __p", "func init", "queryParams['z'"} {
			if strings.Contains(line, payload) {
				t.Errorf("payload %q reached code: %s", payload, line)
			}
		}
	}

	types := out.Files["src/types.ts"]
	for _, line := range codeLines(types) {
		if strings.Contains(line, "var PWN") || strings.Contains(line, "final pwn") {
			t.Errorf("description payload reached code in types.ts: %s", line)
		}
	}

	assertParsesWithNode(t, map[string]string{"rest.ts": rest, "types.ts": types})
}

func TestSchemaNameThatIsNotAnIdentifierIsRejected(t *testing.T) {
	_, err := NewGenerator().Generate(context.Background(), injectionFixture(t, "evil_name.json"), injectionConfig())
	if err == nil {
		t.Fatal("expected a schema name that is not an identifier to be rejected")
	}

	if !strings.Contains(err.Error(), "X {} ; globalThis.PWN_NAME=1; interface Y") ||
		!strings.Contains(err.Error(), "not valid TypeScript identifiers") {
		t.Errorf("error does not name the offending schema clearly: %v", err)
	}
}

func TestDanglingRefNameThatIsNotAnIdentifierIsRejected(t *testing.T) {
	spec := &client.APISpec{
		Info: client.APIInfo{Title: "T", Version: "1"},
		Endpoints: []client.Endpoint{{
			Method: "GET", Path: "/x", OperationID: "getX",
			Responses: map[int]*client.Response{200: {
				Description: "ok",
				Content: map[string]*client.MediaType{"application/json": {
					Schema: &client.Schema{Ref: "#/components/schemas/A>; globalThis.PWN=1; type B<"},
				}},
			}},
		}},
	}

	if _, err := NewGenerator().Generate(context.Background(), spec, injectionConfig()); err == nil {
		t.Fatal("expected a reference whose name is not an identifier to be rejected")
	}
}

const streamPayload = "a`'\"*/ ;globalThis.PWN=1;/*\r${x}"

func TestStreamingSpecTextCannotEscapeItsContext(t *testing.T) {
	spec := &client.APISpec{
		Info: client.APIInfo{Title: "T", Version: "1"},
		WebSockets: []client.WebSocketEndpoint{{
			ID: "chat'];x(", Path: "/ws/" + streamPayload, Description: "d\n" + streamPayload,
		}},
		SSEs: []client.SSEEndpoint{{
			ID: "feed", Path: "/sse/" + streamPayload, Description: streamPayload,
			EventSchemas: map[string]*client.Schema{streamPayload: {Type: "object"}},
		}},
		WebTransports: []client.WebTransportEndpoint{{
			ID: "wt", Path: "/wt/" + streamPayload, Description: streamPayload,
		}},
		Streaming: &client.StreamingSpec{
			EnableRooms: true, EnablePresence: true, EnableTyping: true, EnableChannels: true,
			Rooms:    &client.RoomOperations{Path: "/rooms/" + streamPayload},
			Presence: &client.PresenceOperations{Path: "/presence/" + streamPayload, Statuses: []string{"online", streamPayload}},
			Typing:   &client.TypingOperations{Path: "/typing/" + streamPayload},
			Channels: &client.ChannelOperations{Path: "/channels/" + streamPayload},
		},
	}

	cfg := injectionConfig()
	cfg.IncludeStreaming = true
	cfg.Streaming.EnableRooms = true
	cfg.Streaming.EnablePresence = true
	cfg.Streaming.EnableTyping = true
	cfg.Streaming.EnableChannels = true

	ws, _ := NewWebSocketGenerator().Generate(spec, cfg)
	sse, _ := NewSSEGenerator().Generate(spec, cfg)
	wt, _ := NewWebTransportGenerator().Generate(spec, cfg)

	files := map[string]string{
		"websocket.ts":    ws,
		"sse.ts":          sse,
		"webtransport.ts": wt,
		"rooms.ts":        NewRoomsGenerator().Generate(spec, cfg),
		"presence.ts":     NewPresenceGenerator().Generate(spec, cfg),
		"typing.ts":       NewTypingGenerator().Generate(spec, cfg),
		"channels.ts":     NewChannelsGenerator().Generate(spec, cfg),
	}

	quoted := jsonString(streamPayload)

	for name, src := range files {
		for _, line := range codeLines(src) {
			// Outside comments the payload may only appear as a JSON string.
			if strings.Contains(line, "globalThis.PWN") && !strings.Contains(line, strings.Trim(quoted, `"`)) {
				t.Errorf("%s: payload reached code unquoted: %s", name, line)
			}

			if strings.Contains(line, "'];x(") {
				t.Errorf("%s: channel id reached an identifier unsanitised: %s", name, line)
			}
		}
	}

	assertParsesWithNode(t, files)

	if !strings.Contains(ws, "class ChatxWSClient") {
		t.Errorf("websocket class name was not sanitised:\n%s", ws)
	}
}

// nodeStripScript turns a TypeScript file into JavaScript with Node's own
// type stripper, so `node --check` can parse the result. Only erasable syntax
// survives stripping, which is why the tests pass it the files that use no
// enums or parameter properties.
const nodeStripScript = `
const fs = require('node:fs');
const { stripTypeScriptTypes } = require('node:module');
fs.writeFileSync(process.argv[2], stripTypeScriptTypes(fs.readFileSync(process.argv[1], 'utf8'), { mode: 'strip' }));
`

// assertParsesWithNode checks that the generated files still parse, using a
// Node.js new enough to strip TypeScript types, and skips the check when no
// such Node is installed. tsc is not assumed to be available.
func assertParsesWithNode(t *testing.T, files map[string]string) {
	t.Helper()

	node, err := exec.LookPath("node")
	if err != nil {
		t.Log("node not found; skipping the parse check")

		return
	}

	dir := t.TempDir()

	check := func(name, src string) error {
		in := filepath.Join(dir, name)
		if err := os.WriteFile(in, []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}

		out := in + ".mjs"
		if msg, err := exec.CommandContext(t.Context(), node, "-e", nodeStripScript, in, out).CombinedOutput(); err != nil {
			return fmt.Errorf("%w\n%s", err, msg)
		}

		if msg, err := exec.CommandContext(t.Context(), node, "--check", out).CombinedOutput(); err != nil {
			return fmt.Errorf("%w\n%s", err, msg)
		}

		return nil
	}

	if err := check("probe.ts", "const x: number = 1;\nexport { x };\n"); err != nil {
		t.Logf("this node cannot strip TypeScript; skipping the parse check: %v", err)

		return
	}

	for name, src := range files {
		err := check(name, src)

		switch {
		case err == nil:
		case strings.Contains(err.Error(), "ERR_UNSUPPORTED_TYPESCRIPT_SYNTAX"):
			// Enums and parameter properties are valid TypeScript that only
			// stripping cannot handle; that says nothing about the payload.
			t.Logf("%s uses syntax Node cannot strip; not parse-checked", name)
		default:
			t.Errorf("%s does not parse: %v", name, err)
		}
	}
}
