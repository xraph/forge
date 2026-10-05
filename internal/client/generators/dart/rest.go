package dart

import (
	"fmt"
	"sort"
	"strings"
)

// restReserved are RestClient's own members, which an operation method or
// namespace must not shadow.
var restReserved = map[string]bool{
	"hashCode": true, "runtimeType": true, "toString": true, "noSuchMethod": true,
	"baseUrl": true, "headers": true, "credentials": true, "timeout": true, "close": true,
}

// restNode is one namespace of the REST client: its methods and the
// namespaces nested under it, built from dotted operation ids as the
// TypeScript generator's buildEndpointTree builds them.
type restNode struct {
	class    string
	methods  map[string]*operation
	children map[string]*restNode
}

// restTree nests operations by the dot-separated segments of their operation
// id (or the path-derived id for an operation without one). A leaf that
// collides with a namespace moves into it under its own name.
func restTree(ops []*operation, reg *registry) (*restNode, map[*operation]string) {
	root := &restNode{class: "RestClient", methods: map[string]*operation{}, children: map[string]*restNode{}}

	for _, op := range ops {
		id := op.ep.OperationID
		if id == "" {
			id = operationIDFromPath(*op.ep)
		}

		parts := strings.Split(id, ".")
		node := root

		for _, seg := range parts[:len(parts)-1] {
			child := node.children[seg]
			if child == nil {
				child = &restNode{methods: map[string]*operation{}, children: map[string]*restNode{}}
				node.children[seg] = child

				if existing, ok := node.methods[seg]; ok {
					delete(node.methods, seg)
					child.methods[seg] = existing
				}
			}

			node = child
		}

		leaf := parts[len(parts)-1]
		if child, ok := node.children[leaf]; ok {
			child.methods[leaf] = op
		} else {
			node.methods[leaf] = op
		}
	}

	paths := map[*operation]string{}
	nameNode(root, nil, reg, paths)

	return root, paths
}

// nameNode assigns namespace class names and records each operation's
// accessor path from the root ("orders.list").
func nameNode(node *restNode, path []string, reg *registry, paths map[*operation]string) {
	members := copySet(restReserved)

	for _, seg := range sortedKeys(node.children) {
		member := uniqueNames([]string{seg}, func(s string) string { return memberIdent(s, restReserved) }, members, false)[0]
		child := node.children[seg]
		child.class = reg.claim("Rest" + typeIdent(strings.Join(append(append([]string(nil), path...), seg), "_")) + "Api")
		nameNode(child, append(append([]string(nil), path...), member), reg, paths)
	}

	for _, seg := range sortedKeys(node.methods) {
		member := uniqueNames([]string{seg}, func(s string) string { return memberIdent(s, restReserved) }, members, false)[0]
		paths[node.methods[seg]] = strings.Join(append(append([]string(nil), path...), member), ".")
	}
}

// renderRest renders lib/src/rest.dart: a typed client over package:http
// with one method per operation, nested by operation-id namespace.
func renderRest(ops []*operation, root *restNode, paths map[*operation]string, naming codecNaming, reg *registry, includeAuth bool) string {
	var body strings.Builder

	codecImports := map[string]bool{}

	codecConst := func(id string) string {
		ref, ok := naming.byID[id]
		if id == "" || !ok {
			return ""
		}

		codecImports[fmt.Sprintf("import 'codecs/%s.dart';", ref.file)] = true

		return ref.constant
	}

	body.WriteString(restClientHead(includeAuth))
	writeRestMembers(&body, root, paths, "  ")
	body.WriteString(restClientTail)

	var nodes []*restNode

	collectNodes(root, &nodes)

	for _, node := range nodes[1:] {
		fmt.Fprintf(&body, "\n/// Operations under the `%s` namespace.\n", node.class)
		fmt.Fprintf(&body, "final class %s {\n", node.class)
		fmt.Fprintf(&body, "  %s._(this._client);\n\n", node.class)
		body.WriteString("  final RestClient _client;\n")

		if len(node.children) > 0 {
			body.WriteString("\n")
		}

		writeRestMembers(&body, node, paths, "  ")
		body.WriteString("}\n")
	}

	credentials := ""
	if includeAuth {
		credentials = "    if (credentials case final provide?) request.headers.addAll(await provide());\n"
	}

	text := strings.Replace(body.String(), "@@credentials@@", credentials, 1)

	var textTypes []string
	for _, t := range sortedKeys(textApplicationTypes) {
		textTypes = append(textTypes, dartString(t))
	}

	text = strings.Replace(text, "@@textTypes@@", strings.Join(textTypes, ", "), 1)

	// Method bodies reference the client through `_client` inside a
	// namespace and directly on the root; render them now that the
	// namespaces are placed.
	text = expandMethods(text, ops, paths, codecConst)

	var b strings.Builder

	b.WriteString(generatedHeader)

	dartImports := []string{"import 'dart:async';", "import 'dart:convert';", "import 'dart:typed_data';"}

	local := []string{"import 'errors.dart';"}
	local = append(local, sortedKeys(codecImports)...)
	local = append(local, reg.importsFor(text, "models/")...)

	if shown := usedSymbols(text, append([]string{"WireCodec"}, supportSymbols...)); len(shown) > 0 {
		local = append(local, "import 'support.dart' show "+strings.Join(shown, ", ")+";")
	}

	sort.Strings(local)
	b.WriteString(importBlock(dartImports, []string{"import 'package:http/http.dart' as http;"}, local))
	b.WriteString(text)

	return b.String()
}

func collectNodes(node *restNode, out *[]*restNode) {
	*out = append(*out, node)

	for _, seg := range sortedKeys(node.children) {
		collectNodes(node.children[seg], out)
	}
}

// writeRestMembers writes a node's namespace accessors and method
// placeholders, which expandMethods replaces with the methods themselves.
func writeRestMembers(b *strings.Builder, node *restNode, paths map[*operation]string, indent string) {
	members := copySet(restReserved)

	for _, seg := range sortedKeys(node.children) {
		member := uniqueNames([]string{seg}, func(s string) string { return memberIdent(s, restReserved) }, members, false)[0]
		child := node.children[seg]
		fmt.Fprintf(b, "%s/// Operations under `%s`.\n", indent, seg)
		fmt.Fprintf(b, "%slate final %s %s = %s._(this);\n\n", indent, child.class, member, child.class)
	}

	for _, seg := range sortedKeys(node.methods) {
		fmt.Fprintf(b, "%s@@method:%s@@\n", indent, paths[node.methods[seg]])
	}
}

// expandMethods replaces each method placeholder with the rendered method.
func expandMethods(text string, ops []*operation, paths map[*operation]string, codecConst func(string) string) string {
	for _, op := range ops {
		path := paths[op]
		segments := strings.Split(path, ".")
		name := segments[len(segments)-1]
		receiver := "_client."

		if len(segments) == 1 {
			receiver = ""
		}

		text = strings.Replace(text, "  @@method:"+path+"@@\n", renderRestMethod(op, name, receiver, codecConst), 1)
	}

	return text
}

// renderRestMethod renders one operation's method.
func renderRestMethod(op *operation, name, receiver string, codecConst func(string) string) string {
	var b strings.Builder

	summary := fmt.Sprintf("`%s %s`", strings.ToUpper(op.ep.Method), strings.ReplaceAll(op.ep.Path, "`", "'"))
	if op.ep.Summary != "" {
		summary += ": " + op.ep.Summary
	}

	b.WriteString(docComment(summary, "", "  "))

	if op.ep.Deprecated {
		b.WriteString("  @Deprecated('Deprecated by the API specification.')\n")
	}

	var sig []string

	for _, p := range op.params {
		if p.required {
			sig = append(sig, "required "+p.typ.name+" "+p.member)
		} else {
			sig = append(sig, p.typ.nullableName()+" "+p.member)
		}
	}

	var bodyMember string

	if op.body != nil {
		bodyMember = op.body.member
		if bodyMember == "" {
			bodyMember = "body"
		}

		typ := op.body.typ

		if op.body.required {
			sig = append(sig, "required "+typ.name+" "+bodyMember)
		} else {
			sig = append(sig, typ.nullableName()+" "+bodyMember)
		}
	}

	returnType := "void"

	switch op.response {
	case "json":
		returnType = op.responseType.name
		if op.responseType.dynamic {
			returnType = "Object?"
		}
	case "text":
		returnType = "String"
	case "bytes":
		returnType = "Uint8List"
	}

	params := ""
	if len(sig) > 0 {
		params = "{" + strings.Join(sig, ", ") + "}"
	}

	fmt.Fprintf(&b, "  Future<%s> %s(%s) async {\n", returnType, name, params)

	call := "await " + receiver + "_send(\n"

	if op.response == "none" {
		b.WriteString("    " + call)
	} else {
		b.WriteString("    final result = " + call)
	}

	fmt.Fprintf(&b, "      %s,\n", dartString(strings.ToUpper(op.ep.Method)))
	fmt.Fprintf(&b, "      %s,\n", restPathExpr(op))

	var query, headers []string

	for _, p := range op.params {
		key := dartString(p.wire)

		switch p.in {
		case "query":
			if p.required {
				query = append(query, key+": "+p.typ.encode(p.member, 0))
			} else {
				query = append(query, key+": "+p.typ.encodeNullable(p.member, 0))
			}
		case "header":
			if p.required {
				headers = append(headers, key+": "+stringExpr(p.typ, p.member))
			} else {
				headers = append(headers, fmt.Sprintf("if (%s case final v?) %s: %s", p.member, key, stringExpr(p.typ, "v")))
			}
		}
	}

	if len(query) > 0 {
		fmt.Fprintf(&b, "      query: {%s},\n", strings.Join(query, ", "))
	}

	if len(headers) > 0 {
		fmt.Fprintf(&b, "      headers: {%s},\n", strings.Join(headers, ", "))
	}

	if op.body != nil {
		typ := op.body.typ

		value := typ.encode(bodyMember, 0)
		if !op.body.required {
			value = typ.encodeNullable(bodyMember, 0)
		}

		fmt.Fprintf(&b, "      body: %s,\n", value)

		switch op.body.kind {
		case "form", "multipart":
			b.WriteString("      form: true,\n")
		case "text":
			b.WriteString("      plain: true,\n")
		case "json":
			if c := codecConst(op.body.codec); c != "" {
				fmt.Fprintf(&b, "      bodyCodec: %s,\n", c)
			}
		}

		if declared := op.body.contentType; declared != "" && declared != defaultBodyType[op.body.kind] {
			fmt.Fprintf(&b, "      contentType: %s,\n", dartString(declared))
		}
	}

	switch op.response {
	case "json":
		if c := codecConst(op.responseCodec); c != "" {
			fmt.Fprintf(&b, "      responseCodec: %s,\n", c)
		}
	case "text":
		b.WriteString("      response: _Body.text,\n")
	case "bytes":
		b.WriteString("      response: _Body.bytes,\n")
	case "none":
		b.WriteString("      response: _Body.none,\n")
	}

	b.WriteString("    );\n")

	switch op.response {
	case "json":
		if op.responseType.dynamic {
			b.WriteString("    return result;\n")
		} else {
			fmt.Fprintf(&b, "    return %s;\n", op.responseType.decode("result", 0))
		}
	case "text":
		b.WriteString("    return result! as String;\n")
	case "bytes":
		b.WriteString("    return result! as Uint8List;\n")
	}

	b.WriteString("  }\n\n")

	return b.String()
}

// defaultBodyType is the content type _send gives each body kind when the
// operation declares none other.
var defaultBodyType = map[string]string{
	"json": "application/json", "text": "text/plain", "bytes": "application/octet-stream",
}

// restPathExpr renders the request path as a Dart string literal with each
// path parameter percent-encoded into its placeholder.
func restPathExpr(op *operation) string {
	path := op.ep.Path

	var b strings.Builder

	b.WriteByte('\'')

	for len(path) > 0 {
		start := strings.IndexByte(path, '{')
		end := strings.IndexByte(path, '}')

		if start < 0 || end < start {
			b.WriteString(escapeInString(path))

			break
		}

		b.WriteString(escapeInString(path[:start]))

		name := path[start+1 : end]
		written := false

		for _, p := range op.params {
			if p.in == "path" && p.wire == name {
				fmt.Fprintf(&b, "${Uri.encodeComponent(%s)}", stringExpr(p.typ, p.member))

				written = true

				break
			}
		}

		if !written {
			b.WriteString(escapeInString(path[start : end+1]))
		}

		path = path[end+1:]
	}

	b.WriteByte('\'')

	return b.String()
}

// escapeInString escapes text for the inside of a single-quoted literal.
func escapeInString(s string) string {
	quoted := dartString(s)

	return quoted[1 : len(quoted)-1]
}

func restClientHead(includeAuth bool) string {
	var b strings.Builder

	if includeAuth {
		b.WriteString(`
/// Supplies credentials for one request.
typedef CredentialsProvider = FutureOr<Map<String, String>> Function();
`)
	}

	b.WriteString(`
/// A typed REST client over ` + "`package:http`" + `.
///
/// Every method throws an [ApiError] subclass for a non-2xx response.
final class RestClient {
  /// Creates a client for [baseUrl].
  RestClient({
    required this.baseUrl,
    http.Client? httpClient,
    this.headers = const {},
`)

	if includeAuth {
		b.WriteString("    this.credentials,\n")
	}

	b.WriteString(`    this.timeout,
  }) : _http = httpClient ?? http.Client();

  /// The API root every operation path is resolved against.
  final Uri baseUrl;

  /// Headers sent with every request.
  final Map<String, String> headers;
`)

	if includeAuth {
		b.WriteString(`
  /// Called before each request; its headers are merged over [headers].
  final CredentialsProvider? credentials;
`)
	}

	b.WriteString(`
  /// Fails a request that takes longer than this.
  final Duration? timeout;

  final http.Client _http;

`)

	return b.String()
}

// restClientTail closes RestClient with close() and _send, the one place the
// generated package touches the network.
const restClientTail = `  /// Closes the underlying HTTP client.
  void close() => _http.close();

  Future<Object?> _send(
    String method,
    String path, {
    Map<String, Object?> query = const {},
    Map<String, String> headers = const {},
    Object? body,
    bool form = false,
    bool plain = false,
    String? contentType,
    WireCodec? bodyCodec,
    WireCodec? responseCodec,
    _Body response = _Body.json,
  }) async {
    final params = <String, List<String>>{
      for (final MapEntry(:key, :value) in query.entries)
        if (value != null)
          key: value is Iterable<Object?> ? [for (final v in value) '$v'] : ['$value'],
    };
    final base = baseUrl.toString().replaceAll(RegExp(r'/+$'), '');
    var uri = Uri.parse('$base$path');
    if (params.isNotEmpty) uri = uri.replace(queryParameters: params);
    final abort = Completer<void>();
    final request = http.AbortableRequest(method, uri, abortTrigger: timeout == null ? null : abort.future)
      ..headers.addAll(this.headers);
@@credentials@@    request.headers.addAll(headers);
    switch (body) {
      case null:
        break;
      case final Map<String, String> fields when form:
        request.bodyFields = fields;
      case final Uint8List bytes:
        request.bodyBytes = bytes;
        request.headers.putIfAbsent('content-type', () => contentType ?? 'application/octet-stream');
      case final String text when plain:
        request.headers.putIfAbsent('content-type', () => contentType ?? 'text/plain');
        request.body = text;
      default:
        request.body = jsonEncode(bodyCodec == null ? body : bodyCodec.encode(body));
        request.headers['content-type'] = contentType ?? 'application/json';
    }
    Future<http.Response> exchange() async => http.Response.fromStream(await _http.send(request));
    final http.Response reply;
    try {
      reply = await (timeout == null ? exchange() : exchange().timeout(timeout!));
    } on TimeoutException {
      if (!abort.isCompleted) abort.complete();
      rethrow;
    }
    if (reply.statusCode < 200 || reply.statusCode >= 300) {
      throw ApiError.fromResponse(
        reply.statusCode,
        _decodeBody(reply),
        headers: reply.headers,
      );
    }
    switch (response) {
      case _Body.none:
        return null;
      case _Body.bytes:
        return reply.bodyBytes;
      case _Body.text:
        return _text(reply);
      case _Body.json:
        if (reply.bodyBytes.isEmpty) return null;
        final decoded = jsonDecode(utf8.decode(reply.bodyBytes, allowMalformed: true));
        return responseCodec == null ? decoded : responseCodec.decode(decoded);
    }
  }

  static Object? _decodeBody(http.Response reply) {
    if (reply.bodyBytes.isEmpty) return null;
    final type = _essence(reply);
    final text = _text(reply);
    if (type.isNotEmpty && !_isJson(type)) return text;
    try {
      return jsonDecode(text);
    } on FormatException {
      return text;
    }
  }

  static String _essence(http.Response reply) =>
      (reply.headers['content-type'] ?? '').split(';').first.trim().toLowerCase();

  // The one content-type rule every Forge Dart client applies: JSON is
  // application/json, text/json or any +json type; text is text/*, +xml, +yaml
  // or one of the listed application types; everything else is bytes.
  static bool _isJson(String essence) =>
      essence == 'application/json' || essence == 'text/json' || essence.endsWith('+json');

  static bool _isText(String essence) =>
      essence.startsWith('text/') ||
      essence.endsWith('+xml') ||
      essence.endsWith('+yaml') ||
      _textTypes.contains(essence);

  static final _charset = RegExp(r'charset\s*=\s*"?([^";\s]+)', caseSensitive: false);

  /// A body read as text: a text body in the charset it declares, anything else
  /// (JSON included) as UTF-8, which is also the fallback for a charset this
  /// platform cannot decode. ` + "`package:http`" + ` would read an undeclared charset as
  /// Latin-1.
  static String _text(http.Response reply) {
    final essence = _essence(reply);
    final name = _isText(essence) && !_isJson(essence)
        ? _charset.firstMatch(reply.headers['content-type'] ?? '')?.group(1)
        : null;
    final encoding = name == null ? null : Encoding.getByName(name);
    if (encoding != null && encoding != utf8) {
      try {
        return encoding.decode(reply.bodyBytes);
      } on FormatException {
        // The bytes do not fit the declared charset; read them as UTF-8.
      }
    }
    return utf8.decode(reply.bodyBytes, allowMalformed: true);
  }
}

const _textTypes = {@@textTypes@@};

enum _Body { none, json, text, bytes }
`
