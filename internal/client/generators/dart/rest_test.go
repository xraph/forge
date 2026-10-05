package dart

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/xraph/forge/internal/client"
)

func TestRestClientNestsOperationsByNamespace(t *testing.T) {
	rest := file(t, generate(t, fixture(t, "default")), "lib/src/rest.dart")

	assertContains(t, "rest.dart", rest,
		"final class RestClient {",
		"late final RestOrdersApi orders = RestOrdersApi._(this);",
		"final class RestOrdersApi {",
		"Future<Order> get({required String id, bool? includeLines}) async {",
		"'/orders/${Uri.encodeComponent(id)}',",
		"query: {'include_lines': includeLines},",
		"headers: {'X-Tenant': xTenant},",
		"Future<Order> update({required String id, required UpdateOrderRequest body}) async {",
		"bodyCodec: updateOrderRequestCodec,",
		"responseCodec: orderCodec,",
		"Future<void> delete({required String id}) async {",
		"response: _Body.none,",
		"Future<Pet> get({required Int64 petId}) async {",
		"'/pets/${Uri.encodeComponent(petId.value)}',",
		"Future<String> get() async {",
		"form: true,",
		"throw ApiError.fromResponse(",
		"import 'package:http/http.dart' as http;",
	)

	if strings.Contains(rest, "forge_client") {
		t.Error("rest.dart must depend on package:http alone")
	}
}

func TestRestClientCredentialsFollowIncludeAuth(t *testing.T) {
	withAuth := file(t, generate(t, fixture(t, "default")), "lib/src/rest.dart")
	assertContains(t, "rest.dart", withAuth, "typedef CredentialsProvider", "if (credentials case final provide?)")

	noAuth := file(t, generate(t, fixture(t, "no-auth")), "lib/src/rest.dart")
	if strings.Contains(noAuth, "credentials") {
		t.Errorf("IncludeAuth=false must drop the credentials hook:\n%s", noAuth)
	}
}

func TestRestWarnsAboutMultipartAndCookies(t *testing.T) {
	warnings := strings.Join(generate(t, fixture(t, "default")).Warnings, "\n")

	assertContains(t, "warnings", warnings, `operation "uploads.create": a multipart body is sent as form fields`)
}

func TestRestDropsCookieParametersWithAWarning(t *testing.T) {
	out := generate(t, restFixture())

	assertContains(t, "warnings", strings.Join(out.Warnings, "\n"),
		`operation "items.delete": cookie parameter "session" is not generated; the Dart client sends no cookies of its own`)

	if rest := file(t, out, "lib/src/rest.dart"); strings.Contains(rest, "session") {
		t.Errorf("a cookie parameter must not become a method parameter:\n%s", rest)
	}
}

func TestErrorsAreASealedHierarchy(t *testing.T) {
	errorsFile := file(t, generate(t, fixture(t, "default")), "lib/src/errors.dart")

	assertContains(t, "errors.dart", errorsFile,
		"sealed class ApiError implements Exception {",
		"404 => NotFound(body, headers: headers),",
		"422 => UnprocessableEntity(body, headers: headers),",
		"_ => UnexpectedStatus(status, body, headers: headers),",
		"final class GatewayTimeout extends ApiError {",
		"ApiError? apiErrorOf(Object error) => switch (error) {",
		"HttpStatusError(:final status, :final body, :final headers) =>",
	)

	standalone := file(t, generate(t, fixture(t, "no-hooks")), "lib/src/errors.dart")
	if strings.Contains(standalone, "HttpStatusError") || strings.Contains(standalone, "apiErrorOf") {
		t.Errorf("without hooks errors.dart cannot reference forge_client:\n%s", standalone)
	}
}

func TestReservedIdentifiersCoverGeneratedAndRuntimeNames(t *testing.T) {
	reserved := ReservedIdentifiers()

	for _, name := range []string{"ApiError", "NotFound", "RestClient", "Value", "Assign", "QueryState", "OperationMeta", "Int64", "String", "Page"} {
		if !reserved[name] {
			t.Errorf("ReservedIdentifiers lacks %q", name)
		}
	}
}

// restFixture holds the REST shapes the default fixture lacks: binary
// responses and bodies, a JSON body that is a bare string, text, and an
// operation of each response kind. Hooks are off, so the generated package
// resolves against package:http alone.
func restFixture() gateFixture {
	cfg := baseConfig()
	cfg.PackageName = "rest_client"
	cfg.Hooks = false

	octet := func(s *client.Schema) map[string]*client.MediaType {
		return map[string]*client.MediaType{"application/octet-stream": {Schema: s}}
	}

	binary := &client.Schema{Type: "string", Format: "binary"}
	id := []client.Parameter{{Name: "id", In: "path", Required: true, Schema: &client.Schema{Type: "string"}}}

	return gateFixture{
		Name:   "rest",
		Config: cfg,
		Spec: &client.APISpec{
			Info: client.APIInfo{Title: "Rest API", Version: "1"},
			Schemas: map[string]*client.Schema{
				"Item": {
					Type: "object", Required: []string{"id"},
					Properties: map[string]*client.Schema{"id": {Type: "string"}, "label": {Type: "string"}},
				},
			},
			Endpoints: []client.Endpoint{
				{
					Method: "GET", Path: "/files/{id}/content", OperationID: "files.download", PathParams: id,
					Responses: map[int]*client.Response{200: {Content: octet(binary)}},
				},
				{
					Method: "GET", Path: "/files/{id}/thumb", OperationID: "files.thumbnail", PathParams: id,
					Responses: map[int]*client.Response{200: {Content: map[string]*client.MediaType{"image/png": {Schema: binary}}}},
				},
				{
					Method: "PUT", Path: "/files/{id}/content", OperationID: "files.upload", PathParams: id,
					RequestBody: &client.RequestBody{Required: true, Content: octet(binary)},
					Responses:   map[int]*client.Response{204: {Description: "stored"}},
				},
				{
					Method: "GET", Path: "/notes/{id}", OperationID: "notes.get", PathParams: id,
					Responses: map[int]*client.Response{200: {Content: map[string]*client.MediaType{"text/plain": {Schema: &client.Schema{Type: "string"}}}}},
				},
				{
					Method: "PUT", Path: "/notes/{id}", OperationID: "notes.put", PathParams: id,
					RequestBody: &client.RequestBody{Required: true, Content: map[string]*client.MediaType{"text/plain": {Schema: &client.Schema{Type: "string"}}}},
					Responses:   map[int]*client.Response{204: {Description: "stored"}},
				},
				{
					Method: "POST", Path: "/forms", OperationID: "forms.submit",
					RequestBody: &client.RequestBody{Required: true, Content: map[string]*client.MediaType{"application/x-www-form-urlencoded": {Schema: &client.Schema{Type: "object"}}}},
					Responses:   map[int]*client.Response{204: {Description: "ok"}},
				},
				{
					Method: "PUT", Path: "/names/{id}", OperationID: "names.set", PathParams: id,
					RequestBody: &client.RequestBody{Required: true, Content: jsonContent(&client.Schema{Type: "string"})},
					Responses:   map[int]*client.Response{204: {Description: "set"}},
				},
				{
					Method: "GET", Path: "/items/{id}", OperationID: "items.get", PathParams: id,
					QueryParams:  []client.Parameter{{Name: "q", In: "query", Schema: &client.Schema{Type: "string"}}},
					HeaderParams: []client.Parameter{{Name: "X-Tenant", In: "header", Required: true, Schema: &client.Schema{Type: "string"}}},
					Responses:    map[int]*client.Response{200: {Content: jsonContent(ref("Item"))}},
				},
				{
					Method: "DELETE", Path: "/items/{id}", OperationID: "items.delete", PathParams: id,
					CookieParams: []client.Parameter{{Name: "session", In: "cookie", Schema: &client.Schema{Type: "string"}}},
					Responses:    map[int]*client.Response{204: {Description: "gone"}},
				},
			},
		},
	}
}

// restScript drives the generated RestClient against package:http's
// MockClient and prints what it did as JSON. Every value in the output was
// produced by the generated Dart.
const restScript = `
import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:rest_client/rest_client.dart';

// Not valid UTF-8: any text decode of it would replace bytes.
final blob = Uint8List.fromList([0, 255, 254, 128, 0xc3, 0x28, 0x89, 0x50]);

Map<String, Object?> sentOf(http.Request r) => {
  'method': r.method,
  'url': r.url.toString(),
  'headers': r.headers,
  'body': utf8.decode(r.bodyBytes, allowMalformed: true),
  'bytes': r.bodyBytes.toList(),
};

Future<Map<String, Object?>> failure(Future<Object?> call) async {
  try {
    await call;
  } on ApiError catch (e) {
    return {
      'type': e.runtimeType.toString(),
      'status': e.status,
      'message': e.message,
      'bodyType': e.body.runtimeType.toString(),
      'body': e.body is String || e.body is Map ? e.body : null,
    };
  }
  return {'type': 'none'};
}

Future<void> main() async {
  final out = <String, Object?>{};
  final sent = <http.Request>[];
  late http.Response Function(http.Request) reply;
  final rest = RestClient(
    baseUrl: Uri.parse('https://api.test/v1/'),
    headers: const {'x-app': 'probe'},
    credentials: () => {'authorization': 'Bearer t'},
    httpClient: MockClient((request) async {
      sent.add(request);
      return reply(request);
    }),
  );

  reply = (_) => http.Response.bytes(
    blob, 200, headers: {'content-type': 'application/octet-stream'});
  final bin = await rest.files.download(id: 'a b');
  out['binary'] = {'isBytes': bin is Uint8List, 'bytes': bin.toList(), 'sent': sentOf(sent.last)};

  reply = (_) => http.Response.bytes(blob, 200, headers: {'content-type': 'image/png'});
  out['image'] = (await rest.files.thumbnail(id: '1')).toList();

  reply = (_) => http.Response.bytes(
    utf8.encode('café'), 200, headers: {'content-type': 'text/plain'});
  out['textUtf8'] = await rest.notes.get(id: '1');

  reply = (_) => http.Response.bytes(
    latin1.encode('café'), 200, headers: {'content-type': 'text/plain; charset=iso-8859-1'});
  out['textLatin1'] = await rest.notes.get(id: '1');

  reply = (_) => http.Response('', 204);
  await rest.files.upload(id: '9', body: blob);
  out['upload'] = sentOf(sent.last);

  await rest.names.set(id: '7', body: 'abc');
  out['stringBody'] = sentOf(sent.last);

  await rest.notes.put(id: '7', body: 'abc');
  out['textBody'] = sentOf(sent.last);

  await rest.forms.submit(body: {'a': 'b c', 'd': 'é'});
  out['formBody'] = sentOf(sent.last);

  await rest.items.delete(id: '3');
  out['delete'] = sentOf(sent.last);

  reply = (_) => http.Response.bytes(
    utf8.encode('{"id":"1","label":"é"}'), 200, headers: {'content-type': 'application/json'});
  final item = await rest.items.get(id: 'a/b', q: 'x y', xTenant: 't1');
  out['item'] = {'id': item.id, 'label': item.label, 'sent': sentOf(sent.last)};

  http.Response problem(int status) => http.Response.bytes(
    utf8.encode('{"message":"é gone"}'), status,
    headers: {'content-type': 'application/problem+json'});

  reply = (_) => problem(404);
  out['notFound'] = await failure(rest.items.get(id: '1', xTenant: 't'));
  reply = (_) => problem(418);
  out['teapot'] = await failure(rest.items.get(id: '1', xTenant: 't'));
  reply = (_) => http.Response.bytes(
    blob, 503, headers: {'content-type': 'application/octet-stream'});
  out['binaryError'] = await failure(rest.items.get(id: '1', xTenant: 't'));
  reply = (_) => http.Response('<html>down</html>', 502, headers: {'content-type': 'application/json'});
  out['badJsonError'] = await failure(rest.items.get(id: '1', xTenant: 't'));

  stdout.write(jsonEncode(out));
  rest.close();
}
`

// TestRestClientRunsAgainstPackageHTTP compiles the generated client without
// forge_client and drives every request and response shape through it.
func TestRestClientRunsAgainstPackageHTTP(t *testing.T) {
	fvm := requireDart(t)
	dir := writePackage(t, restFixture())

	if err := os.MkdirAll(filepath.Join(dir, "tool"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dir, "tool", "run_rest.dart"), []byte(restScript), 0o644); err != nil {
		t.Fatal(err)
	}

	runFvm(t, fvm, dir, "pub", "get")

	var got map[string]any
	if err := json.Unmarshal([]byte(runFvm(t, fvm, dir, "run", "tool/run_rest.dart")), &got); err != nil {
		t.Fatal(err)
	}

	blob := []any{0.0, 255.0, 254.0, 128.0, 195.0, 40.0, 137.0, 80.0}

	check := func(name string, want any) {
		t.Helper()

		if !reflect.DeepEqual(got[name], want) {
			t.Errorf("%s:\n got  %v\n want %v", name, got[name], want)
		}
	}

	binary := got["binary"].(map[string]any)

	if binary["isBytes"] != true || !reflect.DeepEqual(binary["bytes"], blob) {
		t.Errorf("an application/octet-stream response must come back as the Uint8List it arrived as, got %v", binary)
	}

	check("image", blob)
	check("textUtf8", "café")
	check("textLatin1", "café")

	sent := binary["sent"].(map[string]any)

	if sent["method"] != "GET" || sent["url"] != "https://api.test/v1/files/a%20b/content" {
		t.Errorf("request line: %v", sent)
	}

	headers := sent["headers"].(map[string]any)

	if headers["x-app"] != "probe" || headers["authorization"] != "Bearer t" {
		t.Errorf("client headers and credentials must reach the request: %v", headers)
	}

	upload := got["upload"].(map[string]any)

	if upload["method"] != "PUT" || !reflect.DeepEqual(upload["bytes"], blob) ||
		upload["headers"].(map[string]any)["content-type"] != "application/octet-stream" {
		t.Errorf("a binary body is sent as raw octets: %v", upload)
	}

	str := got["stringBody"].(map[string]any)

	if str["body"] != `"abc"` || str["headers"].(map[string]any)["content-type"] != "application/json" {
		t.Errorf("a JSON string body is JSON-encoded, not sent as text/plain: %v", str)
	}

	text := got["textBody"].(map[string]any)

	if text["body"] != "abc" || !strings.HasPrefix(text["headers"].(map[string]any)["content-type"].(string), "text/plain") {
		t.Errorf("a text body is sent as text/plain: %v", text)
	}

	form := got["formBody"].(map[string]any)

	if form["body"] != "a=b+c&d=%C3%A9" || !strings.HasPrefix(form["headers"].(map[string]any)["content-type"].(string), "application/x-www-form-urlencoded") {
		t.Errorf("a form body is sent as urlencoded fields: %v", form)
	}

	if del := got["delete"].(map[string]any); del["method"] != "DELETE" || del["url"] != "https://api.test/v1/items/3" {
		t.Errorf("delete: %v", del)
	}

	item := got["item"].(map[string]any)
	itemSent := item["sent"].(map[string]any)

	if item["id"] != "1" || item["label"] != "é" {
		t.Errorf("a JSON response is read as UTF-8 and decoded into the model: %v", item)
	}

	if itemSent["url"] != "https://api.test/v1/items/a%2Fb?q=x+y" ||
		itemSent["headers"].(map[string]any)["X-Tenant"] != "t1" {
		t.Errorf("path, query and header parameters: %v", itemSent)
	}

	check("notFound", map[string]any{"type": "NotFound", "status": 404.0, "message": "é gone", "bodyType": "_Map<String, dynamic>", "body": map[string]any{"message": "é gone"}})
	check("teapot", map[string]any{"type": "UnexpectedStatus", "status": 418.0, "message": "é gone", "bodyType": "_Map<String, dynamic>", "body": map[string]any{"message": "é gone"}})

	binaryError := got["binaryError"].(map[string]any)

	if binaryError["type"] != "ServiceUnavailable" || binaryError["status"] != 503.0 || binaryError["bodyType"] != "String" {
		t.Errorf("a binary error body must not hide its status: %v", binaryError)
	}

	badJSON := got["badJsonError"].(map[string]any)

	if badJSON["type"] != "UnexpectedStatus" || badJSON["status"] != 502.0 || badJSON["body"] != "<html>down</html>" {
		t.Errorf("an unparseable error body keeps its status and its text: %v", badJSON)
	}
}
