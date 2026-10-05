package dart

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/xraph/forge/internal/client"
)

// direction is one direction of a stream: the type a message has and the
// codec that renames it. A direction whose messages are not all one type is
// left untyped: a socket message carries no envelope naming its type, and an
// SSE client with several events gets the raw {'event', 'data', 'id'} frames.
type direction struct {
	typ   dartType
	codec codecRef
	raw   bool

	// code is the decode and encode expressions the direction renders, kept
	// so the imports a file needs come from what it calls and not from a scan
	// of its documentation.
	code []string
}

// hasCodec reports whether the direction renames its messages.
func (d direction) hasCodec() bool { return d.codec.constant != "" }

// streamClient is one typed WebSocket, SSE or WebTransport client.
type streamClient struct {
	class   string
	session string
	file    string
	path    string
	params  []string
	connect string
	ws      bool
	sse     bool

	// events are the event names an SSE endpoint declares.
	events []string

	// heartbeat is how often a socket pings the server, in milliseconds, or 0
	// for never.
	heartbeat int

	receive direction
	send    *direction
}

var pathParamPattern = regexp.MustCompile(`\{([^{}]+)\}`)

// streamParamReserved are the names a path parameter must not take: the
// members of a generated client, the locals of its connect method, and the
// members every Dart object has. A parameter with one of these names would
// shadow it, and the method body would read the parameter's String where it
// means the field or the local.
var streamParamReserved = map[string]bool{
	"base": true, "url": true, "scheme": true, "connection": true, "headers": true,
	"baseUrl": true, "heartbeat": true, "connect": true, "socket": true, "options": true,
	"hashCode": true, "runtimeType": true, "toString": true, "noSuchMethod": true,
}

// streamStems name the files of the streaming directory that are not typed
// clients, so a typed client never takes one of their names.
var streamStems = []string{"live_socket", "rooms", "presence", "typing", "channels", "streaming_client"}

// planStreams resolves every streaming endpoint into a typed client. The
// warnings name each direction left untyped.
func planStreams(spec *client.APISpec, config client.GeneratorConfig, reg *registry, naming codecNaming) ([]streamClient, []string) {
	var warnings []string

	// messageKey tells two schemas apart for the question "is this direction
	// one type": the codec they share when they have one, and otherwise the
	// Dart type they resolve to.
	resolveDir := func(label, which string, messages map[string]*client.Schema, single *client.Schema) direction {
		schemas := map[string]*client.Schema{}

		for name, schema := range messages {
			if schema != nil {
				schemas[name] = schema
			}
		}

		if len(schemas) == 0 && single != nil {
			schemas[""] = single
		}

		distinct := map[string]*client.Schema{}

		for _, name := range sortedKeys(schemas) {
			schema := schemas[name]
			key := schemaCodecRef(schema)

			if key == "" {
				key = reg.resolve(schema, rctx{imports: map[string]bool{}}).name
			}

			if _, ok := distinct[key]; !ok {
				distinct[key] = schema
			}
		}

		switch {
		case len(distinct) > 1:
			warnings = append(warnings, fmt.Sprintf(
				"stream %s: the %s direction carries %d message types, so the Dart client types it as Object?",
				label, which, len(distinct)))

			return direction{typ: dynamicType(), raw: true}
		case len(distinct) == 0:
			return direction{typ: dynamicType(), raw: true}
		}

		var schema *client.Schema
		for _, s := range distinct {
			schema = s
		}

		d := direction{typ: reg.resolve(schema, rctx{imports: map[string]bool{}})}
		if ref, ok := naming.byID[schemaCodecRef(schema)]; ok {
			d.codec = ref
		}

		return d
	}

	type candidate struct {
		id, path, suffix, connect string
		ws                        bool
		events                    []string
		receive                   direction
		send                      *direction
	}

	var found []candidate

	for i := range spec.WebSockets {
		ws := &spec.WebSockets[i]
		send := resolveDir(ws.Path, "send", ws.SendMessages, ws.SendSchema)
		found = append(found, candidate{
			id: ws.ID, path: ws.Path, suffix: "Socket", connect: "webSocketConnection", ws: true,
			receive: resolveDir(ws.Path, "receive", ws.ReceiveMessages, ws.ReceiveSchema), send: &send,
		})
	}

	for i := range spec.SSEs {
		sse := &spec.SSEs[i]
		found = append(found, candidate{
			id: sse.ID, path: sse.Path, suffix: "Events", connect: "eventSourceConnection",
			events:  sortedKeys(sse.EventSchemas),
			receive: resolveDir(sse.Path, "event", sse.EventSchemas, nil),
		})
	}

	for i := range spec.WebTransports {
		wt := &spec.WebTransports[i]

		var (
			recv direction
			send *direction
		)

		switch {
		case wt.BiStreamSchema != nil:
			recv = resolveDir(wt.Path, "receive", nil, wt.BiStreamSchema.ReceiveSchema)
			s := resolveDir(wt.Path, "send", nil, wt.BiStreamSchema.SendSchema)
			send = &s
		case wt.UniStreamSchema != nil:
			recv = resolveDir(wt.Path, "receive", nil, wt.UniStreamSchema.ReceiveSchema)
		default:
			recv = resolveDir(wt.Path, "datagram", nil, wt.DatagramSchema)
		}

		found = append(found, candidate{
			id: wt.ID, path: wt.Path, suffix: "Transport", connect: "webTransportConnection",
			receive: recv, send: send,
		})
	}

	// Names are claimed in a fixed order, so the same document always yields
	// the same names whatever order its endpoints were parsed in.
	sort.SliceStable(found, func(i, j int) bool {
		a, b := found[i], found[j]
		if a.path != b.path {
			return a.path < b.path
		}

		if a.suffix != b.suffix {
			return a.suffix < b.suffix
		}

		return a.id < b.id
	})

	files := map[string]bool{}
	for _, stem := range streamStems {
		files[stem] = true
	}

	heartbeat := 0
	if config.Features.Heartbeat {
		heartbeat = 30000
	}

	out := make([]streamClient, 0, len(found))

	for _, c := range found {
		base := c.id
		if base == "" {
			// A path names its words with slashes, which the casing
			// functions do not split on.
			base = strings.Map(func(r rune) rune {
				if r < 0x80 && (unicode.IsLetter(r) || unicode.IsDigit(r)) {
					return r
				}

				return ' '
			}, c.path)
		}

		class := reg.claim(typeIdent(base) + c.suffix)

		sc := streamClient{
			class:   class,
			session: reg.claim(class + "Session"),
			file:    uniqueNames([]string{class}, fileStem, files, true)[0],
			path:    c.path,
			params:  pathParams(c.path),
			connect: c.connect,
			ws:      c.ws,
			sse:     c.connect == "eventSourceConnection",
			events:  c.events,
			receive: c.receive,
			send:    c.send,
		}

		if c.ws {
			sc.heartbeat = heartbeat
		}

		out = append(out, sc)
	}

	return out, warnings
}

// pathParams lists a channel path's placeholders in order.
func pathParams(path string) []string {
	var out []string

	for _, m := range pathParamPattern.FindAllStringSubmatch(path, -1) {
		out = append(out, m[1])
	}

	return out
}

// docText flattens text onto one line and takes out the backticks that would
// end a Markdown span in a documentation comment.
func docText(s string) string {
	return strings.ReplaceAll(strings.Join(strings.Fields(s), " "), "`", "'")
}

// decodeExpr is the expression that turns one inbound message m into the
// direction's type, and encodeExpr the one that turns message into the wire
// value. Both name the direction's codec when it has one.
func (d direction) decodeExpr(wire string) string {
	if d.hasCodec() {
		wire = d.codec.constant + ".decode(" + wire + ")"
	}

	return d.typ.decode(wire, 0)
}

func (d direction) encodeExpr(value string) string {
	enc := d.typ.encode(value, 0)
	if d.hasCodec() {
		enc = d.codec.constant + ".encode(" + enc + ")"
	}

	return enc
}

// renderStream renders one typed streaming client and its session.
func renderStream(sc streamClient, reg *registry, naming codecNaming) string {
	var b strings.Builder

	kind := map[string]string{
		"webSocketConnection":    "WebSocket",
		"eventSourceConnection":  "server-sent events",
		"webTransportConnection": "WebTransport",
	}[sc.connect]

	label := docText(sc.path)

	// A parameter takes its path placeholder's name unless that would shadow
	// something the body reads.
	members := copySet(streamParamReserved)
	path := dartString(sc.path)
	path = path[1 : len(path)-1]

	var sig []string

	for _, p := range sc.params {
		member := uniqueNames([]string{p}, func(s string) string { return memberIdent(s, streamParamReserved) }, members, false)[0]
		sig = append(sig, "required String "+member)
		path = strings.Replace(path, escapeInString("{"+p+"}"), "${Uri.encodeComponent("+member+")}", 1)
	}

	params := ""
	if len(sig) > 0 {
		params = "{" + strings.Join(sig, ", ") + "}"
	}

	fmt.Fprintf(&b, "\n/// Typed %s client for `%s`.\n", kind, label)
	fmt.Fprintf(&b, "final class %s {\n", sc.class)
	b.WriteString("  /// Creates a client that connects relative to [baseUrl].\n")

	factory := sc.connect + "()"
	if sc.sse && len(sc.events) > 0 {
		factory = sc.connect + "(events: " + dartStringList(sc.events) + ")"
	}

	b.WriteString("  " + sc.class + "({\n    required this.baseUrl,\n    StreamConnect? connect,\n    this.headers = const {},\n    this.options = const LiveOptions(),\n")

	if sc.ws {
		if sc.heartbeat > 0 {
			fmt.Fprintf(&b, "    this.heartbeat = const Duration(milliseconds: %d),\n", sc.heartbeat)
		} else {
			b.WriteString("    this.heartbeat,\n")
		}
	}

	fmt.Fprintf(&b, "  }) : _connect = connect ?? %s;\n\n", factory)

	b.WriteString("  /// The server root.\n")
	b.WriteString("  final Uri baseUrl;\n\n")
	b.WriteString("  /// Headers sent when connecting, where the platform can send them.\n")
	b.WriteString("  final Map<String, String> headers;\n\n")
	b.WriteString("  /// How the connection opens, reopens and queues what it cannot send.\n")
	b.WriteString("  final LiveOptions options;\n\n")

	if sc.ws {
		b.WriteString("  /// How often to tell the server this client is still here, or null for never.\n")
		b.WriteString("  ///\n")
		b.WriteString("  /// The server closes a socket that has said nothing for a while, and a\n")
		b.WriteString("  /// client that only listens says nothing.\n")
		b.WriteString("  final Duration? heartbeat;\n\n")
	}

	b.WriteString("  final StreamConnect _connect;\n\n")

	b.WriteString("  /// Opens the connection. If the first attempt fails, nothing is left open.\n")
	fmt.Fprintf(&b, "  Future<%s> connect(%s) async {\n", sc.session, params)
	b.WriteString("    final socket = LiveSocket(\n")
	b.WriteString("      open: liveOpen(\n")
	b.WriteString("        connect: _connect,\n")
	b.WriteString("        baseUrl: baseUrl,\n")
	fmt.Fprintf(&b, "        path: '%s',\n", path)
	fmt.Fprintf(&b, "        endpoint: %s,\n", dartString(sc.path))
	b.WriteString("        headers: headers,\n")
	b.WriteString("        options: options,\n")

	if !sc.ws {
		b.WriteString("        socket: false,\n")
	}

	b.WriteString("      ),\n")
	b.WriteString("      options: options,\n")

	switch {
	case sc.ws:
		b.WriteString("      heartbeat: heartbeat,\n")
		b.WriteString("      keepalive: true,\n")
	case sc.sse:
		fmt.Fprintf(&b, "      events: const {%s},\n", strings.Trim(dartStringList(sc.events), "[]"))
	}

	b.WriteString("    );\n")
	b.WriteString("    try {\n")
	b.WriteString("      await socket.connect();\n")
	b.WriteString("    } on Object {\n")
	b.WriteString("      await socket.dispose();\n")
	b.WriteString("      rethrow;\n")
	b.WriteString("    }\n")
	fmt.Fprintf(&b, "    return %s._(socket);\n", sc.session)
	b.WriteString("  }\n}\n")

	fmt.Fprintf(&b, "\n/// An open `%s` connection.\n", label)
	fmt.Fprintf(&b, "final class %s {\n", sc.session)
	fmt.Fprintf(&b, "  %s._(this._socket);\n\n", sc.session)
	b.WriteString("  final LiveSocket _socket;\n\n")

	var code []string

	switch {
	case sc.sse && sc.receive.raw:
		b.WriteString("  /// The events from the server as frames: the event name, its data and its id.\n")
	case sc.sse:
		b.WriteString("  /// Event payloads from the server: each frame's data.\n")
	default:
		b.WriteString("  /// Messages from the server.\n")
	}

	b.WriteString("  ///\n")
	b.WriteString("  /// Any number of listeners may listen, and the stream carries on across a\n")
	b.WriteString("  /// reconnect, and ends when the connection ends and nothing will reopen it.\n")
	b.WriteString("  /// A message that cannot be decoded is an error event on it.\n")

	if sc.receive.raw {
		b.WriteString("  Stream<Object?> get messages => _socket.until(_socket.frames);\n\n")
	} else {
		// An SSE connection delivers {'event', 'data', 'id'}; the payload is
		// the data. Every other connection delivers the payload itself.
		wire := "m"
		if sc.sse {
			wire = "(m! as Map<Object?, Object?>)['data']"
		}

		expr := sc.receive.decodeExpr(wire)
		code = append(code, expr)

		fmt.Fprintf(&b, "  Stream<%s> get messages =>\n", sc.receive.typ.name)
		fmt.Fprintf(&b, "      _socket.until(_socket.frames).map((m) => %s);\n\n", expr)
	}

	if sc.send != nil {
		b.WriteString("  /// Sends [message] to the server, or queues it while the socket is down.\n")
		b.WriteString("  ///\n")
		b.WriteString("  /// The future completes once it is sent, and fails if it cannot be.\n")

		if sc.send.raw {
			b.WriteString("  Future<void> send(Object? message) => _socket.deliver(() => message);\n\n")
		} else {
			expr := sc.send.encodeExpr("message")
			code = append(code, expr)

			fmt.Fprintf(&b, "  Future<void> send(%s message) => _socket.deliver(() => %s);\n\n", sc.send.typ.name, expr)
		}
	}

	b.WriteString("  /// The connection state.\n")
	b.WriteString("  LiveConnectionState get state => _socket.state;\n\n")
	b.WriteString("  /// Changes of [state].\n")
	b.WriteString("  Stream<LiveConnectionState> get states => _socket.states;\n\n")
	b.WriteString("  /// Failures with no caller to take them, such as a heartbeat that could not be sent.\n")
	b.WriteString("  Stream<Object> get errors => _socket.errors;\n\n")

	if sc.send != nil {
		b.WriteString("  /// How many sends are waiting for a connection.\n")
		b.WriteString("  int get queueSize => _socket.queueSize;\n\n")
		b.WriteString("  /// Drops what is waiting to be sent, failing it when [rejectPending] is true.\n")
		b.WriteString("  void clearQueue({bool rejectPending = true}) => _socket.clearQueue(rejectPending: rejectPending);\n\n")
	}

	b.WriteString("  /// Completes when the connection has ended and nothing will reopen it by itself.\n")
	b.WriteString("  /// A later [connect] starts a new future.\n")
	b.WriteString("  Future<void> get closed => _socket.closed;\n\n")
	b.WriteString("  /// Opens the connection again after [disconnect].\n")
	b.WriteString("  Future<void> connect() => _socket.connect();\n\n")
	b.WriteString("  /// Closes the connection and stops reconnecting; [connect] opens it again.\n")
	b.WriteString("  /// What is waiting to be sent fails unless [rejectQueued] is false.\n")
	b.WriteString("  Future<void> disconnect({bool rejectQueued = true}) => _socket.disconnect(rejectQueued: rejectQueued);\n\n")
	b.WriteString("  /// Closes the connection for good.\n")
	b.WriteString("  Future<void> close({bool rejectQueued = true}) => _socket.dispose(rejectQueued: rejectQueued);\n}\n")

	text := b.String()

	var out strings.Builder

	out.WriteString(generatedHeader)

	var dartImports []string
	if usesTypedData(sc.receive.typ.name, sendTypeName(sc.send)) {
		dartImports = append(dartImports, "import 'dart:typed_data';")
	}

	var local []string

	for _, d := range []*direction{&sc.receive, sc.send} {
		if d != nil && d.hasCodec() {
			local = appendUnique(local, fmt.Sprintf("import '../codecs/%s.dart';", d.codec.file))
		}
	}

	names := map[string]bool{}

	for _, d := range []*direction{&sc.receive, sc.send} {
		if d != nil {
			for _, n := range d.typ.imports {
				names[n] = true
			}
		}
	}

	local = append(local, reg.typeImports(names, "../models/")...)
	local = append(local, "import 'live_socket.dart';")

	if shown := usedSymbols(strings.Join(code, "\n"), supportSymbols); len(shown) > 0 {
		local = append(local, "import '../support.dart' show "+strings.Join(shown, ", ")+";")
	}

	sortStrings(local)

	out.WriteString(importBlock(dartImports,
		[]string{"import 'package:forge_client/forge_client.dart' show StreamConnect, " + sc.connect + ";"},
		local))
	out.WriteString(text)

	return out.String()
}

func sendTypeName(d *direction) string {
	if d == nil {
		return ""
	}

	return d.typ.name
}

func appendUnique(list []string, item string) []string {
	if contains(list, item) {
		return list
	}

	return append(list, item)
}
