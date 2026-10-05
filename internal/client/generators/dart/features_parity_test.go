package dart

import (
	"context"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/xraph/forge/internal/client/generators/typescript"
)

// frame is one outbound JSON frame: the keys it carries in order and which of
// them the sender leaves out when it has no value.
type frame struct {
	keys     []string
	optional map[string]bool
	spreads  bool
}

var (
	tsFrameBlock      = regexp.MustCompile(`(?s)JSON\.stringify\(\{(.*?)\}\)`)
	tsRequestBlock    = regexp.MustCompile(`(?s)sendRequest\('(\w+)', \{(.*?)\}\)`)
	dartFrameBlock    = regexp.MustCompile(`(?s)(?:\.send\(|_send\(|=>\s*)(?:const )?\{(.*?)\}(?:\)|,|;)`)
	dartRequestBlock  = regexp.MustCompile(`(?s)_request\('(\w+)', \{(.*?)\}\)`)
	tsToken           = regexp.MustCompile(`^(\.\.\.)?(\w+)(?::\s*(.*))?$`)
	dartToken         = regexp.MustCompile(`^'(\w+)':\s*(\?)?(.*)$`)
	frameTokenBreaker = regexp.MustCompile(`[\n,]`)
)

// literal returns the string literal a token's value is, or "".
func literal(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 && (value[0] == '\'' || value[0] == '"') && value[len(value)-1] == value[0] {
		return value[1 : len(value)-1]
	}

	return ""
}

// tsFrames reads every outbound frame in TypeScript source, keyed by its
// "type:x" or "action:x" discriminator.
func tsFrames(source string) map[string][]frame {
	out := map[string][]frame{}

	for _, m := range tsFrameBlock.FindAllStringSubmatch(source, -1) {
		f, id := parseTS(m[1])
		if id != "" {
			out[id] = append(out[id], f)
		}
	}

	for _, m := range tsRequestBlock.FindAllStringSubmatch(source, -1) {
		f, _ := parseTS(m[2])
		f.keys = append([]string{"type", "request_id"}, f.keys...)
		out["type:"+m[1]] = append(out["type:"+m[1]], f)
	}

	return out
}

func parseTS(block string) (frame, string) {
	f := frame{optional: map[string]bool{}}
	id := ""

	for _, token := range frameTokenBreaker.Split(block, -1) {
		m := tsToken.FindStringSubmatch(strings.TrimSpace(token))
		if m == nil {
			continue
		}

		if m[1] != "" {
			f.spreads = true

			continue
		}

		f.keys = append(f.keys, m[2])

		if (m[2] == "type" || m[2] == "action") && literal(m[3]) != "" {
			id = m[2] + ":" + literal(m[3])
		}
	}

	return f, id
}

// dartFrames reads every outbound frame in Dart source.
func dartFrames(source string) map[string][]frame {
	out := map[string][]frame{}

	for _, m := range dartFrameBlock.FindAllStringSubmatch(source, -1) {
		f, id := parseDart(m[1])
		if id != "" {
			out[id] = append(out[id], f)
		}
	}

	for _, m := range dartRequestBlock.FindAllStringSubmatch(source, -1) {
		f, _ := parseDart(m[2])
		f.keys = append([]string{"type", "request_id"}, f.keys...)
		out["type:"+m[1]] = append(out["type:"+m[1]], f)
	}

	return out
}

func parseDart(block string) (frame, string) {
	f := frame{optional: map[string]bool{}}
	id := ""

	for _, token := range frameTokenBreaker.Split(block, -1) {
		m := dartToken.FindStringSubmatch(strings.TrimSpace(token))
		if m == nil {
			continue
		}

		f.keys = append(f.keys, m[1])
		f.optional[m[1]] = m[2] != ""

		if (m[1] == "type" || m[1] == "action") && literal(m[3]) != "" {
			id = m[1] + ":" + literal(m[3])
		}
	}

	return f, id
}

// TestFeatureFramesMatchTheTypeScriptClients reads the frames the generated
// TypeScript feature clients send and the ones the Dart clients send, and
// holds the Dart ones to the same keys in the same order. A key the Dart frame
// adds is one TypeScript spreads in from an options object, and Dart sends it
// only when given a value.
func TestFeatureFramesMatchTheTypeScriptClients(t *testing.T) {
	f := streamingFixture()

	ts := f.Config
	ts.Language = "typescript"

	tsOut, err := typescript.NewGenerator().Generate(context.Background(), f.Spec, ts)
	if err != nil {
		t.Fatalf("typescript: %v", err)
	}

	var tsSource strings.Builder

	for _, name := range []string{"src/rooms.ts", "src/presence.ts", "src/typing.ts", "src/channels.ts", "src/websocket.ts"} {
		code, ok := tsOut.Files[name]
		if !ok {
			t.Fatalf("the TypeScript generator wrote no %s", name)
		}

		tsSource.WriteString(code)
	}

	dartOut := generate(t, f)

	var dartSource strings.Builder

	for name, code := range dartOut.Files {
		if strings.HasPrefix(name, "lib/src/streaming/") {
			dartSource.WriteString(code)
		}
	}

	want := tsFrames(tsSource.String())
	got := dartFrames(dartSource.String())

	inventory := []string{
		"type:join", "type:leave", "type:message", "type:history",
		"type:presence", "type:subscribe_presence", "type:unsubscribe_presence", "type:heartbeat",
		"type:typing",
		"action:subscribe", "action:unsubscribe", "action:publish",
		"type:system",
	}

	for _, id := range inventory {
		tsFrame, ok := want[id]
		if !ok {
			t.Errorf("the TypeScript clients no longer send %s; update the inventory", id)

			continue
		}

		dartFrame, ok := got[id]
		if !ok {
			t.Errorf("the Dart clients never send %s, which the TypeScript clients do", id)

			continue
		}

		for _, other := range tsFrame[1:] {
			if !slices.Equal(other.keys, tsFrame[0].keys) {
				t.Errorf("%s: the TypeScript clients disagree among themselves: %v and %v", id, tsFrame[0].keys, other.keys)
			}
		}

		for _, d := range dartFrame {
			assertFrame(t, id, tsFrame[0], d)
		}
	}

	var extra []string

	for id := range got {
		if _, ok := want[id]; !ok {
			extra = append(extra, id)
		}
	}

	sort.Strings(extra)

	if len(extra) > 0 {
		t.Errorf("the Dart clients send frames the TypeScript clients do not: %v", extra)
	}
}

func assertFrame(t *testing.T, id string, ts, dart frame) {
	t.Helper()

	inTS := map[string]bool{}
	for _, k := range ts.keys {
		inTS[k] = true
	}

	var shared []string

	for _, k := range dart.keys {
		if inTS[k] {
			shared = append(shared, k)

			continue
		}

		if !ts.spreads {
			t.Errorf("%s: Dart sends %q, which TypeScript does not and has no options to spread it from", id, k)
		}

		if !dart.optional[k] {
			t.Errorf("%s: Dart sends %q, a TypeScript options field, whether or not it has a value", id, k)
		}
	}

	// TypeScript's custom_status is optional by being undefined; Dart omits it
	// the same way. Every other key is always sent.
	if !slices.Equal(shared, ts.keys) {
		t.Errorf("%s: Dart sends keys %v, TypeScript sends %v", id, shared, ts.keys)
	}

	for _, k := range shared {
		if dart.optional[k] && k != "custom_status" {
			t.Errorf("%s: Dart leaves out %q when it has no value, TypeScript always sends it", id, k)
		}
	}
}
