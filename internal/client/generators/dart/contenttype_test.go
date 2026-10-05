package dart

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xraph/forge/internal/client"
)

// mediaVector is one row of packages/client-fixtures/media/content-types.json.
type mediaVector struct {
	ContentType string `json:"contentType"`
	Kind        string `json:"kind"`
}

// readMediaVectors loads the content-type vectors the Go planner,
// forge_client's transport and the generated client all classify.
func readMediaVectors(t *testing.T) []mediaVector {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "packages", "client-fixtures", "media", "content-types.json"))
	if err != nil {
		t.Fatalf("the shared media vectors are missing: %v", err)
	}

	var doc struct {
		Kind    string        `json:"kind"`
		Vectors []mediaVector `json:"vectors"`
	}

	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}

	if doc.Kind != "media-content-types" || len(doc.Vectors) == 0 {
		t.Fatal("content-types.json is not a media-content-types file, or holds no vectors")
	}

	return doc.Vectors
}

// TestMediaKindFollowsTheSharedVectors classifies every shared vector: a
// request body by bodyKind, which tells form fields apart, and anything by
// mediaKind, which reads a form type as text.
func TestMediaKindFollowsTheSharedVectors(t *testing.T) {
	for _, v := range readMediaVectors(t) {
		if got := bodyKind(v.ContentType); got != v.Kind {
			t.Errorf("bodyKind(%q) = %q, want %q", v.ContentType, got, v.Kind)
		}

		want := v.Kind
		if want == "form" {
			want = "text"
		}

		if got := mediaKind(v.ContentType); got != want {
			t.Errorf("mediaKind(%q) = %q, want %q", v.ContentType, got, want)
		}
	}

	// No content type at all is bytes to the planner; the runtimes sniff it.
	if got := mediaKind(""); got != "bytes" {
		t.Errorf(`mediaKind("") = %q, want bytes`, got)
	}
}

// TestGeneratedClientClassifiesTheSharedVectors generates one operation per
// shared vector, its response declared with that content type, and drives
// each through the generated RestClient. A success reads as the vector's
// kind (a form type as text); an error body is parsed only for a JSON kind.
// This is the whole chain: the Go planner picks the method's return kind,
// and the generated runtime classifies the error body by the same rule.
func TestGeneratedClientClassifiesTheSharedVectors(t *testing.T) {
	fvm := requireDart(t)
	vectors := readMediaVectors(t)

	spec := &client.APISpec{Info: client.APIInfo{Title: "Media API", Version: "1"}}

	var calls []string

	for i, v := range vectors {
		id := fmt.Sprintf("probe%d", i)
		spec.Endpoints = append(spec.Endpoints, client.Endpoint{
			Method: "GET", Path: "/" + id, OperationID: id,
			Responses: map[int]*client.Response{200: {Content: map[string]*client.MediaType{v.ContentType: {}}}},
		})
		calls = append(calls, fmt.Sprintf("  out.add(await probe(%s, rest.%s));", dartString(v.ContentType), id))
	}

	cfg := baseConfig()
	cfg.PackageName = "media_client"
	cfg.Hooks = false

	dir := writePackage(t, gateFixture{Name: "media", Spec: spec, Config: cfg})

	script := `import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:media_client/media_client.dart';

late String type;
var status = 200;

String kindOf(Object? value) => switch (value) {
  Map<Object?, Object?>() => 'json',
  String() => 'text',
  Uint8List() => 'bytes',
  _ => 'other: ${value.runtimeType}',
};

Future<List<String>> probe(String contentType, Future<Object?> Function() call) async {
  type = contentType;
  status = 200;
  final ok = kindOf(await call());
  status = 500;
  try {
    await call();
    return [ok, 'no error'];
  } on ApiError catch (e) {
    return [ok, kindOf(e.body)];
  }
}

Future<void> main() async {
  final rest = RestClient(
    baseUrl: Uri.parse('https://api.test'),
    httpClient: MockClient((request) async =>
        http.Response.bytes(utf8.encode('{"a":1}'), status, headers: {'content-type': type})),
  );
  final out = <List<String>>[];
` + strings.Join(calls, "\n") + `
  stdout.write(jsonEncode(out));
  rest.close();
}
`

	if err := os.MkdirAll(filepath.Join(dir, "tool"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dir, "tool", "run_media.dart"), []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}

	runFvm(t, fvm, dir, "pub", "get")

	var got [][]string
	if err := json.Unmarshal([]byte(runFvm(t, fvm, dir, "run", "tool/run_media.dart")), &got); err != nil {
		t.Fatal(err)
	}

	if len(got) != len(vectors) {
		t.Fatalf("%d vectors, %d results", len(vectors), len(got))
	}

	for i, v := range vectors {
		want := v.Kind
		if want == "form" {
			want = "text"
		}

		wantError := "text"
		if want == "json" {
			wantError = "json"
		}

		if got[i][0] != want || got[i][1] != wantError {
			t.Errorf("%q: response read as %s, error body as %s; want %s and %s", v.ContentType, got[i][0], got[i][1], want, wantError)
		}
	}
}
