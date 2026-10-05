package dart

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/xraph/forge/internal/client"
	"github.com/xraph/forge/internal/client/generators/typescript"
)

// codecParityFile is the wire payloads both runtimes read. It sits in
// packages/client-fixtures/codec beside the files the two runtime libraries
// share, and is skipped by their loaders through its kind.
const codecParityFile = "codec/generated-codecs.json"

type codecParityCase struct {
	Name  string `json:"name"`
	Codec string `json:"codec"`
	Wire  any    `json:"wire"`

	// Client, when present, is the client shape both decoders must produce.
	// The two runtimes agreeing is the assertion; this only stops them from
	// agreeing on the wrong thing.
	Client    any  `json:"client"`
	HasClient bool `json:"-"`

	// RoundTrip says encoding the decoded value returns the wire payload.
	RoundTrip bool `json:"roundTrip"`
}

type codecParityVariant struct {
	FieldOverrides map[string]string `json:"fieldOverrides"`
	Cases          []codecParityCase `json:"cases"`
}

type codecParityDoc struct {
	Kind     string                        `json:"kind"`
	Variants map[string]codecParityVariant `json:"variants"`
}

// UnmarshalJSON records whether the case carried a client shape, since an
// explicit null is a shape and an absent key is not.
func (c *codecParityCase) UnmarshalJSON(data []byte) error {
	type plain codecParityCase

	var p plain
	if err := json.Unmarshal(data, &p); err != nil {
		return err
	}

	var keys map[string]json.RawMessage
	if err := json.Unmarshal(data, &keys); err != nil {
		return err
	}

	*c = codecParityCase(p)
	_, c.HasClient = keys["client"]

	return nil
}

func readCodecParity(t *testing.T) codecParityDoc {
	t.Helper()

	path, err := filepath.Abs(filepath.Join("..", "..", "..", "..", "packages", "client-fixtures", filepath.FromSlash(codecParityFile)))
	if err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the shared wire payloads are missing: %v", err)
	}

	var doc codecParityDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}

	if doc.Kind != "generated-codec-parity" || len(doc.Variants) == 0 {
		t.Fatalf("%s is not a generated-codec-parity file", path)
	}

	return doc
}

// codecParitySpec is the orders spec plus the shapes a rename has to survive
// that it lacks: two schemas that refer to each other, and unions nested in an
// object, a list and a record.
func codecParitySpec() *client.APISpec {
	spec := ordersSpec()

	spec.Schemas["Author"] = &client.Schema{
		Type: "object", Required: []string{"name"},
		Properties: map[string]*client.Schema{
			"name":          {Type: "string"},
			"books":         {Type: "array", Items: ref("Book")},
			"favorite_book": {Ref: "#/components/schemas/Book", Nullable: true},
		},
	}
	spec.Schemas["Book"] = &client.Schema{
		Type: "object", Required: []string{"book_title"},
		Properties: map[string]*client.Schema{
			"book_title": {Type: "string"},
			"pages":      {Type: "integer", Format: "int64"},
			"written_by": ref("Author"),
		},
	}
	spec.Schemas["Owner"] = &client.Schema{
		Type: "object", Required: []string{"owner_name"},
		Properties: map[string]*client.Schema{
			"owner_name":   {Type: "string"},
			"pet_info":     ref("Pet"),
			"shapes":       {Type: "array", Items: ref("Shape")},
			"pets_by_name": {Type: "object", AdditionalProperties: ref("Pet")},
		},
	}

	spec.Endpoints = append(spec.Endpoints,
		client.Endpoint{
			Method: "GET", Path: "/authors/{id}", OperationID: "authors.get",
			PathParams: []client.Parameter{{Name: "id", In: "path", Required: true, Schema: &client.Schema{Type: "string"}}},
			Responses:  map[int]*client.Response{200: {Content: jsonContent(ref("Author"))}},
		},
		client.Endpoint{
			Method: "GET", Path: "/owners/{id}", OperationID: "owners.get",
			PathParams: []client.Parameter{{Name: "id", In: "path", Required: true, Schema: &client.Schema{Type: "string"}}},
			Responses:  map[int]*client.Response{200: {Content: jsonContent(ref("Owner"))}},
		},
	)

	return spec
}

// dartCodecConst is the name of a component's codec in the generated Dart.
func dartCodecConst(schema string) string {
	return strings.ToLower(schema[:1]) + schema[1:] + "Codec"
}

// requireNode skips unless node and a bundler are both available, so the skip
// message is the same whichever is missing.
func requireNode(t *testing.T) (esbuild []string, node string) {
	t.Helper()

	if path, err := exec.LookPath("node"); err != nil {
		t.Skip("node not found on PATH; skipping the cross-runtime codec parity test")
	} else {
		node = path
	}

	if path, err := exec.LookPath("esbuild"); err == nil {
		return []string{path}, node
	}

	// npx being on PATH does not mean esbuild is reachable through it.
	if path, err := exec.LookPath("npx"); err == nil {
		if exec.CommandContext(context.Background(), path, "--no-install", "esbuild", "--version").Run() == nil {
			return []string{path, "--no-install", "esbuild"}, node
		}
	}

	t.Skip("esbuild not available (not on PATH, and not reachable via npx --no-install); skipping the cross-runtime codec parity test")

	return nil, ""
}

// writeTree writes files under dir.
func writeTree(t *testing.T, dir string, files map[string]string) {
	t.Helper()

	for name, content := range files {
		full := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// runTypeScriptCodecs generates the TypeScript client for spec, bundles its
// codec table with a driver that runs every request, and returns one result per
// request, exactly as runCodecs does for Dart.
func runTypeScriptCodecs(t *testing.T, spec *client.APISpec, overrides map[string]string, requests []map[string]any) []any {
	t.Helper()

	esbuild, node := requireNode(t)

	cfg := client.DefaultConfig()
	cfg.Language = "typescript"
	cfg.PackageName = "codecparity"
	cfg.FieldOverrides = overrides

	out, err := typescript.NewGenerator().Generate(context.Background(), spec, cfg)
	if err != nil {
		t.Fatalf("typescript: generate: %v", err)
	}

	payload, err := json.Marshal(requests)
	if err != nil {
		t.Fatal(err)
	}

	// The payload goes in as a string for JSON.parse, never as an object
	// literal: a literal {"__proto__": ...} sets a prototype instead of a key.
	quoted, err := json.Marshal(string(payload))
	if err != nil {
		t.Fatal(err)
	}

	driver := "import { decode, encode } from './codecs';\n\n" +
		"const requests = JSON.parse(" + string(quoted) + ") as Array<{ codec: string; direction: string; input: unknown }>;\n" +
		"const results = requests.map((r) => (r.direction === 'decode' ? decode(r.input, r.codec) : encode(r.input, r.codec)));\n" +
		"console.log(JSON.stringify(results));\n"

	dir := t.TempDir()
	writeTree(t, dir, out.Files)
	writeTree(t, dir, map[string]string{"src/__codec_parity.ts": driver})

	bundle := filepath.Join(dir, "__bundle.mjs")
	args := append(append([]string{}, esbuild[1:]...), "src/__codec_parity.ts", "--bundle", "--platform=node", "--format=esm", "--outfile="+bundle)

	cmd := exec.CommandContext(context.Background(), esbuild[0], args...)
	cmd.Dir = dir

	if combined, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("esbuild failed: %v\n%s", err, combined)
	}

	run := exec.CommandContext(context.Background(), node, bundle)
	run.Dir = dir

	stdout, err := run.Output()
	if err != nil {
		t.Fatalf("node failed: %v\n%s", err, stdout)
	}

	var results []any
	if err := json.Unmarshal(stdout, &results); err != nil {
		t.Fatalf("decode node output: %v\n%s", err, stdout)
	}

	return results
}

// canonical renders v as the JSON Go writes for it: object keys sorted, so two
// runtimes that order keys differently still compare equal.
func canonical(t *testing.T, v any) string {
	t.Helper()

	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}

	return string(raw)
}

// TestGeneratedCodecsAgreeAcrossRuntimes runs the same wire payloads through
// the TypeScript codecs under node and the Dart codecs under fvm, both
// generated from one specification. The client shape each decodes to, and the
// wire shape each encodes that back to, must be identical as canonical JSON.
//
// A difference is a payload that one language reads as a field the other
// reads as nothing: the rename tables agree (TestTablesAgreeWithTypeScript),
// and this is the proof the walkers do too.
func TestGeneratedCodecsAgreeAcrossRuntimes(t *testing.T) {
	// Probe both toolchains before generating anything, so the skip is cheap
	// and names what is missing.
	requireNode(t)
	requireDart(t)

	doc := readCodecParity(t)

	for name, variant := range doc.Variants {
		t.Run(name, func(t *testing.T) {
			// Phase one decodes every wire payload in both runtimes; phase two
			// encodes what each decoded, since each encode is fed its own
			// runtime's client shape.
			var decodeRequests []map[string]any

			for _, c := range variant.Cases {
				decodeRequests = append(decodeRequests, map[string]any{"codec": c.Codec, "direction": "decode", "input": c.Wire})
			}

			tsDecoded := runTypeScriptCodecs(t, codecParitySpec(), variant.FieldOverrides, decodeRequests)

			dartConfig := baseConfig()
			dartConfig.PackageName = "codec_parity_client"
			dartConfig.FieldOverrides = variant.FieldOverrides

			dartFixture := gateFixture{Name: "codec-parity-" + name, Spec: codecParitySpec(), Config: dartConfig}

			dartDecodeRequests := make([]map[string]any, len(decodeRequests))
			for i, r := range decodeRequests {
				dartDecodeRequests[i] = map[string]any{"codec": dartCodecConst(variant.Cases[i].Codec), "direction": "decode", "input": r["input"]}
			}

			dartDecoded := runCodecs(t, dartFixture, dartDecodeRequests)

			if len(tsDecoded) != len(variant.Cases) || len(dartDecoded) != len(variant.Cases) {
				t.Fatalf("%d cases, typescript decoded %d, dart decoded %d", len(variant.Cases), len(tsDecoded), len(dartDecoded))
			}

			// Each runtime encodes the client shape it decoded to itself.
			tsEncodeRequests := make([]map[string]any, len(variant.Cases))
			dartEncodeRequests := make([]map[string]any, len(variant.Cases))

			for i, c := range variant.Cases {
				tsEncodeRequests[i] = map[string]any{"codec": c.Codec, "direction": "encode", "input": tsDecoded[i]}
				dartEncodeRequests[i] = map[string]any{"codec": dartCodecConst(c.Codec), "direction": "encode", "input": dartDecoded[i]}
			}

			tsEncoded := runTypeScriptCodecs(t, codecParitySpec(), variant.FieldOverrides, tsEncodeRequests)
			dartEncoded := runCodecs(t, dartFixture, dartEncodeRequests)

			for i, c := range variant.Cases {
				t.Run(c.Name, func(t *testing.T) {
					if got, want := canonical(t, dartDecoded[i]), canonical(t, tsDecoded[i]); got != want {
						t.Errorf("decode(%s) differs\n wire       %s\n typescript %s\n dart       %s", c.Codec, canonical(t, c.Wire), want, got)
					}

					if got, want := canonical(t, dartEncoded[i]), canonical(t, tsEncoded[i]); got != want {
						t.Errorf("encode(decode(%s)) differs\n typescript %s\n dart       %s", c.Codec, want, got)
					}

					if c.HasClient && !reflect.DeepEqual(roundTrip(t, tsDecoded[i]), roundTrip(t, c.Client)) {
						t.Errorf("both runtimes agree, but not on the client shape the fixture states\n got  %s\n want %s", canonical(t, tsDecoded[i]), canonical(t, c.Client))
					}

					if c.RoundTrip && canonical(t, tsEncoded[i]) != canonical(t, c.Wire) {
						t.Errorf("encode(decode(wire)) is not the wire payload\n wire %s\n got  %s", canonical(t, c.Wire), canonical(t, tsEncoded[i]))
					}
				})
			}
		})
	}
}
